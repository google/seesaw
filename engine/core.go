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
Package engine implements the Seesaw v2 engine component, which is
responsible for maintaining configuration information, handling state
transitions and providing communication between Seesaw v2 components.
*/
package engine

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/engine/config"
	ncclient "github.com/google/seesaw/ncc/client"
	ncctypes "github.com/google/seesaw/ncc/types"
	spb "github.com/google/seesaw/pb/seesaw"

	log "github.com/golang/glog"
)

const (
	fwmAllocBase = 1 << 8
	fwmAllocSize = 8000
)

// Engine contains the data necessary to run the Seesaw v2 Engine.
type Engine struct {
	config   *config.EngineConfig
	notifier *config.Notifier

	fwmAlloc *markAllocator

	bgpManager *bgpManager
	haManager  *haManager
	hcManager  *healthcheckManager

	ncc         ncclient.NCC
	lbInterface ncclient.LBInterface

	cluster     *config.Cluster
	clusterLock sync.RWMutex

	shutdown    chan bool
	shutdownARP chan bool
	shutdownIPC chan bool
	shutdownRPC chan bool

	syncClient *syncClient
	syncServer *syncServer

	overrides    map[string]seesaw.Override
	overrideChan chan seesaw.Override

	vlans    map[uint16]*seesaw.VLAN
	vlanLock sync.RWMutex

	vservers map[string]*vserver

	vserverAccess *vserverAccess

	vserverSnapshots map[string]*seesaw.Vserver
	vserverLock      sync.RWMutex
	vserverChan      chan *seesaw.Vserver

	startTime time.Time

	arpMap  map[string][]net.IP // iface name -> IP list
	arpLock sync.Mutex
}

func newEngineWithNCC(cfg *config.EngineConfig, ncc ncclient.NCC) *Engine {
	if cfg == nil {
		defaultCfg := config.DefaultEngineConfig()
		cfg = &defaultCfg
	}

	// TODO(jsing): Validate node, peer and cluster IP configuration.
	engine := &Engine{
		config:   cfg,
		fwmAlloc: newMarkAllocator(fwmAllocBase, fwmAllocSize),
		ncc:      ncc,

		overrides:    make(map[string]seesaw.Override),
		overrideChan: make(chan seesaw.Override),

		vlans:    make(map[uint16]*seesaw.VLAN),
		vservers: make(map[string]*vserver),

		shutdown:    make(chan bool),
		shutdownARP: make(chan bool),
		shutdownIPC: make(chan bool),
		shutdownRPC: make(chan bool),

		vserverAccess: newVserverAccess(),

		vserverSnapshots: make(map[string]*seesaw.Vserver),
		vserverChan:      make(chan *seesaw.Vserver, 1000),
	}
	engine.bgpManager = newBGPManager(engine, cfg.BGPUpdateInterval)
	engine.haManager = newHAManager(engine, cfg.HAStateTimeout)
	engine.hcManager = newHealthcheckManager(engine)
	engine.syncClient = newSyncClient(engine)
	engine.syncServer = newSyncServer(engine)
	return engine
}

// NewEngine returns an initialised Engine struct.
func NewEngine(cfg *config.EngineConfig) *Engine {
	ncc, err := ncclient.NewNCC(cfg.NCCSocket)
	if err != nil {
		log.Fatalf("Failed to create ncc client: %v", err)
	}
	return newEngineWithNCC(cfg, ncc)
}

// Run starts the Engine.
func (e *Engine) Run() {
	log.Infof("Seesaw Engine starting for %s", e.config.ClusterName)

	e.startTime = time.Now()
	e.initNetwork()

	n, err := config.NewNotifier(e.config)
	if err != nil {
		log.Fatalf("config.NewNotifier() failed: %v", err)
	}
	e.notifier = n

	if e.config.AnycastEnabled {
		go e.bgpManager.run()
	}
	go e.hcManager.run()

	go e.syncClient.run()
	go e.syncServer.run()

	go e.syncRPC()
	go e.engineIPC()
	go e.gratuitousARP()

	e.manager()
}

// Shutdown attempts to perform a graceful shutdown of the engine.
func (e *Engine) Shutdown() {
	e.shutdown <- true
}

// haStatus returns the current HA status from the engine.
func (e *Engine) haStatus() seesaw.HAStatus {
	e.haManager.statusLock.RLock()
	defer e.haManager.statusLock.RUnlock()
	return e.haManager.status
}

// queueOverride queues an Override for processing.
func (e *Engine) queueOverride(o seesaw.Override) {
	e.overrideChan <- o
}

// setHAState tells the engine what its current HAState should be.
func (e *Engine) setHAState(state spb.HaState) error {
	select {
	case e.haManager.stateChan <- state:
	default:
		return fmt.Errorf("state channel if full")
	}
	return nil
}

// setHAStatus tells the engine what the current HA status is.
func (e *Engine) setHAStatus(status seesaw.HAStatus) error {
	select {
	case e.haManager.statusChan <- status:
	default:
		return fmt.Errorf("status channel if full")
	}
	return nil
}

// haConfig returns the HAConfig for an engine.
func (e *Engine) haConfig() (*seesaw.HAConfig, error) {
	n, err := e.thisNode()
	if err != nil {
		return nil, err
	}
	// TODO(jsing): This does not allow for IPv6-only operation.
	return &seesaw.HAConfig{
		Enabled:    n.State != spb.HaState_DISABLED,
		LocalAddr:  e.config.Node.IPv4Addr,
		RemoteAddr: e.config.VRRPDestIP,
		Priority:   n.Priority,
		VRID:       e.config.VRID,
	}, nil
}

// thisNode returns the Node for the machine on which this engine is running.
func (e *Engine) thisNode() (*seesaw.Node, error) {
	e.clusterLock.RLock()
	c := e.cluster
	e.clusterLock.RUnlock()

	if c == nil {
		return nil, fmt.Errorf("cluster configuration not loaded")
	}
	// TODO(jsing): This does not allow for IPv6-only operation.
	ip := e.config.Node.IPv4Addr
	for _, n := range c.Nodes {
		if ip.Equal(n.IPv4Addr) {
			return n, nil
		}
	}
	return nil, fmt.Errorf("node %v not configured", ip)
}

// engineIPC starts an RPC server to handle IPC via a Unix Domain socket.
func (e *Engine) engineIPC() {
	if err := server.RemoveUnixSocket(e.config.SocketPath); err != nil {
		log.Fatalf("Failed to remove socket: %v", err)
	}
	ln, err := net.Listen("unix", e.config.SocketPath)
	if err != nil {
		log.Fatalf("Listen failed: %v", err)
	}
	defer os.Remove(e.config.SocketPath)

	seesawIPC := rpc.NewServer()
	seesawIPC.Register(&SeesawEngine{e})
	go server.RPCAccept(ln, seesawIPC)

	<-e.shutdownIPC
	ln.Close()
	e.shutdownIPC <- true
}

// syncRPC starts a server to handle synchronisation RPCs via a TCP socket.
func (e *Engine) syncRPC() {
	// TODO(jsing): Make this default to IPv6, if configured.
	addr := &net.TCPAddr{
		IP:   e.config.Node.IPv4Addr,
		Port: e.config.SyncPort,
	}
	ln, err := net.ListenTCP("tcp", addr)
	if err != nil {
		log.Fatalf("Listen failed: %v", err)
	}

	go e.syncServer.serve(ln)

	<-e.shutdownRPC
	ln.Close()
	e.shutdownRPC <- true
}

// initNetwork initialises the network configuration for load balancing.
func (e *Engine) initNetwork() {
	if e.config.AnycastEnabled {
		if err := e.ncc.BGPWithdrawAll(); err != nil {
			log.Fatalf("Failed to withdraw all BGP advertisements: %v", err)
		}
	}
	if err := e.ncc.IPVSFlush(); err != nil {
		log.Fatalf("Failed to flush IPVS table: %v", err)
	}

	lbCfg := &ncctypes.LBConfig{
		ClusterVIP:     e.config.ClusterVIP,
		DummyInterface: e.config.DummyInterface,
		NodeInterface:  e.config.NodeInterface,
		Node:           e.config.Node,
		RoutingTableID: e.config.RoutingTableID,
		VRID:           e.config.VRID,
		UseVMAC:        e.config.UseVMAC,
	}
	e.lbInterface = e.ncc.NewLBInterface(e.config.LBInterface, lbCfg)

	if err := e.lbInterface.Init(); err != nil {
		log.Fatalf("Failed to initialise LB interface: %v", err)
	}

	if e.config.AnycastEnabled {
		e.initAnycast()
	}
}

// initAnycast initialises the anycast configuration.
func (e *Engine) initAnycast() {
	vips := make([]*seesaw.VIP, 0)
	if e.config.ClusterVIP.IPv4Addr != nil {
		for _, ip := range e.config.ServiceAnycastIPv4 {
			vips = append(vips, seesaw.NewVIP(ip, nil))
		}
	}
	if e.config.ClusterVIP.IPv6Addr != nil {
		for _, ip := range e.config.ServiceAnycastIPv6 {
			vips = append(vips, seesaw.NewVIP(ip, nil))
		}
	}
	for _, vip := range vips {
		if err := e.lbInterface.AddVIP(vip); err != nil {
			log.Fatalf("Failed to add VIP %v: %v", vip, err)
		}
		log.Infof("Advertising BGP route for %v", vip)
		if err := e.ncc.BGPAdvertiseVIP(vip.IP.IP()); err != nil {
			log.Fatalf("Failed to advertise VIP %v: %v", vip, err)
		}
	}
}

// gratuitousARP sends gratuitous ARP messages at regular intervals, if this
// node is the HA master.
func (e *Engine) gratuitousARP() {
	arpTicker := time.NewTicker(e.config.GratuitousARPInterval)
	var announced bool
	for {
		select {
		case <-arpTicker.C:
			if e.haManager.state() != spb.HaState_LEADER {
				if announced {
					log.Info("Stopping gratuitous ARPs")
					announced = false
				}
				continue
			}
			if !announced {
				log.Infof("Starting gratuitous ARPs every %s", e.config.GratuitousARPInterval)
				announced = true
			}
			e.arpLock.Lock()
			arpMap := e.arpMap
			e.arpLock.Unlock()
			if err := e.ncc.ARPSendGratuitous(arpMap); err != nil {
				log.Fatalf("Failed to send gratuitous ARP: %v", err)
			}

		case <-e.shutdownARP:
			e.shutdownARP <- true
			return
		}
	}
}

// manager is responsible for managing and co-ordinating various parts of the
// seesaw engine.
func (e *Engine) manager() {
	for {
		// process ha state updates first before processing others
		select {
		case state := <-e.haManager.stateChan:
			log.Infof("Received HA state notification %v", state)
			e.haManager.setState(state)
			continue
		case status := <-e.haManager.statusChan:
			log.V(1).Infof("Received HA status notification (%v)", status.State)
			e.haManager.setStatus(status)
			continue
		default:
		}
		select {
		case state := <-e.haManager.stateChan:
			log.Infof("Received HA state notification %v", state)
			e.haManager.setState(state)

		case status := <-e.haManager.statusChan:
			log.V(1).Infof("Received HA status notification (%v)", status.State)
			e.haManager.setStatus(status)

		case n := <-e.notifier.C:
			log.V(1).Infof("Received cluster config notification; %v", &n)
			e.syncServer.notify(&SyncNote{Type: SNTConfigUpdate, Time: time.Now()})

			vua, err := newVserverUserAccess(n.Cluster)
			if err != nil {
				log.Errorf("Ignoring notification due to invalid vserver access configuration: %v", err)
				return
			}

			e.clusterLock.Lock()
			e.cluster = n.Cluster
			e.clusterLock.Unlock()

			e.vserverAccess.update(vua)

			if n.MetadataOnly {
				log.V(1).Infof("Only metadata changes found, processing complete.")
				continue
			}

			log.Infof("Processing new config")

			if ha, err := e.haConfig(); err != nil {
				log.Errorf("Manager failed to determine haConfig: %v", err)
			} else if ha.Enabled {
				e.haManager.enable()
			} else {
				e.haManager.disable()
			}

			node, err := e.thisNode()
			if err != nil {
				log.Errorf("Manager failed to identify local node: %v", err)
				continue
			}
			if !node.VserversEnabled {
				e.shutdownVservers()
				e.deleteVLANs()
				continue
			}

			// Process new cluster configuration.
			e.updateVLANs()

			// TODO(jsing): Ensure this does not block.
			e.updateVservers()

			e.updateARPMap()

		case <-e.haManager.timer():
			log.Infof("Timed out waiting for HAState")
			e.haManager.setState(spb.HaState_UNKNOWN)

		case svs := <-e.vserverChan:
			if _, ok := e.vservers[svs.Name]; !ok {
				log.Infof("Received vserver snapshot for unconfigured vserver %s, ignoring", svs.Name)
				break
			}
			log.V(1).Infof("Updating vserver snapshot for %s", svs.Name)
			e.vserverLock.Lock()
			e.vserverSnapshots[svs.Name] = svs
			e.vserverLock.Unlock()

		case override := <-e.overrideChan:
			sn := &SyncNote{Type: SNTOverride, Time: time.Now()}
			switch o := override.(type) {
			case *seesaw.BackendOverride:
				sn.BackendOverride = o
			case *seesaw.DestinationOverride:
				sn.DestinationOverride = o
			case *seesaw.VserverOverride:
				sn.VserverOverride = o
			}
			e.syncServer.notify(sn)
			e.handleOverride(override)

		case <-e.shutdown:
			log.Info("Shutting down engine...")

			// Tell other components to shutdown and then wait for
			// them to do so.
			e.shutdownIPC <- true
			e.shutdownRPC <- true
			<-e.shutdownIPC
			<-e.shutdownRPC

			e.syncClient.disable()
			e.shutdownVservers()
			e.hcManager.shutdown()
			e.deleteVLANs()
			e.ncc.Close()

			log.Info("Shutdown complete")
			return
		}
	}
}

// updateVservers processes a list of vserver configurations then stops
// deleted vservers, spawns new vservers and updates the existing vservers.
func (e *Engine) updateVservers() {
	e.clusterLock.RLock()
	cluster := e.cluster
	e.clusterLock.RUnlock()

	// Delete vservers that no longer exist in the new configuration.
	for name, vserver := range e.vservers {
		if cluster.Vservers[name] == nil {
			log.Infof("Stopping unconfigured vserver %s", name)
			vserver.stop()
			<-vserver.stopped
			delete(e.vservers, name)
			e.vserverLock.Lock()
			delete(e.vserverSnapshots, name)
			e.vserverLock.Unlock()
		}
	}

	// Spawn new vservers and provide current configurations.
	for _, config := range cluster.Vservers {
		if e.vservers[config.Name] == nil {
			vserver := newVserver(e)
			go vserver.run()
			e.vservers[config.Name] = vserver
		}
	}
	for _, override := range e.overrides {
		e.distributeOverride(override)
	}
	for _, config := range cluster.Vservers {
		e.vservers[config.Name].updateConfig(config)
	}
}

// updateVservers processes a list of vserver configurations then stops
// deleted vservers, spawns new vservers and updates the existing vservers.
func (e *Engine) updateARPMap() {
	arpMap := make(map[string][]net.IP)
	defer func() {
		e.arpLock.Lock()
		defer e.arpLock.Unlock()
		e.arpMap = arpMap
	}()

	arpMap[e.config.LBInterface] = []net.IP{e.config.ClusterVIP.IPv4Addr}
	if e.config.UseVMAC {
		// If using VMAC, only announce ClusterVIP is enough.
		return
	}

	e.clusterLock.RLock()
	cluster := e.cluster
	e.clusterLock.RUnlock()

	e.vlanLock.RLock()
	defer e.vlanLock.RUnlock()
	for _, vserver := range cluster.Vservers {
		for _, vip := range vserver.VIPs {
			if vip.Type == seesaw.AnycastVIP {
				continue
			}
			ip := vip.IP.IP()
			if ip.To4() == nil {
				// IPv6 address is not yet supported.
				continue
			}
			found := false
			for _, vlan := range e.vlans {
				ipNet := vlan.IPv4Net()
				if ipNet == nil {
					continue
				}
				if ipNet.Contains(ip) {
					ifName := fmt.Sprintf("%s.%d", e.config.LBInterface, vlan.ID)
					arpMap[ifName] = append(arpMap[ifName], ip)
					found = true
					break
				}
			}
			if !found {
				// Use LB interface if no vlan matches
				arpMap[e.config.LBInterface] = append(arpMap[e.config.LBInterface], ip)
			}
		}
	}
}

// shutdownVservers shuts down all running vservers.
func (e *Engine) shutdownVservers() {
	for _, v := range e.vservers {
		v.stop()
	}
	for name, v := range e.vservers {
		<-v.stopped
		delete(e.vservers, name)
	}
	e.vserverLock.Lock()
	e.vserverSnapshots = make(map[string]*seesaw.Vserver)
	e.vserverLock.Unlock()
}

// updateVLANs creates and destroys VLAN interfaces for the load balancer per
// the cluster configuration.
func (e *Engine) updateVLANs() {
	e.clusterLock.RLock()
	cluster := e.cluster
	e.clusterLock.RUnlock()

	add := make([]*seesaw.VLAN, 0)
	remove := make([]*seesaw.VLAN, 0)

	e.vlanLock.Lock()
	defer e.vlanLock.Unlock()

	for key, vlan := range e.vlans {
		if cluster.VLANs[key] == nil {
			remove = append(remove, vlan)
		} else if !vlan.Equal(cluster.VLANs[key]) {
			// TODO(angusc): This will break any VIPs that are currently configured
			// on the VLAN interface. Fix!
			remove = append(remove, vlan)
			add = append(add, cluster.VLANs[key])
		}
	}
	for key, vlan := range cluster.VLANs {
		if e.vlans[key] == nil {
			add = append(add, vlan)
		}
	}

	for _, vlan := range remove {
		log.Infof("Removing VLAN interface %v", vlan)
		if err := e.lbInterface.DeleteVLAN(vlan); err != nil {
			log.Fatalf("Failed to remove VLAN interface %v: %v", vlan, err)
		}
	}
	for _, vlan := range add {
		log.Infof("Adding VLAN interface %v", vlan)
		if err := e.lbInterface.AddVLAN(vlan); err != nil {
			log.Fatalf("Failed to add VLAN interface %v: %v", vlan, err)
		}
	}

	e.vlans = cluster.VLANs
}

// deleteVLANs removes all the VLAN interfaces that have been created by this
// engine.
func (e *Engine) deleteVLANs() {
	e.vlanLock.Lock()
	defer e.vlanLock.Unlock()

	for k, v := range e.vlans {
		if err := e.lbInterface.DeleteVLAN(v); err != nil {
			log.Fatalf("Failed to remove VLAN interface %v: %v", v, err)
		}
		delete(e.vlans, k)
	}
}

// handleOverride handles an incoming Override.
func (e *Engine) handleOverride(o seesaw.Override) {
	e.overrides[o.Target()] = o
	e.distributeOverride(o)
	if o.State() == seesaw.OverrideDefault {
		delete(e.overrides, o.Target())
	}
}

// distributeOverride distributes an Override to the appropriate vservers.
func (e *Engine) distributeOverride(o seesaw.Override) {
	// Send VserverOverrides and DestinationOverrides to the appropriate vserver.
	// Send BackendOverrides to all vservers.
	switch override := o.(type) {
	case *seesaw.VserverOverride:
		if vserver, ok := e.vservers[override.VserverName]; ok {
			vserver.queueOverride(o)
		}
	case *seesaw.DestinationOverride:
		if vserver, ok := e.vservers[override.VserverName]; ok {
			vserver.queueOverride(o)
		}
	case *seesaw.BackendOverride:
		for _, vserver := range e.vservers {
			vserver.queueOverride(o)
		}
	}
}

// becomeMaster performs the necessary actions for the Seesaw Engine to
// become the master node.
func (e *Engine) becomeMaster() {
	e.syncClient.disable()
	e.notifier.SetSource(config.SourceServer)

	if err := e.lbInterface.Up(); err != nil {
		log.Fatalf("Failed to bring LB interface up: %v", err)
	}
}

// becomeBackup performs the neccesary actions for the Seesaw Engine to
// stop being the master node and become the backup node.
func (e *Engine) becomeBackup() {
	e.syncClient.enable()
	e.notifier.SetSource(config.SourceServer)

	if err := e.lbInterface.Down(); err != nil {
		log.Fatalf("Failed to bring LB interface down: %v", err)
	}
}

// markAllocator handles the allocation of marks.
type markAllocator struct {
	lock  sync.RWMutex
	marks []uint32
}

// newMarkAllocator returns a mark allocator initialised with the specified
// base and size.
func newMarkAllocator(base, size int) *markAllocator {
	ma := &markAllocator{
		marks: make([]uint32, 0, size),
	}
	for i := 0; i < size; i++ {
		ma.put(uint32(base + i))
	}
	return ma
}

// get returns the next available mark from the mark allocator.
func (ma *markAllocator) get() (uint32, error) {
	ma.lock.Lock()
	defer ma.lock.Unlock()
	if len(ma.marks) == 0 {
		return 0, errors.New("allocator exhausted")
	}
	mark := ma.marks[0]
	ma.marks = ma.marks[1:]
	return mark, nil
}

// put returns the specified mark to the mark allocator.
func (ma *markAllocator) put(mark uint32) {
	ma.lock.Lock()
	defer ma.lock.Unlock()
	ma.marks = append(ma.marks, mark)
}
