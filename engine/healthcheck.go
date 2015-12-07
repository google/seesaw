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

// This file contains structs and functions to manage communications with the
// healthcheck component.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/healthcheck"
	"github.com/google/seesaw/ipvs"
	ncclient "github.com/google/seesaw/ncc/client"

	log "github.com/golang/glog"
)

const (
	channelSize int = 1000

	dsrMarkBase = 1 << 16
	dsrMarkSize = 16000
)

// healthcheckManager manages the healthcheck configuration for a Seesaw Engine.
type healthcheckManager struct {
	engine *Engine
	ncc    ncclient.NCC

	markAlloc     *markAllocator
	marks         map[seesaw.IP]uint32
	next          healthcheck.Id
	vserverChecks map[string]map[checkKey]*check // keyed by vserver name

	cfgs    map[healthcheck.Id]*healthcheck.Config
	checks  map[healthcheck.Id]*check
	ids     map[checkKey]healthcheck.Id
	enabled bool
	lock    sync.RWMutex // Guards cfgs, checks, enabled and ids.

	quit    chan bool
	stopped chan bool
	vcc     chan vserverChecks
}

// newHealthcheckManager creates a new healthcheckManager.
func newHealthcheckManager(e *Engine) *healthcheckManager {
	return &healthcheckManager{
		engine:        e,
		marks:         make(map[seesaw.IP]uint32),
		markAlloc:     newMarkAllocator(dsrMarkBase, dsrMarkSize),
		ncc:           ncclient.NewNCC(e.config.NCCSocket),
		next:          healthcheck.Id((uint64(os.Getpid()) & 0xFFFF) << 48),
		vserverChecks: make(map[string]map[checkKey]*check),
		quit:          make(chan bool),
		stopped:       make(chan bool),
		vcc:           make(chan vserverChecks, 100),
	}
}

// configs returns the healthcheck Configs for a Seesaw Engine. The returned
// map should only be read, not mutated. If the healthcheckManager is disabled,
// then nil is returned.
func (h *healthcheckManager) configs() map[healthcheck.Id]*healthcheck.Config {
	h.lock.RLock()
	defer h.lock.RUnlock()
	if !h.enabled {
		return nil
	}
	return h.cfgs
}

// update updates the healthchecks for a vserver.
func (h *healthcheckManager) update(vserverName string, checks map[checkKey]*check) {
	if checks == nil {
		delete(h.vserverChecks, vserverName)
	} else {
		h.vserverChecks[vserverName] = checks
	}
	h.buildMaps()
}

// enable enables the healthcheck manager for the Seesaw Engine.
func (h *healthcheckManager) enable() {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.enabled = true
}

// disable disables the healthcheck manager for the Seesaw Engine.
func (h *healthcheckManager) disable() {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.enabled = false
}

// shutdown requests the healthcheck manager to shutdown.
func (h *healthcheckManager) shutdown() {
	h.quit <- true
	<-h.stopped
}

// buildMaps builds the cfgs, checks, and ids maps based on the vserverChecks.
func (h *healthcheckManager) buildMaps() {
	allChecks := make(map[checkKey]*check)
	for _, vchecks := range h.vserverChecks {
		for k, c := range vchecks {
			if allChecks[k] == nil {
				allChecks[k] = c
			} else {
				log.Warningf("Duplicate key: %v", k)
			}
		}
	}

	h.lock.RLock()
	ids := h.ids
	cfgs := h.cfgs
	checks := h.checks
	h.lock.RUnlock()
	newIDs := make(map[checkKey]healthcheck.Id)
	newCfgs := make(map[healthcheck.Id]*healthcheck.Config)
	newChecks := make(map[healthcheck.Id]*check)

	for key, c := range allChecks {
		id, ok := ids[key]
		if !ok {
			id = h.next
			h.next++
		}

		// Create a new healthcheck configuration if one did not
		// previously exist, or if the check configuration changed.
		cfg, ok := cfgs[id]
		if !ok || *checks[id].healthcheck != *c.healthcheck {
			newCfg, err := h.newConfig(id, key, c.healthcheck)
			if err != nil {
				log.Error(err)
				continue
			}
			cfg = newCfg
		}

		newIDs[key] = id
		newCfgs[id] = cfg
		newChecks[id] = c
	}

	h.lock.Lock()
	h.ids = newIDs
	h.cfgs = newCfgs
	h.checks = newChecks
	h.lock.Unlock()

	h.pruneMarks()
}

// healthState handles Notifications from the healthcheck component.
func (h *healthcheckManager) healthState(n *healthcheck.Notification) error {
	log.V(1).Infof("Received healthcheck notification: %v", n)

	h.lock.RLock()
	enabled := h.enabled
	h.lock.RUnlock()

	if !enabled {
		log.Warningf("Healthcheck manager is disabled; ignoring healthcheck notification %v", n)
		return nil
	}

	h.engine.syncServer.notify(&SyncNote{Type: SNTHealthcheck, Healthcheck: n})

	return h.queueHealthState(n)
}

// queueHealthState queues a health state Notification for processing by a
// vserver.
func (h *healthcheckManager) queueHealthState(n *healthcheck.Notification) error {
	h.lock.RLock()
	cfg := h.cfgs[n.Id]
	check := h.checks[n.Id]
	h.lock.RUnlock()

	if cfg == nil || check == nil {
		log.Warningf("Unknown healthcheck ID %v", n.Id)
		return nil
	}

	note := &checkNotification{
		key:         check.key,
		description: cfg.Checker.String(),
		status:      n.Status,
	}
	check.vserver.queueCheckNotification(note)

	return nil
}

func (h *healthcheckManager) newConfig(id healthcheck.Id, key checkKey, hc *config.Healthcheck) (*healthcheck.Config, error) {
	host := key.backendIP.IP()
	port := int(hc.Port)
	mark := 0

	// For DSR we use the VIP address as the target and specify a mark for
	// the backend.
	ip := host
	if key.healthcheckMode == seesaw.HCModeDSR {
		ip = key.vserverIP.IP()
		mark = int(h.markBackend(key.backendIP))
	}

	var checker healthcheck.Checker
	var target *healthcheck.Target
	switch hc.Type {
	case seesaw.HCTypeDNS:
		dns := healthcheck.NewDNSChecker(ip, port)
		target = &dns.Target
		queryType, err := healthcheck.DNSType(hc.Method)
		if err != nil {
			return nil, err
		}
		dns.Answer = hc.Receive
		dns.Question.Name = hc.Send
		dns.Question.Qtype = queryType

		checker = dns
	case seesaw.HCTypeHTTP:
		http := healthcheck.NewHTTPChecker(ip, port)
		target = &http.Target
		if hc.Send != "" {
			http.Request = hc.Send
		}
		if hc.Receive != "" {
			http.Response = hc.Receive
		}
		if hc.Code != 0 {
			http.ResponseCode = hc.Code
		}
		http.Proxy = hc.Proxy
		if hc.Method != "" {
			http.Method = hc.Method
		}
		checker = http
	case seesaw.HCTypeHTTPS:
		https := healthcheck.NewHTTPChecker(ip, port)
		target = &https.Target
		if hc.Send != "" {
			https.Request = hc.Send
		}
		if hc.Receive != "" {
			https.Response = hc.Receive
		}
		if hc.Code != 0 {
			https.ResponseCode = hc.Code
		}
		https.Secure = true
		https.TLSVerify = hc.TLSVerify
		https.Proxy = hc.Proxy
		if hc.Method != "" {
			https.Method = hc.Method
		}
		checker = https
	case seesaw.HCTypeICMP:
		// DSR cannot be used with ICMP (at least for now).
		if key.healthcheckMode != seesaw.HCModePlain {
			return nil, errors.New("ICMP healthchecks cannot be used with DSR mode")
		}
		ping := healthcheck.NewPingChecker(ip)
		target = &ping.Target
		checker = ping
	case seesaw.HCTypeRADIUS:
		radius := healthcheck.NewRADIUSChecker(ip, port)
		target = &radius.Target
		// TODO(jsing): Ugly hack since we do not currently have
		// separate protobuf messages for each healthcheck type...
		send := strings.Split(hc.Send, ":")
		if len(send) != 3 {
			return nil, errors.New("RADIUS healthcheck has invalid send value")
		}
		radius.Username = send[0]
		radius.Password = send[1]
		radius.Secret = send[2]
		if hc.Receive != "" {
			radius.Response = hc.Receive
		}
		checker = radius
	case seesaw.HCTypeTCP:
		tcp := healthcheck.NewTCPChecker(ip, port)
		target = &tcp.Target
		tcp.Send = hc.Send
		tcp.Receive = hc.Receive
		checker = tcp
	case seesaw.HCTypeTCPTLS:
		tcp := healthcheck.NewTCPChecker(ip, port)
		target = &tcp.Target
		tcp.Send = hc.Send
		tcp.Receive = hc.Receive
		tcp.Secure = true
		tcp.TLSVerify = hc.TLSVerify
		checker = tcp
	case seesaw.HCTypeUDP:
		udp := healthcheck.NewUDPChecker(ip, port)
		target = &udp.Target
		udp.Send = hc.Send
		udp.Receive = hc.Receive
		checker = udp
	default:
		return nil, fmt.Errorf("Unknown healthcheck type: %v", hc.Type)
	}

	target.Host = host
	target.Mark = mark
	target.Mode = hc.Mode

	hcc := healthcheck.NewConfig(id, checker)
	hcc.Interval = hc.Interval
	hcc.Timeout = hc.Timeout
	hcc.Retries = hc.Retries

	return hcc, nil
}

// run runs the healthcheck manager and processes incoming vserver checks.
func (h *healthcheckManager) run() {
	for {
		select {
		case <-h.quit:
			h.unmarkAllBackends()
			h.stopped <- true
		case vc := <-h.vcc:
			h.update(vc.vserverName, vc.checks)
		}
	}
}

// expire invalidates the state of all configured healthchecks.
func (h *healthcheckManager) expire() {
	h.lock.RLock()
	ids := h.ids
	h.lock.RUnlock()

	status := healthcheck.Status{State: healthcheck.StateUnknown}
	for _, id := range ids {
		h.queueHealthState(&healthcheck.Notification{id, status})
	}
}

// markBackend returns a mark for the specified backend and sets up the IPVS
// service entry if it does not exist.
func (h *healthcheckManager) markBackend(backend seesaw.IP) uint32 {
	mark, ok := h.marks[backend]
	if ok {
		return mark
	}

	mark, err := h.markAlloc.get()
	if err != nil {
		log.Fatalf("Failed to get mark: %v", err)
	}
	h.marks[backend] = mark

	ip := net.IPv6zero
	if backend.AF() == seesaw.IPv4 {
		ip = net.IPv4zero
	}

	ipvsSvc := &ipvs.Service{
		Address:      ip,
		Protocol:     ipvs.IPProto(0),
		Port:         0,
		Scheduler:    "rr",
		FirewallMark: mark,
		Destinations: []*ipvs.Destination{
			{
				Address: backend.IP(),
				Port:    0,
				Weight:  1,
				Flags:   ipvs.DFForwardRoute,
			},
		},
	}

	if err := h.ncc.Dial(); err != nil {
		log.Fatalf("Failed to connect to NCC: %v", err)
	}
	defer h.ncc.Close()

	log.Infof("Adding DSR IPVS service for %s (mark %d)", backend, mark)
	if err := h.ncc.IPVSAddService(ipvsSvc); err != nil {
		log.Fatalf("Failed to add IPVS service for DSR: %v", err)
	}

	return mark
}

// unmarkBackend removes the mark for a given backend and removes the IPVS
// service entry if it exists.
func (h *healthcheckManager) unmarkBackend(backend seesaw.IP) {
	mark, ok := h.marks[backend]
	if !ok {
		return
	}

	ip := net.IPv6zero
	if backend.AF() == seesaw.IPv4 {
		ip = net.IPv4zero
	}

	ipvsSvc := &ipvs.Service{
		Address:      ip,
		Protocol:     ipvs.IPProto(0),
		Port:         0,
		Scheduler:    "rr",
		FirewallMark: mark,
		Destinations: []*ipvs.Destination{
			{
				Address: backend.IP(),
				Port:    0,
				Weight:  1,
				Flags:   ipvs.DFForwardRoute,
			},
		},
	}

	if err := h.ncc.Dial(); err != nil {
		log.Fatalf("Failed to connect to NCC: %v", err)
	}
	defer h.ncc.Close()

	log.Infof("Removing DSR IPVS service for %s (mark %d)", backend, mark)
	if err := h.ncc.IPVSDeleteService(ipvsSvc); err != nil {
		log.Fatalf("Failed to remove DSR IPVS service: %v", err)
	}

	delete(h.marks, backend)
	h.markAlloc.put(mark)
}

// pruneMarks unmarks backends that no longer have DSR healthchecks configured.
func (h *healthcheckManager) pruneMarks() {
	h.lock.RLock()
	checks := h.checks
	h.lock.RUnlock()

	backends := make(map[seesaw.IP]bool)
	for _, check := range checks {
		if check.key.healthcheckMode != seesaw.HCModeDSR {
			continue
		}
		backends[check.key.backendIP] = true
	}

	for ip := range h.marks {
		if _, ok := backends[ip]; !ok {
			h.unmarkBackend(ip)
		}
	}
}

// unmarkAllBackends unmarks all backends that were previously marked.
func (h *healthcheckManager) unmarkAllBackends() {
	for ip := range h.marks {
		h.unmarkBackend(ip)
	}
}
