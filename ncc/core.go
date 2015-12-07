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

/*
	Package ncc implements the Seesaw v2 Network Control Centre component,
	which provides an interface for the Seesaw engine to manipulate and
	control network related configuration.
*/
package ncc

import (
	"net"
	"net/rpc"
	"os"

	"github.com/google/seesaw/common/server"

	log "github.com/golang/glog"
)

func init() {
	initIPTRuleTemplates()
}

// SeesawNCC provides the IPC interface for the network control component.
type SeesawNCC struct{}

// Init performs initialisation of the NCC components.
// Note: we cannot use a package-based init here since it would be triggered
// when ncc is imported by all other packages, including the NCC client.
func Init() {
	initIPVS()
}

// Server contains the data necessary to run the Seesaw v2 NCC server.
type Server struct {
	nccSocket string
	shutdown  chan bool
}

// NewServer returns an initialised NCC Server struct.
func NewServer(socket string) *Server {
	return &Server{
		nccSocket: socket,
		shutdown:  make(chan bool),
	}
}

// Run starts the NCC server.
func (n *Server) Run() {
	if err := server.RemoveUnixSocket(n.nccSocket); err != nil {
		log.Fatalf("Failed to remove socket: %v", err)
	}
	ln, err := net.Listen("unix", n.nccSocket)
	if err != nil {
		log.Fatalf("listen error: %v", err)
	}
	defer ln.Close()
	defer os.Remove(n.nccSocket)

	seesawNCC := rpc.NewServer()
	seesawNCC.Register(&SeesawNCC{})
	go server.RPCAccept(ln, seesawNCC)

	<-n.shutdown
}

// Shutdown signals the NCC server to shutdown.
func (n *Server) Shutdown() {
	n.shutdown <- true
}
