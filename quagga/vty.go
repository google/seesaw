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

// Package quagga provides a library for interfacing with Quagga daemons.
package quagga

// This file contains functions for interfacing with a Quagga daemon over
// its VTY socket.

import (
	"fmt"
	"net"
	"time"
)

// VTYError encapsulates an error that was returned from a command issued
// to a Quagga VTY daemon.
type VTYError struct {
	Response string
	Status   byte
}

func (e VTYError) Error() string {
	return fmt.Sprintf("VTY status %d: %v", e.Status, e.Response)
}

// VTY represents a connection to a Quagga VTY.
type VTY struct {
	conn         net.Conn
	socket       string
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// NewVTY returns an initialised VTY struct.
func NewVTY(socket string) *VTY {
	return &VTY{
		socket:       socket,
		readTimeout:  1 * time.Second,
		writeTimeout: 1 * time.Second,
	}
}

// Dial establishes a connection to a Quagga VTY socket.
func (v *VTY) Dial() error {
	if v.conn != nil {
		return fmt.Errorf("connection already established")
	}
	conn, err := net.Dial("unix", v.socket)
	if err != nil {
		return err
	}
	v.conn = conn
	return nil
}

// Close closes a connection to a Quagga VTY socket.
func (v *VTY) Close() error {
	if v.conn == nil {
		return fmt.Errorf("No connection established")
	}
	err := v.conn.Close()
	v.conn = nil
	return err
}

// Commands issues a sequence of commands over the Quagga VTY, discarding
// responses.
func (v *VTY) Commands(cmds []string) error {
	for _, cmd := range cmds {
		if _, err := v.Command(cmd); err != nil {
			return err
		}
	}
	return nil
}

// Command issues the given command over the Quagga VTY and reads the response.
func (v *VTY) Command(cmd string) (string, error) {
	if err := v.write(cmd); err != nil {
		return "", err
	}
	r, s, err := v.read()
	if err != nil {
		return "", err
	}
	if s != 0 {
		return "", VTYError{r, s}
	}
	return r, nil
}

// write writes a command string to the VTY socket. A command from a VTY
// client is terminated with a single trailing NULL byte (0).
func (v *VTY) write(s string) error {
	b := []byte(s)
	b = append(b, 0)
	v.conn.SetWriteDeadline(time.Now().Add(v.writeTimeout))
	for len(b) > 0 {
		n, err := v.conn.Write(b[:])
		if err != nil {
			return fmt.Errorf("VTY write failed: %v", err)
		}
		b = b[n:]
	}
	return nil
}

// read reads a command response from the VTY socket. A response from a
// VTY server is terminated with three trailing NULL bytes (0), followed by
// a status byte.
func (v *VTY) read() (string, byte, error) {
	b := make([]byte, 1024)
	r := make([]byte, 0)
	eom := 0
	v.conn.SetReadDeadline(time.Now().Add(v.readTimeout))
	for {
		n, err := v.conn.Read(b)
		if err != nil {
			return "", 0, fmt.Errorf("VTY read failed: %v", err)
		}
		for _, v := range b[:n] {
			if eom == 3 {
				return string(r), v, nil
			} else if v == 0 {
				eom++
			} else {
				eom = 0
				r = append(r, v)
			}
		}
	}
}
