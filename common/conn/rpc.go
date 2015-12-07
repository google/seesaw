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
	"crypto/tls"
	"fmt"
	"net/rpc"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/quagga"
)

func init() {
	RegisterEngineConn("rpc", newEngineRPC)
}

// engineRPC contains the structures necessary for communication with the
// Seesaw Engine via RPC.
type engineRPC struct {
	client *rpc.Client
	conn   *tls.Conn
	ctx    *ipc.Context
}

// newEngineRPC returns a new engine RPC interface.
func newEngineRPC(ctx *ipc.Context) EngineConn {
	return &engineRPC{ctx: ctx}
}

// Dial establishes a connection to the Seesaw Engine.
func (c *engineRPC) Dial(addr string) error {
	// TODO(jsing): Configure CA certificate chain and disable insecure
	// skip verify.
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}
	conn, err := tls.Dial("tcp", addr, tlsConfig)
	if err != nil {
		return fmt.Errorf("Dial failed: %v", err)
	}
	c.conn = conn
	c.client = rpc.NewClient(conn)
	c.ctx.Peer.Identity = fmt.Sprintf("tcp %s", c.conn.LocalAddr())
	return nil
}

// Close closes an existing connection to the Seesaw Engine.
func (c *engineRPC) Close() error {
	if c.client == nil {
		return fmt.Errorf("No client to close")
	}
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("Close failed: %v", err)
	}
	c.client = nil
	c.conn = nil
	return nil
}

// ClusterStatus requests the status of the Seesaw Cluster.
func (c *engineRPC) ClusterStatus() (*seesaw.ClusterStatus, error) {
	var cs seesaw.ClusterStatus
	if err := c.client.Call("SeesawECU.ClusterStatus", c.ctx, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// ConfigStatus requests the status of the Seesaw Cluster's configuration.
func (c *engineRPC) ConfigStatus() (*seesaw.ConfigStatus, error) {
	var cs seesaw.ConfigStatus
	if err := c.client.Call("SeesawECU.ConfigStatus", c.ctx, &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// HAStatus requests the HA status of the Seesaw Node.
func (c *engineRPC) HAStatus() (*seesaw.HAStatus, error) {
	var ha seesaw.HAStatus
	if err := c.client.Call("SeesawECU.HAStatus", c.ctx, &ha); err != nil {
		return nil, err
	}
	return &ha, nil
}

// ConfigSource requests the configuration source be changed to the
// specified source. An empty string results in the source remaining
// unchanged. The current configuration source is returned.
func (c *engineRPC) ConfigSource(source string) (string, error) {
	cs := &ipc.ConfigSource{c.ctx, source}
	if err := c.client.Call("SeesawECU.ConfigSource", cs, &source); err != nil {
		return "", err
	}
	return source, nil
}

// ConfigReload requests the configuration to be reloaded.
func (c *engineRPC) ConfigReload() error {
	return c.client.Call("SeesawECU.ConfigReload", c.ctx, nil)
}

// BGPNeighbors requests a list of all BGP neighbors that this seesaw is
// peering with.
func (c *engineRPC) BGPNeighbors() ([]*quagga.Neighbor, error) {
	var bn quagga.Neighbors
	if err := c.client.Call("SeesawECU.BGPNeighbors", c.ctx, &bn); err != nil {
		return nil, err
	}
	return bn.Neighbors, nil
}

// VLANs requests a list of VLANs configured on the cluster.
func (c *engineRPC) VLANs() (*seesaw.VLANs, error) {
	var v seesaw.VLANs
	if err := c.client.Call("SeesawEngine.VLANs", c.ctx, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// Vservers requests a list of all vservers that are configured on the cluster.
func (c *engineRPC) Vservers() (map[string]*seesaw.Vserver, error) {
	var vm seesaw.VserverMap
	if err := c.client.Call("SeesawECU.Vservers", c.ctx, &vm); err != nil {
		return nil, err
	}
	return vm.Vservers, nil
}

// Backends requests a list of all backends that are configured on the cluster.
func (c *engineRPC) Backends() (map[string]*seesaw.Backend, error) {
	var bm seesaw.BackendMap
	if err := c.client.Call("SeesawECU.Backends", c.ctx, &bm); err != nil {
		return nil, err
	}
	return bm.Backends, nil
}

// OverrideBackend requests that the specified VserverOverride be applied.
func (c *engineRPC) OverrideBackend(backend *seesaw.BackendOverride) error {
	override := &ipc.Override{Ctx: c.ctx, Backend: backend}
	return c.client.Call("SeesawECU.OverrideBackend", override, nil)
}

// OverrideDestination requests that the specified VserverOverride be applied.
func (c *engineRPC) OverrideDestination(destination *seesaw.DestinationOverride) error {
	override := &ipc.Override{Ctx: c.ctx, Destination: destination}
	return c.client.Call("SeesawECU.OverrideDestination", override, nil)
}

// OverrideVserver requests that the specified VserverOverride be applied.
func (c *engineRPC) OverrideVserver(vserver *seesaw.VserverOverride) error {
	override := &ipc.Override{Ctx: c.ctx, Vserver: vserver}
	return c.client.Call("SeesawECU.OverrideVserver", override, nil)
}

// Failover requests a failover between the Seesaw Nodes.
func (c *engineRPC) Failover() error {
	return c.client.Call("SeesawECU.Failover", c.ctx, nil)
}
