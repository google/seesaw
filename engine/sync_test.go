// Copyright 2013 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Author: jsing@google.com (Joel Sing)

package engine

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func newLocalTCPListener(n string) (*net.TCPListener, *net.TCPAddr, error) {
	var addr string
	switch n {
	case "tcp4":
		addr = "127.0.0.1:0"
	case "tcp6":
		addr = "[::1]:0"
	default:
		return nil, nil, fmt.Errorf("unknown network %q", n)
	}

	tcpAddr, err := net.ResolveTCPAddr(n, addr)
	if err != nil {
		return nil, nil, err
	}
	l, err := net.ListenTCP(n, tcpAddr)
	if err != nil {
		return nil, nil, err
	}
	tcpAddr = l.Addr().(*net.TCPAddr)

	return l, tcpAddr, nil
}

type testNoteDispatcher struct {
	notes chan *SyncNote
}

func newTestNoteDispatcher() *testNoteDispatcher {
	return &testNoteDispatcher{
		notes: make(chan *SyncNote, sessionNotesQueueSize),
	}
}

func (tsd *testNoteDispatcher) dispatch(note *SyncNote) {
	tsd.notes <- note
}

func (tsd *testNoteDispatcher) nextNote() (*SyncNote, error) {
	select {
	case n := <-tsd.notes:
		return n, nil
	case <-time.After(time.Second):
		return nil, errors.New("timed out waiting for note")
	}
}

func newSyncTest() (ln *net.TCPListener, client *syncClient, server *syncServer, dispatcher *testNoteDispatcher, err error) {
	ln, addr, err := newLocalTCPListener("tcp4")
	if err != nil {
		err = fmt.Errorf("Failed to create local TCP listener: %v", err)
		return
	}

	engine := newTestEngine()
	engine.config.Node.IPv4Addr = addr.IP
	engine.config.Peer.IPv4Addr = addr.IP
	engine.config.SyncPort = addr.Port

	server = newSyncServer(engine)
	go server.serve(ln)

	client = newSyncClient(engine)
	dispatcher = newTestNoteDispatcher()
	client.dispatch = dispatcher.dispatch

	return
}

func TestBasicSync(t *testing.T) {
	ln, client, server, dispatcher, err := newSyncTest()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go client.runOnce()
	defer func() { client.quit <- true }()

	// The server should send a desync at the start of the session.
	n, err := dispatcher.nextNote()
	if err != nil {
		t.Fatalf("Expected desync, got error: %v", err)
	}
	if n.Type != SNTDesync {
		t.Fatalf("Got %v, want %v", n.Type, SNTDesync)
	}

	// Send a notification for each sync note type.
	for nt := range syncNoteTypeNames {
		server.notify(&SyncNote{Type: nt})
		n, err := dispatcher.nextNote()
		if err != nil {
			t.Fatalf("Expected note, got error: %v", err)
		}
		if n.Type != nt {
			t.Errorf("Got sync note %v, want %v", n.Type, nt)
		}
	}
}

func TestSyncHeartbeats(t *testing.T) {
	ln, client, server, dispatcher, err := newSyncTest()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	server.heartbeatInterval = 500 * time.Millisecond
	go server.run()
	go client.run()

	client.enable()
	time.Sleep(1500 * time.Millisecond)
	client.disable()

	want := []SyncNoteType{SNTDesync, SNTHeartbeat, SNTHeartbeat}
	for _, nt := range want {
		n, err := dispatcher.nextNote()
		if err != nil {
			t.Fatalf("Expected note, got error: %v", err)
		}
		if n.Type != nt {
			t.Errorf("Got sync note %v, want %v", n.Type, nt)
		}
		t.Logf("Got %v sync note", n.Type)
	}
}

func TestSyncDesync(t *testing.T) {
	ln, client, server, dispatcher, err := newSyncTest()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// Switch the dispatcher to a blocking channel.
	dispatcher.notes = make(chan *SyncNote)

	go client.runOnce()
	defer func() { client.quit <- true }()

	// The server should send a desync at the start of the session.
	n, err := dispatcher.nextNote()
	if err != nil {
		t.Fatalf("Expected desync, got error: %v", err)
	}
	if n.Type != SNTDesync {
		t.Fatalf("Got %v, want %v", n.Type, SNTDesync)
	}

	// Send a single notification to complete the poll.
	server.notify(&SyncNote{Type: SNTHeartbeat})
	time.Sleep(250 * time.Millisecond)

	// Send enough notifications to fill the session channel buffer.
	server.notify(&SyncNote{Type: SNTHeartbeat})
	server.notify(&SyncNote{Type: SNTConfigUpdate})
	for i := 0; i < (sessionNotesQueueSize * 1.1); i++ {
		server.notify(&SyncNote{Type: SNTHealthcheck})
	}

	// At this point the client should receive a desync.
	received := make(map[SyncNoteType]int)
noteLoop:
	for i := 0; i < (sessionNotesQueueSize * 1.1); i++ {
		n, err := dispatcher.nextNote()
		if err != nil {
			t.Fatalf("Expected note, got error: %v", err)
		}
		received[n.Type]++

		switch n.Type {
		case SNTHeartbeat:
		case SNTHealthcheck:
		case SNTDesync:
			break noteLoop
		default:
			t.Fatalf("Unexpected notification %v", n.Type)
		}
	}
	t.Logf("Received notifications %#v", received)
	if received[SNTDesync] != 1 {
		t.Errorf("Did not receive desync notification")
	}
}
