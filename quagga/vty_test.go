// Copyright 2012 Google Inc. All Rights Reserved.
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

package quagga

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
)

type fakeVTYServer struct {
	clientConn net.Conn
	serverConn net.Conn

	clientFile *os.File
	serverFile *os.File

	done     chan bool
	send     chan []byte
	received chan []byte
}

func newFakeVTYServer() (*fakeVTYServer, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}

	vtyClientFile := os.NewFile(uintptr(fds[1]), "vty-client")
	vtyClientConn, err := net.FileConn(vtyClientFile)
	if err != nil {
		return nil, err
	}

	vtyServerFile := os.NewFile(uintptr(fds[0]), "vty-server")
	vtyServerConn, err := net.FileConn(vtyServerFile)
	if err != nil {
		return nil, err
	}
	syscall.SetNonblock(fds[0], false)

	vs := &fakeVTYServer{
		clientConn: vtyClientConn,
		serverConn: vtyServerConn,
		clientFile: vtyClientFile,
		serverFile: vtyServerFile,

		send:     make(chan []byte),
		received: make(chan []byte),
	}

	go vs.read()
	go vs.write()

	return vs, nil
}

func (vs *fakeVTYServer) cleanup() {
	if vs.clientConn != nil {
		vs.clientConn.Close()
	}
	if vs.clientFile != nil {
		vs.clientFile.Close()
	}

	// Unblock the read goroutine and wait for it to finish.
	for range vs.received {
	}

	// Unblock the write goroutine if it is still running.
	select {
	case vs.done <- true:
	default:
	}

	if vs.serverConn != nil {
		vs.serverConn.Close()
	}
	if vs.serverFile != nil {
		vs.serverFile.Close()
	}
}

func (vs *fakeVTYServer) read() {
	b := make([]byte, 100)
	var r []byte
	for {
		n, err := vs.serverFile.Read(b)
		if err != nil {
			if err != io.EOF {
				panic(err)
			}
			break
		}
		for _, v := range b[:n] {
			r = append(r, v)
			// VTY client commands are terminated with a single NULL byte.
			// See also: VTY.write.
			if v == '\x00' {
				vs.received <- r
				r = r[:0]
			}
		}
	}
	close(vs.received)
}

func (vs *fakeVTYServer) write() {
	var s []byte
	select {
	case s = <-vs.send:
	case <-vs.done:
		return
	}
	for {
		if len(s) < 1 {
			return
		}
		n, err := vs.serverFile.Write(s)
		if err != nil {
			panic(err)
		}
		s = s[n:]
	}
}

// newTestVTY returns a new VTY that is connected to a fake VTY server.
func newTestVTY() (*VTY, *fakeVTYServer, error) {
	vs, err := newFakeVTYServer()
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to get VTY server: %v", err)
	}
	vty := NewVTY("")
	vty.conn = vs.clientConn
	return vty, vs, nil
}

func TestVTYRead(t *testing.T) {
	vty, vs, err := newTestVTY()
	if err != nil {
		t.Fatal(err)
	}
	defer vs.cleanup()

	msg := "this is a test"

	vs.send <- append([]byte(msg), 0, 0, 0, 0)

	got, status, err := vty.read()
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}
	if got != msg {
		t.Errorf("Client received %#v, want %#v", got, msg)
	}
	if status != 0 {
		t.Errorf("Client received status %d, want 0", status)
	}
}

func TestVTYWrite(t *testing.T) {
	vty, vs, err := newTestVTY()
	if err != nil {
		t.Fatal(err)
	}
	defer vs.cleanup()

	msg := "this is a test"
	want := append([]byte(msg), 0)
	if err := vty.write(msg); err != nil {
		t.Fatalf("vty.write failed: %v", err)
	}

	got := <-vs.received
	if !bytes.Equal(got, want) {
		t.Errorf("Server received %#v, want %#v", got, want)
	}
}

func TestVTYCommand(t *testing.T) {
	vty, vs, err := newTestVTY()
	if err != nil {
		t.Fatal(err)
	}
	defer vs.cleanup()

	reply := "No BGP network exists\n"
	vs.send <- append([]byte(reply), 0, 0, 0, 0)

	got, err := vty.Command("show bgp")
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}
	if got != reply {
		t.Errorf("Command returned %#v, want %#v", got, reply)
	}
}

func TestVTYCommandWithError(t *testing.T) {
	vty, vs, err := newTestVTY()
	if err != nil {
		t.Fatal(err)
	}
	defer vs.cleanup()

	reply := "% [BGP] Unknown command: enable\n"
	status := byte(2)
	vs.send <- append([]byte(reply), 0, 0, 0, status)

	_, err = vty.Command("enable")
	if err == nil {
		t.Error("Command failed to return an error")
		return
	}
	vtyError, ok := err.(VTYError)
	if !ok {
		t.Errorf("Expected VTYError, got %#v", err)
	} else if vtyError.Status != status {
		t.Errorf("Got VTY status %d, want %d", vtyError.Status, status)
	}
}
