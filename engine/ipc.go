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

package engine

import (
	"encoding/gob"
	"errors"
	"fmt"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/healthcheck"
	"github.com/google/seesaw/quagga"

	log "github.com/golang/glog"
)

func init() {
	gob.Register(&healthcheck.DNSChecker{})
	gob.Register(&healthcheck.HTTPChecker{})
	gob.Register(&healthcheck.PingChecker{})
	gob.Register(&healthcheck.TCPChecker{})
	gob.Register(&healthcheck.UDPChecker{})
}

// SeesawEngine provides the IPC interface to the Seesaw Engine.
type SeesawEngine struct {
	engine *Engine
}

func (s *SeesawEngine) trace(call string, ctx *ipc.Context) {
	log.V(2).Infof("SeesawEngine.%s called by %v", call, ctx)
}

// Failover requests the Seesaw Engine to relinquish master state.
func (s *SeesawEngine) Failover(ctx *ipc.Context, reply *int) error {
	s.trace("Failover", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	return s.engine.haManager.requestFailover(false)
}

// HAConfig returns the high-availability configuration for this node as
// determined by the engine.
func (s *SeesawEngine) HAConfig(ctx *ipc.Context, reply *seesaw.HAConfig) error {
	s.trace("HAConfig", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	c, err := s.engine.haConfig()
	if err != nil {
		return err
	}
	if reply != nil {
		reply.Copy(c)
	}
	return nil
}

// HAUpdate advises the Engine of the current high-availability status for
// the Seesaw HA component. The Seesaw Engine may also request a failover
// in response.
func (s *SeesawEngine) HAUpdate(args *ipc.HAStatus, failover *bool) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("HAUpdate", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	s.engine.setHAStatus(args.Status)
	if failover != nil {
		*failover = s.engine.haManager.failover()
	}
	return nil
}

// HAState advises the Engine of the current high-availability state as
// determined by the Seesaw HA component.
func (s *SeesawEngine) HAState(args *ipc.HAState, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("HAState", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	s.engine.setHAState(args.State)
	return nil
}

// HAStatus returns the current HA status from the Seesaw Engine.
func (s *SeesawEngine) HAStatus(ctx *ipc.Context, status *seesaw.HAStatus) error {
	s.trace("HAStatus", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if status != nil {
		*status = s.engine.haStatus()
	}
	return nil
}

// Healthchecks returns a list of currently configured healthchecks that
// should be performed by the Seesaw Healthcheck component.
func (s *SeesawEngine) Healthchecks(ctx *ipc.Context, reply *healthcheck.Checks) error {
	s.trace("Healthchecks", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	configs := s.engine.hcManager.configs()
	if reply != nil {
		reply.Configs = configs
	}
	return nil
}

// HealthState advises the Seesaw Engine of state transitions for a set of
// healthchecks that are being performed by the Seesaw Healthcheck component.
func (s *SeesawEngine) HealthState(args *healthcheck.HealthState, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("HealthState", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	for _, n := range args.Notifications {
		if err := s.engine.hcManager.healthState(n); err != nil {
			return err
		}
	}

	return nil
}

// ClusterStatus returns status information about this Seesaw Cluster.
func (s *SeesawEngine) ClusterStatus(ctx *ipc.Context, reply *seesaw.ClusterStatus) error {
	s.trace("ClusterStatus", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	s.engine.clusterLock.RLock()
	cluster := s.engine.cluster
	s.engine.clusterLock.RUnlock()

	if cluster == nil {
		return errors.New("no cluster configuration loaded")
	}

	reply.Version = seesaw.SeesawVersion
	reply.Site = cluster.Site
	reply.Nodes = make([]*seesaw.Node, 0, len(cluster.Nodes))
	for _, node := range cluster.Nodes {
		reply.Nodes = append(reply.Nodes, node.Clone())
	}
	return nil
}

// ConfigStatus returns status information about this Seesaw's current configuration.
func (s *SeesawEngine) ConfigStatus(ctx *ipc.Context, reply *seesaw.ConfigStatus) error {
	s.trace("ConfigStatus", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	s.engine.clusterLock.RLock()
	cluster := s.engine.cluster
	s.engine.clusterLock.RUnlock()

	if cluster == nil {
		return errors.New("no cluster configuration loaded")
	}

	cs := cluster.Status

	reply.LastUpdate = cs.LastUpdate
	reply.Attributes = make([]seesaw.ConfigMetadata, 0, len(cs.Attributes))
	for _, a := range cs.Attributes {
		reply.Attributes = append(reply.Attributes, a)
	}
	reply.Warnings = make([]string, 0, len(cs.Warnings))
	for _, warning := range cs.Warnings {
		reply.Warnings = append(reply.Warnings, warning)
	}
	return nil
}

// ConfigReload requests a configuration reload.
func (s *SeesawEngine) ConfigReload(ctx *ipc.Context, reply *int) error {
	s.trace("ConfigReload", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	return s.engine.notifier.Reload()
}

// ConfigSource requests the configuration source be changed to the specified
// source. The name of the original source is returned.
func (s *SeesawEngine) ConfigSource(args *ipc.ConfigSource, oldSource *string) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("ConfigSource", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if oldSource != nil {
		*oldSource = s.engine.notifier.Source().String()
	}
	newSource := args.Source
	if newSource == "" {
		return nil
	}
	source, err := config.SourceByName(newSource)
	if err != nil {
		return err
	}
	s.engine.notifier.SetSource(source)
	return nil
}

// BGPNeighbors returns a list of the BGP neighbors that we are peering with.
func (s *SeesawEngine) BGPNeighbors(ctx *ipc.Context, reply *quagga.Neighbors) error {
	s.trace("BGPNeighbors", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if reply == nil {
		return fmt.Errorf("Neighbors is nil")
	}
	s.engine.bgpManager.lock.RLock()
	reply.Neighbors = s.engine.bgpManager.neighbors
	s.engine.bgpManager.lock.RUnlock()
	return nil
}

// VLANs returns a list of VLANs configured for this cluster.
func (s *SeesawEngine) VLANs(ctx *ipc.Context, reply *seesaw.VLANs) error {
	s.trace("VLANs", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if reply == nil {
		return errors.New("VLANs is nil")
	}
	reply.VLANs = make([]*seesaw.VLAN, 0)
	s.engine.vlanLock.RLock()
	for _, vlan := range s.engine.vlans {
		reply.VLANs = append(reply.VLANs, vlan)
	}
	s.engine.vlanLock.RUnlock()
	return nil
}

// Vservers returns a list of currently configured vservers.
func (s *SeesawEngine) Vservers(ctx *ipc.Context, reply *seesaw.VserverMap) error {
	s.trace("Vservers", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if reply == nil {
		return fmt.Errorf("VserverMap is nil")
	}
	reply.Vservers = make(map[string]*seesaw.Vserver)
	s.engine.vserverLock.RLock()
	for name := range s.engine.vserverSnapshots {
		reply.Vservers[name] = s.engine.vserverSnapshots[name]
	}
	s.engine.vserverLock.RUnlock()
	return nil
}

// OverrideBackend passes a BackendOverride to the engine.
func (s *SeesawEngine) OverrideBackend(args *ipc.Override, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("OverrideBackend", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if args.Backend == nil {
		return errors.New("backend is nil")
	}
	s.engine.queueOverride(args.Backend)
	return nil
}

// OverrideDestination passes a DestinationOverride to the engine.
func (s *SeesawEngine) OverrideDestination(args *ipc.Override, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("OverrideDestination", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if args.Destination == nil {
		return errors.New("destination is nil")
	}
	s.engine.queueOverride(args.Destination)
	return nil
}

// OverrideVserver passes a VserverOverride to the engine.
func (s *SeesawEngine) OverrideVserver(args *ipc.Override, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("OverrideVserver", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	if args.Vserver == nil {
		return errors.New("vserver is nil")
	}
	s.engine.queueOverride(args.Vserver)
	return nil
}

// Backends returns a list of currently configured Backends.
func (s *SeesawEngine) Backends(ctx *ipc.Context, reply *int) error {
	s.trace("Backends", ctx)
	if ctx == nil {
		return errors.New("context is nil")
	}

	if !ctx.IsTrusted() {
		return errors.New("insufficient access")
	}

	// TODO(jsing): Implement this function.
	return fmt.Errorf("Unimplemented")
}
