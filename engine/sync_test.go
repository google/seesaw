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

	"github.com/google/seesaw/common/seesaw"
)

func newLocalTCPListener() (*net.TCPListener, *net.TCPAddr, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return nil, nil, err
	}
	l, err := net.ListenTCP("tcp", tcpAddr)
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

func newSyncTest() (*net.TCPListener, *syncClient, *syncServer, *testNoteDispatcher, error) {
	ln, addr, err := newLocalTCPListener()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Failed to create local TCP listener: %v", err)
	}

	engine := newTestEngine()
	engine.config.Node.IPv4Addr = addr.IP
	engine.config.Peer.IPv4Addr = addr.IP
	engine.config.SyncPort = addr.Port

	server := newSyncServer(engine)
	go server.serve(ln)

	client := newSyncClient(engine)
	dispatcher := newTestNoteDispatcher()
	client.dispatch = dispatcher.dispatch

	return ln, client, server, dispatcher, nil
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
		t.Fatalf("Expected initial desync, got error: %v", err)
	}
	if n.Type != SNTDesync {
		t.Fatalf("Initial note type = %v, want %v", n.Type, SNTDesync)
	}

	// Send a notification for each sync note type.
	for nt := range syncNoteTypeNames {
		server.notify(&SyncNote{Type: nt})
		n, err := dispatcher.nextNote()
		if err != nil {
			t.Fatalf("After sending %v, nextNote failed: %v", nt, err)
		}
		if n.Type != nt {
			t.Errorf("After sending %v, nextNote = %v, want %v", nt, n, nt)
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

	// Enabling briefly shouldn't have time to get a heartbeat, but should get
	// the initial desync.
	client.enable()
	// Make sure we can read the desync (and flush it so the later client doesn't
	// see it).
	if n, err := dispatcher.nextNote(); err != nil || n.Type != SNTDesync {
		t.Errorf("During short enablement, nextNote() = %v, %v; expected desync", n, err)
	} else {
		t.Logf("Got short note: %v, %v", n, err)
	}
	client.disable()

	// Now let it run long enough to get some heartbeats.
	// Any heartbeats sent to the first enablement of the client shouldn't appear here.
	client.enable()
	time.Sleep(2*server.heartbeatInterval + 50*time.Millisecond)
	client.disable()
	// Waiting longer after disabling shouldn't send another heartbeat (and
	// should make sure we receive the ones we had queued).
	time.Sleep(server.heartbeatInterval + 50*time.Millisecond)

	wantNotes := []SyncNoteType{SNTDesync, SNTHeartbeat, SNTHeartbeat}
	for _, nt := range wantNotes {
		n, err := dispatcher.nextNote()
		if err != nil || n.Type != nt {
			t.Errorf("After long enablement, nextNote() = %v, %v; want Type = %v", n, err, nt)
			continue
		}
		t.Logf("After long enablement: got sync note %v", n)
	}

	n, err := dispatcher.nextNote()
	if err != nil {
		// TODO: Confirm it's a timeout?
		t.Logf("After flushing expected notes, final nextNote read failed expectedly: %v", err)
		return
	}
	// If we end up registering near the heartbeat boundaries, we might
	// legitimately get 3 heartbeats, but no more!
	if n.Type != SNTHeartbeat {
		t.Errorf("After long enablement, got additional note %v, expected only %d notes", n, len(wantNotes))
	} else {
		n, err := dispatcher.nextNote()
		if err == nil {
			t.Errorf("After long enablement, got additional note %v, expected at most %d notes", n, len(wantNotes)+1)
		}
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
	defer func() {
		close(client.quit)
		for {
			// Drain the notes to unblock the quit reader.
			if _, err := dispatcher.nextNote(); err != nil {
				break
			}
		}
		<-client.stopped
	}()

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
	// syncClient.poll should now be blocked writing that Note to
	// testNoteDispatcher's notes chan.

	// Send enough notifications to fill the session channel buffer.
	server.notify(&SyncNote{Type: SNTHeartbeat})
	server.notify(&SyncNote{Type: SNTConfigUpdate})
	for i := 0; i < (sessionNotesQueueSize * 1.1); i++ {
		server.notify(&SyncNote{Type: SNTHealthcheck})
	}

	// Now we unblock syncClient.poll by reading the initial notification from
	// the local chan. At that point, it should do another Poll and see the
	// desync.
	received := make(map[SyncNoteType]int)
noteLoop:
	for i := 0; i < (sessionNotesQueueSize * 1.1); i++ {
		n, err := dispatcher.nextNote()
		if err != nil {
			t.Fatalf("nextNote() failed: %v; expected note", err)
		}
		received[n.Type]++

		switch n.Type {
		case SNTHeartbeat, SNTHealthcheck: // ok
		case SNTDesync:
			break noteLoop
		default:
			t.Fatalf("Unexpected notification %v", n)
		}
	}
	if received[SNTDesync] != 1 {
		t.Errorf("While waiting for desync, received: %v; expected 1 Desync", received)
	}
	if received[SNTHeartbeat] != 1 {
		t.Errorf("While waiting for desync, received: %v; expected 1 Heartbeat", received)
	}
}

func TestSyncOverrideAuthentication(t *testing.T) {
	ln, client, server, _, err := newSyncTest()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	client.dispatch = client.handleNote
	client.engine.overrideChan = make(chan seesaw.Override, 8)
	go client.runOnce()
	defer func() {
		client.quit <- true
		<-client.stopped
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		server.sessionLock.RLock()
		n := len(server.sessions)
		server.sessionLock.RUnlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Timed out waiting for sync session registration")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Unauthenticated.
	server.notify(&SyncNote{
		Type: SNTOverride,
		VserverOverride: &seesaw.VserverOverride{
			VserverName:   "unauthenticated",
			OverrideState: seesaw.OverrideDisable,
		},
	})
	select {
	case got := <-client.engine.overrideChan:
		vo, ok := got.(*seesaw.VserverOverride)
		if !ok {
			t.Fatalf("overrideChan delivered %T, want *seesaw.VserverOverride", got)
		}
		if vo.Target() != "unauthenticated" || vo.State() != seesaw.OverrideDisable {
			t.Errorf("got override (%q, %v), want (%q, %v)", vo.Target(), vo.State(), "authenticated", seesaw.OverrideDisable)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for unauthenticated override")
	}

	// Reject wrong key.
	client.engine.config.SyncKey = "somekey"
	note := &SyncNote{
		Type: SNTOverride,
		Time: time.Now(),
		VserverOverride: &seesaw.VserverOverride{
			VserverName:   "wrong",
			OverrideState: seesaw.OverrideDisable,
		},
	}
	note.sign([]byte("wrongkey"))
	client.handleNote(note)
	select {
	case got := <-client.engine.overrideChan:
		t.Errorf("Wrong key override was unexpectedly accepted: %v", got)
	default:
	}

	// Expired.
	expired := &SyncNote{
		Type: SNTOverride,
		Time: time.Now().Add(-2 * sessionDeadtime),
		VserverOverride: &seesaw.VserverOverride{
			VserverName:   "expired",
			OverrideState: seesaw.OverrideDisable,
		},
	}
	expired.sign([]byte(client.engine.config.SyncKey))
	client.handleNote(expired)
	select {
	case got := <-client.engine.overrideChan:
		t.Errorf("Expired override was unexpectedly accepted: %v", got)
	default:
	}

	// Good.
	server.notify(&SyncNote{
		Type: SNTOverride,
		VserverOverride: &seesaw.VserverOverride{
			VserverName:   "good",
			OverrideState: seesaw.OverrideDisable,
		},
	})
	select {
	case got := <-client.engine.overrideChan:
		vo, ok := got.(*seesaw.VserverOverride)
		if !ok {
			t.Fatalf("overrideChan delivered %T, want *seesaw.VserverOverride", got)
		}
		if vo.Target() != "good" || vo.State() != seesaw.OverrideDisable {
			t.Errorf("got override (%q, %v), want (%q, %v)", vo.Target(), vo.State(), "good", seesaw.OverrideDisable)
		}
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for authenticated override")
	}
}
