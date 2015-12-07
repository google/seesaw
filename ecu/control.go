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

package ecu

// This file contains types and functions that implement RPC calls to the
// Seesaw v2 ECU.

import (
	"errors"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/quagga"

	log "github.com/golang/glog"
)

// SeesawECU provides the RPC interface to the Seesaw ECU.
type SeesawECU struct {
	ecu *ECU
}

func (s *SeesawECU) trace(call string, ctx *ipc.Context) {
	log.V(2).Infof("SeesawECU.%s called by %v", call, ctx)
}

// Failover requests the Seesaw Engine to relinquish master state.
func (s *SeesawECU) Failover(ctx *ipc.Context, reply *int) error {
	s.trace("Failover", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	if err := authConn.Failover(); err != nil {
		return err
	}
	return nil
}

// ClusterStatus returns status information about this Seesaw Cluster.
func (s *SeesawECU) ClusterStatus(ctx *ipc.Context, reply *seesaw.ClusterStatus) error {
	s.trace("ClusterStatus", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	cs, err := authConn.ClusterStatus()
	if err != nil {
		return err
	}

	if reply != nil {
		*reply = *cs
	}
	return nil
}

// HAStatus returns the current HA status from the Seesaw Engine.
func (s *SeesawECU) HAStatus(ctx *ipc.Context, status *seesaw.HAStatus) error {
	s.trace("HAStatus", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	hs, err := authConn.HAStatus()
	if err != nil {
		return err
	}

	if status != nil {
		*status = *hs
	}
	return nil
}

// ConfigStatus returns status information about this Seesaw's current configuration.
func (s *SeesawECU) ConfigStatus(ctx *ipc.Context, reply *seesaw.ConfigStatus) error {
	s.trace("ConfigStatus", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	cs, err := authConn.ConfigStatus()
	if err != nil {
		return err
	}

	if reply != nil {
		*reply = *cs
	}
	return nil
}

// ConfigReload requests a configuration reload.
func (s *SeesawECU) ConfigReload(ctx *ipc.Context, reply *int) error {
	s.trace("ConfigReload", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	if err := authConn.ConfigReload(); err != nil {
		return err
	}
	return nil
}

// ConfigSource requests the configuration source be changed to the specified
// source. The name of the original source is returned.
func (s *SeesawECU) ConfigSource(args *ipc.ConfigSource, oldSource *string) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("ConfigSource", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	source, err := authConn.ConfigSource(args.Source)
	if err != nil {
		return err
	}

	if oldSource != nil {
		*oldSource = source
	}
	return nil
}

// BGPNeighbors returns a list of the BGP neighbors that we are peering with.
func (s *SeesawECU) BGPNeighbors(ctx *ipc.Context, reply *quagga.Neighbors) error {
	s.trace("BGPNeighbors", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	neighbors, err := authConn.BGPNeighbors()
	if err != nil {
		return err
	}

	if reply != nil {
		reply.Neighbors = neighbors
	}
	return nil
}

// VLANs returns a list of currently configured VLANs.
func (s *SeesawECU) VLANs(ctx *ipc.Context, reply *seesaw.VLANs) error {
	s.trace("VLANs", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	vlans, err := authConn.VLANs()
	if err != nil {
		return err
	}

	if reply != nil {
		*reply = *vlans
	}
	return nil
}

// Vservers returns a list of currently configured vservers.
func (s *SeesawECU) Vservers(ctx *ipc.Context, reply *seesaw.VserverMap) error {
	s.trace("Vservers", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	vservers, err := authConn.Vservers()
	if err != nil {
		return err
	}

	if reply != nil {
		reply.Vservers = vservers
	}
	return nil
}

// Backends returns a list of currently configured Backends.
func (s *SeesawECU) Backends(ctx *ipc.Context, reply *int) error {
	s.trace("Backends", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	backends, err := authConn.Backends()
	if err != nil {
		return err
	}

	if reply != nil {
		_ = backends
	}
	return nil
}

// OverrideBackend requests that the specified BackendOverride be applied.
func (s *SeesawECU) OverrideBackend(args *ipc.Override, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("OverrideBackend", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	if args.Backend == nil {
		return errors.New("backend override is nil")
	}
	return authConn.OverrideBackend(args.Backend)
}

// OverrideDestination requests that the specified DestinationOverride be applied.
func (s *SeesawECU) OverrideDestination(args *ipc.Override, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("OverrideDestination", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	if args.Destination == nil {
		return errors.New("destination override is nil")
	}
	return authConn.OverrideDestination(args.Destination)
}

// OverrideVserver requests that the specified VserverOverride be applied.
func (s *SeesawECU) OverrideVserver(args *ipc.Override, reply *int) error {
	if args == nil {
		return errors.New("args is nil")
	}
	ctx := args.Ctx
	s.trace("OverrideVserver", ctx)

	authConn, err := s.ecu.authConnect(ctx)
	if err != nil {
		return err
	}
	defer authConn.Close()

	if args.Vserver == nil {
		return errors.New("vserver override is nil")
	}
	return authConn.OverrideVserver(args.Vserver)
}
