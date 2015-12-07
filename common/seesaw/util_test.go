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
	"fmt"
	"net"
	"reflect"
	"sort"
	"testing"
)

func newTestHost(offset int, shortname string, ipv4, ipv6 bool) Host {
	var h Host
	if shortname != "" {
		h.Hostname = fmt.Sprintf("%s.example.com", shortname)
	}
	if ipv4 {
		h.IPv4Addr = net.ParseIP(fmt.Sprintf("1.2.3.%d", 4+offset))
		h.IPv4Mask = net.CIDRMask(24, 32)
	}
	if ipv6 {
		h.IPv6Addr = net.ParseIP(fmt.Sprintf("2012::%x", 0xcafe+offset))
		h.IPv6Mask = net.CIDRMask(64, 128)
	}
	return h
}

var hosts = []Host{
	newTestHost(0, "", false, false),
	newTestHost(0, "myhost1", true, false),
	newTestHost(0, "myhost2", false, true),
	newTestHost(0, "myhost3", true, true),
}

func TestHostClone(t *testing.T) {
	for _, h := range hosts {
		c := h.Clone()
		if !reflect.DeepEqual(h, *c) {
			t.Errorf("Clone failed for host - want %#v, got %#v", h, *c)
		}
	}
}

func TestHostCopy(t *testing.T) {
	for _, h := range hosts {
		var c Host
		c.Copy(&h)
		if !reflect.DeepEqual(h, c) {
			t.Errorf("Copy failed for host - want %#v, got %#v", h, c)
		}
	}
}

var nodes = []Node{
	{},
	{
		Host:            newTestHost(0, "seesaw1-1", true, true),
		Priority:        255,
		State:           HAMaster,
		VserversEnabled: true,
	},
	{
		Host:            newTestHost(1, "seesaw1-2", true, true),
		Priority:        1,
		State:           HABackup,
		VserversEnabled: true,
	},
}

func TestNodeClone(t *testing.T) {
	for _, n := range nodes {
		c := n.Clone()
		if !reflect.DeepEqual(n, *c) {
			t.Errorf("Clone failed for node - want %#v, got %#v", n, *c)
		}
	}
}

func TestNodeCopy(t *testing.T) {
	for _, n := range nodes {
		var c Node
		c.Copy(&n)
		if !reflect.DeepEqual(n, c) {
			t.Errorf("Copy failed for node - want %#v, got %#v", n, c)
		}
	}
}

var backends = []Backend{
	{},
	{
		newTestHost(0, "backend1", true, true),
		1,
		true,
		false,
	},
	{
		newTestHost(1, "backend2", true, true),
		2,
		false,
		false,
	},
}

func TestBackendClone(t *testing.T) {
	for _, b := range backends {
		c := b.Clone()
		if !reflect.DeepEqual(b, *c) {
			t.Errorf("Clone failed for backend - want %#v, got %#v", b, *c)
		}
	}
}

func TestBackendCopy(t *testing.T) {
	for _, b := range backends {
		var c Backend
		c.Copy(&b)
		if !reflect.DeepEqual(b, c) {
			t.Errorf("Copy failed for backend - want %#v, got %#v", b, c)
		}
	}
}

var haConfigs = []HAConfig{
	{},
	{
		Enabled:    true,
		LocalAddr:  net.ParseIP("1.2.3.4"),
		RemoteAddr: net.ParseIP("2012::cafe"),
		Priority:   200,
		VRID:       99,
	},
}

func TestHAConfigClone(t *testing.T) {
	for _, ha := range haConfigs {
		c := ha.Clone()
		if !ha.Equal(c) {
			t.Errorf("Clone, Equal failed for HAConfig - want %v, got %v", ha, *c)
		}
		if !reflect.DeepEqual(ha, *c) {
			t.Errorf("Clone, DeepEqual failed for HAConfig - want %#v, got %#v", ha, *c)
		}
	}
}

func TestHAConfigCopy(t *testing.T) {
	for _, ha := range haConfigs {
		var c HAConfig
		c.Copy(&ha)
		if !ha.Equal(&c) {
			t.Errorf("Copy, Equal failed for HAConfig - want %v, got %v", ha, c)
		}
		if !reflect.DeepEqual(ha, c) {
			t.Errorf("Copy, DeepEqual failed for HAConfig - want %#v, got %#v", ha, c)
		}
	}
}

var vlanTests = []struct {
	desc  string
	vlanA *VLAN
	vlanB *VLAN
	equal bool
}{
	{
		"default",
		&VLAN{},
		&VLAN{},
		true,
	},
	{
		"identical",
		&VLAN{
			ID:   123,
			Host: newTestHost(0, "seesaw-vlan123", true, true),
		},
		&VLAN{
			ID:   123,
			Host: newTestHost(0, "seesaw-vlan123", true, true),
		},
		true,
	},
	{
		"ID differs",
		&VLAN{
			ID:   123,
			Host: newTestHost(0, "seesaw-vlan123", true, true),
		},
		&VLAN{
			ID:   124,
			Host: newTestHost(0, "seesaw-vlan123", true, true),
		},
		false,
	},
	{
		"host differs",
		&VLAN{
			ID:   123,
			Host: newTestHost(0, "seesaw-vlan123", true, true),
		},
		&VLAN{
			ID:   123,
			Host: newTestHost(1, "seesaw-vlan123", true, true),
		},
		false,
	},
	{
		"counts differ",
		&VLAN{
			ID:           123,
			Host:         newTestHost(0, "seesaw-vlan123", true, true),
			BackendCount: map[AF]uint{IPv4: 1, IPv6: 2},
			VIPCount:     map[AF]uint{IPv4: 1, IPv6: 2},
		},
		&VLAN{
			ID:           123,
			Host:         newTestHost(0, "seesaw-vlan123", true, true),
			BackendCount: map[AF]uint{IPv4: 2, IPv6: 3},
			VIPCount:     map[AF]uint{IPv4: 2, IPv6: 3},
		},
		true,
	},
}

func TestVLANEqual(t *testing.T) {
	for _, test := range vlanTests {
		if got, want := test.vlanA.Equal(test.vlanB), test.equal; got != want {
			t.Errorf("VLAN %q: Equal = %v, want %v", test.desc, got, want)
		}
	}
}

var anycastTests = []struct {
	ip      string
	anycast bool
}{
	{"192.168.23.1", false},
	{"192.168.255.1", true},
	{"192.168.255.255", true},
	{"2012::cafe", false},
	{"2015:cafe:ffff::1", true},
	{"2015:cafe:ffff::ffff:ffff:ffff:ffff", true},
}

func TestIsAnycast(t *testing.T) {
	for _, test := range anycastTests {
		ip := net.ParseIP(test.ip)
		if ip == nil {
			t.Errorf("Failed to parse IP address %q", test.ip)
			continue
		}
		if IsAnycast(ip) == test.anycast {
			continue
		}
		if test.anycast {
			t.Errorf("Expected %s to be an anycast address", ip)
		} else {
			t.Errorf("Expected %s to be a unicast address", ip)
		}
	}
}

// cidrToNet is a convenience function for creating net.IPNets inside struct literals.
func cidrToNet(cidr string) *net.IPNet {
	_, n, _ := net.ParseCIDR(cidr)
	return n
}

var inSubnetsTests = []struct {
	desc      string
	ip        net.IP
	subnets   map[string]*net.IPNet
	inSubnets bool
}{
	{
		"IPv4 Dedicated VIP",
		net.ParseIP("10.0.0.1"),
		map[string]*net.IPNet{
			"10.0.0.0/8":            cidrToNet("10.0.0.0/8"),
			"2620:0:10cc:100d::/64": cidrToNet("2620:0:10cc:100d::/64"),
		},
		true,
	},
	{
		"IPv6 Dedicated VIP",
		net.ParseIP("2620:0:10cc:100d:a800:1ff:fe00:cff"),
		map[string]*net.IPNet{
			"10.0.0.0/8":            cidrToNet("10.0.0.0/8"),
			"2620:0:10cc:100d::/64": cidrToNet("2620:0:10cc:100d::/64"),
		},
		true,
	},
	{
		"IPv4 Unicast VIP",
		net.ParseIP("192.168.1.254"),
		map[string]*net.IPNet{
			"10.0.0.0/8":            cidrToNet("10.0.0.0/8"),
			"2620:0:10cc:100d::/64": cidrToNet("2620:0:10cc:100d::/64"),
			"192.168.0.0/24":         cidrToNet("192.168.0.0/24"),
		},
		false,
	},
	{
		"IPv6 Unicast VIP",
		net.ParseIP("2620:0:10dd:100e:a800:1ff:fe00:cff"),
		map[string]*net.IPNet{
			"10.0.0.0/8":            cidrToNet("10.0.0.0/8"),
			"2620:0:10cc:100d::/64": cidrToNet("2620:0:10cc:100d::/64"),
		},
		false,
	},
	{
		"IPv4 Dedicated VIP with a single DVS",
		net.ParseIP("10.0.0.1"),
		map[string]*net.IPNet{
			"10.0.0.0/8": cidrToNet("10.0.0.0/8"),
		},
		true,
	},
}

func TestInSubnets(t *testing.T) {
	for _, test := range inSubnetsTests {
		result := InSubnets(test.ip, test.subnets)
		if result != test.inSubnets {
			t.Errorf("%v: InSubnets(%v, %v) = %v, want %v", test.desc, test.ip, test.subnets, result, test.inSubnets)
		}
	}
}

func TestServiceKeysSort(t *testing.T) {
	sk := ServiceKeys{
		{IPv4, IPProtoTCP, 80},
		{IPv6, IPProtoTCP, 80},
		{IPv4, IPProtoUDP, 53},
		{IPv6, IPProtoTCP, 53},
		{IPv4, IPProtoTCP, 443},
		{IPv6, IPProtoTCP, 443},
		{IPv4, IPProtoTCP, 53},
		{IPv6, IPProtoUDP, 53},
	}
	want := ServiceKeys{
		{IPv4, IPProtoTCP, 53},
		{IPv4, IPProtoTCP, 80},
		{IPv4, IPProtoTCP, 443},
		{IPv4, IPProtoUDP, 53},
		{IPv6, IPProtoTCP, 53},
		{IPv6, IPProtoTCP, 80},
		{IPv6, IPProtoTCP, 443},
		{IPv6, IPProtoUDP, 53},
	}
	sort.Sort(sk)
	for i := range want {
		if *sk[i] != *want[i] {
			t.Fatalf("ServiceKeys sort failed - element %d got %v, want %v", i, *sk[i], *want[i])
		}
	}
}
