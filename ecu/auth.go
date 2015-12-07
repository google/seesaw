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

package ecu

// This file contains types and functions that implement authentication for
// RPC calls to the Seesaw v2 ECU.

import (
	"errors"
	"fmt"

	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
)

// authInit initialises the authentication system.
func (e *ECU) authInit() error {
	return nil
}

// authenticate attempts to validate the given authentication token.
func (e *ECU) authenticate(ctx *ipc.Context) (*ipc.Context, error) {
	return nil, errors.New("unimplemented")
}

// authConnect attempts to authenticate the user using the given context. If
// authentication succeeds an authenticated IPC connection to the Seesaw
// Engine is returned.
func (e *ECU) authConnect(ctx *ipc.Context) (*conn.Seesaw, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	authCtx, err := e.authenticate(ctx)
	if err != nil {
		return nil, fmt.Errorf("authentication failed: %v", err)
	}

	seesawConn, err := conn.NewSeesawIPC(authCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to engine: %v", err)
	}
	if err := seesawConn.Dial(e.cfg.EngineSocket); err != nil {
		return nil, fmt.Errorf("failed to connect to engine: %v", err)
	}

	return seesawConn, nil
}
