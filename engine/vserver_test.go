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
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/healthcheck"
	"github.com/kylelemons/godebug/pretty"

	log "github.com/golang/glog"
)

const testDataDir = "testdata"

var (
	statusHealthy   = healthcheck.Status{State: healthcheck.StateHealthy}
	statusUnhealthy = healthcheck.Status{State: healthcheck.StateUnhealthy}
)

var (
	hc1       = &config.Healthcheck{Name: "NONE/1_0", Type: seesaw.HCTypeNone, Port: 1}
	hc2       = &config.Healthcheck{Name: "NONE/2_0", Type: seesaw.HCTypeNone, Port: 2}
	hc3       = &config.Healthcheck{Name: "NONE/3_0", Type: seesaw.HCTypeNone, Port: 3}
	hc4       = &config.Healthcheck{Name: "NONE/4_0", Type: seesaw.HCTypeNone, Port: 4}
	vserverHC = &config.Healthcheck{Name: "NONE/5_0", Type: seesaw.HCTypeNone, Port: 5}
)

func newTestBackend(num int) *seesaw.Backend {
	last := strconv.Itoa(num + 9)
	return &seesaw.Backend{
		Host: seesaw.Host{
			Hostname: fmt.Sprintf("dns1-%d.example.com", num),
			IPv4Addr: net.ParseIP("1.1.1." + last),
			IPv6Addr: net.ParseIP("2012::" + last),
		},
		Enabled:   true,
		InService: true,
		Weight:    int32(num),
	}
}

var (
	backend1 = newTestBackend(1)
	backend2 = newTestBackend(2)
	backend3 = newTestBackend(3)
	backend4 = newTestBackend(4)
	backend5 = newTestBackend(5)
)

var vserverHost = seesaw.Host{
	Hostname: "dns-vip1.example.com",
	IPv4Addr: net.ParseIP("192.168.255.1"),
	IPv4Mask: net.CIDRMask(24, 32),
	IPv6Addr: net.ParseIP("2012::1"),
	IPv6Mask: net.CIDRMask(64, 128),
}

var vserverConfig = config.Vserver{
	Name: "dns.resolver@au-syd",
	Host: vserverHost,
	Entries: map[string]*config.VserverEntry{
		"53/UDP": {
			Mode:      seesaw.LBModeDSR,
			Port:      53,
			Proto:     seesaw.IPProtoUDP,
			Scheduler: seesaw.LBSchedulerWRR,
			Healthchecks: map[string]*config.Healthcheck{
				hc1.Key(): hc1,
				hc2.Key(): hc2,
			},
		},
		"8053/TCP": {
			Mode:      seesaw.LBModeDSR,
			Port:      8053,
			Proto:     seesaw.IPProtoTCP,
			Scheduler: seesaw.LBSchedulerWRR,
			Healthchecks: map[string]*config.Healthcheck{
				hc3.Key(): hc3,
				hc4.Key(): hc4,
			},
			LowWatermark:  0.1,
			HighWatermark: 0.2,
		},
	},
	Backends: map[string]*seesaw.Backend{
		backend1.Hostname: backend1,
		backend2.Hostname: backend2,
	},
	Healthchecks: map[string]*config.Healthcheck{vserverHC.Key(): vserverHC},
	Enabled:      true,
}

var expectedServices = map[serviceKey]struct {
	ip            seesaw.IP
	expectedDests []destination
}{
	serviceKey{
		af:    seesaw.IPv4,
		proto: seesaw.IPProtoUDP,
		port:  53,
	}: {
		ip: seesaw.ParseIP("192.168.255.1"),
		expectedDests: []destination{
			{destinationKey: newDestinationKey(net.ParseIP("1.1.1.10")), stats: &seesaw.DestinationStats{}},
			{destinationKey: newDestinationKey(net.ParseIP("1.1.1.11")), stats: &seesaw.DestinationStats{}},
		},
	},
	serviceKey{
		af:    seesaw.IPv4,
		proto: seesaw.IPProtoTCP,
		port:  8053,
	}: {
		ip: seesaw.ParseIP("192.168.255.1"),
		expectedDests: []destination{
			{destinationKey: newDestinationKey(net.ParseIP("1.1.1.10")), stats: &seesaw.DestinationStats{}},
			{destinationKey: newDestinationKey(net.ParseIP("1.1.1.11")), stats: &seesaw.DestinationStats{}},
		},
	},
	serviceKey{
		af:    seesaw.IPv6,
		proto: seesaw.IPProtoUDP,
		port:  53,
	}: {
		ip: seesaw.ParseIP("2012::1"),
		expectedDests: []destination{
			{destinationKey: newDestinationKey(net.ParseIP("2012::10")), stats: &seesaw.DestinationStats{}},
			{destinationKey: newDestinationKey(net.ParseIP("2012::11")), stats: &seesaw.DestinationStats{}},
		},
	},
	serviceKey{
		af:    seesaw.IPv6,
		proto: seesaw.IPProtoTCP,
		port:  8053,
	}: {
		ip: seesaw.ParseIP("2012::1"),
		expectedDests: []destination{
			{destinationKey: newDestinationKey(net.ParseIP("2012::10")), stats: &seesaw.DestinationStats{}},
			{destinationKey: newDestinationKey(net.ParseIP("2012::11")), stats: &seesaw.DestinationStats{}},
		},
	},
}

var expectedFWMServices = map[serviceKey]struct {
	ip            seesaw.IP
	expectedDests []destination
}{
	serviceKey{
		af:  seesaw.IPv4,
		fwm: fwmAllocBase + 0,
	}: {
		ip: seesaw.ParseIP("192.168.255.1"),
		expectedDests: []destination{
			{destinationKey: newDestinationKey(net.ParseIP("1.1.1.10")), stats: &seesaw.DestinationStats{}},
			{destinationKey: newDestinationKey(net.ParseIP("1.1.1.11")), stats: &seesaw.DestinationStats{}},
		},
	},
	serviceKey{
		af:  seesaw.IPv6,
		fwm: fwmAllocBase + 1,
	}: {
		ip: seesaw.ParseIP("2012::1"),
		expectedDests: []destination{
			{destinationKey: newDestinationKey(net.ParseIP("2012::10")), stats: &seesaw.DestinationStats{}},
			{destinationKey: newDestinationKey(net.ParseIP("2012::11")), stats: &seesaw.DestinationStats{}},
		},
	},
}

func TestExpandServices(t *testing.T) {
	vserver := newTestVserver(nil)
	vserver.config = &vserverConfig
	svcs := vserver.expandServices()
	if len(svcs) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(svcs))
		return
	}
	for sk, s := range svcs {
		es, ok := expectedServices[sk]
		if !ok {
			t.Errorf("Failed to find service - %#v", sk)
			continue
		}
		if !s.ip.Equal(es.ip) {
			t.Errorf("Service failed to match - got %v, want %v",
				s.ip, es.ip)
		}
		dsts := vserver.expandDests(s)
		if len(dsts) != len(es.expectedDests) {
			t.Errorf("Expected %d destinations, got %d",
				len(es.expectedDests), len(dsts))
			continue
		}
		for _, ed := range es.expectedDests {
			if dsts[ed.destinationKey] == nil {
				t.Errorf("Failed to find dest - %v", ed.ip)
			}
		}
	}
}

func TestExpandFWMServices(t *testing.T) {
	vserver := newTestVserver(nil)
	// Use a copy of the config so other tests are not affected.
	fwmConfig := vserverConfig
	fwmConfig.UseFWM = true
	vserver.config = &fwmConfig
	svcs := vserver.expandServices()
	if len(svcs) != len(expectedFWMServices) {
		t.Errorf("Expected %d FWM services, got %d",
			len(expectedFWMServices), len(svcs))
		return
	}
	for sk, s := range svcs {
		es, ok := expectedFWMServices[sk]
		if !ok {
			t.Errorf("Failed to find FWM service - %#v", sk)
			continue
		}
		if !s.ip.Equal(es.ip) {
			t.Errorf("FWM service IP failed to match - got %v, want %v",
				s.ip, es.ip)
		}
		dsts := vserver.expandDests(s)
		if len(dsts) != len(es.expectedDests) {
			t.Errorf("Expected %d FWM destinations, got %d",
				len(es.expectedDests), len(dsts))
			continue
		}
		for _, ed := range es.expectedDests {
			if dsts[ed.destinationKey] == nil {
				t.Errorf("Failed to find FWM dest - %v", ed.ip)
			}
		}
	}
}

func TestExpandChecks(t *testing.T) {
	vserver := newTestVserver(nil)
	vserver.handleConfigUpdate(&vserverConfig)

	// 1 vserver healthcheck x 2 backends x 2 IPs +
	// 2 ventry healthchecks x 2 backends x 2 IPs x 2 ventries = 20
	if len(vserver.checks) != 20 {
		t.Errorf("Expected 20 total checks, got %d", len(vserver.checks))
	}

	count := 0
	for _, s := range vserver.services {
		for _, d := range s.dests {
			// 1 vserver healthcheck + 2 ventry healthchecks = 3 dests
			if len(d.checks) != 3 {
				t.Errorf("Expected 3 checks, got %d", len(d.checks))
			}
			for _, c := range d.checks {
				// vserver healthcheck
				if c.healthcheck.Port == vserverHC.Port && len(c.dests) != 2 {
					t.Errorf("Expected 2 dests for vserver healthcheck, got %d", len(c.dests))
				}
				// ventry healthcheck
				if c.healthcheck.Port != 5 && len(c.dests) != 1 {
					t.Errorf("Expected 1 dest for ventry healthcheck, got %d", len(c.dests))
				}
				count++
			}
		}
	}
	// 2 ventry x 2 IPs = 4 services
	// 4 services x 2 dests x (1 vserver healthcheck + 2 ventry healthchecks) = 24
	if count != 24 {
		t.Errorf("Expected 24 non-unique checks, got %d", count)
	}
}

type testStates struct {
	active  []bool
	healthy []bool
}

var expectedVserverActive = map[seesaw.IP][]bool{
	seesaw.ParseIP("192.168.255.1"): {
		false, false, true, true, false, false, false,
	},
	seesaw.ParseIP("2012::1"): {
		false, false, true, true, true, true, false,
	},
}

var expectedServiceStates = map[serviceKey]struct {
	ip seesaw.IP
	testStates
	dests map[destinationKey]testStates
}{
	serviceKey{seesaw.IPv4, 0, seesaw.IPProtoUDP, 53}: {
		seesaw.ParseIP("192.168.255.1"),
		testStates{
			active:  []bool{false, false, true, true, false, false, false},
			healthy: []bool{false, false, true, true, false, false, false},
		},
		map[destinationKey]testStates{
			destinationKey{seesaw.ParseIP("1.1.1.10")}: {
				active:  []bool{false, false, true, false, false, false, false},
				healthy: []bool{false, false, true, false, false, false, false},
			},
			destinationKey{seesaw.ParseIP("1.1.1.11")}: {
				active:  []bool{false, false, true, true, false, false, false},
				healthy: []bool{false, false, true, true, false, false, false},
			},
		},
	},
	serviceKey{seesaw.IPv4, 0, seesaw.IPProtoTCP, 8053}: {
		seesaw.ParseIP("192.168.255.1"),
		testStates{
			active:  []bool{false, false, true, true, false, false, false},
			healthy: []bool{false, false, true, true, true, true, false},
		},
		map[destinationKey]testStates{
			destinationKey{seesaw.ParseIP("1.1.1.10")}: {
				active:  []bool{false, false, true, true, false, false, false},
				healthy: []bool{false, false, true, true, true, true, false},
			},
			destinationKey{seesaw.ParseIP("1.1.1.11")}: {
				active:  []bool{false, false, true, true, false, false, false},
				healthy: []bool{false, false, true, true, true, true, false},
			},
		},
	},
	serviceKey{seesaw.IPv6, 0, seesaw.IPProtoUDP, 53}: {
		seesaw.ParseIP("2012::1"),
		testStates{
			active:  []bool{false, false, true, true, true, false, false},
			healthy: []bool{false, false, true, true, true, false, false},
		},
		map[destinationKey]testStates{
			destinationKey{seesaw.ParseIP("2012::10")}: {
				active:  []bool{false, false, true, true, true, false, false},
				healthy: []bool{false, false, true, true, true, false, false},
			},
			destinationKey{seesaw.ParseIP("2012::11")}: {
				active:  []bool{false, false, true, true, true, false, false},
				healthy: []bool{false, false, true, true, true, false, false},
			},
		},
	},
	serviceKey{seesaw.IPv6, 0, seesaw.IPProtoTCP, 8053}: {
		seesaw.ParseIP("2012::1"),
		testStates{
			active:  []bool{false, false, true, true, true, true, false},
			healthy: []bool{false, false, true, true, true, true, false},
		},
		map[destinationKey]testStates{
			destinationKey{seesaw.ParseIP("2012::10")}: {
				active:  []bool{false, false, true, true, true, true, false},
				healthy: []bool{false, false, true, true, true, true, false},
			},
			destinationKey{seesaw.ParseIP("2012::11")}: {
				active:  []bool{false, false, true, true, true, true, false},
				healthy: []bool{false, false, true, true, true, true, false},
			},
		},
	},
}

func checkStates(i int, v *vserver, t *testing.T) {
	for ip, got := range v.active {
		want, ok := expectedVserverActive[ip]
		if !ok {
			t.Fatalf("Failed to find vserver %v", ip)
		}
		if got != want[i] {
			t.Errorf("Vserver IP %s - got active %v, want %v", ip, got, want)
		}
	}
	for svcKey, gotSvc := range v.services {
		wantSvc, ok := expectedServiceStates[svcKey]
		if !ok {
			t.Fatalf("Failed to find service %v", svcKey)
		}
		if got, want := gotSvc.active, wantSvc.active[i]; got != want {
			t.Errorf("Service %v (%d): got active %v, want %v", gotSvc, i, got, want)
		}
		if got, want := gotSvc.healthy, wantSvc.healthy[i]; got != want {
			t.Errorf("Service %v (%d): got healthy %v, want %v", gotSvc, i, got, want)
		}
		for dstKey, gotDst := range gotSvc.dests {
			wantDst, ok := wantSvc.dests[dstKey]
			if !ok {
				t.Fatalf("Failed to find dest %v", dstKey)
			}
			if got, want := gotDst.active, wantDst.active[i]; got != want {
				t.Errorf("Destination %v (%d): got active %v, want %v", gotDst, i, got, want)
			}
			if got, want := gotDst.healthy, wantDst.healthy[i]; got != want {
				t.Errorf("Destination %v (%d): got healthy %v, want %v", gotDst, i, got, want)
			}
		}
	}
}

func TestHealthcheckNotification(t *testing.T) {
	vserver := newTestVserver(nil)
	vserver.handleConfigUpdate(&vserverConfig)
	checkStates(0, vserver, t)

	// One healthcheck reports healthy, but there are two other
	// healthchecks per destination, so services and vservers should
	// still be inactive.
	key := CheckKey{
		VserverIP:       seesaw.ParseIP("192.168.255.1"),
		BackendIP:       seesaw.ParseIP("1.1.1.10"),
		HealthcheckPort: 5,
		Name:            "NONE/5_0",
	}
	n := &checkNotification{key: key, status: statusHealthy}
	vserver.handleCheckNotification(n)
	checkStates(1, vserver, t)

	// Bringing up all destinations should bring up all services and
	// vservers. All services and destinations should now be healthy.
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	checkStates(2, vserver, t)

	// One backend is unhealthy, all services should still be active.
	// All services should be healthy and destinations for other backends
	// should still be healthy.
	key = CheckKey{
		VserverIP:       seesaw.ParseIP("192.168.255.1"),
		BackendIP:       seesaw.ParseIP("1.1.1.10"),
		ServiceProtocol: seesaw.IPProtoUDP,
		ServicePort:     53,
		HealthcheckPort: 1,
		Name:            "NONE/1_0",
	}
	n = &checkNotification{key: key, status: statusUnhealthy}
	vserver.handleCheckNotification(n)
	checkStates(3, vserver, t)

	// The other backend is also unhealthy. 192.168.255.1 is anycast, so all
	// services and the VIP should be inactive. 2012::1 should be unchanged.
	key.BackendIP = seesaw.ParseIP("1.1.1.11")
	n = &checkNotification{key: key, status: statusUnhealthy}
	vserver.handleCheckNotification(n)
	checkStates(4, vserver, t)

	// Both backends are unhealthy for one service. 2012::1 is unicast so the
	// other service and the VIP should still be active.
	key.VserverIP = seesaw.ParseIP("2012::1")
	key.BackendIP = seesaw.ParseIP("2012::10")
	n = &checkNotification{key: key, status: statusUnhealthy}
	vserver.handleCheckNotification(n)
	key.BackendIP = seesaw.ParseIP("2012::11")
	n = &checkNotification{key: key, status: statusUnhealthy}
	vserver.handleCheckNotification(n)
	checkStates(5, vserver, t)

	// Bring down everything.
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusUnhealthy}
		vserver.handleCheckNotification(n)
	}
	checkStates(6, vserver, t)
}

func TestWatermarks(t *testing.T) {
	vserver := newTestVserver(nil)
	vsConfig := vserverConfig
	entriesCopy := make(map[string]*config.VserverEntry)
	vsConfig.Entries = entriesCopy
	for k, vse := range vserverConfig.Entries {
		vseCopy := *vse
		vseCopy.HighWatermark = 0.51
		vseCopy.LowWatermark = 0.49
		vsConfig.Entries[k] = &vseCopy
	}
	vserver.handleConfigUpdate(&vsConfig)

	// Both backends must be healthy for the service to come up.
	ipv4 := seesaw.NewIP(backend1.IPv4Addr)
	ipv6 := seesaw.NewIP(backend1.IPv6Addr)
	for _, c := range vserver.checks {
		if c.key.BackendIP.Equal(ipv4) || c.key.BackendIP.Equal(ipv6) {
			n := &checkNotification{key: c.key, status: statusHealthy}
			vserver.handleCheckNotification(n)
		}
	}
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}
	// One backend goes down, one stays up, service should still be up.
	for _, c := range vserver.checks {
		if c.key.BackendIP.Equal(ipv4) || c.key.BackendIP.Equal(ipv6) {
			n := &checkNotification{key: c.key, status: statusUnhealthy}
			vserver.handleCheckNotification(n)
		}
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}
	// Remaining dests go unhealthy, service should go down.
	for _, c := range vserver.checks {
		if !c.key.BackendIP.Equal(ipv4) && !c.key.BackendIP.Equal(ipv6) {
			n := &checkNotification{key: c.key, status: statusUnhealthy}
			vserver.handleCheckNotification(n)
		}
	}
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}

	// Check edge condition: high/low watermark = 50% and one backend becomes
	// healthy => service should come up.
	vserver = newTestVserver(nil)
	for k, vse := range vserverConfig.Entries {
		vseCopy := *vse
		vseCopy.LowWatermark = 0.5
		vseCopy.HighWatermark = vseCopy.LowWatermark
		vsConfig.Entries[k] = &vseCopy
	}
	vserver.handleConfigUpdate(&vsConfig)
	for _, c := range vserver.checks {
		if c.key.BackendIP.Equal(ipv4) || c.key.BackendIP.Equal(ipv6) {
			n := &checkNotification{key: c.key, status: statusHealthy}
			vserver.handleCheckNotification(n)
		}
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}

	// More than two backends...
	vserver = newTestVserver(nil)
	vsConfig = vserverConfig
	vsConfig.Entries = entriesCopy
	for k, vse := range vserverConfig.Entries {
		vseCopy := *vse
		vseCopy.LowWatermark = 0.4
		vseCopy.HighWatermark = vseCopy.LowWatermark
		vsConfig.Entries[k] = &vseCopy
	}
	vsConfig.Backends = map[string]*seesaw.Backend{
		backend1.Hostname: backend1,
		backend2.Hostname: backend2,
		backend3.Hostname: backend3,
		backend4.Hostname: backend4,
		backend5.Hostname: backend5,
	}
	vserver.handleConfigUpdate(&vsConfig)
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}
	t.Logf("All backends healthy, vserver has been brought up")

	// Now take them down one at a time, the tipping point should be
	// when we only have one remaining backend.
	i := len(vsConfig.Backends)
	for _, b := range vsConfig.Backends {
		t.Logf("Taking down %s (%d)", b.Hostname, i)
		for _, c := range vserver.checks {
			if b.IPv4Addr.Equal(c.key.BackendIP.IP()) || b.IPv6Addr.Equal(c.key.BackendIP.IP()) {
				n := &checkNotification{key: c.key, status: statusUnhealthy}
				vserver.handleCheckNotification(n)
			}
		}
		i--
		var errs []error
		if i <= 1 {
			t.Logf("Expecting vserver to be down")
			errs = checkAllDown(vserver)
		} else {
			t.Logf("Expecting vserver to be up")
			errs = checkAllUp(vserver)
		}
		for _, err := range errs {
			t.Error(err)
		}
	}

	// Now bring them up again one at a time, the tipping point should be
	// when we have at least two healthy backends.
	i = 1
	for _, b := range vsConfig.Backends {
		t.Logf("Bringing up %s (%d)", b.Hostname, i)
		for _, c := range vserver.checks {
			if b.IPv4Addr.Equal(c.key.BackendIP.IP()) || b.IPv6Addr.Equal(c.key.BackendIP.IP()) {
				n := &checkNotification{key: c.key, status: statusHealthy}
				vserver.handleCheckNotification(n)
			}
		}
		var errs []error
		if i <= 1 {
			t.Logf("Expecting vserver to be down")
			errs = checkAllDown(vserver)
		} else {
			t.Logf("Expecting vserver to be up")
			errs = checkAllUp(vserver)
		}
		i++
		for _, err := range errs {
			t.Error(err)
		}
	}
}

func TestDisableVserver(t *testing.T) {
	vserver := newTestVserver(nil)
	vserver.handleConfigUpdate(&vserverConfig)

	// Bring everything up.
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	// Make sure the services are active.
	if len(vserver.services) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(vserver.services))
		return
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}

	// Disable the vserver. Use a copy of the config so other tests are not
	// affected.
	disabledVserver := vserverConfig
	disabledVserver.Enabled = false
	vserver.handleConfigUpdate(&disabledVserver)

	// The vserver should still have the same number of services.
	if len(vserver.services) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(vserver.services))
		return
	}

	// All services and VIPs should be down.
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}

	// Healthchecks should have no effect.
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}
}

func TestVserverOverride(t *testing.T) {
	vserver := newTestVserver(nil)
	vserver.handleConfigUpdate(&vserverConfig)

	// Bring everything up.
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	// Make sure the services are active.
	if len(vserver.services) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(vserver.services))
		return
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}

	// Disable the vserver with an Override.
	var o seesaw.Override
	o = &seesaw.VserverOverride{vserverConfig.Name, seesaw.OverrideDisable}
	vserver.handleOverride(o)

	// The vserver should still have the same number of services.
	if len(vserver.services) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(vserver.services))
		return
	}

	// All services and VIPs should be down.
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}

	// Healthchecks should have no effect.
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}

	// Bring it back.
	o = &seesaw.VserverOverride{vserverConfig.Name, seesaw.OverrideDefault}
	vserver.handleOverride(o)
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	if len(vserver.services) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(vserver.services))
		return
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}

	// Disable the vserver via config. Use a copy of the config so other tests
	// are not affected.
	vserverConfigCopy := vserverConfig
	vserverConfigCopy.Enabled = false
	vserver.handleConfigUpdate(&vserverConfigCopy)

	// All services and VIPs should be down.
	for _, err := range checkAllDown(vserver) {
		t.Error(err)
	}

	// Enable the vserver with an Override.
	o = &seesaw.VserverOverride{vserverConfig.Name, seesaw.OverrideEnable}
	vserver.handleOverride(o)
	for _, c := range vserver.checks {
		n := &checkNotification{key: c.key, status: statusHealthy}
		vserver.handleCheckNotification(n)
	}
	if len(vserver.services) != len(expectedServices) {
		t.Errorf("Expected %d services, got %d",
			len(expectedServices), len(vserver.services))
		return
	}
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}

	// Enable it in the config, nothing should change.
	vserver.handleConfigUpdate(&vserverConfig)
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}

	// Remove the enable override, nothing should change.
	o = &seesaw.VserverOverride{vserverConfig.Name, seesaw.OverrideDefault}
	vserver.handleOverride(o)
	for _, err := range checkAllUp(vserver) {
		t.Error(err)
	}
}

var (
	serviceKey1 = seesaw.ServiceKey{
		AF:    seesaw.IPv4,
		Proto: seesaw.IPProtoUDP,
		Port:  53,
	}
	serviceKey2 = seesaw.ServiceKey{
		AF:    seesaw.IPv4,
		Proto: seesaw.IPProtoTCP,
		Port:  8053,
	}
	serviceKey3 = seesaw.ServiceKey{
		AF:    seesaw.IPv6,
		Proto: seesaw.IPProtoUDP,
		Port:  53,
	}
	serviceKey4 = seesaw.ServiceKey{
		AF:    seesaw.IPv6,
		Proto: seesaw.IPProtoTCP,
		Port:  8053,
	}

	destination1 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/1.1.1.10:53/UDP",
		VserverName: "dns.resolver@au-syd",
		Weight:      1,
		Backend:     backend1,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination2 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/1.1.1.11:53/UDP",
		VserverName: "dns.resolver@au-syd",
		Weight:      2,
		Backend:     backend2,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination3 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/1.1.1.10:8053/TCP",
		VserverName: "dns.resolver@au-syd",
		Weight:      1,
		Backend:     backend1,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination4 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/1.1.1.11:8053/TCP",
		VserverName: "dns.resolver@au-syd",
		Weight:      2,
		Backend:     backend2,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination5 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/[2012::10]:53/UDP",
		VserverName: "dns.resolver@au-syd",
		Weight:      1,
		Backend:     backend1,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination6 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/[2012::11]:53/UDP",
		VserverName: "dns.resolver@au-syd",
		Weight:      2,
		Backend:     backend2,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination7 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/[2012::10]:8053/TCP",
		VserverName: "dns.resolver@au-syd",
		Weight:      1,
		Backend:     backend1,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}
	destination8 = &seesaw.Destination{
		Name:        "dns.resolver@au-syd/[2012::11]:8053/TCP",
		VserverName: "dns.resolver@au-syd",
		Weight:      2,
		Backend:     backend2,
		Enabled:     true,
		Stats:       &seesaw.DestinationStats{},
	}

	destinations1 = map[string]*seesaw.Destination{
		destination1.Name: destination1,
		destination2.Name: destination2,
	}
	destinations2 = map[string]*seesaw.Destination{
		destination3.Name: destination3,
		destination4.Name: destination4,
	}
	destinations3 = map[string]*seesaw.Destination{
		destination5.Name: destination5,
		destination6.Name: destination6,
	}
	destinations4 = map[string]*seesaw.Destination{
		destination7.Name: destination7,
		destination8.Name: destination8,
	}
)

var expectedSnapshot = seesaw.Vserver{
	Name:    "dns.resolver@au-syd",
	Enabled: true,
	Host:    vserverHost,
	Services: map[seesaw.ServiceKey]*seesaw.Service{
		serviceKey1: {
			ServiceKey:       serviceKey1,
			IP:               net.ParseIP("192.168.255.1"),
			Mode:             seesaw.LBModeDSR,
			Scheduler:        seesaw.LBSchedulerWRR,
			Stats:            &seesaw.ServiceStats{},
			Destinations:     destinations1,
			Enabled:          true,
			Healthy:          false,
			Active:           false,
			LowWatermark:     0,
			HighWatermark:    0,
			CurrentWatermark: 0,
		},
		serviceKey2: {
			ServiceKey:       serviceKey2,
			IP:               net.ParseIP("192.168.255.1"),
			Mode:             seesaw.LBModeDSR,
			Scheduler:        seesaw.LBSchedulerWRR,
			Stats:            &seesaw.ServiceStats{},
			Destinations:     destinations2,
			Enabled:          true,
			Healthy:          false,
			Active:           false,
			LowWatermark:     0.1,
			HighWatermark:    0.2,
			CurrentWatermark: 0,
		},
		serviceKey3: {
			ServiceKey:       serviceKey3,
			IP:               net.ParseIP("2012::1"),
			Mode:             seesaw.LBModeDSR,
			Scheduler:        seesaw.LBSchedulerWRR,
			Stats:            &seesaw.ServiceStats{},
			Destinations:     destinations3,
			Enabled:          true,
			Healthy:          false,
			Active:           false,
			LowWatermark:     0,
			HighWatermark:    0,
			CurrentWatermark: 0,
		},
		serviceKey4: {
			ServiceKey:       serviceKey4,
			IP:               net.ParseIP("2012::1"),
			Mode:             seesaw.LBModeDSR,
			Scheduler:        seesaw.LBSchedulerWRR,
			Stats:            &seesaw.ServiceStats{},
			Destinations:     destinations4,
			Enabled:          true,
			Healthy:          false,
			Active:           false,
			LowWatermark:     0.1,
			HighWatermark:    0.2,
			CurrentWatermark: 0,
		},
	},
}

func TestVserverSnapshot(t *testing.T) {
	vserver := newTestVserver(nil)
	if vserver.snapshot() != nil {
		t.Error("Expected snapshot to return nil")
	}

	vserver.handleConfigUpdate(&vserverConfig)
	oldest := time.Date(2000, 2, 1, 12, 30, 0, 0, time.UTC)
	lc := oldest
	for _, c := range vserver.checks {
		n := &checkNotification{
			key: c.key,
			status: healthcheck.Status{
				State:     healthcheck.StateUnhealthy,
				LastCheck: lc,
			},
		}
		vserver.handleCheckNotification(n)
		lc.Add(1 * time.Hour)
	}
	snapshot := vserver.snapshot()

	if got, want := snapshot.Name, expectedSnapshot.Name; got != want {
		t.Errorf("Snapshot name mismatch - got %s, want %s", got, want)
	}
	if got, want := snapshot.Enabled, expectedSnapshot.Enabled; got != want {
		t.Errorf("Snapshot enabled mismatch - got %t, want %t", got, want)
	}
	if got, want := snapshot.Host, expectedSnapshot.Host; !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot vserver host mismatch - got %#v, want %#v", got, want)
	}
	if got, want := len(snapshot.Services), len(expectedSnapshot.Services); got != want {
		t.Errorf("Snapshot has incorrect number of services - got %d, want %d", got, want)
	}
	if got, want := snapshot.OldestHealthCheck, oldest; got != want {
		t.Errorf("Snapshot has incorrect OldestHealthCheck - got %s, want %s", got, want)
	}

	for _, svc := range snapshot.Services {
		expectedSvc, ok := expectedSnapshot.Services[svc.ServiceKey]
		if !ok {
			t.Errorf("Snapshot service %#v not found in expected snapshot services",
				svc.ServiceKey)
			continue
		}
		expectedDests := expectedSvc.Destinations
		dests := svc.Destinations
		expectedSvc.Destinations = nil
		svc.Destinations = nil
		if !reflect.DeepEqual(expectedSvc, svc) {
			t.Errorf("Snapshot service mismatch - got %#v, want %#v",
				svc, expectedSvc)
		}
		expectedSvc.Destinations = expectedDests
		svc.Destinations = dests

		if got, want := len(dests), len(expectedDests); got != want {
			t.Errorf("Snapshot service has incorrect number of destinations - got %d, want %d",
				got, want)
			continue
		}
		for _, dst := range dests {
			expectedDst, ok := expectedDests[dst.Name]
			if !ok {
				t.Errorf("Snapshot destination %s not found in expected snapshot destinations", dst.Name)
				continue
			}
			expectedBackend := expectedDst.Backend
			backend := dst.Backend
			expectedDst.Backend = nil
			dst.Backend = nil
			if !reflect.DeepEqual(expectedDst, dst) {
				t.Errorf("Snapshot destination mismatch - got %#v, want %#v",
					dst, expectedDst)
			}
			if !reflect.DeepEqual(expectedBackend, backend) {
				t.Errorf("Snapshot backend mismatch - got %#v, want %#v",
					backend, expectedBackend)
			}
			expectedDst.Backend = expectedBackend
			dst.Backend = backend
		}
	}
}

func checkAllDown(v *vserver) []error {
	errs := make([]error, 0)
	for _, svc := range v.services {
		if svc.active {
			errs = append(errs, fmt.Errorf("Expected service %v to be inactive", svc))
		}
	}
	for ip, active := range v.active {
		if active {
			errs = append(errs, fmt.Errorf("Expected vserver IP %s to be inactive", ip))
		}
	}
	return errs
}

func checkAllUp(v *vserver) []error {
	errs := make([]error, 0)
	for _, svc := range v.services {
		if !svc.active {
			errs = append(errs, fmt.Errorf("Expected service %v to be active", svc))
		}
	}
	for ip, active := range v.active {
		if !active {
			errs = append(errs, fmt.Errorf("Expected vserver IP %s to be active", ip))
		}
	}
	return errs
}

var (
	vsUpdateCheckKey1 = CheckKey{
		seesaw.ParseIP("192.168.255.1"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey2 = CheckKey{
		seesaw.ParseIP("192.168.255.1"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoTCP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey3 = CheckKey{
		seesaw.ParseIP("192.168.255.1"),
		seesaw.ParseIP("192.168.37.3"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey4 = CheckKey{
		seesaw.ParseIP("192.168.255.1"),
		seesaw.ParseIP("192.168.37.3"),
		53,
		seesaw.IPProtoTCP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey5 = CheckKey{
		seesaw.ParseIP("192.168.255.99"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey6 = CheckKey{
		seesaw.ParseIP("192.168.255.99"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoTCP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey7 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey8 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoTCP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey9 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.3"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey10 = CheckKey{
		seesaw.ParseIP("192.168.36.1"),
		seesaw.ParseIP("192.168.37.3"),
		53,
		seesaw.IPProtoTCP,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTP,
		53,
		"HTTP/53_0",
	}
	vsUpdateCheckKey11 = CheckKey{
		seesaw.ParseIP("192.168.255.99"),
		seesaw.ParseIP("192.168.37.2"),
		53,
		seesaw.IPProtoUDP,
		seesaw.HCModeDSR,
		seesaw.HCTypeDNS,
		53,
		"DNS/53_0",
	}
	vsUpdateCheckKey12 = CheckKey{
		seesaw.ParseIP("192.168.255.99"),
		seesaw.ParseIP("192.168.37.2"),
		0,
		0,
		seesaw.HCModeDSR,
		seesaw.HCTypeHTTPS,
		16767,
		"HTTP/16767_0",
	}
	vsUpdateHealthcheck1 = config.Healthcheck{
		Name:      "HTTP/53_0",
		Mode:      seesaw.HCModeDSR,
		Type:      seesaw.HCTypeHTTP,
		Port:      53,
		Interval:  10 * time.Second,
		Timeout:   5 * time.Second,
		Send:      "foo",
		Receive:   "bar",
		Code:      200,
		TLSVerify: true,
	}
	vsUpdateHealthcheck2 = config.Healthcheck{
		Name:      "DNS/53_0",
		Mode:      seesaw.HCModeDSR,
		Type:      seesaw.HCTypeDNS,
		Port:      53,
		Interval:  2 * time.Second,
		Timeout:   1 * time.Second,
		Method:    "A",
		Send:      "dns-anycast.example.com",
		Receive:   "192.168.255.1",
		TLSVerify: true,
	}
	vsUpdateHealthcheck3 = config.Healthcheck{
		Name:      "HTTP/16767_0",
		Mode:      seesaw.HCModeDSR,
		Type:      seesaw.HCTypeHTTPS,
		Port:      16767,
		Interval:  10 * time.Second,
		Timeout:   5 * time.Second,
		Send:      "/healthz",
		Receive:   "Ok",
		Code:      200,
		TLSVerify: false,
	}
	vsUpdateHealthcheck4 = config.Healthcheck{
		Name:      "DNS/53_0",
		Mode:      seesaw.HCModeDSR,
		Type:      seesaw.HCTypeDNS,
		Port:      53,
		Interval:  2 * time.Second,
		Timeout:   1 * time.Second,
		Retries:   2,
		Method:    "A",
		Send:      "dns-anycast.example.com",
		Receive:   "192.168.255.1",
		TLSVerify: true,
	}
	vsUpdateHealthcheck5 = config.Healthcheck{
		Name:      "HTTP/16767_0",
		Mode:      seesaw.HCModeDSR,
		Type:      seesaw.HCTypeHTTPS,
		Port:      16767,
		Interval:  10 * time.Second,
		Timeout:   5 * time.Second,
		Send:      "https://dns-anycast.example.com/healthz",
		Receive:   "Ok",
		Code:      200,
		TLSVerify: false,
	}
	updateServiceKey1 = serviceKey{
		af:    seesaw.IPv4,
		port:  53,
		proto: seesaw.IPProtoUDP,
	}
	updateServiceKey2 = serviceKey{
		af:    seesaw.IPv4,
		port:  53,
		proto: seesaw.IPProtoTCP,
	}
)

var updateTests = []struct {
	desc             string
	file             string
	notifications    []*checkNotification
	expectedServices []*service
	expectedChecks   map[CheckKey]*check
	expectAllUp      bool
	expectAllDown    bool
}{
	{
		desc: "anycast initial config",
		file: "vserver_update_anycast_1.pb",
		notifications: []*checkNotification{
			{
				key:    vsUpdateCheckKey1,
				status: statusHealthy,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey1: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    true,
				active:     true,
			},
		},
		expectAllUp:   true,
		expectAllDown: false,
	},
	{
		// Add another service with the same IP. Expect the vserver to be
		// reinitialised.
		desc:          "anycast add service",
		file:          "vserver_update_anycast_2.pb",
		notifications: []*checkNotification{},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey1: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey2: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey3: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey4: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    false,
				active:     false,
			},
		},
		expectAllUp:   false,
		expectAllDown: true,
	},
	{
		// No service change, just bring up all the destinations. Expect everything
		// to be up.
		desc: "anycast destinations up",
		file: "vserver_update_anycast_2.pb",
		notifications: []*checkNotification{
			{
				key:    vsUpdateCheckKey1,
				status: statusHealthy,
			},
			{
				key:    vsUpdateCheckKey2,
				status: statusHealthy,
			},
			{
				key:    vsUpdateCheckKey4,
				status: statusHealthy,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey1: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey2: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey3: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey4: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    true,
				active:     true,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    true,
				active:     true,
			},
		},
		expectAllUp:   true,
		expectAllDown: false,
	},
	{
		// Update the service configuration's persistence value, and remove one
		// backend. Expect the services and IP to remain up.
		desc:          "anycast modify service and remove backend",
		file:          "vserver_update_anycast_3.pb",
		notifications: []*checkNotification{},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey1: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey2: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    true,
				active:     true,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.255.1"),
				healthy:    true,
				active:     true,
			},
		},
		expectAllUp:   true,
		expectAllDown: false,
	},
	{
		// Change the service's IP address. Expect the services and IP go down.
		desc:          "anycast change IP",
		file:          "vserver_update_anycast_4.pb",
		notifications: []*checkNotification{},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.99"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.255.99"),
				healthy:    false,
				active:     false,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey5: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey6: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectAllUp:   false,
		expectAllDown: true,
	},
	{
		// Change the service's healthchecks. Expect the services and IP go down.
		desc:          "anycast change healthchecks",
		file:          "vserver_update_anycast_5.pb",
		notifications: []*checkNotification{},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.99"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.255.99"),
				healthy:    false,
				active:     false,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey11: {
				healthcheck: &vsUpdateHealthcheck2,
			},
			vsUpdateCheckKey12: {
				healthcheck: &vsUpdateHealthcheck3,
			},
		},
		expectAllUp:   false,
		expectAllDown: true,
	},
	{
		// Change the service's healthchecks. Expect the services and IP go down.
		desc:          "anycast change healthchecks",
		file:          "vserver_update_anycast_6.pb",
		notifications: []*checkNotification{},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.255.99"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.255.99"),
				healthy:    false,
				active:     false,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey11: {
				healthcheck: &vsUpdateHealthcheck4,
			},
			vsUpdateCheckKey12: {
				healthcheck: &vsUpdateHealthcheck5,
			},
		},
		expectAllUp:   false,
		expectAllDown: true,
	},
	{
		// Initial config. This will also delete the anycast service.
		desc: "unicast initial config",
		file: "vserver_update_unicast_1.pb",
		notifications: []*checkNotification{
			{
				key:    vsUpdateCheckKey7,
				status: statusHealthy,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey7: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    true,
				active:     true,
			},
		},
		expectAllUp:   true,
		expectAllDown: false,
	},
	{
		// Add another service with the same IP. Expect the vserver to be
		// reinitialised.
		desc:          "unicast add service",
		file:          "vserver_update_unicast_2.pb",
		notifications: []*checkNotification{},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey7: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey8: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey9: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey10: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    false,
				active:     false,
			},
		},
		expectAllUp:   false,
		expectAllDown: false,
	},
	{
		// Disable backends. Expect the services to be down.
		desc: "unicast disable backends",
		file: "vserver_update_unicast_3.pb",
		notifications: []*checkNotification{
			{
				key:    vsUpdateCheckKey7,
				status: statusHealthy,
			},
			{
				key:    vsUpdateCheckKey8,
				status: statusHealthy,
			},
		},
		expectedChecks: map[CheckKey]*check{},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    false,
				active:     false,
			},
		},
		expectAllUp:   false,
		expectAllDown: true,
	},
	{
		// Enable a backends. Expect the services to be up.
		desc: "unicast enable backends",
		file: "vserver_update_unicast_4.pb",
		notifications: []*checkNotification{
			{
				key:    vsUpdateCheckKey7,
				status: statusHealthy,
			},
			{
				key:    vsUpdateCheckKey8,
				status: statusHealthy,
			},
		},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey7: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey8: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    true,
				active:     true,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    true,
				active:     true,
			},
		},
		expectAllUp:   true,
		expectAllDown: false,
	},
	{
		// Change one of the VserverEntries from DSR to NAT. Expect the vserver to
		// be reinitialised.
		desc:          "unicast change to NAT",
		file:          "vserver_update_unicast_5.pb",
		notifications: []*checkNotification{},
		expectedChecks: map[CheckKey]*check{
			vsUpdateCheckKey7: {
				healthcheck: &vsUpdateHealthcheck1,
			},
			vsUpdateCheckKey8: {
				healthcheck: &vsUpdateHealthcheck1,
			},
		},
		expectedServices: []*service{
			{
				serviceKey: updateServiceKey1,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    false,
				active:     false,
			},
			{
				serviceKey: updateServiceKey2,
				ip:         seesaw.ParseIP("192.168.36.1"),
				healthy:    false,
				active:     false,
			},
		},
		expectAllUp:   false,
		expectAllDown: true,
	},
}

func applyConfig(vserver *vserver, file, clusterName, serviceName string) error {
	filename := filepath.Join(testDataDir, file)
	n, err := config.ReadConfig(filename, clusterName)
	if err != nil {
		return err
	}
	config := n.Cluster.Vservers[serviceName]
	if config == nil {
		return fmt.Errorf("unable to find vserver config for %s", serviceName)
	}
	vserver.handleConfigUpdate(config)
	return nil
}

func compareChecks(got, want map[CheckKey]*check) []error {
	errs := make([]error, 0)
	if len(got) != len(want) {
		errs = append(errs, fmt.Errorf("Got %d checks, want %d", len(got), len(want)))
		return errs
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			errs = append(errs, fmt.Errorf("Expected check %v, not found", k))
			continue
		}
		if *got[k].healthcheck != *want[k].healthcheck {
			errs = append(errs, fmt.Errorf("Got check %#+v, want %#+v", *got[k].healthcheck, *want[k].healthcheck))
		}
	}
	return errs
}

func compareServiceStates(want []*service, got map[serviceKey]*service) []error {
	var errs []error
	if len(got) != len(want) {
		errs = append(errs, fmt.Errorf("Got %d services, want %d", len(got), len(want)))
	}
	for _, wantSvc := range want {
		gotSvc, ok := got[wantSvc.serviceKey]
		if !ok {
			errs = append(errs, fmt.Errorf("Got service <nil>, want %v", wantSvc))
			continue
		}
		if gotSvc.ip != wantSvc.ip {
			errs = append(errs, fmt.Errorf("Service %v.ip = %v, want %v",
				gotSvc, gotSvc.ip, wantSvc.ip))
		}
		if gotSvc.healthy != wantSvc.healthy {
			errs = append(errs, fmt.Errorf("Service %v.healthy = %v, want %v",
				gotSvc, gotSvc.healthy, wantSvc.healthy))
		}
		if gotSvc.active != wantSvc.active {
			errs = append(errs, fmt.Errorf("Service %v.active = %v, want %v",
				gotSvc, gotSvc.active, wantSvc.active))
		}
	}
	return errs
}

func TestUpdateVserver(t *testing.T) {
	vserver := newTestVserver(nil)
	clusterName := "au-syd"
	serviceName := "dns.resolver@au-syd"
	for _, test := range updateTests {
		log.Infof("Applying config: %s", test.desc)
		if err := applyConfig(vserver, test.file, clusterName, serviceName); err != nil {
			t.Fatalf("Failed to apply configuration: %v", err)
		}
		for _, n := range test.notifications {
			vserver.handleCheckNotification(n)
		}
		for _, err := range compareChecks(vserver.checks, test.expectedChecks) {
			t.Errorf("%q: %v", test.desc, err)
		}
		for _, err := range compareServiceStates(test.expectedServices, vserver.services) {
			t.Errorf("%q: %v", test.desc, err)
		}
		if test.expectAllUp {
			for _, err := range checkAllUp(vserver) {
				t.Errorf("%q: %v", test.desc, err)
			}
		}
		if test.expectAllDown {
			for _, err := range checkAllDown(vserver) {
				t.Errorf("%q: %v", test.desc, err)
			}
		}
	}
}

func TestReIPVserver(t *testing.T) {
	e := newTestEngine()
	vserver := newTestVserver(e)
	lbIF := e.lbInterface.(*dummyLBInterface)
	clusterName := "au-syd"
	serviceName := "dns.resolver@au-syd"
	tests := []struct {
		desc    string
		file    string
		health  healthcheck.State
		wantIPs []string
	}{
		{desc: "initial", file: "re-ip/config_1.pb", wantIPs: []string{"192.168.36.1"}},
		{desc: "re-ip unicast", file: "re-ip/config_2.pb", wantIPs: []string{"192.168.36.5"}},
		{desc: "unicast unhealthy", health: healthcheck.StateUnhealthy, wantIPs: []string{"192.168.36.5"}},
		{desc: "unicast disabled", file: "re-ip/config_3.pb", wantIPs: nil},
		{desc: "re-ip anycast (unhealthy)", file: "re-ip/config_4.pb", wantIPs: nil},
		{desc: "anycast health", health: healthcheck.StateHealthy, wantIPs: []string{"192.168.255.1"}},
		{desc: "anycast disabled", file: "re-ip/config_5.pb", wantIPs: nil},
		{desc: "back to unicast", file: "re-ip/config_1.pb", wantIPs: []string{"192.168.36.1"}},
	}
	for _, tc := range tests {
		log.Infof("Applying config: %s", tc.desc)
		if tc.file != "" {
			if err := applyConfig(vserver, tc.file, clusterName, serviceName); err != nil {
				t.Fatalf("Failed to apply configuration: %v", err)
			}
		}

		if tc.health != healthcheck.StateUnknown {
			for k := range vserver.checks {
				n := &checkNotification{
					key:         k,
					description: fmt.Sprintf("forced health=%v for step %q", tc.health, tc.desc),
					status:      healthcheck.Status{State: tc.health},
				}
				vserver.handleCheckNotification(n)
			}
		}

		wantVIPs := make(map[string]bool)
		for _, s := range tc.wantIPs {
			wantVIPs[s] = true
		}

		if seesaw.IsAnycast(vserver.config.IPv4Addr) {
			// vserver.vips only tracks unicast VIPs today, so we can only check that
			// there are none.
			if len(vserver.vips) > 0 {
				t.Errorf("vserver(%q).vips = %v, wanted no unicast vips", tc.desc, vserver.vips)
			}
		} else {
			gotVIPs := make(map[string]bool)
			for v := range vserver.vips {
				gotVIPs[v.IP.String()] = true
			}
			if diff := pretty.Compare(wantVIPs, gotVIPs); diff != "" {
				t.Errorf("vserver(%q).vips unexpected (-want +got):\n%s", tc.desc, diff)
			}
		}

		gotLBVS := make(map[string]bool)
		for v := range vserver.lbVservers {
			gotLBVS[v.String()] = true
		}
		if diff := pretty.Compare(wantVIPs, gotLBVS); diff != "" {
			t.Errorf("vserver(%q).lbVservers (-want +got):\n%s", tc.desc, diff)
		}

		gotIFIPs := make(map[string]bool)
		for v := range lbIF.vips {
			gotIFIPs[v.IP.String()] = true
		}
		if diff := pretty.Compare(wantVIPs, gotIFIPs); diff != "" {
			t.Errorf("After %s, unexpected IPs on the LB interface (-want +got):\n%s", tc.desc, diff)
		}
	}
}
