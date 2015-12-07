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

// The seesaw_ha binary implements HA peering between Seesaw nodes.
package main

import (
	"flag"
	"net"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/ha"

	log "github.com/golang/glog"
)

var (
	engineSocket = flag.String("engine", seesaw.EngineSocket,
		"Seesaw Engine Socket")

	configCheckInterval = flag.Duration("config_check_interval", 15*time.Second,
		"How frequently to poll the engine for HAConfig changes")

	configCheckMaxFailures = flag.Int("config_check_max_failures", 3,
		"The maximum allowable number of consecutive config check failures")

	configCheckRetryDelay = flag.Duration("config_check_retry_delay", 2*time.Second,
		"Time between config check retries")

	initConfigRetryDelay = flag.Duration("init_config_retry_delay", 5*time.Second,
		"Time between retries when retrieving the initial HAConfig from the engine")

	masterAdvertInterval = flag.Duration("master_advert_interval", 500*time.Millisecond,
		"How frequently to send advertisements when this node is master")

	preempt = flag.Bool("preempt", false,
		"If true, a higher priority node will preempt the mastership of a lower priority node")

	statusReportInterval = flag.Duration("status_report_interval", 3*time.Second,
		"How frequently to report the current HAStatus to the engine")

	statusReportMaxFailures = flag.Int("status_report_max_failures", 3,
		"The maximum allowable number of consecutive status report failures")

	statusReportRetryDelay = flag.Duration("status_report_retry_delay", 2*time.Second,
		"Time between status report retries")

	testLocalAddr = flag.String("local_addr", "",
		"Local IP Address - used only when test_mode=true")

	testMode = flag.Bool("test_mode", false,
		"Use command line flags for configuration rather than the engine")

	testPriority = flag.Int("priority", 100,
		"Priority - used only when test_mode=true")

	testRemoteAddr = flag.String("remote_addr", "224.0.0.18",
		"Remote IP Address - used only when test_mode=true")

	testVRID = flag.Int("vrid", 100,
		"VRID - used only when test_mode=true")
)

// config reads the HAConfig from the engine. It does not return until it
// successfully retrieves an HAConfig that has HA peering enabled.
func config(e ha.Engine) *seesaw.HAConfig {
	for {
		c, err := e.HAConfig()
		switch {
		case err != nil:
			log.Errorf("config: Failed to retrieve HAConfig: %v", err)

		case !c.Enabled:
			log.Infof("config: HA peering is currently disabled for this node")

		default:
			return c
		}
		time.Sleep(*initConfigRetryDelay)
	}
}

func engine() ha.Engine {
	if *testMode {
		log.Infof("Using command-line flags for config")
		config := seesaw.HAConfig{
			Enabled:    true,
			LocalAddr:  net.ParseIP(*testLocalAddr),
			RemoteAddr: net.ParseIP(*testRemoteAddr),
			Priority:   uint8(*testPriority),
			VRID:       uint8(*testVRID),
		}
		return &ha.DummyEngine{Config: &config}
	}
	return &ha.EngineClient{Socket: *engineSocket}
}

func main() {
	flag.Parse()

	log.Infof("Starting up")
	engine := engine()
	config := config(engine)
	log.Infof("Received HAConfig: %v", config)
	conn, err := ha.NewIPHAConn(config.LocalAddr, config.RemoteAddr)
	if err != nil {
		log.Fatalf("%v", err)
	}
	nc := ha.NodeConfig{
		HAConfig:                *config,
		ConfigCheckInterval:     *configCheckInterval,
		ConfigCheckMaxFailures:  *configCheckMaxFailures,
		ConfigCheckRetryDelay:   *configCheckRetryDelay,
		MasterAdvertInterval:    *masterAdvertInterval,
		Preempt:                 *preempt,
		StatusReportInterval:    *statusReportInterval,
		StatusReportMaxFailures: *statusReportMaxFailures,
		StatusReportRetryDelay:  *statusReportRetryDelay,
	}
	n := ha.NewNode(nc, conn, engine)
	server.ShutdownHandler(n)

	if err = n.Run(); err != nil {
		log.Fatalf("%v", err)
	}
}
