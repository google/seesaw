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

package engine

// This file contains structures and functions to manage the BGP configuration
// for a Seesaw Engine.

import (
	"sync"
	"time"

	"github.com/google/seesaw/quagga"

	log "github.com/golang/glog"
)

// bgpManager contains the data necessary to run a BGP configuration manager.
type bgpManager struct {
	engine         *Engine
	updateInterval time.Duration

	lock      sync.RWMutex
	neighbors []*quagga.Neighbor
}

// newBGPManager returns an initialised bgpManager struct.
func newBGPManager(engine *Engine, interval time.Duration) *bgpManager {
	return &bgpManager{engine: engine, updateInterval: interval}
}

// run runs the BGP configuration manager.
func (b *bgpManager) run() {
	ticker := time.NewTicker(b.updateInterval)
	for {
		log.V(1).Infof("Updating BGP state and statistics...")
		b.update()
		<-ticker.C
	}
}

// update updates BGP related state and statistics from the BGP daemon.
func (b *bgpManager) update() {
	ncc := b.engine.ncc
	if err := ncc.Dial(); err != nil {
		log.Warningf("BGP manager failed to connect to NCC: %v", err)
		return
	}
	defer ncc.Close()

	neighbors, err := ncc.BGPNeighbors()
	if err != nil {
		log.Warningf("Failed to get BGP neighbors: %v", err)
		return
	}
	b.lock.Lock()
	b.neighbors = neighbors
	b.lock.Unlock()
}
