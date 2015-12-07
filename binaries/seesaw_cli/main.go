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

// The seesaw_cli binary implements the Seesaw v2 Command Line Interface (CLI),
// which allows for user control of the Seesaw v2 Engine component.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
	"time"

	"github.com/google/seesaw/cli"
	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"

	"golang.org/x/crypto/ssh/terminal"
)

var (
	command      = flag.String("c", "", "Command to execute")
	engineSocket = flag.String("engine", seesaw.EngineSocket, "Seesaw Engine Socket")

	oldTermState *terminal.State
	prompt       string
	seesawCLI    *cli.SeesawCLI
	seesawConn   *conn.Seesaw
	term         *terminal.Terminal
)

func exit() {
	if oldTermState != nil {
		terminal.Restore(syscall.Stdin, oldTermState)
	}
	fmt.Printf("\n")
	os.Exit(0)
}

func fatalf(format string, a ...interface{}) {
	if oldTermState != nil {
		terminal.Restore(syscall.Stdin, oldTermState)
	}
	fmt.Fprintf(os.Stderr, format, a...)
	fmt.Fprintf(os.Stderr, "\n")
	os.Exit(1)
}

func suspend() {
	if oldTermState != nil {
		terminal.Restore(syscall.Stdin, oldTermState)
	}
	go resume()
	syscall.Kill(os.Getpid(), syscall.SIGTSTP)
}

func resume() {
	time.Sleep(1 * time.Second)
	fmt.Println("resuming...")
	terminalInit()
}

func terminalInit() {
	var err error
	oldTermState, err = terminal.MakeRaw(syscall.Stdin)
	if err != nil {
		fatalf("Failed to get raw terminal: %v", err)
	}

	term = terminal.NewTerminal(os.Stdin, prompt)
	term.AutoCompleteCallback = autoComplete
}

// commandChain builds a command chain from the given command slice.
func commandChain(chain []*cli.Command, args []string) string {
	s := make([]string, 0)
	for _, c := range chain {
		s = append(s, c.Command)
	}
	s = append(s, args...)
	if len(s) > 0 && len(args) == 0 {
		s = append(s, "")
	}
	return strings.Join(s, " ")
}

// autoComplete attempts to complete the user's input when certain
// characters are typed.
func autoComplete(line string, pos int, key rune) (string, int, bool) {
	switch key {
	case 0x01: // Ctrl-A
		return line, 0, true
	case 0x03: // Ctrl-C
		exit()
	case 0x05: // Ctrl-E
		return line, len(line), true
	case 0x09: // Ctrl-I (Tab)
		_, _, chain, args := cli.FindCommand(string(line))
		line := commandChain(chain, args)
		return line, len(line), true
	case 0x15: // Ctrl-U
		return "", 0, true
	case 0x1a: // Ctrl-Z
		suspend()
	case '?':
		cmd, subcmds, chain, args := cli.FindCommand(string(line[0:pos]))
		if cmd == nil {
			term.Write([]byte(prompt))
			term.Write([]byte(line))
			term.Write([]byte("?\n"))
		}
		if subcmds != nil {
			for _, c := range *subcmds {
				term.Write([]byte(" " + c.Command))
				term.Write([]byte("\n"))
			}
		} else if cmd == nil {
			term.Write([]byte("Unknown command.\n"))
		}

		line := commandChain(chain, args)
		return line, len(line), true
	}
	return "", 0, false
}

// interactive invokes the interactive CLI interface.
func interactive() {
	status, err := seesawConn.ClusterStatus()
	if err != nil {
		fatalf("Failed to get cluster status: %v", err)
	}
	fmt.Printf("\nSeesaw CLI - Engine version %d\n\n", status.Version)

	u, err := user.Current()
	if err != nil {
		fatalf("Failed to get current user: %v", err)
	}

	ha, err := seesawConn.HAStatus()
	if err != nil {
		fatalf("Failed to get HA status: %v", err)
	}
	if ha.State != seesaw.HAMaster {
		fmt.Println("WARNING: This seesaw is not currently the master.")
	}

	prompt = fmt.Sprintf("%s@%s> ", u.Username, status.Site)

	// Setup signal handler before we switch to a raw terminal.
	sigc := make(chan os.Signal, 3)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	go func() {
		<-sigc
		exit()
	}()

	terminalInit()

	for {
		cmdline, err := term.ReadLine()
		if err != nil {
			break
		}
		cmdline = strings.TrimSpace(cmdline)
		if cmdline == "" {
			continue
		}
		if err := seesawCLI.Execute(cmdline); err != nil {
			fmt.Println(err)
		}
	}
}

func main() {
	flag.Parse()

	ctx := ipc.NewTrustedContext(seesaw.SCLocalCLI)

	var err error
	seesawConn, err = conn.NewSeesawIPC(ctx)
	if err != nil {
		fatalf("Failed to connect to engine: %v", err)
	}
	if err := seesawConn.Dial(*engineSocket); err != nil {
		fatalf("Failed to connect to engine: %v", err)
	}
	defer seesawConn.Close()
	seesawCLI = cli.NewSeesawCLI(seesawConn, exit)

	if *command == "" {
		interactive()
		exit()
	}

	if err := seesawCLI.Execute(*command); err != nil {
		fatalf("%v", err)
	}
}
