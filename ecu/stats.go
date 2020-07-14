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

// Publisher is an interface for a statistics publisher. Publishers are
// notified of updates to the statistics with the Update method.
type Publisher interface {
	Update(s *Stats)
}

// Stats contains the statistics collected from the Seesaw Engine.
type Stats struct {
	LastUpdate  time.Time
	LastSuccess time.Time

	ClusterStatus seesaw.ClusterStatus
	ConfigStatus  seesaw.ConfigStatus
	HAStatus      seesaw.HAStatus
	Neighbors     []*quagga.Neighbor
	VLANs         []*seesaw.VLAN
	Vservers      map[string]*seesaw.Vserver
}

// ecuStats contains that data needed to the ECU stats collector.
type ecuStats struct {
	ecu *ECU

	publishersMu sync.RWMutex
	publishers   []Publisher

	lastStats *Stats
}

// newECUStats returns an initialised ecuStats struct.
func newECUStats(ecu *ECU) *ecuStats {
	return &ecuStats{ecu: ecu}
}

// notify registers publishers for update notifications.
func (e *ecuStats) notify(pubs ...Publisher) {
	e.publishersMu.Lock()
	defer e.publishersMu.Unlock()
	e.publishers = append(e.publishers, pubs...)
}

func (e *ecuStats) runOnce() {
	log.Info("Updating ECU statistics from Seesaw Engine...")
	start := time.Now()
	s, err := e.stats()
	if err != nil {
		log.Warningf("Couldn't update statistics: %v", err)
		s = e.lastStats
	}
	s.LastUpdate = start
	e.lastStats = s
	e.publishersMu.RLock()
	defer e.publishersMu.RUnlock()
	for _, p := range e.publishers {
		t := *s
		p.Update(&t)
	}
}

// run attempts to update the cached statistics from the Seesaw Engine at
// regular intervals.
func (e *ecuStats) run() {
	e.runOnce()
	for range time.Tick(e.ecu.cfg.UpdateInterval) {
		e.runOnce()
	}
}

// stats attempts to gather statistics from the Seesaw Engine.
func (e *ecuStats) stats() (*Stats, error) {
	// TODO(jsing): Make this untrusted.
	ctx := ipc.NewTrustedContext(seesaw.SCECU)
	seesawConn, err := conn.NewSeesawIPC(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect to engine: %v", err)
	}
	if err := seesawConn.Dial(e.ecu.cfg.EngineSocket); err != nil {
		return nil, fmt.Errorf("connect to engine: %v", err)
	}
	defer seesawConn.Close()

	clusterStatus, err := seesawConn.ClusterStatus()
	if err != nil {
		return nil, fmt.Errorf("get cluster status: %v", err)
	}

	configStatus, err := seesawConn.ConfigStatus()
	if err != nil {
		return nil, fmt.Errorf("get config status: %v", err)
	}

	ha, err := seesawConn.HAStatus()
	if err != nil {
		return nil, fmt.Errorf("get HA status: %v", err)
	}

	neighbors, err := seesawConn.BGPNeighbors()
	if err != nil {
		return nil, fmt.Errorf("get BGP neighbors: %v", err)
	}

	vlans, err := seesawConn.VLANs()
	if err != nil {
		return nil, fmt.Errorf("get VLANs: %v", err)
	}

	vservers, err := seesawConn.Vservers()
	if err != nil {
		return nil, fmt.Errorf("get vservers: %v", err)
	}

	return &Stats{
		LastSuccess:   time.Now(),
		ClusterStatus: *clusterStatus,
		ConfigStatus:  *configStatus,
		HAStatus:      *ha,
		Neighbors:     neighbors,
		VLANs:         vlans.VLANs,
		Vservers:      vservers,
	}, nil
}
