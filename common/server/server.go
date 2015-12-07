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

// Package server contains utility functions for Seesaw v2 server components.
package server

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"os/signal"
	"path"
	"syscall"
	"time"

	"github.com/google/seesaw/common/seesaw"

	log "github.com/golang/glog"
)

// Shutdowner is an interface for a server that can be shutdown.
type Shutdowner interface {
	Shutdown()
}

var signalNames = map[syscall.Signal]string{
	syscall.SIGINT:  "SIGINT",
	syscall.SIGQUIT: "SIGQUIT",
	syscall.SIGTERM: "SIGTERM",
}

// signalName returns a string containing the standard name for a given signal.
func signalName(s syscall.Signal) string {
	if name, ok := signalNames[s]; ok {
		return name
	}
	return fmt.Sprintf("SIG %d", s)
}

// ShutdownHandler configures signal handling and initiates a shutdown if a
// SIGINT, SIGQUIT or SIGTERM is received by the process.
func ShutdownHandler(server Shutdowner) {
	sigc := make(chan os.Signal, 3)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	go func() {
		for s := range sigc {
			name := s.String()
			if sig, ok := s.(syscall.Signal); ok {
				name = signalName(sig)
			}
			log.Infof("Received %v, initiating shutdown...", name)
			server.Shutdown()
		}
	}()
}

// RemoveUnixSocket checks to see if the given socket already exists and
// removes it if nothing responds to connections.
func RemoveUnixSocket(socket string) error {
	if _, err := os.Stat(socket); err == nil {
		c, err := net.DialTimeout("unix", socket, 5*time.Second)
		if err == nil {
			c.Close()
			return fmt.Errorf("Socket %v is in use", socket)
		}
		log.Infof("Removing stale socket %v", socket)
		return os.Remove(socket)
	}
	return nil
}

// ServerRunDirectory ensures that the run directory exists and has the
// appropriate ownership and permissions.
func ServerRunDirectory(server string, owner, group int) error {
	serverRunDir := path.Join(seesaw.RunPath, server)
	if err := os.MkdirAll(seesaw.RunPath, 0755); err != nil {
		return fmt.Errorf("Failed to make run directory: %v", err)
	}
	if err := os.MkdirAll(serverRunDir, 0700); err != nil {
		return fmt.Errorf("Failed to make run directory: %v", err)
	}
	if err := os.Chown(serverRunDir, owner, group); err != nil {
		return fmt.Errorf("Failed to change ownership on run directory: %v", err)
	}
	if err := os.Chmod(serverRunDir, 0770); err != nil {
		return fmt.Errorf("Failed to change permissions on run directory: %v", err)
	}
	return nil
}

// RPCAccept accepts connections on the listener and dispatches them to the
// RPC server for service. Unfortunately the native Go rpc.Accept function
// fatals on any accept error, including temporary failures and closure of
// the listener.
func RPCAccept(ln net.Listener, server *rpc.Server) error {
	errClosing := errors.New("use of closed network connection")
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Warningf("RPC accept temporary error: %v", err)
				time.Sleep(1 * time.Second)
				continue
			}
			if oe, ok := err.(*net.OpError); ok && oe.Err.Error() == errClosing.Error() {
				log.Infoln("RPC accept connection closed")
				return nil
			}
			log.Errorf("RPC accept error: %v", err)
			return err
		}
		go server.ServeConn(conn)
	}
}
