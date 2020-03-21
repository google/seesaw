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

package engine

// This file contains the tests for engine_healthcheck.go.

import (
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/healthcheck"
)

var (
	healthchecks = map[string]*config.Healthcheck{
		"TCP/81": {
			Type:     seesaw.HCTypeTCP,
			Port:     81,
			Interval: 100 * time.Second,
			Timeout:  50 * time.Second,
			Send:     "some tcp request",
			Receive:  "some tcp response",
			Name:     "TCP/81",
		},
		"HTTP/3901": {
			Type:     seesaw.HCTypeHTTP,
			Port:     3901,
			Interval: 100 * time.Second,
			Timeout:  50 * time.Second,
			Send:     "some request",
			Receive:  "some response",
			Code:     200,
			Name:     "HTTP/3901",
		},
	}
	hcTestEntries = map[string]*config.VserverEntry{
		"80/TCP": {
			Port:  80,
			Proto: seesaw.IPProtoTCP,
			Mode:  seesaw.LBModeDSR,
			Healthchecks: map[string]*config.Healthcheck{
				"TCP/81": healthchecks["TCP/81"],
			},
		},
	}

	hcTestBackends = map[string]*seesaw.Backend{
		"dns1-1.example.com.": {
			Host: seesaw.Host{
				Hostname: "dns1-1.example.com.",
				IPv4Addr: net.ParseIP("1.1.1.2"),
				IPv4Mask: net.CIDRMask(24, 32),
				IPv6Addr: net.ParseIP("2012::cafd"),
				IPv6Mask: net.CIDRMask(64, 128),
			},
			Enabled: true,
		},
	}

	hcTestHealthchecks = map[string]*config.Healthcheck{
		"HTTP/3901": healthchecks["HTTP/3901"],
	}

	key1 = checkerKey{
		key: CheckKey{
			BackendIP:       seesaw.ParseIP("1.1.1.2"),
			HealthcheckType: seesaw.HCTypeHTTP,
			HealthcheckPort: 3901,
		},
		cfg: *healthchecks["HTTP/3901"],
	}

	key2 = checkerKey{
		key: CheckKey{
			BackendIP:       seesaw.ParseIP("2012::cafd"),
			HealthcheckType: seesaw.HCTypeHTTP,
			HealthcheckPort: 3901,
		},
		cfg: *healthchecks["HTTP/3901"],
	}

	key3 = checkerKey{
		key: CheckKey{
			BackendIP:       seesaw.ParseIP("1.1.1.2"),
			HealthcheckType: seesaw.HCTypeTCP,
			HealthcheckPort: 81,
		},
		cfg: *healthchecks["TCP/81"],
	}

	key4 = checkerKey{
		key: CheckKey{
			BackendIP:       seesaw.ParseIP("2012::cafd"),
			HealthcheckType: seesaw.HCTypeTCP,
			HealthcheckPort: 81,
		},
		cfg: *healthchecks["TCP/81"],
	}
)

var hcTests = []struct {
	desc   string
	in     *config.Cluster
	expect map[checkerKey]*healthcheck.Config
}{
	{
		"Empty",
		&config.Cluster{},
		make(map[checkerKey]*healthcheck.Config),
	},
	{
		"One Vserver HC, 1 VserverEntry HC, 1 enabled backend, 1 disabled backend",
		&config.Cluster{
			Vservers: map[string]*config.Vserver{
				"foo": {
					Host: seesaw.Host{
						Hostname: "dns-vip1.example.com.",
						IPv4Addr: net.ParseIP("1.1.1.1"),
						IPv4Mask: net.CIDRMask(24, 32),
						IPv6Addr: net.ParseIP("2012::cafe"),
						IPv6Mask: net.CIDRMask(64, 128),
					},
					Entries:      hcTestEntries,
					Backends:     hcTestBackends,
					Healthchecks: hcTestHealthchecks,
					Enabled:      true,
				},
			},
		},
		map[checkerKey]*healthcheck.Config{
			key1: {
				Interval: 100 * time.Second,
				Timeout:  50 * time.Second,
				Checker: &healthcheck.HTTPChecker{
					Target: healthcheck.Target{
						IP:    net.ParseIP("1.1.1.2"),
						Host:  net.ParseIP("1.1.1.2"),
						Mode:  seesaw.HCModePlain,
						Port:  3901,
						Proto: seesaw.IPProtoTCP,
					},
					Secure:       false,
					TLSVerify:    true,
					Method:       "GET",
					Request:      "some request",
					Response:     "some response",
					ResponseCode: 200,
				},
			},
			key2: {
				Interval: 100 * time.Second,
				Timeout:  50 * time.Second,
				Checker: &healthcheck.HTTPChecker{
					Target: healthcheck.Target{
						IP:    net.ParseIP("2012::cafd"),
						Host:  net.ParseIP("2012::cafd"),
						Mode:  seesaw.HCModePlain,
						Port:  3901,
						Proto: seesaw.IPProtoTCP,
					},
					Secure:       false,
					TLSVerify:    true,
					Method:       "GET",
					Request:      "some request",
					Response:     "some response",
					ResponseCode: 200,
				},
			},
			key3: {
				Interval: 100 * time.Second,
				Timeout:  50 * time.Second,
				Checker: &healthcheck.TCPChecker{
					Target: healthcheck.Target{
						IP:    net.ParseIP("1.1.1.2"),
						Host:  net.ParseIP("1.1.1.2"),
						Mode:  seesaw.HCModePlain,
						Port:  81,
						Proto: seesaw.IPProtoTCP,
					},
					Send:    "some tcp request",
					Receive: "some tcp response",
				},
			},
			key4: {
				Interval: 100 * time.Second,
				Timeout:  50 * time.Second,
				Checker: &healthcheck.TCPChecker{
					Target: healthcheck.Target{
						IP:    net.ParseIP("2012::cafd"),
						Host:  net.ParseIP("2012::cafd"),
						Mode:  seesaw.HCModePlain,
						Port:  81,
						Proto: seesaw.IPProtoTCP,
					},
					Send:    "some tcp request",
					Receive: "some tcp response",
				},
			},
		},
	},
}

func joinMaps(m1 map[checkerKey]healthcheck.Id, m2 map[healthcheck.Id]*healthcheck.Config) map[checkerKey]*healthcheck.Config {
	m3 := make(map[checkerKey]*healthcheck.Config)
	for k, id := range m1 {
		m3[k] = m2[id]
	}
	return m3
}

func clearIDs(m map[checkerKey]*healthcheck.Config) {
	for _, v := range m {
		v.Id = 0
	}
}

func TestHealthchecks(t *testing.T) {
	e := newTestEngine()
	hcm := e.hcManager
	for i, test := range hcTests {
		vservers := make(map[string]*vserver)
		for _, config := range test.in.Vservers {
			vserver := newTestVserver(e)
			vserver.handleConfigUpdate(config)
			vservers[config.Name] = vserver
			hcm.update(config.Name, vserver.checks)
		}

		if len(hcm.ids) != len(hcm.cfgs) {
			t.Errorf("TestHealthchecks ids vs. cfgs length failed for %q (#%d), %d != %d",
				test.desc, i, len(hcm.ids), len(hcm.cfgs))
		}
		// Doing another update() should not change anything
		oldIDs := hcm.ids
		oldCfgs := hcm.cfgs

		for name, vserver := range vservers {
			hcm.update(name, vserver.checks)
		}
		if !reflect.DeepEqual(oldIDs, hcm.ids) {
			t.Errorf("TestHealthchecks failed, IDs changed, old: %#v, new: %#v", oldIDs, hcm.ids)
		}
		if !reflect.DeepEqual(oldCfgs, hcm.cfgs) {
			t.Errorf("TestHealthchecks failed, cfgs changed, old: %#v, new: %#v", oldCfgs, hcm.cfgs)
		}

		got := joinMaps(hcm.ids, hcm.cfgs)
		if len(got) != len(hcm.cfgs) {
			t.Errorf("TestHealthchecks got vs. ids length failed for %q (#%d), %d != %d",
				test.desc, i, len(got), len(hcm.cfgs))
		}

		// Delete the IDs so we can compare maps
		clearIDs(got)

		if !reflect.DeepEqual(test.expect, got) {
			t.Errorf("TestHealthchecks failed for %q (#%d), \nwant %#v, \ngot %#v",
				test.desc, i, test.expect, got)
			for k, v := range test.expect {
				if !reflect.DeepEqual(v, got[k]) {
					t.Errorf("want Config: %#v", *v)
					if got[k] != nil {
						t.Errorf(" got Config: %#v", *got[k])
					} else {
						t.Errorf(" got Config: nil")
					}
					t.Errorf("want Checker: %#v", v.Checker)
					if got[k] != nil {
						t.Errorf(" got Checker: %#v", got[k].Checker)
					} else {
						t.Errorf(" got Checker: nil")
					}
				}
			}
		}
		for _, config := range test.in.Vservers {
			hcm.update(config.Name, nil)
		}
		if len(hcm.ids) != 0 {
			t.Errorf("len(hcm.ids): want 0, got %d", len(hcm.ids))
		}
		if len(hcm.cfgs) != 0 {
			t.Errorf("len(hcm.cfgs): want 0, got %d", len(hcm.cfgs))
		}
	}
}

var (
	hcUpdateCheckKey1 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModePlain,
		seesaw.HCTypeDNS,
		53,
		"HTTP/53_0",
	}
	hcUpdateCheckKey2 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.3"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModePlain,
		seesaw.HCTypeDNS,
		53,
		"HTTP/53_0",
	}
	hcUpdateCheckKey3 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModePlain,
		seesaw.HCTypeHTTPS,
		16767,
		"HTTP/16767_0",
	}
	hcUpdateCheckKey4 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.3"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModePlain,
		seesaw.HCTypeHTTPS,
		16767,
		"HTTP/16767_0",
	}
	hcUpdateCheckKey5 = CheckKey{
		seesaw.ParseIP("192.168.36.2"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModePlain,
		seesaw.HCTypeHTTPS,
		16767,
		"HTTP/16767_0",
	}

	hcUpdateHealthcheck1 = config.Healthcheck{
		Name:      "DNS/53_0",
		Type:      seesaw.HCTypeDNS,
		Port:      53,
		Interval:  2 * time.Second,
		Timeout:   1 * time.Second,
		Method:    "A",
		Send:      "dns-anycast.example.com",
		Receive:   "192.168.255.1",
		TLSVerify: true,
	}
	hcUpdateHealthcheck2 = config.Healthcheck{
		Name:      "HTTP/16767_0",
		Type:      seesaw.HCTypeHTTPS,
		Port:      16767,
		Interval:  10 * time.Second,
		Timeout:   5 * time.Second,
		Send:      "/healthz",
		Receive:   "Ok",
		Code:      200,
		TLSVerify: false,
	}
	hcUpdateHealthcheck3 = config.Healthcheck{
		Name:      "HTTP/16767_0",
		Type:      seesaw.HCTypeHTTPS,
		Port:      16767,
		Interval:  10 * time.Second,
		Timeout:   5 * time.Second,
		Retries:   2,
		Send:      "https://dns-anycast.example.com/healthz",
		Receive:   "Ok",
		Code:      200,
		TLSVerify: false,
	}
)

func makeCheckerKey(key CheckKey, cfg *config.Healthcheck) checkerKey {
	return checkerKey{
		key: dedup(key),
		cfg: *cfg,
	}
}

var hcUpdateTests = []struct {
	desc     string
	checks   map[CheckKey]*check
	expected []checkerKey
}{
	{
		desc: "initial healthchecks",
		checks: map[CheckKey]*check{
			hcUpdateCheckKey1: {
				healthcheck: &hcUpdateHealthcheck1,
			},
			hcUpdateCheckKey2: {
				healthcheck: &hcUpdateHealthcheck1,
			},
		},
		expected: []checkerKey{
			makeCheckerKey(hcUpdateCheckKey1, &hcUpdateHealthcheck1),
			makeCheckerKey(hcUpdateCheckKey2, &hcUpdateHealthcheck1),
		},
	},
	{
		desc: "change of healthcheck type/port",
		checks: map[CheckKey]*check{
			hcUpdateCheckKey3: {
				healthcheck: &hcUpdateHealthcheck2,
			},
			hcUpdateCheckKey4: {
				healthcheck: &hcUpdateHealthcheck2,
			},
		},
		expected: []checkerKey{
			makeCheckerKey(hcUpdateCheckKey3, &hcUpdateHealthcheck2),
			makeCheckerKey(hcUpdateCheckKey4, &hcUpdateHealthcheck2),
		},
	},
	{
		desc: "change to healthcheck configuration",
		checks: map[CheckKey]*check{
			hcUpdateCheckKey3: {
				healthcheck: &hcUpdateHealthcheck3,
			},
			hcUpdateCheckKey4: {
				healthcheck: &hcUpdateHealthcheck3,
			},
		},
		expected: []checkerKey{
			makeCheckerKey(hcUpdateCheckKey3, &hcUpdateHealthcheck3),
			makeCheckerKey(hcUpdateCheckKey4, &hcUpdateHealthcheck3),
		},
	},
	{
		desc: "healthcheck dedup",
		checks: map[CheckKey]*check{
			hcUpdateCheckKey3: {
				healthcheck: &hcUpdateHealthcheck3,
			},
			hcUpdateCheckKey4: {
				healthcheck: &hcUpdateHealthcheck3,
			},
			hcUpdateCheckKey5: {
				healthcheck: &hcUpdateHealthcheck3,
			},
		},
		expected: []checkerKey{
			makeCheckerKey(hcUpdateCheckKey3, &hcUpdateHealthcheck3),
			makeCheckerKey(hcUpdateCheckKey4, &hcUpdateHealthcheck3),
		},
	},

	{
		desc:   "remove healthchecks",
		checks: map[CheckKey]*check{},
	},
}

func TestHealthcheckUpdates(t *testing.T) {
	engine := newTestEngine()
	hcm := newHealthcheckManager(engine)
	for _, test := range hcUpdateTests {
		hcm.update("test", test.checks)

		// Basic sanity checks...
		if len(hcm.ids) != len(test.expected) {
			t.Errorf("%q: got %d IDs, want %d", test.desc, len(hcm.ids), len(test.expected))
			continue
		}
		if len(hcm.cfgs) != len(test.expected) {
			t.Errorf("%q: got %d configs, want %d", test.desc, len(hcm.cfgs), len(test.expected))
			continue
		}
		if len(hcm.checks) != len(test.expected) {
			t.Errorf("%q: got %d checks, want %d", test.desc, len(hcm.checks), len(test.expected))
			continue
		}

		// Find the healthcheck ID and compare the configuration.
		for _, key := range test.expected {
			id, ok := hcm.ids[key]
			if !ok {
				t.Errorf("%q: failed to find ID for key %#v", test.desc, key)
				continue
			}
			hcList, ok := hcm.checks[id]
			if !ok {
				t.Errorf("%q: failed to find check for key %#v via ID %d", test.desc, key, id)
				continue
			}

			for _, hc := range hcList {
				if *hc.healthcheck != key.cfg {
					t.Errorf("%q: got healthcheck config %#+v, want %#+v", test.desc, *hc.healthcheck, key.cfg)
				}
			}

		}
	}
}
