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
	The seesaw_ncc binary implements the Seesaw Network Control Centre
	component, which provides an interface that allows the Seesaw Engine
	to control network configuration, including IPVS, iptables, network
	interfaces, Quagga and routing. This binary will run with root
	privileges and will allow unprivileged processes to perform
	specific controls.
*/
package main

import (
	"flag"
	"os"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/ncc"

	log "github.com/golang/glog"
)

var socketPath = flag.String("socket", seesaw.NCCSocket, "Seesaw NCC socket")

func main() {
	flag.Parse()

	if os.Getuid() != 0 {
		log.Fatal("must be run as root")
	}

	ncc.Init()
	ncc := ncc.NewServer(*socketPath)
	server.ShutdownHandler(ncc)
	server.ServerRunDirectory("ncc", 0, 0)
	ncc.Run()
}
