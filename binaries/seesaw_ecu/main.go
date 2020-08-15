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

// The seesaw_ecu binary implements the Seesaw ECU component, which provides
// an externally accessible interface to monitor and control the Seesaw Node.
package main

import (
	"flag"

	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/ecu"
)

var (
	controlAddress = flag.String("control_address",
		ecu.DefaultConfig().ControlAddress, "ECU control address")
	monitorAddress = flag.String("monitor_address",
		ecu.DefaultConfig().MonitorAddress, "ECU monitor address")
)

func main() {
	flag.Parse()

	ecuCfg := ecu.DefaultConfig()
	ecuCfg.ControlAddress = *controlAddress
	ecuCfg.MonitorAddress = *monitorAddress

	ecu := ecu.New(&ecuCfg)
	server.ShutdownHandler(ecu)
	ecu.Run()
}
