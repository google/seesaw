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

package config

// This file contains the notifier, which is responsible for monitoring cluster
// configuration sources and providing notifications on configuration change.

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/seesaw/common/seesaw"
	pb "github.com/google/seesaw/pb/config"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
)

// Notifier monitors cluster configuration sources and sends Notifications via
// C on configuration changes.
type Notifier struct {
	// Immutable fields.
	C         <-chan Notification
	outgoing  chan<- Notification
	reload    chan bool
	shutdown  chan bool
	engineCfg *EngineConfig

	// Mutable fields accessed by a single goroutine.
	last         *Notification
	peerFailures int

	// Lock for mutable fields accessed by more than one go routine.
	lock sync.RWMutex

	// Mutable fields accessed by more than one go routine.
	source Source
}

// NewNotifier creates a new Notifier.
func NewNotifier(ec *EngineConfig) (*Notifier, error) {
	outgoing := make(chan Notification, 1)
	n := &Notifier{
		C:         outgoing,
		outgoing:  outgoing,
		reload:    make(chan bool, 1),
		shutdown:  make(chan bool, 1),
		engineCfg: ec,
		source:    SourcePeer,
	}

	note, err := n.bootstrap()
	if err != nil {
		return nil, err
	}

	// If the on disk configuration is different, update it.
	if note.Source != SourceDisk {
		dNote, _ := n.pullConfig(SourceDisk)
		if dNote == nil || !dNote.Cluster.Equal(note.Cluster) {
			if err := saveConfig(note.protobuf, n.engineCfg.ClusterFile, true); err != nil {
				log.Warningf("Failed to save config to %s: %v", n.engineCfg.ClusterFile, err)
			}
		}
	}

	n.last = note
	n.outgoing <- *note

	go n.run()
	return n, nil
}

// Source returns the current configuration source.
func (n *Notifier) Source() Source {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return n.source
}

// SetSource sets the configuration Source for a Notifier.
func (n *Notifier) SetSource(source Source) {
	n.lock.Lock()
	n.source = source
	n.lock.Unlock()
	if err := n.Reload(); err != nil {
		log.Warningf("Reload failed after setting source: %v", err)
	}
}

// Reload requests an immediate reload from the configuration source.
func (n *Notifier) Reload() error {
	select {
	case n.reload <- true:
	default:
		return errors.New("reload request already queued")
	}
	return nil
}

// Shutdown shuts down a Notifier.
func (n *Notifier) Shutdown() {
	n.shutdown <- true
}

func (n *Notifier) run() {
	log.Infof("Configuration notifier started")
	configTicker := time.NewTicker(n.engineCfg.ConfigInterval)
	for {
		select {
		case <-n.shutdown:
			return
		case <-n.reload:
			n.configCheck()
		case <-configTicker.C:
			n.configCheck()
		}
	}
}

// configCheck checks for configuration changes.
func (n *Notifier) configCheck() {
	log.Infof("Checking for config changes...")

	s := n.Source()
	last := n.last
	note, err := n.pullConfig(s)
	if err != nil && s == SourcePeer {
		log.Errorf("Failed to pull configuration from peer: %v", err)
		n.peerFailures++
		if n.peerFailures < n.engineCfg.MaxPeerConfigSyncErrors {
			return
		}
		log.Infof("Sync from peer failed %v times, falling back to config server",
			n.engineCfg.MaxPeerConfigSyncErrors)
		s = SourceServer
		note, err = n.pullConfig(s)
	}
	n.peerFailures = 0
	if err != nil {
		log.Errorf("Failed to pull configuration: %v", err)
		return
	}

	if s != SourceDisk && s != SourcePeer {
		oldMeta := last.protobuf.Metadata
		newMeta := note.protobuf.Metadata
		if oldMeta != nil && newMeta != nil && oldMeta.GetLastUpdated() > newMeta.GetLastUpdated() {
			log.Infof("Ignoring out-of-date config from %v", note.SourceDetail)
			return
		}
	}

	if note.Cluster.Equal(last.Cluster) {
		log.Infof("No config changes found")
		return
	}

	// If there's only metadata differences, note it so we can skip some processing later.
	oldCluster := *n.last.Cluster
	oldCluster.Status = seesaw.ConfigStatus{}
	newCluster := *note.Cluster
	newCluster.Status = seesaw.ConfigStatus{}
	if newCluster.Equal(&oldCluster) {
		note.MetadataOnly = true
	}

	log.Infof("Sending config update notification")
	n.last = note
	n.outgoing <- *note
	log.Infof("Sent config update notification")

	if s != SourceDisk {
		if err := saveConfig(note.protobuf, n.engineCfg.ClusterFile, !note.MetadataOnly); err != nil {
			log.Warningf("Failed to save config to %s: %v", n.engineCfg.ClusterFile, err)
		}
	}
}

func (n *Notifier) pullConfig(s Source) (*Notification, error) {
	switch s {
	case SourceDisk:
		return n.configFromDisk()
	case SourcePeer:
		return n.configFromPeer()
	case SourceServer:
		return n.configFromServer()
	}
	return nil, fmt.Errorf("pullConfig: Unsupported Notifier source %v", s)
}

func (n *Notifier) bootstrap() (*Notification, error) {
	var note *Notification
	var err error
	if note, err = n.pullConfig(SourcePeer); err == nil {
		return note, nil
	}
	log.Warningf("Failed to load cluster config from peer: %v", err)

	if note, err = n.pullConfig(SourceDisk); err == nil {
		return note, nil
	}
	log.Warningf("Failed to load cluster config from disk: %v", err)

	if note, err = n.pullConfig(SourceServer); err == nil {
		return note, nil
	}
	log.Warningf("Failed to load cluster config from server: %v", err)

	return nil, fmt.Errorf("Notifier.bootstrap: Failed to load any cluster config")
}

func (n *Notifier) configFromDisk() (*Notification, error) {
	return ReadConfig(n.engineCfg.ClusterFile, n.engineCfg.ClusterName)
}

func (n *Notifier) configFromPeer() (*Notification, error) {
	// TODO(angusc): Implement this function.
	return nil, fmt.Errorf("configFromPeer not implemented")
}

func (n *Notifier) configFromServer() (*Notification, error) {
	f, err := newFetcher(n.engineCfg)
	if err != nil {
		return nil, err
	}
	source, body, err := f.config()
	if err != nil {
		return nil, err
	}
	p := &pb.Cluster{}
	if err := proto.Unmarshal(body, p); err != nil {
		return nil, fmt.Errorf("invalid configuration from %v: %v", source, err)
	}
	c, err := protoToCluster(p, n.engineCfg.ClusterName)
	if err != nil {
		return nil, fmt.Errorf("invalid configuration from %v: %v", source, err)
	}
	return &Notification{c, false, p, SourceServer, source, time.Now()}, nil
}
