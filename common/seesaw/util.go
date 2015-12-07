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

package seesaw

import (
	"bytes"
	"fmt"
	"net"
	"path"
	"reflect"
)

// TODO(jsing): These should be configurable.
var _, netIPv4Anycast, _ = net.ParseCIDR("192.168.255.0/24")
var _, netIPv6Anycast, _ = net.ParseCIDR("2015:cafe:ffff::/64")

var testAnycastHost = Host{
	Hostname: "test-anycast.example.com",
	IPv4Addr: net.ParseIP("192.168.255.254"),
	IPv6Addr: net.ParseIP("2015:cafe:ffff::254"),
}

// Copy deep copies from the given Seesaw Node.
func (n *Node) Copy(c *Node) {
	n.State = c.State
	n.Priority = c.Priority
	n.Host.Copy(&c.Host)
	n.VserversEnabled = c.VserversEnabled
}

// Clone creates an identical copy of the given Seesaw Node.
func (n *Node) Clone() *Node {
	var c Node
	c.Copy(n)
	return &c
}

// Key returns the unique identifier for a Seesaw Node.
func (n *Node) Key() string {
	return n.Hostname
}

// NodesByIPv4 allows sorting Nodes by IPv4Addr.
type NodesByIPv4 struct{ Nodes }

// NodesByIPv6 allows sorting Nodes by IPv6Addr.
type NodesByIPv6 struct{ Nodes }

func (n Nodes) Len() int      { return len(n) }
func (n Nodes) Swap(i, j int) { n[i], n[j] = n[j], n[i] }

func (n NodesByIPv4) Less(i, j int) bool {
	return bytes.Compare(n.Nodes[i].IPv4Addr, n.Nodes[j].IPv4Addr) < 0
}

func (n NodesByIPv6) Less(i, j int) bool {
	return bytes.Compare(n.Nodes[i].IPv6Addr, n.Nodes[j].IPv6Addr) < 0
}

// Copy deep copies from the given Seesaw Host.
func (h *Host) Copy(c *Host) {
	h.Hostname = c.Hostname
	h.IPv4Addr = copyIP(c.IPv4Addr)
	h.IPv4Mask = copyIPMask(c.IPv4Mask)
	h.IPv6Addr = copyIP(c.IPv6Addr)
	h.IPv6Mask = copyIPMask(c.IPv6Mask)
}

// Clone creates an identical copy of the given Seesaw Host.
func (h *Host) Clone() *Host {
	var c Host
	c.Copy(h)
	return &c
}

// Equal returns whether two hosts are equal.
func (h *Host) Equal(other *Host) bool {
	return reflect.DeepEqual(h, other)
}

// IPv4Net returns a net.IPNet for the host's IPv4 address.
func (h *Host) IPv4Net() *net.IPNet {
	return &net.IPNet{IP: h.IPv4Addr, Mask: h.IPv4Mask}
}

// IPv6Net returns a net.IPNet for the host's IPv6 address.
func (h *Host) IPv6Net() *net.IPNet {
	return &net.IPNet{IP: h.IPv6Addr, Mask: h.IPv6Mask}
}

// IPv4Printable returns a printable representation of the host's IPv4 address.
func (h *Host) IPv4Printable() string {
	if h.IPv4Addr == nil {
		return "<not configured>"
	}
	return h.IPv4Net().String()
}

// IPv6Printable returns a printable representation of the host's IPv4 address.
func (h *Host) IPv6Printable() string {
	if h.IPv6Addr == nil {
		return "<not configured>"
	}
	return h.IPv6Net().String()
}

// Copy deep copies from the given Seesaw Backend.
func (b *Backend) Copy(c *Backend) {
	b.Enabled = c.Enabled
	b.InService = c.InService
	b.Weight = c.Weight
	b.Host.Copy(&c.Host)
}

// Clone creates an identical copy of the given Seesaw Backend.
func (b *Backend) Clone() *Backend {
	var c Backend
	c.Copy(b)
	return &c
}

// Key returns the unique identifier for a Seesaw Backend.
func (b *Backend) Key() string {
	return b.Hostname
}

// Copy deep copies from the given Seesaw HAConfig.
func (h *HAConfig) Copy(c *HAConfig) {
	h.Enabled = c.Enabled
	h.LocalAddr = copyIP(c.LocalAddr)
	h.RemoteAddr = copyIP(c.RemoteAddr)
	h.Priority = c.Priority
	h.VRID = c.VRID
}

// Clone creates an identical copy of the given Seesaw HAConfig.
func (h *HAConfig) Clone() *HAConfig {
	var c HAConfig
	c.Copy(h)
	return &c
}

// Equal reports whether this HAConfig is equal to the given HAConfig.
func (h *HAConfig) Equal(other *HAConfig) bool {
	return h.Enabled == other.Enabled &&
		h.LocalAddr.Equal(other.LocalAddr) &&
		h.RemoteAddr.Equal(other.RemoteAddr) &&
		h.Priority == other.Priority &&
		h.VRID == other.VRID
}

// String returns the string representation of an HAConfig.
func (h HAConfig) String() string {
	return fmt.Sprintf("Enabled: %v, LocalAddr: %v, RemoteAddr: %v, Priority: %d, VRID: %d",
		h.Enabled, h.LocalAddr, h.RemoteAddr, h.Priority, h.VRID)
}

// String returns the string representation of an HAState.
func (h HAState) String() string {
	switch h {
	case HAUnknown:
		return "Unknown"
	case HABackup:
		return "Backup"
	case HADisabled:
		return "Disabled"
	case HAError:
		return "Error"
	case HAMaster:
		return "Master"
	case HAShutdown:
		return "Shutdown"
	}
	return "(invalid)"
}

// Equal reports whether this VLAN is equal to the given VLAN.
func (v *VLAN) Equal(other *VLAN) bool {
	// Exclude backend and VIP counters from comparison.
	return v.ID == other.ID && v.Host.Equal(&other.Host)
}

// Key returns the unique identifier for a VLAN.
func (v *VLAN) Key() uint16 {
	return v.ID
}

// String returns the string representation of a VLAN.
func (v *VLAN) String() string {
	return fmt.Sprintf("{ID=%d, Host=%s, IPv4=%s, IPv6=%s}", v.ID, v.Hostname, v.IPv4Printable(), v.IPv6Printable())
}

func (v VLANs) Len() int {
	return len(v.VLANs)
}
func (v VLANs) Swap(i, j int) {
	v.VLANs[i], v.VLANs[j] = v.VLANs[j], v.VLANs[i]
}
func (v VLANs) Less(i, j int) bool {
	return v.VLANs[i].ID < v.VLANs[j].ID
}

// String returns the string representation of a ServiceKey.
func (sk ServiceKey) String() string {
	return fmt.Sprintf("%s %s %d", sk.AF, sk.Proto, sk.Port)
}

func (sk ServiceKeys) Len() int      { return len(sk) }
func (sk ServiceKeys) Swap(i, j int) { sk[i], sk[j] = sk[j], sk[i] }
func (sk ServiceKeys) Less(i, j int) bool {
	if sk[i].AF != sk[j].AF {
		return sk[i].AF < sk[j].AF
	}
	if sk[i].Proto != sk[j].Proto {
		return sk[i].Proto < sk[j].Proto
	}
	return sk[i].Port < sk[j].Port
}

func (o *VserverOverride) Target() string       { return o.VserverName }
func (o *VserverOverride) State() OverrideState { return o.OverrideState }

func (o *BackendOverride) Target() string       { return o.Hostname }
func (o *BackendOverride) State() OverrideState { return o.OverrideState }

func (o *DestinationOverride) Target() string       { return o.DestinationName }
func (o *DestinationOverride) State() OverrideState { return o.OverrideState }

// IP returns the destination IP address for a given address family.
func (d *Destination) IP(af AF) net.IP {
	switch af {
	case IPv4:
		return d.Backend.IPv4Addr
	case IPv6:
		return d.Backend.IPv6Addr
	}
	return nil
}

func (d Destinations) Len() int      { return len(d) }
func (d Destinations) Swap(i, j int) { d[i], d[j] = d[j], d[i] }

func (d Destinations) Less(i, j int) bool {
	return d[i].Name < d[j].Name
}

// copyIP creates a copy of an IP.
func copyIP(src net.IP) net.IP {
	return net.IP(copyBytes(src))
}

// copyIPMask creates a copy of an IPMask.
func copyIPMask(src net.IPMask) net.IPMask {
	return net.IPMask(copyBytes(src))
}

// copyBytes creates a copy of a byte slice.
func copyBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// IsAnycast reports whether an IP address is an anycast address.
func IsAnycast(ip net.IP) bool {
	return netIPv4Anycast.Contains(ip) || netIPv6Anycast.Contains(ip)
}

// InSubnets reports whether an IP address is in one of a map of subnets.
func InSubnets(ip net.IP, subnets map[string]*net.IPNet) bool {
	for _, subnet := range subnets {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

// TestAnycastHost returns a Host containing the details of the
// test anycast service.
func TestAnycastHost() Host {
	return testAnycastHost
}

// socketPath returns the path of a socket for the given Seesaw v2 component.
func socketPath(component string) string {
	return path.Join(RunPath, component, component+".sock")
}
