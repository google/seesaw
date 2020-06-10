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

// This file contains the unit tests for the config package.

import (
	"io/ioutil"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"
	pb "github.com/google/seesaw/pb/config"
	spb "github.com/google/seesaw/pb/seesaw"

	"github.com/golang/protobuf/proto"
)

const testDataDir = "testdata"

var healthcheckTests = []struct {
	desc   string
	in     string
	expect *Healthcheck
}{
	{
		"Minimal Healthcheck",
		"healthcheck0.pb",
		&Healthcheck{
			Mode:      seesaw.HCModePlain,
			Type:      seesaw.HCTypeICMP,
			Interval:  time.Duration(10 * time.Second), // protobuf default
			Timeout:   time.Duration(5 * time.Second),  // protobuf default
			TLSVerify: true,                            // protobuf default
		},
	},
	{
		"Full Healthcheck",
		"healthcheck1.pb",
		&Healthcheck{
			Mode:      seesaw.HCModeDSR,
			Type:      seesaw.HCTypeHTTP,
			Interval:  time.Duration(100 * time.Second),
			Timeout:   time.Duration(200 * time.Second),
			TLSVerify: true,
			Code:      200,
			Method:    "HEAD",
			Port:      99,
			Send:      "foo",
			Receive:   "bar",
			Proxy:     true,
		},
	},
	{
		"DNS Healthcheck",
		"healthcheck2.pb",
		&Healthcheck{
			Mode:      seesaw.HCModeDSR,
			Type:      seesaw.HCTypeDNS,
			Interval:  time.Duration(2 * time.Second),
			Timeout:   time.Duration(1 * time.Second),
			TLSVerify: true,
			Method:    "A",
			Port:      53,
			Send:      "www.example.com",
			Receive:   "192.168.0.1",
		},
	},
}

var nodeTests = []struct {
	desc   string
	in     string
	expect map[string]*seesaw.Node
}{
	{
		"0 Nodes",
		"nodes0.pb",
		make(map[string]*seesaw.Node),
	},
	{
		"Production VIP, 2 Production Nodes",
		"nodes1.pb",
		map[string]*seesaw.Node{
			"seesaw1-1.example.com.": {
				Host: seesaw.Host{
					Hostname: "seesaw1-1.example.com.",
					IPv4Addr: net.ParseIP("1.2.3.5").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
				},
				Priority:        255,
				State:           spb.HaState_UNKNOWN,
				AnycastEnabled:  true,
				BGPEnabled:      true,
				VserversEnabled: true,
			},
			"seesaw1-2.example.com.": {
				Host: seesaw.Host{
					Hostname: "seesaw1-2.example.com.",
					IPv4Addr: net.ParseIP("1.2.3.6").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
				},
				Priority:        1,
				State:           spb.HaState_UNKNOWN,
				AnycastEnabled:  true,
				BGPEnabled:      true,
				VserversEnabled: true,
			},
		},
	},
	{
		"Disabled VIP, 1 Production Node",
		"nodes2.pb",
		map[string]*seesaw.Node{
			"seesaw1-1.example.com.": {
				Host: seesaw.Host{
					Hostname: "seesaw1-1.example.com.",
					IPv4Addr: net.ParseIP("1.2.3.5").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
				},
				Priority:        255,
				State:           spb.HaState_DISABLED,
				AnycastEnabled:  true,
				BGPEnabled:      true,
				VserversEnabled: true,
			},
		},
	},
	{
		"Production VIP, 1 Disabled Node",
		"nodes3.pb",
		map[string]*seesaw.Node{
			"seesaw1-2.example.com.": {
				Host: seesaw.Host{
					Hostname: "seesaw1-2.example.com.",
					IPv4Addr: net.ParseIP("1.2.3.6").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
				},
				Priority:        255,
				State:           spb.HaState_DISABLED,
				AnycastEnabled:  false,
				BGPEnabled:      false,
				VserversEnabled: false,
			},
		},
	},
	{
		"Production VIP, 1 Disabled Node, 1 Production Node",
		"nodes4.pb",
		map[string]*seesaw.Node{
			"seesaw1-1.example.com.": {
				Host: seesaw.Host{
					Hostname: "seesaw1-1.example.com.",
					IPv4Addr: net.ParseIP("1.2.3.5").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
				},
				Priority:        255,
				State:           spb.HaState_DISABLED,
				AnycastEnabled:  false,
				BGPEnabled:      false,
				VserversEnabled: false,
			},
			"seesaw1-2.example.com.": {
				Host: seesaw.Host{
					Hostname: "seesaw1-2.example.com.",
					IPv4Addr: net.ParseIP("1.2.3.6").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
				},
				Priority:        1,
				State:           spb.HaState_UNKNOWN,
				AnycastEnabled:  true,
				BGPEnabled:      true,
				VserversEnabled: true,
			},
		},
	},
}

// cidrToNet is a convenience function for creating net.IPNets inside struct literals.
func cidrToNet(cidr string) *net.IPNet {
	_, n, _ := net.ParseCIDR(cidr)
	return n
}

var vipSubnetTests = []struct {
	desc   string
	in     string
	expect map[string]*net.IPNet
}{
	{
		"0 Dedicated VIP Subnets",
		"vipsubnets0.pb",
		make(map[string]*net.IPNet),
	},
	{
		"2 Dedicated VIP Subnets",
		"vipsubnets1.pb",
		map[string]*net.IPNet{
			"192.168.9.0/24":   cidrToNet("192.168.9.0/24"),
			"2015:cafe:9::/64": cidrToNet("2015:cafe:9::/64"),
		},
	},
}

var vipSubnetFailureTests = []struct {
	desc string
	in   string
}{
	{
		"Duplicate Dedicated VIP Subnets",
		"vipsubnets2.pb",
	},
}

var vlanTests = []struct {
	desc   string
	in     string
	expect map[uint16]*seesaw.VLAN
}{
	{
		"0 VLANs",
		"vlans0.pb",
		make(map[uint16]*seesaw.VLAN),
	},
	{
		"2 VLANs",
		"vlans1.pb",
		map[uint16]*seesaw.VLAN{
			114: {
				ID: 114,
				Host: seesaw.Host{
					Hostname: "seesaw-vlan114.example.com.",
					IPv4Addr: net.ParseIP("192.168.130.10").To4(),
					IPv4Mask: net.CIDRMask(28, 32),
				},
				BackendCount: map[seesaw.AF]uint{},
				VIPCount:     map[seesaw.AF]uint{},
			},
			116: {
				ID: 116,
				Host: seesaw.Host{
					Hostname: "seesaw-vlan116.example.com.",
					IPv4Addr: net.ParseIP("192.168.130.100").To4(),
					IPv4Mask: net.CIDRMask(29, 32),
				},
				BackendCount: map[seesaw.AF]uint{},
				VIPCount:     map[seesaw.AF]uint{},
			},
		},
	},
	{
		"2 VLANs with backends",
		"vlans2.pb",
		map[uint16]*seesaw.VLAN{
			100: {
				ID: 100,
				Host: seesaw.Host{
					Hostname: "seesaw1-vlan100.example.com.",
					IPv4Addr: net.ParseIP("192.168.100.0").To4(),
					IPv4Mask: net.CIDRMask(24, 32),
					IPv6Addr: net.ParseIP("2015:100:cafe::10"),
					IPv6Mask: net.CIDRMask(64, 128),
				},
				BackendCount: map[seesaw.AF]uint{
					seesaw.IPv4: 2,
					seesaw.IPv6: 1,
				},
				VIPCount: map[seesaw.AF]uint{
					seesaw.IPv4: 1,
				},
			},
			101: {
				ID: 101,
				Host: seesaw.Host{
					Hostname: "seesaw1-vlan101.example.com.",
					IPv4Addr: net.ParseIP("192.168.101.0").To4(),
					IPv4Mask: net.CIDRMask(24, 32),
					IPv6Addr: net.ParseIP("2015:101:cafe::10"),
					IPv6Mask: net.CIDRMask(64, 128),
				},
				BackendCount: map[seesaw.AF]uint{
					seesaw.IPv4: 1,
					seesaw.IPv6: 2,
				},
				VIPCount: map[seesaw.AF]uint{
					seesaw.IPv6: 1,
				},
			},
		},
	},
}

var vserverTests = []struct {
	desc   string
	in     string
	expect map[string]*Vserver
}{
	{
		"0 Vservers",
		"vservers0.pb",
		make(map[string]*Vserver),
	},
	{
		"3 Production Vservers",
		"vservers1.pb",
		map[string]*Vserver{
			"dns.resolver.anycast@au-syd": {
				Name: "dns.resolver.anycast@au-syd",
				Host: seesaw.Host{
					Hostname: "dns-anycast.example.com.",
					IPv4Addr: net.ParseIP("192.168.255.1").To4(),
					IPv4Mask: net.CIDRMask(24, 32),
				},
				Entries: map[string]*VserverEntry{
					"53/UDP": {
						Port:          53,
						Proto:         seesaw.IPProtoUDP,
						Scheduler:     seesaw.LBSchedulerWLC, // protobuf default
						Mode:          seesaw.LBModeDSR,      // protobuf default
						Persistence:   100,
						OnePacket:     true,
						HighWatermark: 0.8,
						LowWatermark:  0.4,
						Healthchecks: map[string]*Healthcheck{
							"DNS/53_0": {
								Name:      "DNS/53_0",
								Mode:      seesaw.HCModeDSR,
								Type:      seesaw.HCTypeDNS,
								Port:      53,
								Interval:  time.Duration(2 * time.Second),
								Timeout:   time.Duration(1 * time.Second),
								TLSVerify: true,
								Method:    "A",
								Send:      "www.example.com",
								Receive:   "192.168.0.1",
							},
						},
					},
					"53/TCP": {
						Port:         53,
						Proto:        seesaw.IPProtoTCP,
						Scheduler:    seesaw.LBSchedulerWLC, // protobuf default
						Mode:         seesaw.LBModeDSR,      // protobuf default
						Persistence:  100,
						Healthchecks: make(map[string]*Healthcheck),
					},
				},
				Backends: map[string]*seesaw.Backend{
					"dns1-1.example.com.": {
						Host: seesaw.Host{
							Hostname: "dns1-1.example.com.",
							IPv4Addr: net.ParseIP("192.168.36.2").To4(),
							IPv4Mask: net.CIDRMask(26, 32),
						},
						Weight:    5,
						Enabled:   true,
						InService: true,
					},
					"dns1-2.example.com.": {
						Host: seesaw.Host{
							Hostname: "dns1-2.example.com.",
							IPv4Addr: net.ParseIP("192.168.36.3").To4(),
							IPv4Mask: net.CIDRMask(26, 32),
						},
						Weight:    4,
						Enabled:   true,
						InService: true,
					},
				},
				Healthchecks: map[string]*Healthcheck{
					"HTTP/16767_0": {
						Name:      "HTTP/16767_0",
						Mode:      seesaw.HCModeDSR,
						Type:      seesaw.HCTypeHTTPS,
						Port:      16767,
						Interval:  time.Duration(10 * time.Second), // protobuf default
						Timeout:   time.Duration(5 * time.Second),  // protobuf default
						TLSVerify: false,
						Send:      "/healthz",
						Receive:   "Ok",
						Code:      200,
					},
				},
				VIPs: map[string]*seesaw.VIP{
					"192.168.255.1 (Anycast)": {
						IP:   seesaw.NewIP(net.ParseIP("192.168.255.1")),
						Type: seesaw.AnycastVIP,
					},
				},
				AccessGrants: map[string]*AccessGrant{},
				Enabled:      true,
				UseFWM:       false,
				Warnings:     nil,
				MustReady:    false,
			},
			"dns.resolver@au-syd": {
				Name: "dns.resolver@au-syd",
				Host: seesaw.Host{
					Hostname: "dns-vip1.example.com.",
					IPv4Addr: net.ParseIP("192.168.36.1").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
					IPv6Addr: net.ParseIP("2015:cafe:36::a800:1ff:ffee:dd01"),
					IPv6Mask: net.CIDRMask(64, 128),
				},
				Entries: map[string]*VserverEntry{
					"53/UDP": {
						Port:         53,
						Proto:        seesaw.IPProtoUDP,
						Scheduler:    seesaw.LBSchedulerWLC, // protobuf default
						Mode:         seesaw.LBModeDSR,      // protobuf default
						Healthchecks: make(map[string]*Healthcheck),
					},
					"53/TCP": {
						Port:         53,
						Proto:        seesaw.IPProtoTCP,
						Scheduler:    seesaw.LBSchedulerWLC, // protobuf default
						Mode:         seesaw.LBModeDSR,      // protobuf default
						Healthchecks: make(map[string]*Healthcheck),
					},
				},
				Backends:     make(map[string]*seesaw.Backend),
				Healthchecks: make(map[string]*Healthcheck),
				VIPs: map[string]*seesaw.VIP{
					"192.168.36.1 (Dedicated)": {
						IP:   seesaw.NewIP(net.ParseIP("192.168.36.1")),
						Type: seesaw.DedicatedVIP,
					},
					"2015:cafe:36:0:a800:1ff:ffee:dd01 (Dedicated)": {
						IP:   seesaw.NewIP(net.ParseIP("2015:cafe:36::a800:1ff:ffee:dd01")),
						Type: seesaw.DedicatedVIP,
					},
				},
				AccessGrants: map[string]*AccessGrant{},
				Enabled:      true,
				UseFWM:       false,
				Warnings:     nil,
				MustReady:    false,
			},
			"irc.server@au-syd": {
				Name: "irc.server@au-syd",
				Host: seesaw.Host{
					Hostname: "irc-anycast.example.com.",
					IPv4Addr: net.ParseIP("192.168.255.2").To4(),
					IPv4Mask: net.CIDRMask(24, 32),
				},
				Entries: map[string]*VserverEntry{
					"80/TCP": {
						Port:         80,
						Proto:        seesaw.IPProtoTCP,
						Scheduler:    seesaw.LBSchedulerWLC, // protobuf default
						Mode:         seesaw.LBModeDSR,      // protobuf default
						Healthchecks: make(map[string]*Healthcheck),
					},
					"6667/TCP": {
						Port:         6667,
						Proto:        seesaw.IPProtoTCP,
						Scheduler:    seesaw.LBSchedulerWLC, // protobuf default
						Mode:         seesaw.LBModeDSR,      // protobuf default
						Healthchecks: make(map[string]*Healthcheck),
					},
					"6697/TCP": {
						Port:      6697,
						Proto:     seesaw.IPProtoTCP,
						Scheduler: seesaw.LBSchedulerWLC, // protobuf default
						Mode:      seesaw.LBModeDSR,      // protobuf default
						Healthchecks: map[string]*Healthcheck{
							"TCP/6697_0": {
								Name:      "TCP/6697_0",
								Mode:      seesaw.HCModePlain,
								Type:      seesaw.HCTypeTCPTLS,
								Port:      6697,
								Interval:  time.Duration(5 * time.Second),
								Timeout:   time.Duration(5 * time.Second), // protobuf default
								TLSVerify: true,
								Send:      "ADMIN\r\n",
								Receive:   ":",
								Retries:   2,
							},
						},
					},
				},
				Backends: map[string]*seesaw.Backend{
					"irc1-1.example.com.": {
						Host: seesaw.Host{
							Hostname: "irc1-1.example.com.",
							IPv4Addr: net.ParseIP("192.168.36.4").To4(),
							IPv4Mask: net.CIDRMask(26, 32),
							IPv6Addr: net.ParseIP("2015:cafe:36::a800:1ff:ffee:dd04"),
							IPv6Mask: net.CIDRMask(64, 128),
						},
						Weight:    1,
						Enabled:   true,
						InService: true,
					},
				},
				Healthchecks: map[string]*Healthcheck{
					"TCP/6667_0": {
						Name:      "TCP/6667_0",
						Mode:      seesaw.HCModePlain,
						Type:      seesaw.HCTypeTCP,
						Port:      6667,
						Interval:  time.Duration(5 * time.Second),
						Timeout:   time.Duration(5 * time.Second), // protobuf default
						TLSVerify: false,
						Send:      "ADMIN\r\n",
						Receive:   ":",
						Retries:   2,
					},
				},
				VIPs: map[string]*seesaw.VIP{
					"192.168.255.2 (Anycast)": {
						IP:   seesaw.NewIP(net.ParseIP("192.168.255.2")),
						Type: seesaw.AnycastVIP,
					},
				},
				AccessGrants: map[string]*AccessGrant{
					"group:irc-admin": {
						Grantee: "irc-admin",
						IsGroup: true,
					},
					"group:irc-oncall": {
						Grantee: "irc-oncall",
						IsGroup: true,
					},
				},
				Enabled:   true,
				UseFWM:    false,
				Warnings:  nil,
				MustReady: false,
			},
		},
	},
	{
		"1 Vserver with Unicast address and Maglev scheduler",
		"vservers2.pb",
		map[string]*Vserver{
			"api.gateway1@as-hkg": {
				Name: "api.gateway1@as-hkg",
				Host: seesaw.Host{
					Hostname: "gateway1-vip1.example.com.",
					IPv4Addr: net.ParseIP("192.168.36.1").To4(),
					IPv4Mask: net.CIDRMask(26, 32),
					IPv6Addr: net.ParseIP("2015:cafe:36::a800:1ff:ffee:dd01"),
					IPv6Mask: net.CIDRMask(64, 128),
				},
				Entries: map[string]*VserverEntry{
					"443/TCP": {
						Port:         443,
						Proto:        seesaw.IPProtoTCP,
						Scheduler:    seesaw.LBSchedulerMH,
						Mode:         seesaw.LBModeTUN,
						Healthchecks: make(map[string]*Healthcheck),
					},
				},
				Backends: map[string]*seesaw.Backend{
					"gateway1-1.example.com.": {
						Host: seesaw.Host{
							Hostname: "gateway1-1.example.com.",
							IPv4Addr: net.ParseIP("192.168.36.2").To4(),
							IPv4Mask: net.CIDRMask(26, 32),
						},
						Weight:    5,
						Enabled:   true,
						InService: true,
					},
				},
				Healthchecks: map[string]*Healthcheck{
					"HTTP/8001_0": {
						Name:      "HTTP/8001_0",
						Mode:      seesaw.HCModeTUN,
						Type:      seesaw.HCTypeHTTP,
						Port:      8001,
						Interval:  time.Duration(10 * time.Second), // protobuf default
						Timeout:   time.Duration(5 * time.Second),  // protobuf default
						TLSVerify: false,
						Send:      "/healthz",
						Receive:   "Ok",
						Code:      200,
					},
				},
				VIPs: map[string]*seesaw.VIP{
					"192.168.36.1 (Unicast)": {
						IP:   seesaw.NewIP(net.ParseIP("192.168.36.1")),
						Type: seesaw.UnicastVIP,
					},
					"2015:cafe:36:0:a800:1ff:ffee:dd01 (Unicast)": {
						IP:   seesaw.NewIP(net.ParseIP("2015:cafe:36::a800:1ff:ffee:dd01")),
						Type: seesaw.UnicastVIP,
					},
				},
				AccessGrants: map[string]*AccessGrant{},
				Enabled:      true,
				UseFWM:       false,
				Warnings:     nil,
				MustReady:    false,
			},
		},
	},
}

func readHealthcheck(f string) (*pb.Healthcheck, error) {
	p := &pb.Healthcheck{}
	b, err := ioutil.ReadFile(f)
	if err != nil {
		return nil, err
	}
	if err = proto.UnmarshalText(string(b), p); err != nil {
		return nil, err
	}
	proto.SetDefaults(p)
	return p, nil
}

func TestHealthchecks(t *testing.T) {
	for _, test := range healthcheckTests {
		filename := filepath.Join(testDataDir, test.in)
		h, err := readHealthcheck(filename)
		if err != nil {
			t.Errorf("readHealthcheck failed to read protobuf file %s for %q: %v",
				test.in, test.desc, err)
			continue
		}
		got := protoToHealthcheck(h, 0)
		if !reflect.DeepEqual(test.expect, got) {
			t.Errorf("Test %q want %#v, got %#v", test.desc, *test.expect, *got)
		}
	}
}

func TestNodes(t *testing.T) {
	for _, test := range nodeTests {
		filename := filepath.Join(testDataDir, test.in)
		n, err := ReadConfig(filename, "")
		if err != nil {
			t.Errorf("ReadConfig failed to read protobuf file %s for %q: %v",
				test.in, test.desc, err)
			continue
		}
		got := n.Cluster.Nodes
		if len(test.expect) != len(got) {
			t.Errorf("Test %q want %d Nodes, got %d Nodes", test.desc, len(test.expect), len(got))
			continue
		}

		for i, node := range test.expect {
			if !reflect.DeepEqual(*node, *got[i]) {
				t.Errorf("Test %q want %#v, got %#v", test.desc, *node, *got[i])
			}
		}
	}
}

func TestVIPSubnets(t *testing.T) {
	for _, test := range vipSubnetTests {
		filename := filepath.Join(testDataDir, test.in)
		c, err := ReadConfig(filename, "")
		if err != nil {
			t.Errorf("ReadConfig failed to read protobuf file %s for %q: %v",
				test.in, test.desc, err)
			continue
		}
		got := c.Cluster.VIPSubnets
		if len(test.expect) != len(got) {
			t.Errorf("Test %q want %d VIPSubnets, got %d VIPSubnets", test.desc, len(test.expect), len(got))
			continue
		}

		for i, vipSubnet := range test.expect {
			if !reflect.DeepEqual(*vipSubnet, *got[i]) {
				t.Errorf("Test %q want %#v, got %#v", test.desc, *vipSubnet, *got[i])
			}
		}
	}
}

func TestVIPSubnetFailures(t *testing.T) {
	for _, test := range vipSubnetFailureTests {
		filename := filepath.Join(testDataDir, test.in)
		if _, err := ReadConfig(filename, ""); err == nil {
			t.Errorf("ReadConfig successfully loaded protobuf file %s for %q, should have failed", test.in, test.desc)
		}
	}
}

func TestVLANs(t *testing.T) {
	for _, test := range vlanTests {
		filename := filepath.Join(testDataDir, test.in)
		c, err := ReadConfig(filename, "")
		if err != nil {
			t.Errorf("ReadConfig failed to read protobuf file %s for %q: %v",
				test.in, test.desc, err)
			continue
		}
		got := c.Cluster.VLANs
		if len(test.expect) != len(got) {
			t.Errorf("Test %q want %d VLANs, got %d VLANs", test.desc, len(test.expect), len(got))
			continue
		}

		for i, vlan := range test.expect {
			if !reflect.DeepEqual(*vlan, *got[i]) {
				t.Errorf("Test %q want %#v, got %#v", test.desc, *vlan, *got[i])
			}
		}
	}
}

func TestVservers(t *testing.T) {
	for _, test := range vserverTests {
		filename := filepath.Join(testDataDir, test.in)
		n, err := ReadConfig(filename, "")
		if err != nil {
			t.Errorf("ReadConfig failed to read protobuf file %s for '%s': %v",
				test.in, test.desc, err)
			continue
		}
		got := n.Cluster.Vservers
		if len(test.expect) != len(got) {
			t.Errorf("Test %q want %d Vservers, got %d Vservers", test.desc, len(test.expect), len(got))
			continue
		}

		for k, v := range test.expect {
			if !reflect.DeepEqual(v, got[k]) {
				t.Errorf("Test %q, %v want \n%#+v, got \n%#+v", test.desc, k, *v, *got[k])
				compareVserverEntries(t, got[k].Entries, v.Entries)
				compareHealthchecks(t, got[k].Healthchecks, v.Healthchecks)
				compareBackends(t, got[k].Backends, v.Backends)
			}
		}
	}
}

func compareBackends(t *testing.T, got, want map[string]*seesaw.Backend) {
	if len(got) != len(want) {
		t.Errorf("Got %d backends, want %d", len(got), len(want))
		return
	}
	for k, b := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("Got does not have backend %v", k)
			continue
		}
		if !reflect.DeepEqual(*got[k], *b) {
			t.Errorf("Got backend %#+v, want %#+v", *got[k], *b)
		}
	}
}

func compareVserverEntries(t *testing.T, got, want map[string]*VserverEntry) {
	if len(got) != len(want) {
		t.Errorf("Got %d vserver entries, want %d", len(got), len(want))
		return
	}
	for k, e := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("Got does not have vserver entry %v", k)
			continue
		}
		if !reflect.DeepEqual(*e, *got[k]) {
			t.Errorf("Got entry %#+v, want %#+v", *got[k], *e)
			compareHealthchecks(t, got[k].Healthchecks, e.Healthchecks)
		}
	}
}

func compareHealthchecks(t *testing.T, got, want map[string]*Healthcheck) {
	if len(got) != len(want) {
		t.Errorf("Got %d healthchecks, want %d", len(got), len(want))
		return
	}
	for k, h := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("Got does not have healthcheck %v", k)
			continue
		}
		if !reflect.DeepEqual(*got[k], *h) {
			t.Errorf("Got healthcheck %#+v, want %#+v", *got[k], *h)
		}
	}
}
