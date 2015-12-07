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

// Package conn provides connectivity and communication with the Seesaw Engine,
// either over IPC or RPC.
package conn

import (
	"errors"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/quagga"
)

// EngineConn defines an interface that implements communication with the
// Seesaw Engine, either over IPC or RPC.
type EngineConn interface {
	Close() error
	Dial(addr string) error

	ClusterStatus() (*seesaw.ClusterStatus, error)
	ConfigStatus() (*seesaw.ConfigStatus, error)
	HAStatus() (*seesaw.HAStatus, error)

	ConfigSource(source string) (string, error)
	ConfigReload() error

	BGPNeighbors() ([]*quagga.Neighbor, error)

	VLANs() (*seesaw.VLANs, error)

	Vservers() (map[string]*seesaw.Vserver, error)
	Backends() (map[string]*seesaw.Backend, error)

	OverrideBackend(override *seesaw.BackendOverride) error
	OverrideDestination(override *seesaw.DestinationOverride) error
	OverrideVserver(override *seesaw.VserverOverride) error

	Failover() error
}

var engineConns = make(map[string]func(ctx *ipc.Context) EngineConn)

// RegisterEngineConn registers the given connection type.
func RegisterEngineConn(connType string, connFunc func(ctx *ipc.Context) EngineConn) {
	engineConns[connType] = connFunc
}

// Seesaw represents a connection to a Seesaw Engine.
type Seesaw struct {
	EngineConn
}

// NewSeesawIPC returns a new Seesaw IPC connection.
func NewSeesawIPC(ctx *ipc.Context) (*Seesaw, error) {
	if newConn, ok := engineConns["ipc"]; ok {
		return &Seesaw{newConn(ctx)}, nil
	}
	return nil, errors.New("No Seesaw IPC connection type registered")
}

// NewSeesawRPC returns a new Seesaw RPC connection.
func NewSeesawRPC(ctx *ipc.Context) (*Seesaw, error) {
	if newConn, ok := engineConns["rpc"]; ok {
		return &Seesaw{newConn(ctx)}, nil
	}
	return nil, errors.New("No Seesaw RPC connection type registered")
}
