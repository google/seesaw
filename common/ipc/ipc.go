// Copyright 2014 Google Inc. All Rights Reserved.
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

// Package ipc contains types and functions used for interprocess communication
// between Seesaw components.
package ipc

import (
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/google/seesaw/common/seesaw"
)

// AuthType specifies the type of authentication established.
type AuthType int

const (
	ATNone AuthType = iota
	ATSSO
	ATTrusted
	ATUntrusted
)

var authTypeNames = map[AuthType]string{
	ATNone:      "none",
	ATSSO:       "SSO",
	ATTrusted:   "trusted",
	ATUntrusted: "untrusted",
}

// String returns the string representation of an AuthType.
func (at AuthType) String() string {
	if name, ok := authTypeNames[at]; ok {
		return name
	}
	return "(unknown)"
}

// Peer contains information identifying a peer process.
type Peer struct {
	Component seesaw.Component
	Identity  string
}

// Context contains information relating to interprocess communication.
type Context struct {
	AuthToken string
	AuthType  AuthType
	Groups    []string
	Peer      Peer // Untrusted - client provided
	Proxy     Peer
	User      string
}

// NewContext returns a new context for the given component.
func NewContext(component seesaw.Component) *Context {
	return &Context{
		AuthType: ATNone,
		Peer: Peer{
			Component: component,
			Identity:  fmt.Sprintf("%s [pid %d]", component, os.Getpid()),
		},
	}
}

// NewAuthContext returns a new authenticated context for the given component.
func NewAuthContext(component seesaw.Component, token string) *Context {
	ctx := NewContext(component)
	ctx.AuthToken = token
	ctx.AuthType = ATUntrusted
	return ctx
}

// NewTrustedContext returns a new trusted context for the given component.
func NewTrustedContext(component seesaw.Component) *Context {
	ctx := NewContext(component)
	ctx.AuthType = ATTrusted
	if u, err := user.Current(); err == nil {
		ctx.User = fmt.Sprintf("%s [uid %s]", u.Username, u.Uid)
	} else {
		ctx.User = fmt.Sprintf("(unknown) [uid %d]", os.Getuid())
	}
	return ctx
}

// String returns the string representation of a context.
func (ctx *Context) String() string {
	if ctx == nil {
		return "(nil context)"
	}

	var s []string
	if ctx.Peer.Component != seesaw.SCNone {
		s = append(s, fmt.Sprintf("peer %s", ctx.Peer.Identity))
	}
	if ctx.Proxy.Component != seesaw.SCNone {
		s = append(s, fmt.Sprintf("via %s", ctx.Proxy.Identity))
	}
	s = append(s, fmt.Sprintf("as %s (%v auth)", ctx.User, ctx.AuthType))
	return strings.Join(s, " ")
}

// IsTrusted returns whether a context came from a trusted source.
func (ctx *Context) IsTrusted() bool {
	return ctx.AuthType == ATTrusted
}

// ConfigSource contains data for a config source IPC.
type ConfigSource struct {
	Ctx    *Context
	Source string
}

// HAStatus contains data for a HA status IPC.
type HAStatus struct {
	Ctx    *Context
	Status seesaw.HAStatus
}

// HAState contains data for a HA state IPC.
type HAState struct {
	Ctx   *Context
	State seesaw.HAState
}

// Override contains data for an override IPC.
type Override struct {
	Ctx         *Context
	Vserver     *seesaw.VserverOverride
	Destination *seesaw.DestinationOverride
	Backend     *seesaw.BackendOverride
}
