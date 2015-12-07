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

// Package cli provides a command line interface that allows for interaction
// with a Seesaw cluster.
package cli

import (
	"errors"
	"strings"

	"github.com/google/seesaw/common/conn"
)

// SeesawCLI represents a Seesaw command line interface.
type SeesawCLI struct {
	seesaw *conn.Seesaw
	exit   func()
}

// NewSeesawCLI returns a new Seesaw command line interface.
func NewSeesawCLI(conn *conn.Seesaw, exit func()) *SeesawCLI {
	return &SeesawCLI{conn, exit}
}

// Execute executes the given command line.
func (cli *SeesawCLI) Execute(cmdline string) error {
	cmd, subcmds, _, args := FindCommand(cmdline)
	if cmd != nil {
		return cmd.function(cli, args)
	}
	if subcmds != nil {
		return errors.New("Incomplete command.")
	}
	return errors.New("Unknown command.")
}

func exit(cli *SeesawCLI, args []string) error {
	cli.exit()
	return nil
}

// Command represents a command that can be executed from the CLI.
type Command struct {
	Command     string
	Subcommands *[]Command
	function    func(cli *SeesawCLI, args []string) error
}

var commands = []Command{
	{"config", &commandConfig, nil},
	{"exit", nil, exit},
	{"quit", nil, exit}, // An alias for exit, matches JunOS behavior.
	{"failover", nil, failover},
	{"override", &commandOverride, nil},
	{"show", &commandShow, nil},
}

var commandConfig = []Command{
	{"reload", nil, configReload},
	{"source", nil, configSource},
	{"status", nil, configStatus},
}

var commandOverride = []Command{
	{"vserver", &commandOverrideVserver, nil},
}

var commandOverrideVserver = []Command{
	{"state", &commandOverrideVserverState, nil},
}

var commandOverrideVserverState = []Command{
	{"default", nil, overrideVserverStateDefault},
	{"disabled", nil, overrideVserverStateDisabled},
	{"enabled", nil, overrideVserverStateEnabled},
}

var commandShow = []Command{
	{"bgp", &commandShowBGP, nil},
	{"backends", nil, showBackend},
	{"destinations", nil, showDestination},
	{"ha", nil, showHAStatus},
	{"nodes", nil, showNode},
	{"version", nil, showVersion},
	{"vlans", nil, showVLANs},
	{"vservers", nil, showVserver},
	{"warnings", nil, showWarning},
}

var commandShowBGP = []Command{
	{"neighbors", nil, showBGPNeighbors},
}

// FindCommand tokenises a command line and attempts to locate the
// corresponding Command. If a matching command is found it is returned,
// along with the remaining arguments. If the command has sub-commands then
// the list of sub-commands is returned instead. A chain of matched commands
// is also returned, along with the slice of remaining arguments.
func FindCommand(cmdline string) (*Command, *[]Command, []*Command, []string) {
	var chain []*Command
	var matches []Command
	var next *Command
	cmds := &commands

	// Tokenise string.
	cmdstr := strings.Fields(cmdline)
	if len(cmdstr) == 0 {
		return nil, cmds, chain, nil
	}

	for idx, subcmd := range cmdstr {
		matches = make([]Command, 0)
		next = nil
		for i := range *cmds {
			cmd := (*cmds)[i]
			if strings.HasPrefix(cmd.Command, subcmd) {
				matches = append(matches, cmd)
				next = &cmd
			}
		}
		if len(matches) == 0 {
			// Sub command not found.
			return nil, nil, chain, cmdstr[idx:]
		} else if len(matches) > 1 {
			// Ambiguious command.
			return nil, &matches, chain, cmdstr[idx:]
		}
		chain = append(chain, next)
		if next.function != nil {
			// We've reached a function.
			return next, nil, chain, cmdstr[idx+1:]
		} else {
			cmds = next.Subcommands
		}
	}
	if next != nil {
		return nil, next.Subcommands, chain, nil
	}

	return nil, nil, chain, nil
}
