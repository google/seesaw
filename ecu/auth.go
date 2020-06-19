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

// Authenticator types provide functionality for authenticating users and adding
// the authenticated information to a context.
type Authenticator interface {
	// AuthInit will be called once when the ECU is run to set up required
	// resources for authentication. If nothing is needed, Authenticators
	// should simply return nil. If an error is returned, a warning will be
	// logged but the ECU will continue to run.
	AuthInit() error

	// Authenticate inspects the provided context (particularly ctx.AuthToken)
	// and either returns an error or creates a child context treated as
	// authenticated and extra fields needed for authorization (e.g. ctx.User).
	Authenticate(ctx *ipc.Context) (*ipc.Context, error)
}

// DefaultAuthenticator implements a "default-deny authenticator" that denies
// all authentications. Using this prevents remote connections to the ECU.
type DefaultAuthenticator struct{}

// AuthInit does nothing and returns nil.
func (DefaultAuthenticator) AuthInit() error {
	return nil
}

// Authenticate returns a "default deny" error.
func (DefaultAuthenticator) Authenticate(ctx *ipc.Context) (*ipc.Context, error) {
	return nil, errors.New("default deny")
}

// authConnect attempts to authenticate the user using the given context. If
// authentication succeeds an authenticated IPC connection to the Seesaw
// Engine is returned.
func (e *ECU) authConnect(ctx *ipc.Context) (*conn.Seesaw, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	authCtx, err := e.cfg.Authenticator.Authenticate(ctx)
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
