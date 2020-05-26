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

// Package ha implements high availability (HA) peering between Seesaw nodes using VRRP v3.
package ha

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/seesaw/common/seesaw"
	spb "github.com/google/seesaw/pb/seesaw"
	"gopkg.in/fsnotify.v1"

	log "github.com/golang/glog"
)

// HAConn represents an HA connection for sending and receiving advertisements between two Nodes.
type HAConn interface {
	send(advert *advertisement, timeout time.Duration) error
	receive() (*advertisement, error)
}

// advertisement represents a VRRPv3 advertisement packet.  Field names and sizes are per RFC 5798.
type advertisement struct {
	VersionType  uint8
	VRID         uint8
	Priority     uint8
	CountIPAddrs uint8
	AdvertInt    uint16
	Checksum     uint16
}

const (
	// vrrpAdvertSize is the expected number of bytes in the advertisement struct.
	vrrpAdvertSize = 8

	// vrrpAdvertType is the type of VRRP advertisements to send and receive.
	vrrpAdvertType = uint8(1)

	// vrrpVersion is the VRRP version this module implements.
	vrrpVersion = uint8(3)

	// vrrpVersionType represents the version and advertisement type of VRRP
	// packets that this module supports.
	vrrpVersionType = vrrpVersion<<4 | vrrpAdvertType
)

// NodeConfig specifies the configuration for a Node.
type NodeConfig struct {
	seesaw.HAConfig
	ConfigCheckInterval     time.Duration
	ConfigCheckMaxFailures  int
	ConfigCheckRetryDelay   time.Duration
	MasterAdvertInterval    time.Duration
	Preempt                 bool
	StatusReportInterval    time.Duration
	StatusReportMaxFailures int
	StatusReportRetryDelay  time.Duration
}

// Node represents one member of a high availability cluster.
type Node struct {
	NodeConfig
	conn                 HAConn
	engine               Engine
	engineSocket         string
	statusLock           sync.RWMutex
	haStatus             seesaw.HAStatus
	sendCount            uint64
	receiveCount         uint64
	masterDownInterval   time.Duration
	lastMasterAdvertTime time.Time
	errChannel           chan error
	recvChannel          chan *advertisement
	stopSenderChannel    chan spb.HaState
	shutdownChannel      chan bool
}

// NewNode creates a new Node with the given NodeConfig and HAConn.
func NewNode(cfg NodeConfig, conn HAConn, engine Engine, socket string) *Node {
	n := &Node{
		NodeConfig:        cfg,
		conn:              conn,
		engine:            engine,
		engineSocket:      socket,
		errChannel:        make(chan error),
		recvChannel:       make(chan *advertisement, 20),
		stopSenderChannel: make(chan spb.HaState),
		shutdownChannel:   make(chan bool),
	}
	n.setState(spb.HaState_BACKUP)
	n.resetMasterDownInterval(cfg.MasterAdvertInterval)
	return n
}

// resetMasterDownInterval calculates masterDownInterval per RFC 5798.
func (n *Node) resetMasterDownInterval(advertInterval time.Duration) {
	skewTime := (time.Duration((256 - int(n.Priority))) * (advertInterval)) / 256
	masterDownInterval := 3*(advertInterval) + skewTime
	if masterDownInterval != n.masterDownInterval {
		n.masterDownInterval = masterDownInterval
		log.Infof("resetMasterDownInterval: skewTime=%v, masterDownInterval=%v",
			skewTime, masterDownInterval)
	}
}

// state returns the current HA state for this node.
func (n *Node) state() spb.HaState {
	n.statusLock.RLock()
	defer n.statusLock.RUnlock()
	return n.haStatus.State
}

// setState changes the HA state for this node.
func (n *Node) setState(s spb.HaState) {
	n.statusLock.Lock()
	defer n.statusLock.Unlock()
	if n.haStatus.State != s {
		n.haStatus.State = s
		n.haStatus.Since = time.Now()
		n.haStatus.Transitions++
	}
}

// status returns the current HA status for this node.
func (n *Node) status() seesaw.HAStatus {
	n.statusLock.Lock()
	defer n.statusLock.Unlock()
	n.haStatus.Sent = atomic.LoadUint64(&n.sendCount)
	n.haStatus.Received = atomic.LoadUint64(&n.receiveCount)
	n.haStatus.ReceivedQueued = uint64(len(n.recvChannel))
	return n.haStatus
}

// newAdvertisement creates a new advertisement with this Node's VRID and priority.
func (n *Node) newAdvertisement() *advertisement {
	return &advertisement{
		VersionType: vrrpVersionType,
		VRID:        n.VRID,
		Priority:    n.Priority,
		AdvertInt:   uint16(n.MasterAdvertInterval / time.Millisecond / 10), // AdvertInt is in centiseconds
	}
}

// Run sends and receives advertisements, changes this Node's state in response to incoming
// advertisements, and periodically notifies the engine of the current state. Run does not return
// until Shutdown is called or an unrecoverable error occurs.
func (n *Node) Run() error {
	go n.receiveAdvertisements()
	go n.reportStatus()
	go n.checkConfig()
	go n.watchEngine()

	for n.state() != spb.HaState_SHUTDOWN {
		if err := n.runOnce(); err != nil {
			return err
		}
	}
	return nil
}

// watchEngine monitors engine socket and shutdown current process if it's removed.
// It's needed to have a quick failover when engine is crashed.
func (n *Node) watchEngine() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatalf("Failed to create fsnotify watcher: %v", err)
	}
	defer func() {
		watcher.Close()
		// graceful shutdown
		n.Shutdown()
	}()

	if err := watcher.Add(n.engineSocket); err != nil {
		log.Errorf("watcher.Add failed: %v", err)
		return
	}

	for {
		select {
		// watch for events
		case event := <-watcher.Events:
			if event.Op == fsnotify.Remove {
				log.Error("Engine has been terminated")
				return
			}

			// watch for errors
		case err := <-watcher.Errors:
			log.Errorf("Failed to watch engine socket: %v: ", err)
			return
		}

	}
}

// Shutdown puts this Node in SHUTDOWN state and causes Run() to return.
func (n *Node) Shutdown() {
	n.shutdownChannel <- true
}

func (n *Node) runOnce() error {
	switch s := n.state(); s {
	case spb.HaState_BACKUP:
		switch newState := n.doBackupTasks(); newState {
		case spb.HaState_BACKUP:
			// do nothing
		case spb.HaState_LEADER:
			log.Infof("Received %v advertisements, %v still queued for processing",
				atomic.LoadUint64(&n.receiveCount), len(n.recvChannel))
			log.Infof("Last master advertisement dequeued at %v", n.lastMasterAdvertTime.Format(time.StampMilli))
			n.becomeMaster()
		case spb.HaState_SHUTDOWN:
			n.becomeShutdown()
		default:
			return fmt.Errorf("runOnce: Can't handle transition from %v to %v", s, newState)
		}

	case spb.HaState_LEADER:
		switch newState := n.doMasterTasks(); newState {
		case spb.HaState_LEADER:
			// do nothing
		case spb.HaState_BACKUP:
			log.Infof("Sent %v advertisements", atomic.LoadUint64(&n.sendCount))
			n.becomeBackup()
		case spb.HaState_SHUTDOWN:
			n.becomeShutdown()
		default:
			return fmt.Errorf("runOnce: Can't handle transition from %v to %v", s, newState)
		}

	default:
		return fmt.Errorf("runOnce: Invalid state - %v", s)
	}
	return nil
}

func (n *Node) becomeMaster() {
	log.Infof("Node.becomeMaster")
	if err := n.engine.HAState(spb.HaState_LEADER); err != nil {
		// Ignore for now - reportStatus will notify the engine or die trying.
		log.Errorf("Failed to notify engine: %v", err)
	}

	go n.sendAdvertisements()
	n.setState(spb.HaState_LEADER)
}

func (n *Node) becomeBackup() {
	log.Infof("Node.becomeBackup")
	if err := n.engine.HAState(spb.HaState_BACKUP); err != nil {
		// Ignore for now - reportStatus will notify the engine or die trying.
		log.Errorf("Failed to notify engine: %v", err)
	}

	n.stopSenderChannel <- spb.HaState_BACKUP
	n.setState(spb.HaState_BACKUP)
}

func (n *Node) becomeShutdown() {
	log.Infof("Node.becomeShutdown")
	if err := n.engine.HAState(spb.HaState_SHUTDOWN); err != nil {
		// Ignore for now - reportStatus will notify the engine or die trying.
		log.Errorf("Failed to notify engine: %v", err)
	}

	if n.state() == spb.HaState_LEADER {
		n.stopSenderChannel <- spb.HaState_SHUTDOWN
		// Sleep for a moment so sendAdvertisements() has a chance to send the shutdown advertisment.
		time.Sleep(500 * time.Millisecond)
	}
	n.setState(spb.HaState_SHUTDOWN)
}

func (n *Node) doMasterTasks() spb.HaState {
	select {
	case advert := <-n.recvChannel:
		if advert.Priority == n.Priority {
			// TODO(angusc): RFC 5798 says we should compare IP addresses at this point.
			log.Warningf("doMasterTasks: ignoring advertisement with my priority (%v)", advert.Priority)
			return spb.HaState_LEADER
		}
		if advert.Priority > n.Priority {
			log.Infof("doMasterTasks: peer priority (%v) > my priority (%v) - becoming BACKUP",
				advert.Priority, n.Priority)
			n.lastMasterAdvertTime = time.Now()
			return spb.HaState_BACKUP
		}

	case <-n.shutdownChannel:
		return spb.HaState_SHUTDOWN

	case err := <-n.errChannel:
		log.Errorf("doMasterTasks: %v", err)
		return spb.HaState_ERROR
	}
	// no change
	return spb.HaState_LEADER
}

func (n *Node) doBackupTasks() spb.HaState {
	remaining := n.masterDownInterval
	if !n.lastMasterAdvertTime.IsZero() {
		deadline := n.lastMasterAdvertTime.Add(n.masterDownInterval)
		remaining = deadline.Sub(time.Now())
	}
	timeout := time.After(remaining)
	select {
	case advert := <-n.recvChannel:
		return n.backupHandleAdvertisement(advert)

	case <-n.shutdownChannel:
		return spb.HaState_SHUTDOWN

	case err := <-n.errChannel:
		log.Errorf("doBackupTasks: %v", err)
		return spb.HaState_ERROR

	case <-timeout:
		log.Infof("doBackupTasks: timed out waiting for advertisement after %v", remaining)
		select {
		case advert := <-n.recvChannel:
			log.Infof("doBackupTasks: found advertisement queued for processing")
			return n.backupHandleAdvertisement(advert)
		default:
			log.Infof("doBackupTasks: becoming MASTER")
			return spb.HaState_LEADER
		}
	}
}

func (n *Node) backupHandleAdvertisement(advert *advertisement) spb.HaState {
	switch {
	case advert.Priority == 0:
		log.Infof("backupHandleAdvertisement: peer priority is 0 - becoming MASTER")
		return spb.HaState_LEADER

	case n.Preempt && advert.Priority < n.Priority:
		log.Infof("backupHandleAdvertisement: peer priority (%v) < my priority (%v) - becoming MASTER",
			advert.Priority, n.Priority)
		return spb.HaState_LEADER
	}

	// Per RFC 5798, set the masterDownInterval based on the advert interval received from the
	// current master.  AdvertInt is in centiseconds.
	n.resetMasterDownInterval(time.Millisecond * time.Duration(10*advert.AdvertInt))
	n.lastMasterAdvertTime = time.Now()
	return spb.HaState_BACKUP
}

func (n *Node) queueAdvertisement(advert *advertisement) {
	if queueLen := len(n.recvChannel); queueLen > 0 {
		log.Warningf("queueAdvertisement: %v advertisements already queued", queueLen)
	}
	select {
	case n.recvChannel <- advert:
	default:
		n.errChannel <- fmt.Errorf("queueAdvertisement: recvChannel is full")
	}
}

func (n *Node) sendAdvertisements() {
	ticker := time.NewTicker(n.MasterAdvertInterval)
	for {
		// TODO(angusc): figure out how to make the timing-related logic here, and thoughout, clockjump
		// safe.
		select {
		case <-ticker.C:
			if err := n.conn.send(n.newAdvertisement(), n.MasterAdvertInterval); err != nil {
				select {
				case n.errChannel <- err:
				default:
					log.Fatalf("sendAdvertisements: Unable to write to errChannel. Error was: %v", err)
				}
				break
			}

			sendCount := atomic.AddUint64(&n.sendCount, 1)
			if sendCount%20 == 0 {
				log.Infof("sendAdvertisements: Sent %d advertisements", sendCount)
			}

		case newState := <-n.stopSenderChannel:
			ticker.Stop()
			if newState == spb.HaState_SHUTDOWN {
				advert := n.newAdvertisement()
				advert.Priority = 0
				if err := n.conn.send(advert, time.Second); err != nil {
					log.Warningf("sendAdvertisements: Failed to send shutdown advertisement, %v", err)
				}
			}
			return
		}
	}
}

func (n *Node) receiveAdvertisements() {
	for {
		if advert, err := n.conn.receive(); err != nil {
			select {
			case n.errChannel <- err:
			default:
				log.Fatalf("receiveAdvertisements: Unable to write to errChannel. Error was: %v", err)
			}
		} else if advert != nil {
			if advert.VersionType != vrrpVersionType || advert.VRID != n.VRID {
				continue
			}
			receiveCount := atomic.AddUint64(&n.receiveCount, 1)
			if receiveCount%20 == 0 {
				log.Infof("receiveAdvertisements: Received %d advertisements", receiveCount)
			}
			n.queueAdvertisement(advert)
		}
	}
}

func (n *Node) reportStatus() {
	for {
		var err error
		failover := false
		failures := 0
		for {
			failover, err = n.engine.HAUpdate(n.status())
			if err == nil {
				break
			}
			failures++
			log.Errorf("reportStatus: %v", err)
			if failures > n.StatusReportMaxFailures {
				n.errChannel <- fmt.Errorf("reportStatus: %d errors, giving up", failures)
				return
			}
			time.Sleep(n.StatusReportRetryDelay)
		}
		if failover && n.state() == spb.HaState_LEADER {
			log.Info("Received failover request, initiating shutdown...")
			n.Shutdown()
		}
		time.Sleep(n.StatusReportInterval)
	}
}

func (n *Node) checkConfig() {
	for {
		failures := 0
		var cfg *seesaw.HAConfig
		var err error
		for {
			cfg, err = n.engine.HAConfig()
			if err == nil {
				break
			}
			failures++
			log.Errorf("checkConfig: %v", err)
			if failures > n.ConfigCheckMaxFailures {
				n.errChannel <- fmt.Errorf("checkConfig: %d errors, giving up", failures)
				return
			}
			time.Sleep(n.ConfigCheckRetryDelay)
		}
		if !cfg.Equal(&n.HAConfig) {
			log.Infof("Previous HAConfig: %v", n.HAConfig)
			log.Infof("New HAConfig: %v", *cfg)
			n.errChannel <- fmt.Errorf("checkConfig: HAConfig has changed")
		}
		time.Sleep(n.ConfigCheckInterval)
	}
}
