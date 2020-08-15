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

// Author: angusc@google.com (Angus Cameron)

package engine

// This file contains structs and functions to manage the high availability
// (HA) state for a Seesaw Engine.

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/seesaw/common/seesaw"
	spb "github.com/google/seesaw/pb/seesaw"

	log "github.com/golang/glog"
)

// haManager manages the HA state for a seesaw engine.
type haManager struct {
	engine          *Engine
	failoverPending bool
	failoverLock    sync.RWMutex
	status          seesaw.HAStatus
	statusLock      sync.RWMutex
	timeout         time.Duration
	stateChan       chan spb.HaState
	statusChan      chan seesaw.HAStatus
}

// newHAManager creates a new haManager with the given HA state timeout.
func newHAManager(engine *Engine, timeout time.Duration) *haManager {
	now := time.Now()
	return &haManager{
		engine: engine,
		status: seesaw.HAStatus{
			LastUpdate: now,
			Since:      now,
			State:      spb.HaState_UNKNOWN,
		},
		timeout:    timeout,
		stateChan:  make(chan spb.HaState, 1),
		statusChan: make(chan seesaw.HAStatus, 1),
	}
}

// state returns the current HA state known by the engine.
func (h *haManager) state() spb.HaState {
	h.statusLock.RLock()
	defer h.statusLock.RUnlock()
	return h.status.State
}

// enable enables HA peering for the node on which the engine is running.
func (h *haManager) enable() {
	if h.state() == spb.HaState_DISABLED {
		h.setState(spb.HaState_UNKNOWN)
	}
}

// disable disables HA peering for the node on which the engine is running.
func (h *haManager) disable() {
	h.setState(spb.HaState_DISABLED)
}

// failover returns true if the HA component should relinquish master state.
func (h *haManager) failover() bool {
	h.failoverLock.Lock()
	pending := h.failoverPending
	h.failoverPending = false
	h.failoverLock.Unlock()
	return pending
}

// requestFailover requests the node to initiate a failover.
func (h *haManager) requestFailover(peer bool) error {
	state := h.state()
	if state == spb.HaState_LEADER {
		h.failoverLock.Lock()
		defer h.failoverLock.Unlock()
		if h.failoverPending {
			return fmt.Errorf("Failover request already pending")
		}
		h.failoverPending = true
		return nil
	}

	if peer {
		return fmt.Errorf("Node is not master (current state is %v)", state)
	}

	return h.engine.syncClient.failover()
}

// setState sets the HAState of the engine and dispatches events when the state
// changes.
func (h *haManager) setState(s spb.HaState) {
	state := h.state()

	if state == spb.HaState_DISABLED && s != spb.HaState_UNKNOWN {
		log.Warningf("Invalid HA state transition %v -> %v", state, s)
		return
	}

	if state != s {
		log.Infof("HA state transition %v -> %v starting", state, s)
		if s == spb.HaState_LEADER {
			h.engine.becomeMaster()
		} else if state == spb.HaState_LEADER || s == spb.HaState_BACKUP {
			h.engine.becomeBackup()
		}
		log.Infof("HA state transition %v -> %v complete", state, s)
	}

	now := time.Now()

	h.statusLock.Lock()
	h.status.State = s
	h.status.Since = now
	h.status.LastUpdate = now
	h.statusLock.Unlock()
}

// setStatus updates the engine HAStatus.
func (h *haManager) setStatus(s seesaw.HAStatus) {
	h.setState(s.State)

	h.statusLock.Lock()
	h.status.Since = s.Since
	h.status.Sent = s.Sent
	h.status.Received = s.Received
	h.status.Transitions = s.Transitions
	h.statusLock.Unlock()
}

// timer returns a channel that receives a Time object when the current HA state
// expires.
func (h *haManager) timer() <-chan time.Time {
	if s := h.state(); s == spb.HaState_DISABLED || s == spb.HaState_UNKNOWN {
		return make(chan time.Time)
	}
	// TODO(angusc): Make this clock-jump safe.
	h.statusLock.RLock()
	deadline := h.status.LastUpdate.Add(h.timeout)
	h.statusLock.RUnlock()
	return time.After(deadline.Sub(time.Now()))
}
