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

package ecu

// This file contains functions that implement statistics collection for data
// that will be exported.

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/quagga"

	log "github.com/golang/glog"
)

// publisher implements an interface for a statistics publisher.
type publisher interface {
	update(s *stats)
}

// stats contains the statistics collected from the Seesaw Engine.
type stats struct {
	lock sync.RWMutex

	lastUpdate  time.Time
	lastSuccess time.Time

	seesaw.ClusterStatus
	seesaw.ConfigStatus
	seesaw.HAStatus
	neighbors []*quagga.Neighbor
	vlans     []*seesaw.VLAN
	vservers  map[string]*seesaw.Vserver
}

// ecuStats contains that data needed to the ECU stats collector.
type ecuStats struct {
	ecu        *ECU
	publishers []publisher
	stats      *stats
}

// newECUStats returns an initialised ecuStats struct.
func newECUStats(ecu *ECU) *ecuStats {
	return &ecuStats{
		ecu: ecu,
		stats: &stats{
			lastUpdate:  time.Unix(0, 0),
			lastSuccess: time.Unix(0, 0),
		},
	}
}

// notify registers a publisher for update notifications.
func (e *ecuStats) notify(p publisher) {
	e.publishers = append(e.publishers, p)
}

// run attempts to update the cached statistics from the Seesaw Engine at
// regular intervals.
func (e *ecuStats) run() {
	ticker := time.NewTicker(e.ecu.cfg.UpdateInterval)
	for {
		e.stats.lock.Lock()
		e.stats.lastUpdate = time.Now()
		e.stats.lock.Unlock()

		log.Info("Updating ECU statistics from Seesaw Engine...")
		if err := e.update(); err != nil {
			log.Warning(err)
		} else {
			e.stats.lock.Lock()
			e.stats.lastSuccess = time.Now()
			e.stats.lock.Unlock()
		}
		for _, p := range e.publishers {
			p.update(e.stats)
		}
		<-ticker.C
	}
}

// update attempts to update the cached statistics from the Seesaw Engine.
func (e *ecuStats) update() error {
	// TODO(jsing): Make this untrusted.
	ctx := ipc.NewTrustedContext(seesaw.SCECU)
	seesawConn, err := conn.NewSeesawIPC(ctx)
	if err != nil {
		return fmt.Errorf("Failed to connect to engine: %v", err)
	}
	if err := seesawConn.Dial(e.ecu.cfg.EngineSocket); err != nil {
		return fmt.Errorf("Failed to connect to engine: %v", err)
	}
	defer seesawConn.Close()

	clusterStatus, err := seesawConn.ClusterStatus()
	if err != nil {
		return fmt.Errorf("Failed to get cluster status: %v", err)
	}
	e.stats.lock.Lock()
	e.stats.ClusterStatus = *clusterStatus
	e.stats.lock.Unlock()

	configStatus, err := seesawConn.ConfigStatus()
	if err != nil {
		return fmt.Errorf("Failed to get config status: %v", err)
	}
	e.stats.lock.Lock()
	e.stats.ConfigStatus = *configStatus
	e.stats.lock.Unlock()

	ha, err := seesawConn.HAStatus()
	if err != nil {
		return fmt.Errorf("Failed to get HA status: %v", err)
	}
	e.stats.lock.Lock()
	e.stats.HAStatus = *ha
	e.stats.lock.Unlock()

	neighbors, err := seesawConn.BGPNeighbors()
	if err != nil {
		return fmt.Errorf("Failed to get BGP neighbors: %v", err)
	}
	e.stats.lock.Lock()
	e.stats.neighbors = neighbors
	e.stats.lock.Unlock()

	vlans, err := seesawConn.VLANs()
	if err != nil {
		return fmt.Errorf("Failed to get VLANs: %v", err)
	}
	e.stats.lock.Lock()
	e.stats.vlans = vlans.VLANs
	e.stats.lock.Unlock()

	vservers, err := seesawConn.Vservers()
	if err != nil {
		return fmt.Errorf("Failed to get vservers: %v", err)
	}
	e.stats.lock.Lock()
	e.stats.vservers = vservers
	e.stats.lock.Unlock()

	return nil
}
