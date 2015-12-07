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

package conn

import (
	"fmt"
	"net/rpc"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/quagga"
)

func init() {
	RegisterEngineConn("ipc", newEngineIPC)
}

// engineIPC contains the structures necessary for communication with the
// Seesaw Engine via IPC.
type engineIPC struct {
	client *rpc.Client
	ctx    *ipc.Context
}

// newEngineIPC returns a new engine IPC interface.
func newEngineIPC(ctx *ipc.Context) EngineConn {
	return &engineIPC{ctx: ctx}
}

// Dial establishes a connection to the Seesaw Engine.
func (c *engineIPC) Dial(addr string) error {
	client, err := rpc.Dial("unix", addr)
	if err != nil {
		return fmt.Errorf("Dial failed: %v", err)
	}
	c.client = client
	return nil
}

// Close closes an existing connection to the Seesaw Engine.
func (c *engineIPC) Close() error {
	if c.client == nil {
		return fmt.Errorf("No client to close")
	}
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("Close failed: %v", err)
	}
	c.client = nil
	return nil
}

// ClusterStatus requests the status of the Seesaw Cluster.
func (c *engineIPC) ClusterStatus() (*seesaw.ClusterStatus, error) {
	var cs seesaw.ClusterStatus
	if err := c.client.Call("SeesawEngine.ClusterStatus", c.ctx, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// ConfigStatus requests the status of the Seesaw Cluster's configuration.
func (c *engineIPC) ConfigStatus() (*seesaw.ConfigStatus, error) {
	var cs seesaw.ConfigStatus
	if err := c.client.Call("SeesawEngine.ConfigStatus", c.ctx, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// HAStatus requests the HA status of the Seesaw Node.
func (c *engineIPC) HAStatus() (*seesaw.HAStatus, error) {
	var ha seesaw.HAStatus
	if err := c.client.Call("SeesawEngine.HAStatus", c.ctx, &ha); err != nil {
		return nil, err
	}
	return &ha, nil
}

// ConfigSource requests the configuration source be changed to the
// specified source. An empty string results in the source remaining
// unchanged. The current configuration source is returned.
func (c *engineIPC) ConfigSource(source string) (string, error) {
	cs := &ipc.ConfigSource{c.ctx, source}
	if err := c.client.Call("SeesawEngine.ConfigSource", cs, &source); err != nil {
		return "", err
	}
	return source, nil
}

// ConfigReload requests the configuration to be reloaded.
func (c *engineIPC) ConfigReload() error {
	return c.client.Call("SeesawEngine.ConfigReload", c.ctx, nil)
}

// BGPNeighbors requests a list of all BGP neighbors that this seesaw is
// peering with.
func (c *engineIPC) BGPNeighbors() ([]*quagga.Neighbor, error) {
	var bn quagga.Neighbors
	if err := c.client.Call("SeesawEngine.BGPNeighbors", c.ctx, &bn); err != nil {
		return nil, err
	}
	return bn.Neighbors, nil
}

// VLANs requests a list of VLANs configured on the cluster.
func (c *engineIPC) VLANs() (*seesaw.VLANs, error) {
	var v seesaw.VLANs
	if err := c.client.Call("SeesawEngine.VLANs", c.ctx, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Vservers requests a list of all vservers that are configured on the cluster.
func (c *engineIPC) Vservers() (map[string]*seesaw.Vserver, error) {
	var vm seesaw.VserverMap
	if err := c.client.Call("SeesawEngine.Vservers", c.ctx, &vm); err != nil {
		return nil, err
	}
	return vm.Vservers, nil
}

// Backends requests a list of all backends that are configured on the cluster.
func (c *engineIPC) Backends() (map[string]*seesaw.Backend, error) {
	var bm seesaw.BackendMap
	if err := c.client.Call("SeesawEngine.Backends", c.ctx, &bm); err != nil {
		return nil, err
	}
	return bm.Backends, nil
}

// OverrideBackend requests that the specified BackendOverride be applied.
func (c *engineIPC) OverrideBackend(backend *seesaw.BackendOverride) error {
	override := &ipc.Override{Ctx: c.ctx, Backend: backend}
	return c.client.Call("SeesawEngine.OverrideDestination", override, nil)
}

// OverrideDestination requests that the specified DestinationOverride be applied.
func (c *engineIPC) OverrideDestination(destination *seesaw.DestinationOverride) error {
	override := &ipc.Override{Ctx: c.ctx, Destination: destination}
	return c.client.Call("SeesawEngine.OverrideDestination", override, nil)
}

// OverrideVserver requests that the specified VserverOverride be applied.
func (c *engineIPC) OverrideVserver(vserver *seesaw.VserverOverride) error {
	override := &ipc.Override{Ctx: c.ctx, Vserver: vserver}
	return c.client.Call("SeesawEngine.OverrideVserver", override, nil)
}

// Failover requests a failover between the Seesaw Nodes.
func (c *engineIPC) Failover() error {
	return c.client.Call("SeesawEngine.Failover", c.ctx, nil)
}
