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

package config

// This file contains types used for Seesaw Engine configuration.

import (
	"fmt"
	"net"
	"reflect"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

// Cluster represents the configuration for a load balancing cluster.
type Cluster struct {
	Site         string
	VIP          seesaw.Host
	BGPLocalASN  uint32
	BGPRemoteASN uint32
	BGPPeers     map[string]*seesaw.Host // by Hostname
	Nodes        map[string]*seesaw.Node // by Node.Key()
	VIPSubnets   map[string]*net.IPNet   // by IPNet.String()
	VLANs        map[uint16]*seesaw.VLAN // by VLAN.Key()
	Vservers     map[string]*Vserver     // by Vserver.Key()
	Status       seesaw.ConfigStatus
}

// NewCluster returns an initialised Cluster structure.
func NewCluster(site string) *Cluster {
	return &Cluster{
		Site:       site,
		BGPPeers:   make(map[string]*seesaw.Host),
		Nodes:      make(map[string]*seesaw.Node),
		VIPSubnets: make(map[string]*net.IPNet),
		Vservers:   make(map[string]*Vserver),
		VLANs:      make(map[uint16]*seesaw.VLAN),
		Status: seesaw.ConfigStatus{
			Warnings:   make([]string, 0),
			Attributes: make([]seesaw.ConfigMetadata, 0),
		},
	}
}

// AddBGPPeer adds a BGP peer to a Seesaw Cluster.
func (c *Cluster) AddBGPPeer(peer *seesaw.Host) error {
	key := peer.Hostname
	if _, ok := c.BGPPeers[key]; ok {
		return fmt.Errorf("Cluster %q already contains peer %q", c.Site, key)
	}
	c.BGPPeers[key] = peer
	return nil
}

// AddNode adds a Seesaw Node to a Seesaw Cluster.
func (c *Cluster) AddNode(node *seesaw.Node) error {
	key := node.Key()
	if _, ok := c.Nodes[key]; ok {
		return fmt.Errorf("Cluster %q already contains Node %q", c.Site, key)
	}
	c.Nodes[key] = node
	return nil
}

// AddVIPSubnet adds a VIP Subnet to a Seesaw Cluster.
func (c *Cluster) AddVIPSubnet(subnet *net.IPNet) error {
	key := subnet.String()
	if _, ok := c.VIPSubnets[key]; ok {
		return fmt.Errorf("Cluster %q already contains VIP Subnet %q", c.Site, key)
	}
	c.VIPSubnets[key] = subnet
	return nil
}

// AddVserver adds a Vserver to a Seesaw Cluster.
func (c *Cluster) AddVserver(vserver *Vserver) error {
	key := vserver.Key()
	if _, ok := c.Vservers[key]; ok {
		return fmt.Errorf("Cluster %q already contains Vserver %q", c.Site, key)
	}
	c.Vservers[key] = vserver
	return nil
}

// AddVLAN adds a VLAN to a Seesaw Cluster.
func (c *Cluster) AddVLAN(vlan *seesaw.VLAN) error {
	key := vlan.Key()
	if _, ok := c.VLANs[key]; ok {
		return fmt.Errorf("Cluster %q already contains VLAN %q", c.Site, key)
	}
	c.VLANs[key] = vlan
	return nil
}

// Equal reports whether this cluster is equal to the given cluster.
func (c *Cluster) Equal(other *Cluster) bool {
	return reflect.DeepEqual(c, other)
}

// Vserver represents the configuration for a virtual server.
type Vserver struct {
	Name string
	seesaw.Host
	Entries      map[string]*VserverEntry   // by VserverEntry.Key()
	Backends     map[string]*seesaw.Backend // by Backend.Key()
	Healthchecks map[string]*Healthcheck    // by Healthcheck.Key()
	VIPs         map[string]*seesaw.VIP     // by VIP.IP.String()
	Enabled      bool
	UseFWM       bool
	Warnings     []string
}

// NewVserver creates a new, initialised Vserver structure.
func NewVserver(name string, host seesaw.Host) *Vserver {
	return &Vserver{
		Name:         name,
		Host:         host,
		Entries:      make(map[string]*VserverEntry),
		Backends:     make(map[string]*seesaw.Backend),
		Healthchecks: make(map[string]*Healthcheck),
		VIPs:         make(map[string]*seesaw.VIP),
		Warnings:     make([]string, 0),
	}
}

// Key returns the unique identifier for a Vserver.
func (v *Vserver) Key() string {
	return v.Name
}

// AddVserverEntry adds an VserverEntry to a Vserver.
func (v *Vserver) AddVserverEntry(e *VserverEntry) error {
	key := e.Key()
	if _, ok := v.Entries[key]; ok {
		return fmt.Errorf("Vserver %q already contains VserverEntry %q", v.Name, key)
	}
	v.Entries[key] = e
	return nil
}

// AddBackend adds a Backend to a Vserver.
func (v *Vserver) AddBackend(backend *seesaw.Backend) error {
	key := backend.Key()
	if _, ok := v.Backends[key]; ok {
		return fmt.Errorf("Vserver %q already contains Backend %q", v.Name, key)
	}
	v.Backends[key] = backend
	return nil
}

// AddHealthcheck adds a Healthcheck to a Vserver.
func (v *Vserver) AddHealthcheck(h *Healthcheck) error {
	key := h.Key()
	if _, ok := v.Healthchecks[key]; ok {
		return fmt.Errorf("Vserver %q already contains Healthcheck %q", v.Name, key)
	}
	v.Healthchecks[key] = h
	return nil
}

// AddVIP adds a VIP to a Vserver.
func (v *Vserver) AddVIP(vip *seesaw.VIP) error {
	key := vip.String()
	if _, ok := v.VIPs[key]; ok {
		return fmt.Errorf("Vserver %q already contains VIP %q", v.Name, key)
	}
	v.VIPs[key] = vip
	return nil
}

// VserverEntry specifies the configuration for a port and protocol combination
// for a Vserver.
type VserverEntry struct {
	Port          uint16
	Proto         seesaw.IPProto
	Scheduler     seesaw.LBScheduler
	Mode          seesaw.LBMode
	Persistence   int
	OnePacket     bool
	HighWatermark float32
	LowWatermark  float32
	LThreshold    int
	UThreshold    int
	Healthchecks  map[string]*Healthcheck // by Healthcheck.Key()
}

// NewVserverEntry creates a new, initialised VserverEntry structure.
func NewVserverEntry(port uint16, proto seesaw.IPProto) *VserverEntry {
	return &VserverEntry{
		Port:         port,
		Proto:        proto,
		Healthchecks: make(map[string]*Healthcheck),
	}
}

// AddHealthcheck adds a Healthcheck to a VserverEntry.
func (v *VserverEntry) AddHealthcheck(h *Healthcheck) error {
	key := h.Key()
	if _, ok := v.Healthchecks[key]; ok {
		return fmt.Errorf("VserverEntry %q already contains Healthcheck %q", v.Key(), key)
	}
	v.Healthchecks[key] = h
	return nil
}

// Key returns the unique identifier for a VserverEntry.
func (v *VserverEntry) Key() string {
	return fmt.Sprintf("%d/%s", v.Port, v.Proto)
}

// Snapshot returns a snapshot for a VserverEntry.
func (v *VserverEntry) Snapshot() *seesaw.VserverEntry {
	return &seesaw.VserverEntry{
		Port:          v.Port,
		Proto:         v.Proto,
		Scheduler:     v.Scheduler,
		Mode:          v.Mode,
		Persistence:   v.Persistence,
		OnePacket:     v.OnePacket,
		HighWatermark: v.HighWatermark,
		LowWatermark:  v.LowWatermark,
		LThreshold:    v.LThreshold,
		UThreshold:    v.UThreshold,
	}
}

// Healthcheck represents a healthcheck that needs to be run against a
// Backend or Destination.
type Healthcheck struct {
	Name      string
	Mode      seesaw.HealthcheckMode
	Type      seesaw.HealthcheckType
	Port      uint16        // The backend port to connect to.
	Interval  time.Duration // How frequently this healthcheck is executed.
	Timeout   time.Duration // The execution timeout.
	Retries   int           // Number of times to retry a healthcheck.
	Send      string        // The request to be sent to the backend.
	Receive   string        // The expected response from the backend.
	Code      int           // The expected response code from the backend.
	Proxy     bool          // Perform healthchecks against an HTTP proxy.
	Method    string        // The request method for an HTTP/S healthcheck.
	TLSVerify bool          // Do TLS verification.
}

// NewHealthcheck creates a new, initialised Healthcheck structure.
func NewHealthcheck(m seesaw.HealthcheckMode, t seesaw.HealthcheckType, port uint16) *Healthcheck {
	return &Healthcheck{
		Mode: m,
		Type: t,
		Port: port,
	}
}

// Key returns the unique identifier for a Healthcheck.
func (h *Healthcheck) Key() string {
	return h.Name
}

// Healthchecks is a list of Healthchecks.
type Healthchecks []*Healthcheck

func (h Healthchecks) Len() int      { return len(h) }
func (h Healthchecks) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h Healthchecks) Less(i, j int) bool {
	if h[i].Type != h[j].Type {
		return h[i].Type < h[j].Type
	}

	if h[i].Port != h[j].Port {
		return h[i].Port < h[j].Port
	}

	if h[i].Send != h[j].Send {
		return h[i].Send < h[j].Send
	}

	if h[i].Receive != h[j].Receive {
		return h[i].Receive < h[j].Receive
	}

	if h[i].Method != h[j].Method {
		return h[i].Method < h[j].Method
	}

	if h[i].Code != h[j].Code {
		return h[i].Code < h[j].Code
	}

	if h[i].Proxy != h[j].Proxy {
		// false < true
		return h[j].Proxy
	}

	if h[i].TLSVerify != h[j].TLSVerify {
		// false < true
		return h[j].TLSVerify
	}

	if h[i].Interval != h[j].Interval {
		return h[i].Interval < h[j].Interval
	}

	if h[i].Timeout != h[j].Timeout {
		return h[i].Timeout < h[j].Timeout
	}

	return false
}
