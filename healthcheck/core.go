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

// Package healthcheck implements backend and service healthchecks.
package healthcheck

import (
	"encoding/gob"
	"fmt"
	"math/rand"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"

	log "github.com/golang/glog"
)

const engineTimeout = 10 * time.Second

func init() {
	rand.Seed(time.Now().UnixNano())

	gob.Register(&DNSChecker{})
	gob.Register(&HTTPChecker{})
	gob.Register(&PingChecker{})
	gob.Register(&RADIUSChecker{})
	gob.Register(&TCPChecker{})
	gob.Register(&UDPChecker{})
}

// Id provides the unique identifier of a given healthcheck.
type Id uint64

// State represents the current state of a healthcheck.
type State int

const (
	StateUnknown State = iota
	StateUnhealthy
	StateHealthy
)

var stateNames = map[State]string{
	StateUnknown:   "Unknown",
	StateUnhealthy: "Unhealthy",
	StateHealthy:   "Healthy",
}

// String returns the string representation for the given healthcheck state.
func (s State) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return "<unknown>"
}

// Checker is the interface that must be implemented by a healthcheck.
type Checker interface {
	Check(timeout time.Duration) *Result
	String() string
}

// Target specifies the target for a healthcheck.
type Target struct {
	IP    net.IP // IP address of the healthcheck target.
	Host  net.IP // Host to run the healthcheck against.
	Mark  int
	Mode  seesaw.HealthcheckMode
	Port  int
	Proto seesaw.IPProto
}

// String returns the string representation of a healthcheck target.
func (t Target) String() string {
	var via string
	if t.Mode == seesaw.HCModeDSR {
		via = fmt.Sprintf(" (via %s mark %d)", t.Host, t.Mark)
	}
	return fmt.Sprintf("%s %s%s", t.addr(), t.Mode, via)
}

// addr returns the address string for the healthcheck target.
func (t *Target) addr() string {
	if t.IP.To4() != nil {
		return fmt.Sprintf("%v:%d", t.IP, t.Port)
	}
	return fmt.Sprintf("[%v]:%d", t.IP, t.Port)
}

// network returns the network name for the healthcheck target.
func (t *Target) network() string {
	version := 4
	if t.IP.To4() == nil {
		version = 6
	}

	var network string
	switch t.Proto {
	case seesaw.IPProtoICMP:
		network = "ip4:icmp"
	case seesaw.IPProtoICMPv6:
		network = "ip6:ipv6-icmp"
	case seesaw.IPProtoTCP:
		network = fmt.Sprintf("tcp%d", version)
	case seesaw.IPProtoUDP:
		network = fmt.Sprintf("udp%d", version)
	default:
		return "(unknown)"
	}

	return network
}

// Result stores the result of a healthcheck performed by a checker.
type Result struct {
	Message string
	Success bool
	time.Duration
	Err error
}

// String returns the string representation of a healthcheck result.
func (r *Result) String() string {
	if r.Err != nil {
		return r.Err.Error()
	}
	return r.Message
}

// complete returns a Result for a completed healthcheck.
func complete(start time.Time, msg string, success bool, err error) *Result {
	// TODO(jsing): Make this clock skew safe.
	duration := time.Since(start)
	return &Result{msg, success, duration, err}
}

// Notification stores a status notification for a healthcheck.
type Notification struct {
	Id
	Status
}

// String returns the string representation for the given notification.
func (n *Notification) String() string {
	return fmt.Sprintf("ID 0x%x %v", n.Id, n.State)
}

// HealthState contains data for a healthcheck state IPC.
type HealthState struct {
	Ctx           *ipc.Context
	Notifications []*Notification
}

// Checks provides a map of healthcheck configurations.
type Checks struct {
	Configs map[Id]*Config
}

// Config contains the configuration for a healthcheck.
type Config struct {
	Id
	Interval time.Duration
	Timeout  time.Duration
	Retries  int
	Checker
}

// NewConfig returns an initialised Config.
func NewConfig(id Id, checker Checker) *Config {
	return &Config{
		Id:       id,
		Interval: 5 * time.Second,
		Timeout:  30 * time.Second,
		Retries:  0,
		Checker:  checker,
	}
}

// Status represents the current status of a healthcheck instance.
type Status struct {
	LastCheck time.Time
	Duration  time.Duration
	Failures  uint64
	Successes uint64
	State
	Message string
}

// Check represents a healthcheck instance.
type Check struct {
	Config

	lock      sync.RWMutex
	blocking  bool
	start     time.Time
	failed    uint64
	failures  uint64
	successes uint64
	state     State
	result    *Result

	update chan Config
	notify chan<- *Notification
	quit   chan bool
}

// NewCheck returns an initialised Check.
func NewCheck(notify chan<- *Notification) *Check {
	return &Check{
		state:  StateUnknown,
		notify: notify,
		update: make(chan Config, 1),
		quit:   make(chan bool, 1),
	}
}

// Status returns the current status for this healthcheck instance.
func (hc *Check) Status() Status {
	hc.lock.RLock()
	defer hc.lock.RUnlock()
	status := Status{
		LastCheck: hc.start,
		Failures:  hc.failures,
		Successes: hc.successes,
		State:     hc.state,
	}
	if hc.result != nil {
		status.Duration = hc.result.Duration
		status.Message = hc.result.String()
	}
	return status
}

// Run invokes a healthcheck. It waits for the initial configuration to be
// provided via the configuration channel, after which the configured
// healthchecker is invoked at the given interval. If a new configuration
// is provided the healthchecker is updated and checks are scheduled at the
// new interval. Notifications are generated and sent via the notification
// channel whenever a state transition occurs. Run will terminate once a
// value is received on the quit channel.
func (hc *Check) Run(start <-chan time.Time) {

	// Wait for initial configuration.
	select {
	case config := <-hc.update:
		hc.Config = config
	case <-hc.quit:
		return
	}

	// Wait for a tick to avoid a thundering herd at startup and to
	// stagger healthchecks that have the same interval.
	if start != nil {
		<-start
	}
	log.Infof("Starting healthchecker for %d (%s)", hc.Id, hc)

	ticker := time.NewTicker(hc.Interval)
	hc.healthcheck()
	for {
		select {
		case <-hc.quit:
			ticker.Stop()
			log.Infof("Stopping healthchecker for %d (%s)", hc.Id, hc)
			return

		case config := <-hc.update:
			if hc.Interval != config.Interval {
				ticker.Stop()
				if start != nil {
					<-start
				}
				ticker = time.NewTicker(config.Interval)
			}
			hc.Config = config

		case <-ticker.C:
			hc.healthcheck()
		}
	}
}

// healthcheck executes the given checker.
func (hc *Check) healthcheck() {
	if hc.Checker == nil {
		return
	}
	start := time.Now()
	result := hc.execute()

	status := "SUCCESS"
	if !result.Success {
		status = "FAILURE"
	}
	log.Infof("%d: (%s) %s: %v", hc.Id, hc, status, result)

	hc.lock.Lock()

	hc.start = start
	hc.result = result

	var state State
	if result.Success {
		state = StateHealthy
		hc.failed = 0
		hc.successes++
	} else {
		hc.failed++
		hc.failures++
		state = StateUnhealthy
	}

	if hc.state == StateHealthy && hc.failed > 0 && hc.failed <= uint64(hc.Config.Retries) {
		log.Infof("%d: Failure %d - retrying...", hc.Id, hc.failed)
		state = StateHealthy
	}
	transition := (hc.state != state)
	hc.state = state

	hc.lock.Unlock()

	if transition {
		hc.Notify()
	}
}

// Notify generates a healthcheck notification for this checker.
func (hc *Check) Notify() {
	hc.notify <- &Notification{
		Id:     hc.Id,
		Status: hc.Status(),
	}
}

// execute invokes the given healthcheck checker with the configured timeout.
func (hc *Check) execute() *Result {
	ch := make(chan *Result, 1)
	checker := hc.Checker
	go func() {
		// TODO(jsing): Determine a way to ensure that this go routine
		// does not linger.
		ch <- checker.Check(hc.Timeout)
	}()
	select {
	case result := <-ch:
		return result
	case <-time.After(hc.Timeout):
		return &Result{"Timed out", false, hc.Timeout, nil}
	}
}

// Stop notifies a running healthcheck that it should quit.
func (hc *Check) Stop() {
	select {
	case hc.quit <- true:
	default:
	}
}

// Blocking enables or disables blocking updates for a healthcheck.
func (hc *Check) Blocking(block bool) {
	len := 0
	if !block {
		len = 1
	}
	hc.blocking = block
	hc.update = make(chan Config, len)
}

// Update queues a healthcheck configuration update for processing.
func (hc *Check) Update(config *Config) {
	if hc.blocking {
		hc.update <- *config
		return
	}
	select {
	case hc.update <- *config:
	default:
		log.Warningf("Unable to update %d (%s), last update still queued", hc.Id, hc)
	}
}

// ServerConfig specifies the configuration for a healthcheck server.
type ServerConfig struct {
	BatchDelay     time.Duration
	BatchSize      int
	ChannelSize    int
	EngineSocket   string
	MaxFailures    int
	NotifyInterval time.Duration
	RetryDelay     time.Duration
}

var defaultServerConfig = ServerConfig{
	BatchDelay:     100 * time.Millisecond,
	BatchSize:      100,
	ChannelSize:    1000,
	EngineSocket:   seesaw.EngineSocket,
	MaxFailures:    10,
	NotifyInterval: 15 * time.Second,
	RetryDelay:     2 * time.Second,
}

// DefaultServerConfig returns the default server configuration.
func DefaultServerConfig() ServerConfig {
	return defaultServerConfig
}

// Server contains the data needed to run a healthcheck server.
type Server struct {
	config *ServerConfig

	healthchecks map[Id]*Check
	configs      chan map[Id]*Config
	notify       chan *Notification
	batch        []*Notification

	quit chan bool
}

// NewServer returns an initialised healthcheck server.
func NewServer(cfg *ServerConfig) *Server {
	if cfg == nil {
		defaultCfg := DefaultServerConfig()
		cfg = &defaultCfg
	}
	return &Server{
		config: cfg,

		healthchecks: make(map[Id]*Check),
		notify:       make(chan *Notification, cfg.ChannelSize),
		configs:      make(chan map[Id]*Config),
		batch:        make([]*Notification, 0, cfg.BatchSize),

		quit: make(chan bool, 1),
	}
}

// Shutdown notifies a healthcheck server to shutdown.
func (s *Server) Shutdown() {
	select {
	case s.quit <- true:
	default:
	}
}

// Run runs a healthcheck server.
func (s *Server) Run() {
	go s.updater()
	go s.notifier()
	go s.manager()

	<-s.quit
}

// getHealthchecks attempts to get the current healthcheck configurations from
// the Seesaw Engine.
func (s *Server) getHealthchecks() (*Checks, error) {
	engineConn, err := net.DialTimeout("unix", s.config.EngineSocket, engineTimeout)
	if err != nil {
		return nil, fmt.Errorf("Dial failed: %v", err)
	}
	engineConn.SetDeadline(time.Now().Add(engineTimeout))
	engine := rpc.NewClient(engineConn)
	defer engine.Close()

	var checks Checks
	ctx := ipc.NewTrustedContext(seesaw.SCHealthcheck)
	if err := engine.Call("SeesawEngine.Healthchecks", ctx, &checks); err != nil {
		return nil, fmt.Errorf("SeesawEngine.Healthchecks failed: %v", err)
	}

	return &checks, nil
}

// updater attempts to fetch healthcheck configurations at regular intervals.
// When configurations are successfully retrieved they are provided to the
// manager via the configs channel.
func (s *Server) updater() {
	for {
		log.Info("Getting healthchecks from engine...")
		checks, err := s.getHealthchecks()
		if err != nil {
			log.Error(err)
			time.Sleep(5 * time.Second)
		} else {
			log.Infof("Engine returned %d healthchecks", len(checks.Configs))
			s.configs <- checks.Configs
			time.Sleep(15 * time.Second)
		}
	}
}

// manager is responsible for controlling the healthchecks that are currently
// running. When healthcheck configurations become available, the manager will
// stop and remove deleted healthchecks, spawn new healthchecks and provide
// the current configurations to each of the running healthchecks.
func (s *Server) manager() {
	checkTicker := time.NewTicker(50 * time.Millisecond)
	notifyTicker := time.NewTicker(s.config.NotifyInterval)
	for {
		select {
		case configs := <-s.configs:

			// Remove healthchecks that have been deleted.
			for id, hc := range s.healthchecks {
				if configs[id] == nil {
					hc.Stop()
					delete(s.healthchecks, id)
				}
			}

			// Spawn new healthchecks.
			for id := range configs {
				if s.healthchecks[id] == nil {
					hc := NewCheck(s.notify)
					s.healthchecks[id] = hc
					go hc.Run(checkTicker.C)
				}
			}

			// Update configurations.
			for id, hc := range s.healthchecks {
				hc.Update(configs[id])
			}
		case <-notifyTicker.C:
			// Send status notifications for all healthchecks.
			for _, hc := range s.healthchecks {
				hc.Notify()
			}
		}
	}
}

// notifier batches healthcheck notifications and sends them to the Seesaw
// Engine.
func (s *Server) notifier() {
	var timer <-chan time.Time
	for {
		var err error
		select {
		case notification := <-s.notify:
			s.batch = append(s.batch, notification)
			// Collect until BatchDelay passes, or BatchSize are queued.
			switch len(s.batch) {
			case 1:
				timer = time.After(s.config.BatchDelay)
			case s.config.BatchSize:
				err = s.send()
				timer = nil
			}

		case <-timer:
			err = s.send()
		}

		if err != nil {
			log.Fatal(err)
		}
	}
}

// send sends a batch of notifications to the Seesaw Engine, retrying on any
// error and giving up after MaxFailures.
func (s *Server) send() error {
	failures := 0
	for err := s.sendBatch(s.batch); err != nil; {
		log.Errorf("Send failed: %v", err)
		failures++
		if failures > s.config.MaxFailures {
			return fmt.Errorf("send: %d errors, giving up", failures)
		}
		time.Sleep(s.config.RetryDelay)
	}
	s.batch = s.batch[:0]
	return nil
}

// sendBatch sends a batch of notifications to the Seesaw Engine.
func (s *Server) sendBatch(batch []*Notification) error {
	engineConn, err := net.DialTimeout("unix", s.config.EngineSocket, engineTimeout)
	if err != nil {
		return err
	}
	engineConn.SetDeadline(time.Now().Add(engineTimeout))
	engine := rpc.NewClient(engineConn)
	defer engine.Close()

	var reply int
	ctx := ipc.NewTrustedContext(seesaw.SCHealthcheck)
	return engine.Call("SeesawEngine.HealthState", &HealthState{ctx, batch}, &reply)
}
