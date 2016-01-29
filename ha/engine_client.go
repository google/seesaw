// Copyright 2012 Google Inc.  All Rights Reserved.
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

// Author: angusc@google.com (Angus Cameron)

package ha

// This file contains functions that interact with the seesaw engine.

import (
	"fmt"
	"net"
	"net/rpc"
	"time"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
)

const engineTimeout = 10 * time.Second

// Engine represents an interface to a Seesaw Engine.
type Engine interface {
	HAConfig() (*seesaw.HAConfig, error)
	HAState(seesaw.HAState) error
	HAUpdate(seesaw.HAStatus) (bool, error)
}

// EngineClient implements the Engine interface. It connects to the Seesaw
// Engine UNIX domain socket specified by Socket.
type EngineClient struct {
	Socket string
}

// HAConfig requests the HAConfig from the Seesaw Engine.
func (e *EngineClient) HAConfig() (*seesaw.HAConfig, error) {
	engineConn, err := net.DialTimeout("unix", e.Socket, engineTimeout)
	if err != nil {
		return nil, fmt.Errorf("HAConfig: Dial failed: %v", err)
	}
	engineConn.SetDeadline(time.Now().Add(engineTimeout))
	engine := rpc.NewClient(engineConn)
	defer engine.Close()

	var config seesaw.HAConfig
	ctx := ipc.NewTrustedContext(seesaw.SCHA)
	if err := engine.Call("SeesawEngine.HAConfig", ctx, &config); err != nil {
		return nil, fmt.Errorf("HAConfig: SeesawEngine.HAConfig failed: %v", err)
	}
	return &config, nil
}

// HAState informs the Seesaw Engine of the current HAState.
func (e *EngineClient) HAState(state seesaw.HAState) error {
	engineConn, err := net.DialTimeout("unix", e.Socket, engineTimeout)
	if err != nil {
		return fmt.Errorf("HAState: Dial failed: %v", err)
	}
	engineConn.SetDeadline(time.Now().Add(engineTimeout))
	engine := rpc.NewClient(engineConn)
	defer engine.Close()

	var reply int
	ctx := ipc.NewTrustedContext(seesaw.SCHA)
	if err := engine.Call("SeesawEngine.HAState", &ipc.HAState{ctx, state}, &reply); err != nil {
		return fmt.Errorf("HAState: SeesawEngine.HAState failed: %v", err)
	}
	return nil
}

// HAUpdate informs the Seesaw Engine of the current HAStatus.
// The Seesaw Engine may request a failover in response.
func (e *EngineClient) HAUpdate(status seesaw.HAStatus) (bool, error) {
	engineConn, err := net.DialTimeout("unix", e.Socket, engineTimeout)
	if err != nil {
		return false, fmt.Errorf("HAUpdate: Dial failed: %v", err)
	}
	engineConn.SetDeadline(time.Now().Add(engineTimeout))
	engine := rpc.NewClient(engineConn)
	defer engine.Close()

	var failover bool
	ctx := ipc.NewTrustedContext(seesaw.SCHA)
	if err := engine.Call("SeesawEngine.HAUpdate", &ipc.HAStatus{ctx, status}, &failover); err != nil {
		return false, fmt.Errorf("HAUpdate: SeesawEngine.HAUpdate failed: %v", err)
	}
	return failover, nil
}

// DummyEngine implements the Engine interface for testing purposes.
type DummyEngine struct {
	Config *seesaw.HAConfig
}

// HAConfig returns the HAConfig for a DummyEngine.
func (e *DummyEngine) HAConfig() (*seesaw.HAConfig, error) {
	return e.Config, nil
}

// HAState does nothing.
func (e *DummyEngine) HAState(state seesaw.HAState) error {
	return nil
}

// HAUpdate does nothing.
func (e *DummyEngine) HAUpdate(status seesaw.HAStatus) (bool, error) {
	return false, nil
}
