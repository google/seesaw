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

// This file contains structures and functions to manage the vservers within
// a running Seesaw Engine.

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/healthcheck"
	"github.com/google/seesaw/ipvs"
	ncclient "github.com/google/seesaw/ncc/client"

	log "github.com/golang/glog"
)

// vserver contains the running state for a vserver.
type vserver struct {
	engine  *Engine
	ncc     ncclient.NCC
	config  *config.Vserver
	enabled bool

	fwm        map[seesaw.AF]uint32
	services   map[serviceKey]*service
	checks     map[checkKey]*check
	active     map[seesaw.IP]bool
	lbVservers map[seesaw.IP]*seesaw.Vserver // vservers with configured iptables rules
	vips       map[seesaw.VIP]bool           // unicast VIPs

	vserverOverride seesaw.VserverOverride
	overrideChan    chan seesaw.Override

	notify  chan *checkNotification
	update  chan *config.Vserver
	quit    chan bool
	stopped chan bool
}

// newVserver returns an initialised vserver struct.
func newVserver(e *Engine) *vserver {
	return &vserver{
		engine: e,
		ncc:    ncclient.NewNCC(e.config.NCCSocket),

		fwm:        make(map[seesaw.AF]uint32),
		active:     make(map[seesaw.IP]bool),
		lbVservers: make(map[seesaw.IP]*seesaw.Vserver),
		vips:       make(map[seesaw.VIP]bool),

		overrideChan: make(chan seesaw.Override, 5),

		notify:  make(chan *checkNotification, 20),
		update:  make(chan *config.Vserver, 1),
		quit:    make(chan bool, 1),
		stopped: make(chan bool, 1),
	}
}

// String returns a string representing a vserver.
func (v *vserver) String() string {
	if v.config != nil {
		return v.config.Name
	}
	return fmt.Sprintf("Unconfigured vserver %+v", *v)
}

// serviceKey provides a unique key for a service.
type serviceKey struct {
	af    seesaw.AF
	fwm   uint32
	proto seesaw.IPProto
	port  uint16
}

// service contains the running state for a vserver service.
type service struct {
	vserver *vserver
	serviceKey
	ip      seesaw.IP // TODO(ncope): Use seesaw.VIP here.
	ventry  *config.VserverEntry
	ipvsSvc *ipvs.Service
	stats   *seesaw.ServiceStats
	dests   map[destinationKey]*destination
	healthy bool
	active  bool
}

// ipvsService returns an IPVS Service for the given service.
func (svc *service) ipvsService() *ipvs.Service {
	var flags ipvs.ServiceFlags
	if svc.ventry.Persistence > 0 {
		flags |= ipvs.SFPersistent
	}
	if svc.ventry.OnePacket {
		flags |= ipvs.SFOnePacket
	}
	var ip net.IP
	switch {
	case svc.fwm > 0 && svc.af == seesaw.IPv4:
		ip = net.IPv4zero
	case svc.fwm > 0 && svc.af == seesaw.IPv6:
		ip = net.IPv6zero
	default:
		ip = svc.ip.IP()
	}
	return &ipvs.Service{
		Address:      ip,
		Protocol:     ipvs.IPProto(svc.proto),
		Port:         svc.port,
		Scheduler:    svc.ventry.Scheduler.String(),
		FirewallMark: svc.fwm,
		Flags:        flags,
		Timeout:      uint32(svc.ventry.Persistence),
	}
}

// ipvsEqual returns true if two services have the same IPVS configuration.
// Transient state and the services' destinations are ignored.
func (s *service) ipvsEqual(other *service) bool {
	return s.ipvsSvc.Equal(*other.ipvsSvc) &&
		s.ventry.Mode == other.ventry.Mode &&
		s.ventry.LThreshold == other.ventry.LThreshold &&
		s.ventry.UThreshold == other.ventry.UThreshold
}

// String returns a string representing a service.
func (s *service) String() string {
	if s.fwm != 0 {
		return fmt.Sprintf("%v/fwm:%d", s.ip, s.fwm)
	}
	ip := s.ip.String()
	port := strconv.Itoa(int(s.port))
	return fmt.Sprintf("%v/%v", net.JoinHostPort(ip, port), s.proto)
}

// destinationKey provides a unique key for a destination.
type destinationKey struct {
	ip seesaw.IP
}

// newDestinationKey returns a new destinationKey.
func newDestinationKey(ip net.IP) destinationKey {
	return destinationKey{seesaw.NewIP(ip)}
}

// destination contains the running state for a vserver destination.
type destination struct {
	destinationKey
	service *service
	backend *seesaw.Backend
	ipvsDst *ipvs.Destination
	stats   *seesaw.DestinationStats
	weight  int32
	checks  []*check
	healthy bool
	active  bool
}

// ipvsDestination returns an IPVS Destination for the given destination.
func (dst *destination) ipvsDestination() *ipvs.Destination {
	var flags ipvs.DestinationFlags
	switch dst.service.ventry.Mode {
	case seesaw.LBModeNone:
		log.Warningf("%v: Unspecified LB mode", dst)
	case seesaw.LBModeDSR:
		flags |= ipvs.DFForwardRoute
	case seesaw.LBModeNAT:
		flags |= ipvs.DFForwardMasq
	}
	return &ipvs.Destination{
		Address:        dst.ip.IP(),
		Port:           dst.service.port,
		Weight:         dst.weight,
		Flags:          flags,
		LowerThreshold: uint32(dst.service.ventry.LThreshold),
		UpperThreshold: uint32(dst.service.ventry.UThreshold),
	}
}

// ipvsEqual returns true if two destinations have the same IPVS configuration.
// Transient state is ignored.
func (d *destination) ipvsEqual(other *destination) bool {
	return d.ipvsDst.Equal(*other.ipvsDst)
}

// String returns a string representing a destination.
func (d *destination) String() string {
	if d.service.fwm != 0 {
		return fmt.Sprintf("%v", d.ip)
	}
	ip := d.ip.String()
	port := strconv.Itoa(int(d.service.port))
	return fmt.Sprintf("%v/%v", net.JoinHostPort(ip, port), d.service.proto)
}

// name returns the name of a destination.
func (d *destination) name() string {
	return fmt.Sprintf("%v/%v", d.service.vserver.String(), d.String())
}

// checkKey provides a unique key for a check.
type checkKey struct {
	vserverIP       seesaw.IP
	backendIP       seesaw.IP
	servicePort     uint16
	serviceProtocol seesaw.IPProto
	healthcheckMode seesaw.HealthcheckMode
	healthcheckType seesaw.HealthcheckType
	healthcheckPort uint16
	name            string
}

// newCheckKey returns an initialised checkKey.
func newCheckKey(vip, bip seesaw.IP, port uint16, proto seesaw.IPProto, h *config.Healthcheck) checkKey {
	return checkKey{
		vserverIP:       vip,
		backendIP:       bip,
		servicePort:     port,
		serviceProtocol: proto,
		healthcheckMode: h.Mode,
		healthcheckType: h.Type,
		healthcheckPort: h.Port,
		name:            h.Name,
	}
}

// String returns the string representation of a checkKey.
func (c checkKey) String() string {
	return fmt.Sprintf("%v:%d/%v backend %v:%d/%v %v %v port %d (%s)",
		c.vserverIP, c.servicePort, c.serviceProtocol,
		c.backendIP, c.servicePort, c.serviceProtocol,
		c.healthcheckMode, c.healthcheckType, c.healthcheckPort, c.name)
}

// check contains the running state for a healthcheck.
type check struct {
	key         checkKey
	vserver     *vserver
	dests       []*destination
	healthcheck *config.Healthcheck
	description string
	status      healthcheck.Status
}

// newCheck returns an initialised check.
func newCheck(key checkKey, v *vserver, h *config.Healthcheck) *check {
	return &check{
		key:         key,
		vserver:     v,
		healthcheck: h,
	}
}

// checkNotification represents a healthcheck status update.
type checkNotification struct {
	key         checkKey
	description string
	status      healthcheck.Status
}

// vserverChecks represents the current set of healthchecks for a vserver.
type vserverChecks struct {
	vserverName string
	checks      map[checkKey]*check
}

// expandServices returns a list of services that have been expanded from the
// vserver configuration.
func (v *vserver) expandServices() map[serviceKey]*service {
	if v.config.UseFWM {
		return v.expandFWMServices()
	}

	svcs := make(map[serviceKey]*service)
	for _, af := range seesaw.AFs() {
		var ip net.IP
		switch af {
		case seesaw.IPv4:
			ip = v.config.Host.IPv4Addr
		case seesaw.IPv6:
			ip = v.config.Host.IPv6Addr
		}
		if ip == nil {
			continue
		}
		for _, entry := range v.config.Entries {
			svc := &service{
				serviceKey: serviceKey{
					af:    af,
					proto: entry.Proto,
					port:  entry.Port,
				},
				ip:      seesaw.NewIP(ip),
				ventry:  entry,
				vserver: v,
				dests:   make(map[destinationKey]*destination, 0),
				stats:   &seesaw.ServiceStats{},
			}
			svc.ipvsSvc = svc.ipvsService()
			svcs[svc.serviceKey] = svc
		}
	}
	return svcs
}

// expandFWMServices returns a list of services that have been expanded from the
// vserver configuration for a firewall mark based vserver.
func (v *vserver) expandFWMServices() map[serviceKey]*service {
	svcs := make(map[serviceKey]*service)
	for _, af := range seesaw.AFs() {
		var ip net.IP
		switch af {
		case seesaw.IPv4:
			ip = v.config.Host.IPv4Addr
		case seesaw.IPv6:
			ip = v.config.Host.IPv6Addr
		}
		if ip == nil {
			continue
		}

		// Persistence, etc., is stored in the VserverEntry. For FWM services, these
		// values must be the same for all VserverEntries, so just use the first
		// one.
		var ventry *config.VserverEntry
		for _, entry := range v.config.Entries {
			ventry = entry
			break
		}

		if v.fwm[af] == 0 {
			mark, err := v.engine.fwmAlloc.get()
			if err != nil {
				log.Fatalf("%v: failed to get mark: %v", v, err)
			}
			v.fwm[af] = mark
		}
		svc := &service{
			serviceKey: serviceKey{
				af:  af,
				fwm: v.fwm[af],
			},
			ip:      seesaw.NewIP(ip),
			ventry:  ventry,
			vserver: v,
			stats:   &seesaw.ServiceStats{},
		}
		svc.ipvsSvc = svc.ipvsService()
		svcs[svc.serviceKey] = svc
	}
	return svcs
}

// expandDests returns a list of destinations that have been expanded from the
// vserver configuration and a given service.
func (v *vserver) expandDests(svc *service) map[destinationKey]*destination {
	dsts := make(map[destinationKey]*destination, len(v.config.Backends))
	for _, backend := range v.config.Backends {
		var ip net.IP
		switch svc.af {
		case seesaw.IPv4:
			ip = backend.Host.IPv4Addr
		case seesaw.IPv6:
			ip = backend.Host.IPv6Addr
		}
		if ip == nil {
			continue
		}
		dst := &destination{
			destinationKey: newDestinationKey(ip),
			service:        svc,
			backend:        backend,
			weight:         backend.Weight,
		}
		dst.ipvsDst = dst.ipvsDestination()
		dst.stats = &seesaw.DestinationStats{}
		dsts[dst.destinationKey] = dst
	}
	return dsts
}

// expandChecks returns a list of checks that have been expanded from the
// vserver configuration.
func (v *vserver) expandChecks() map[checkKey]*check {
	checks := make(map[checkKey]*check)
	for _, svc := range v.services {
		for _, dest := range svc.dests {
			dest.checks = make([]*check, 0)
			if !dest.backend.Enabled {
				continue
			}
			for _, hc := range v.config.Healthchecks {
				// vserver-level healthchecks
				key := newCheckKey(svc.ip, dest.ip, 0, 0, hc)
				c := checks[key]
				if c == nil {
					c = newCheck(key, v, hc)
					checks[key] = c
				}
				dest.checks = append(dest.checks, c)
				c.dests = append(c.dests, dest)
			}

			// ventry-level healthchecks
			if v.config.UseFWM {
				for _, ve := range svc.vserver.config.Entries {
					for _, hc := range ve.Healthchecks {
						key := newCheckKey(svc.ip, dest.ip, ve.Port, ve.Proto, hc)
						c := newCheck(key, v, hc)
						checks[key] = c
						dest.checks = append(dest.checks, c)
						c.dests = append(c.dests, dest)
					}
				}
			} else {
				for _, hc := range svc.ventry.Healthchecks {
					key := newCheckKey(svc.ip, dest.ip, svc.port, svc.proto, hc)
					c := newCheck(key, v, hc)
					checks[key] = c
					dest.checks = append(dest.checks, c)
					c.dests = append(c.dests, dest)
				}
			}
		}
	}
	return checks
}

// healthchecks returns the vserverChecks for a vserver.
func (v *vserver) healthchecks() vserverChecks {
	vc := vserverChecks{vserverName: v.config.Name}
	if v.enabled {
		vc.checks = v.checks
	}
	return vc
}

// run invokes a vserver. This is a long-lived Go routine that lasts for the
// duration of the vserver, reacting to configuration changes and healthcheck
// notifications.
func (v *vserver) run() {
	statsTicker := time.NewTicker(v.engine.config.StatsInterval)
	for {
		select {
		case <-v.quit:
			// Shutdown active vservers, services and destinations.
			// There is no race between this and new healthcheck
			// notifications, since they are also handled via the
			// same vserver go routine.
			v.downAll()
			statsTicker.Stop()
			v.engine.hcManager.vcc <- vserverChecks{vserverName: v.config.Name}
			v.unconfigureVIPs()

			// Return any firewall marks that were allocated to
			// this vserver.
			for _, fwm := range v.fwm {
				v.engine.fwmAlloc.put(fwm)
			}

			v.stopped <- true
			return

		case o := <-v.overrideChan:
			v.handleOverride(o)
			v.engine.hcManager.vcc <- v.healthchecks()

		case config := <-v.update:
			v.handleConfigUpdate(config)
			v.engine.hcManager.vcc <- v.healthchecks()

		case n := <-v.notify:
			v.handleCheckNotification(n)

		case <-statsTicker.C:
			v.updateStats()
		}

		// Something changed - export a new vserver snapshot.
		// The goroutine that drains v.engine.vserverChan also does a blocking write
		// to each vserver's vserver.notify channel, which is drained by each
		// vserver's goroutine (i.e., this one). So we need a timeout to avoid a
		// deadlock.
		timeout := time.After(1 * time.Second)
		select {
		case v.engine.vserverChan <- v.snapshot():
		case <-timeout:
			log.Warningf("%v: failed to send snapshot", v)
		}
	}
}

// stop tells a running vserver that it should quit.
func (v *vserver) stop() {
	select {
	case v.quit <- true:
	default:
	}
}

// updateConfig queues a vserver configuration update for processing. This
// will block if a configuration update is already pending.
func (v *vserver) updateConfig(config *config.Vserver) {
	// TODO(jsing): Consider the implications of potentially blocking here.
	v.update <- config
}

// queueCheckNotification queues a checkNotification for processing.
func (v *vserver) queueCheckNotification(n *checkNotification) {
	// TODO(jsing): Consider the implications of potentially blocking here.
	v.notify <- n
}

// queueOverride queues an Override for processing.
func (v *vserver) queueOverride(o seesaw.Override) {
	// TODO(jsing): Consider the implications of potentially blocking here.
	v.overrideChan <- o
}

// handleConfigUpdate updates the internal structures of a vserver using the
// new configuration.
func (v *vserver) handleConfigUpdate(config *config.Vserver) {
	if config == nil {
		return
	}
	switch {
	case v.config == nil:
		v.configInit(config)
		return

	case v.enabled && !vserverEnabled(config, v.vserverOverride.State()):
		log.Infof("%v: disabling vserver", v)
		v.downAll()
		v.unconfigureVIPs()
		v.configInit(config)
		return

	case !v.enabled && !vserverEnabled(config, v.vserverOverride.State()):
		v.configInit(config)
		return

	case !v.enabled && vserverEnabled(config, v.vserverOverride.State()):
		log.Infof("%v: enabling vserver", v)
		v.configInit(config)
		return

	default:
		// Some updates require changes to the iptables rules. Since we currently
		// can't make fine-grained changes to the iptables rules, we must
		// re-initialise the entire vserver. This is necessary in the following
		// scenarios:
		// - A port or protocol is added, removed or changed
		// - A vserver is changed from FWM to non-FWM or vice versa
		// - A vserverEntry is changed from DSR to NAT or vice versa
		//
		// (Changing a VIP address also requires updating iptables, but
		//  v.configUpdate() handles this case)
		//
		// TODO(angusc): Add support for finer-grained updates to ncc so we can
		// avoid re-initialising the vserver in these cases.
		reInit := false
		switch {
		case config.UseFWM != v.config.UseFWM:
			reInit = true
		case len(config.Entries) != len(v.config.Entries):
			reInit = true
		default:
			for k, entry := range config.Entries {
				if v.config.Entries[k] == nil {
					reInit = true
					break
				}
				if v.config.Entries[k].Mode != entry.Mode {
					reInit = true
					break
				}
			}
		}
		if reInit {
			log.Infof("%v: re-initialising vserver", v)
			v.downAll()
			v.unconfigureVIPs()
			v.configInit(config)
			return
		}

		v.config = config
		v.configUpdate()
	}
}

// configInit initialises all services, destinations, healthchecks and VIPs for
// a vserver.
func (v *vserver) configInit(config *config.Vserver) {
	v.config = config
	v.enabled = vserverEnabled(config, v.vserverOverride.State())
	newSvcs := v.expandServices()
	// Preserve stats if this is a reinit
	for svcK, svc := range newSvcs {
		svc.dests = v.expandDests(svc)
		if oldSvc, ok := v.services[svcK]; ok {
			*svc.stats = *oldSvc.stats
			for dstK, dst := range svc.dests {
				if oldDst, ok := oldSvc.dests[dstK]; ok {
					*dst.stats = *oldDst.stats
				}
			}
		}
	}
	v.services = newSvcs
	v.checks = v.expandChecks()
	if v.enabled {
		v.configureVIPs()
	}
	return
}

// configUpdate updates the services, destinations, checks, and VIPs for a
// vserver.
func (v *vserver) configUpdate() {
	newSvcs := v.expandServices()
	for svcKey, newSvc := range newSvcs {
		if svc, ok := v.services[svcKey]; ok {
			if svc.ip.Equal(newSvc.ip) {
				svc.update(newSvc)
				continue
			}
			log.Infof("%v: service %v: new IP address: %v", v, svc, newSvc.ip)
			v.deleteService(svc)
		}
		log.Infof("%v: adding new service: %v", v, newSvc)
		v.services[svcKey] = newSvc
		v.updateState(newSvc.ip)
	}

	for svcKey, svc := range v.services {
		if newSvcs[svcKey] == nil {
			// Service no longer exists
			v.deleteService(svc)
			continue
		}

		// Update destinations for this service
		newDests := v.expandDests(svc)
		for destKey, newDest := range newDests {
			if dest, ok := svc.dests[destKey]; ok {
				dest.update(newDest)
				continue
			}
			log.Infof("%v: service %v: adding new destination: %v", v, svc, newDest)
			svc.dests[destKey] = newDest
		}

		for destKey, dest := range svc.dests {
			if newDests[destKey] == nil {
				// Destination no longer exists
				if dest.active {
					dest.healthy = false
					svc.updateState()
				}
				log.Infof("%v: service %v: deleting destination: %v", v, svc, dest)
				delete(svc.dests, destKey)
			}
		}
	}

	// If a VIP has been re-IP'd or has no services configured, remove the old
	// VIP from the interface.
	needVIPs := make(map[seesaw.IP]bool)
	for _, svc := range v.services {
		needVIPs[svc.ip] = true
	}
	for vip := range v.vips {
		if !needVIPs[vip.IP] {
			log.Infof("%v: unconfiguring no longer needed VIP %v", v, vip.IP)
			v.unconfigureVIP(&vip)
		}
	}

	checks := v.expandChecks()
	for k, oldCheck := range v.checks {
		if checks[k] != nil {
			checks[k].description = oldCheck.description
			checks[k].status = oldCheck.status
		}
	}
	v.checks = checks
	// TODO(baptr): Should this only happen if it's enabled?
	v.configureVIPs()
	return
}

// deleteService deletes a service for a vserver.
func (v *vserver) deleteService(s *service) {
	if s.active {
		s.healthy = false
		v.updateState(s.ip)
	}
	log.Infof("%v: deleting service: %v", v, s)
	delete(v.services, s.serviceKey)
	// TODO(baptr): Once service contains seesaw.VIP, move check and
	// unconfigureVIP here.
}

// handleCheckNotification processes a checkNotification, bringing
// destinations, services, and vservers up or down appropriately.
func (v *vserver) handleCheckNotification(n *checkNotification) {
	if !v.enabled {
		log.Infof("%v: ignoring healthcheck notification %s (vserver disabled)", v, n.description)
		return
	}

	check := v.checks[n.key]
	if check == nil {
		log.Warningf("%v: unknown check key %v", v, n.key)
		return
	}

	transition := (check.status.State != n.status.State)
	check.description = n.description
	check.status = n.status
	if transition {
		log.Infof("%v: healthcheck %s - %v (%s)", v, n.description, n.status.State, n.status.Message)
		for _, d := range check.dests {
			d.updateState()
		}
	}
}

// handleOverride processes an Override.
func (v *vserver) handleOverride(o seesaw.Override) {
	switch override := o.(type) {
	case *seesaw.VserverOverride:
		if v.vserverOverride == *override {
			// No change
			return
		}
		v.vserverOverride = *override
		if vserverEnabled(v.config, o.State()) == v.enabled {
			// enable state not changed - nothing to do
			return
		}
	// TODO(angusc): handle backend and destination overrides.
	default:
		return
	}
	if v.config != nil {
		v.handleConfigUpdate(v.config)
	}
}

// vserverEnabled returns true if a vserver having the given configuration
// and override state should be enabled.
func vserverEnabled(config *config.Vserver, os seesaw.OverrideState) bool {
	switch {
	case config == nil:
		return false
	case os == seesaw.OverrideDisable:
		return false
	case os == seesaw.OverrideEnable:
		return true
	}
	return config.Enabled
}

// snapshot exports the current running state of the vserver.
func (v *vserver) snapshot() *seesaw.Vserver {
	if v.config == nil {
		return nil
	}
	sv := &seesaw.Vserver{
		Name:    v.config.Name,
		Entries: make([]*seesaw.VserverEntry, 0, len(v.config.Entries)),
		Host: seesaw.Host{
			Hostname: v.config.Hostname,
			IPv4Addr: v.config.IPv4Addr,
			IPv4Mask: v.config.IPv4Mask,
			IPv6Addr: v.config.IPv6Addr,
			IPv6Mask: v.config.IPv6Mask,
		},
		FWM:           make(map[seesaw.AF]uint32),
		Services:      make(map[seesaw.ServiceKey]*seesaw.Service, len(v.services)),
		OverrideState: v.vserverOverride.State(),
		Enabled:       v.enabled,
		ConfigEnabled: v.config.Enabled,
		Warnings:      v.config.Warnings,
	}
	for _, ve := range v.config.Entries {
		sv.Entries = append(sv.Entries, ve.Snapshot())
	}
	sv.FWM[seesaw.IPv4] = v.fwm[seesaw.IPv4]
	sv.FWM[seesaw.IPv6] = v.fwm[seesaw.IPv6]
	for _, s := range v.services {
		ss := s.snapshot()
		sv.Services[ss.ServiceKey] = ss
	}
	return sv
}

// updateState updates the state of a destination based on the state of checks
// and propagates state changes to the service level if necessary.
func (d *destination) updateState() {
	// The destination is healthy if the backend is enabled and *all* the checks
	// for that destination are healthy.
	healthy := d.backend.Enabled
	if healthy {
		for _, c := range d.checks {
			if c.status.State != healthcheck.StateHealthy {
				healthy = false
				break
			}
		}
	}

	if d.healthy == healthy {
		return
	}
	d.healthy = healthy

	switch {
	case d.service.active && d.healthy:
		// The service is already active. Bringing up the dest will have no effect
		// on the service or vserver state.
		d.up()

	case d.service.active != d.healthy || d.service.healthy != d.healthy:
		// The service may need to be brought up or down.
		d.service.updateState()
	}
}

// up brings up a destination.
func (d *destination) up() {
	d.active = true
	log.Infof("%v: %v backend %v up", d.service.vserver, d.service, d)

	ncc := d.service.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", d.service.vserver, err)
	}
	defer ncc.Close()
	if err := ncc.IPVSAddDestination(d.service.ipvsSvc, d.ipvsDst); err != nil {
		log.Fatalf("%v: failed to add destination %v: %v", d.service.vserver, d, err)
	}
}

// down takes down a destination.
func (d *destination) down() {
	d.active = false
	log.Infof("%v: %v backend %v down", d.service.vserver, d.service, d)

	ncc := d.service.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", d.service.vserver, err)
	}
	defer ncc.Close()
	if err := ncc.IPVSDeleteDestination(d.service.ipvsSvc, d.ipvsDst); err != nil {
		log.Fatalf("%v: failed to delete destination %v: %v", d.service.vserver, d, err)
	}
}

// update updates a destination while preserving its running state.
func (d *destination) update(dest *destination) {
	if d.destinationKey != dest.destinationKey {
		log.Fatalf("%v: can't update destination %v using %v: destinationKey mismatch: %v != %v",
			d.service.vserver, d, dest, d.destinationKey, dest.destinationKey)
	}
	log.Infof("%v: %v updating destination %v", d.service.vserver, d.service, d)

	updateIPVS := d.active && !d.ipvsEqual(dest)

	dest.active = d.active
	dest.healthy = d.healthy
	dest.stats = d.stats
	*d = *dest

	if !d.healthy {
		return
	}

	if !d.backend.Enabled {
		log.Infof("%v: %v disabling destination %v", d.service.vserver, d.service, d)
		d.healthy = false
		d.service.updateState()
		return
	}

	if !updateIPVS {
		return
	}

	log.Infof("%v: %v updating IPVS destination %v", d.service.vserver, d.service, d)
	ncc := d.service.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", d.service.vserver, err)
	}
	defer ncc.Close()

	if err := ncc.IPVSUpdateDestination(d.service.ipvsSvc, d.ipvsDst); err != nil {
		log.Fatalf("%v: failed to update destination %v: %v", d.service.vserver, d, err)
	}
}

// snapshot exports the current running state of a destination.
func (d *destination) snapshot() *seesaw.Destination {
	return &seesaw.Destination{
		Backend:     d.backend,
		Name:        d.name(),
		VserverName: d.service.vserver.String(),
		Stats:       d.stats,
		Enabled:     d.backend.Enabled,
		Weight:      d.weight,
		Healthy:     d.healthy,
		Active:      d.active,
	}
}

// updateState updates the state of a service based on the state of its
// destinations and propagates state changes to the vserver level if necessary.
func (s *service) updateState() {
	// The service is considered healthy if:
	// 1) No watermarks are configured, and at least one destination is healthy.
	// OR
	// 2) (Num healthy dests) / (Num backends) >= high watermark.
	// OR
	// 3) Service is already healthy, and
	//    (Num healthy dests) / (Num backends) >= low watermark.

	numBackends := 0
	numHealthyDests := 0
	for _, d := range s.dests {
		if d.backend.InService {
			numBackends++
		}
		if d.healthy {
			numHealthyDests++
		}
	}

	threshold := s.ventry.LowWatermark
	if !s.healthy {
		threshold = s.ventry.HighWatermark
	}

	var healthy bool
	switch {
	case numBackends == 0 || numHealthyDests == 0:
		healthy = false
	case threshold == 0.0:
		healthy = numHealthyDests >= 1
	default:
		healthy = float32(numHealthyDests)/float32(numBackends) >= threshold
	}

	if s.healthy == healthy {
		// no change in service state, just update destinations
		s.updateDests()
		return
	}

	oldHealth := "unhealthy"
	newHealth := "unhealthy"
	if s.healthy {
		oldHealth = "healthy"
	}
	if healthy {
		newHealth = "healthy"
	}
	log.Infof("%v: %v: %d/%d destinations are healthy, service was %v, now %v",
		s.vserver, s, numHealthyDests, numBackends, oldHealth, newHealth)
	s.healthy = healthy
	vserverActive := s.vserver.active[s.ip]

	switch {
	case vserverActive && s.healthy:
		// The vserver is already active. Bringing up the service will have no
		// effect on the vserver state.
		s.up()

	case vserverActive != s.healthy:
		// The vserver may need to be brought up or down.
		s.vserver.updateState(s.ip)
	}
}

// updateDests brings the destinations for a service up or down based on the
// state of the service and the health of each destination.
func (s *service) updateDests() {
	for _, d := range s.dests {
		if !s.active {
			d.stats.DestinationStats = &ipvs.DestinationStats{}
			if d.active {
				d.down()
			}
			continue
		}
		switch {
		case !d.healthy && d.active:
			d.down()
		case d.healthy && !d.active:
			d.up()
		}
	}
}

// up brings up a service and all healthy destinations.
func (s *service) up() {
	s.active = true
	log.Infof("%v: %v service up", s.vserver, s)

	ncc := s.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", s.vserver, err)
	}
	defer ncc.Close()

	log.Infof("%v: adding IPVS service %v", s.vserver, s.ipvsSvc)
	if err := ncc.IPVSAddService(s.ipvsSvc); err != nil {
		log.Fatalf("%v: failed to add service %v: %v", s.vserver, s, err)
	}

	// Update destinations *after* the IPVS service exists.
	s.updateDests()
}

// down takes down all destinations for a service, then takes down the
// service.
func (s *service) down() {
	s.active = false
	s.stats.ServiceStats = &ipvs.ServiceStats{}
	log.Infof("%v: %v service down", s.vserver, s)

	ncc := s.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", s.vserver, err)
	}
	defer ncc.Close()

	// Remove IPVS destinations *before* the IPVS service is removed.
	s.updateDests()

	if err := ncc.IPVSDeleteService(s.ipvsSvc); err != nil {
		log.Fatalf("%v: failed to delete service %v: %v", s.vserver, s, err)
	}
}

// update updates a service while preserving its running state.
func (s *service) update(svc *service) {
	if s.serviceKey != svc.serviceKey {
		log.Fatalf("%v: can't update service %v using %v: serviceKey mismatch: %v != %v",
			s.vserver, s, svc, s.serviceKey, svc.serviceKey)
	}
	log.Infof("%v: updating service %v", s.vserver, s)

	updateIPVS := s.active && !s.ipvsEqual(svc)
	svc.active = s.active
	svc.healthy = s.healthy
	svc.stats = s.stats
	svc.dests = s.dests
	*s = *svc

	if !updateIPVS {
		return
	}

	log.Infof("%v: %v updating IPVS service", s.vserver, s)
	ncc := s.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", s.vserver, err)
	}
	defer ncc.Close()

	if err := ncc.IPVSUpdateService(s.ipvsSvc); err != nil {
		log.Fatalf("%v: failed to update service %v: %v", s.vserver, s, err)
	}
}

// updateStats updates the IPVS statistics for this service.
func (s *service) updateStats() {
	if !s.active {
		return
	}
	log.V(1).Infof("%v: updating IPVS statistics for %v", s.vserver, s)

	ncc := s.vserver.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", s.vserver, err)
	}
	defer ncc.Close()

	ipvsSvc, err := ncc.IPVSGetService(s.ipvsSvc)
	if err != nil {
		log.Warningf("%v: failed to get statistics for %v: %v", s.vserver, s, err)
		return
	}
	s.stats.ServiceStats = ipvsSvc.Statistics

	for _, ipvsDst := range ipvsSvc.Destinations {
		found := false
		for _, d := range s.dests {
			if d.ipvsDst.Address.Equal(ipvsDst.Address) &&
				d.ipvsDst.Port == ipvsDst.Port {
				d.stats.DestinationStats = ipvsDst.Statistics
				found = true
				break
			}
		}
		if !found {
			log.Warningf("%v: got statistics for unknown destination %v", s.vserver, ipvsDst)
		}
	}
}

// snapshot exports the current running state of a service.
func (s *service) snapshot() *seesaw.Service {
	ss := &seesaw.Service{
		ServiceKey: seesaw.ServiceKey{
			AF:    s.af,
			Proto: s.proto,
			Port:  s.port,
		},
		Mode:          s.ventry.Mode,
		Scheduler:     s.ventry.Scheduler,
		OnePacket:     s.ventry.OnePacket,
		Persistence:   s.ventry.Persistence,
		IP:            s.ip.IP(),
		Healthy:       s.healthy,
		Enabled:       s.vserver.enabled,
		Active:        s.active,
		Stats:         s.stats,
		Destinations:  make(map[string]*seesaw.Destination),
		LowWatermark:  s.ventry.LowWatermark,
		HighWatermark: s.ventry.HighWatermark,
	}
	for _, d := range s.dests {
		sd := d.snapshot()
		ss.Destinations[sd.Backend.Hostname] = sd
	}

	numBackends := 0
	numHealthyDests := 0
	for _, d := range s.dests {
		if d.backend.InService {
			numBackends++
		}
		if d.healthy {
			numHealthyDests++
		}
	}
	if numBackends > 0 {
		ss.CurrentWatermark = float32(numHealthyDests) / float32(numBackends)
	}

	return ss
}

// updateState updates the state of an IP for a vserver based on the state of
// that IP's services.
func (v *vserver) updateState(ip seesaw.IP) {
	// A vserver anycast IP is healthy if *all* services for that IP are healthy.
	// A vserver unicast IP is healthy if *any* services for that IP are healthy.
	var healthy bool
	for _, s := range v.services {
		if !s.ip.Equal(ip) {
			continue
		}
		healthy = s.healthy
		if !healthy && seesaw.IsAnycast(ip.IP()) {
			break
		}
		if healthy && !seesaw.IsAnycast(ip.IP()) {
			break
		}
	}

	if v.active[ip] == healthy {
		v.updateServices(ip)
		return
	}
	switch {
	case !healthy && v.active[ip]:
		v.down(ip)
	case healthy && !v.active[ip]:
		v.up(ip)
	}
}

// updateServices brings the services for a vserver up or down based on the
// state of the vserver and the health of each service.
func (v *vserver) updateServices(ip seesaw.IP) {
	for _, s := range v.services {
		if !s.ip.Equal(ip) {
			continue
		}
		if !v.active[ip] {
			if s.active {
				s.down()
			}
			continue
		}
		switch {
		case !s.healthy && s.active:
			s.down()
		case s.healthy && !s.active:
			s.up()
		}
	}
}

// up brings up all healthy services for an IP address for a vserver, then
// brings up the IP address.
func (v *vserver) up(ip seesaw.IP) {
	ncc := v.engine.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", v, err)
	}
	defer ncc.Close()

	v.active[ip] = true
	v.updateServices(ip)

	// If this is an anycast VIP, start advertising a BGP route.
	nip := ip.IP()
	if seesaw.IsAnycast(nip) {
		// TODO(jsing): Create an LBVserver that only encapsulates
		// the necessary state, rather than storing a full vserver
		// snapshot.
		lbVserver := v.snapshot()
		lbVserver.Services = nil
		lbVserver.Warnings = nil
		if err := v.engine.lbInterface.AddVserver(lbVserver, ip.AF()); err != nil {
			log.Fatalf("%v: failed to add Vserver: %v", v, err)
		}
		v.lbVservers[ip] = lbVserver

		vip := seesaw.NewVIP(nip, nil)
		if err := v.engine.lbInterface.AddVIP(vip); err != nil {
			log.Fatalf("%v: failed to add VIP %v: %v", v, ip, err)
		}
		// TODO(angusc): Filter out anycast VIPs for non-anycast clusters further
		// upstream.
		if v.engine.config.AnycastEnabled {
			log.Infof("%v: advertising BGP route for %v", v, ip)
			if err := ncc.BGPAdvertiseVIP(nip); err != nil {
				log.Fatalf("%v: failed to advertise VIP %v: %v", v, ip, err)
			}
		} else {
			log.Warningf("%v: %v is an anycast VIP, but anycast is not enabled", v, ip)
		}
	}

	log.Infof("%v: VIP %v up", v, ip)
}

// downAll takes down all IP addresses and services for a vserver.
func (v *vserver) downAll() {
	for _, s := range v.services {
		if v.active[s.ip] {
			v.down(s.ip)
		}
		if s.active {
			s.down()
		}
	}
}

// down takes down an IP address for a vserver, then takes down all services
// for that IP address.
func (v *vserver) down(ip seesaw.IP) {
	ncc := v.engine.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", v, err)
	}
	defer ncc.Close()

	// If this is an anycast VIP, withdraw the BGP route.
	nip := ip.IP()
	if seesaw.IsAnycast(nip) {
		if v.engine.config.AnycastEnabled {
			log.Infof("%v: withdrawing BGP route for %v", v, ip)
			if err := ncc.BGPWithdrawVIP(nip); err != nil {
				log.Fatalf("%v: failed to withdraw VIP %v: %v", v, ip, err)
			}
		}
		vip := seesaw.NewVIP(nip, nil)
		if err := v.engine.lbInterface.DeleteVIP(vip); err != nil {
			log.Fatalf("%v: failed to remove VIP %v: %v", v, ip, err)
		}
		if err := v.engine.lbInterface.DeleteVserver(v.lbVservers[ip], ip.AF()); err != nil {
			log.Fatalf("%v: failed to delete Vserver: %v", v, err)
		}
		delete(v.lbVservers, ip)
	}
	// TODO(jsing): Should we delay while the BGP routes propagate?

	delete(v.active, ip)
	v.updateServices(ip)
	log.Infof("%v: VIP %v down", v, ip)
}

// updateStats updates the IPVS statistics for this vserver.
func (v *vserver) updateStats() {
	for _, s := range v.services {
		s.updateStats()
	}
}

// configureVIPs configures VIPs on the load balancing interface.
func (v *vserver) configureVIPs() {
	ncc := v.engine.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", v, err)
	}
	defer ncc.Close()

	// TODO(ncope): Return to iterating over v.services once they contain seesaw.VIPs.
	for _, vip := range v.config.VIPs {
		if _, ok := v.vips[*vip]; ok {
			continue
		}
		// TODO(angusc): Set up anycast Vservers here as well (without bringing up the VIP).
		if vip.Type == seesaw.AnycastVIP {
			continue
		}

		// TODO(jsing): Create a ncc.LBVserver that only encapsulates
		// the necessary state, rather than storing a full vserver
		// snapshot.
		lbVserver := v.snapshot()
		lbVserver.Services = nil
		lbVserver.Warnings = nil
		if err := v.engine.lbInterface.AddVserver(lbVserver, vip.IP.AF()); err != nil {
			log.Fatalf("%v: failed to add Vserver: %v", v, err)
		}
		v.lbVservers[vip.IP] = lbVserver

		if err := v.engine.lbInterface.AddVIP(vip); err != nil {
			log.Fatalf("%v: failed to add VIP %v: %v", v, vip.IP, err)
		}
		v.vips[*vip] = true
	}
}

// unconfigureVIP removes a unicast VIP from the load balancing interface.
func (v *vserver) unconfigureVIP(vip *seesaw.VIP) {
	configured, ok := v.vips[*vip]
	if !ok {
		return
	}
	if configured {
		ncc := v.engine.ncc
		if err := ncc.Dial(); err != nil {
			log.Fatalf("%v: failed to connect to NCC: %v", v, err)
		}
		defer ncc.Close()

		if err := v.engine.lbInterface.DeleteVIP(vip); err != nil {
			log.Fatalf("%v: failed to remove VIP %v: %v", v, vip, err)
		}
		if err := v.engine.lbInterface.DeleteVserver(v.lbVservers[vip.IP], vip.IP.AF()); err != nil {
			log.Fatalf("%v: failed to delete Vserver: %v", v, err)
		}
		delete(v.lbVservers, vip.IP)
	}
	delete(v.vips, *vip)
}

// unconfigureVIPs removes unicast VIPs from the load balancing interface.
func (v *vserver) unconfigureVIPs() {
	ncc := v.engine.ncc
	if err := ncc.Dial(); err != nil {
		log.Fatalf("%v: failed to connect to NCC: %v", v, err)
	}
	defer ncc.Close()

	// TODO(jsing): At a later date this will need to support VLAN
	// interfaces and dedicated VIP subnets.
	for vip := range v.vips {
		v.unconfigureVIP(&vip)
	}
}
