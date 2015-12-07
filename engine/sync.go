// Copyright 2013 Google Inc. All Rights Reserved.
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

// This file contains structures and functions to manage synchronisation
// between Seesaw nodes.

import (
	"errors"
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/healthcheck"

	log "github.com/golang/glog"
)

// TODO(jsing): Consider implementing message authentication.

const (
	sessionDeadtime       = 2 * time.Minute
	sessionNotesQueueSize = 100

	syncHeartbeatInterval = 5 * time.Second

	syncPollMsgLimit = 100
	syncPollTimeout  = 30 * time.Second
)

// SyncSessionID specifies a synchronisation session identifier.
type SyncSessionID uint64

// SyncNoteType specifies the type of a synchronisation notification.
type SyncNoteType int

const (
	SNTHeartbeat SyncNoteType = iota
	SNTDesync
	SNTConfigUpdate
	SNTHealthcheck
	SNTOverride
)

var syncNoteTypeNames = map[SyncNoteType]string{
	SNTHeartbeat:    "Heartbeat",
	SNTDesync:       "Desynchronisation",
	SNTConfigUpdate: "Config Update",
	SNTHealthcheck:  "Healthcheck",
	SNTOverride:     "Override",
}

// String returns the string representation of a synchronisation notification
// type.
func (snt SyncNoteType) String() string {
	if name, ok := syncNoteTypeNames[snt]; ok {
		return name
	}
	return fmt.Sprintf("(Unknown %d)", snt)
}

// SyncNote represents a synchronisation notification.
type SyncNote struct {
	Type SyncNoteType

	Config      *config.Notification
	Healthcheck *healthcheck.Notification

	BackendOverride     *seesaw.BackendOverride
	DestinationOverride *seesaw.DestinationOverride
	VserverOverride     *seesaw.VserverOverride
}

// SyncNotes specifies a collection of SyncNotes.
type SyncNotes struct {
	Notes []SyncNote
}

// SeesawSync provides the synchronisation RPC interface to the Seesaw Engine.
type SeesawSync struct {
	sync *syncServer
}

// Register registers our Seesaw peer for synchronisation notifications.
func (s *SeesawSync) Register(node net.IP, id *SyncSessionID) error {
	if id == nil {
		return errors.New("id is nil")
	}

	// TODO(jsing): Reject if not master?

	s.sync.sessionLock.Lock()
	session := newSyncSession(node, s.sync.nextSessionID)
	s.sync.nextSessionID++
	s.sync.sessions[session.id] = session
	s.sync.sessionLock.Unlock()

	session.Lock()
	session.expiryTime = time.Now().Add(sessionDeadtime)
	session.Unlock()

	*id = session.id

	log.Infof("Synchronisation session %d registered by %v", *id, node)

	return nil
}

// Deregister deregisters a Seesaw peer for synchronisation.
func (s *SeesawSync) Deregister(id SyncSessionID, reply *int) error {
	s.sync.sessionLock.Lock()
	session, ok := s.sync.sessions[id]
	delete(s.sync.sessions, id)
	s.sync.sessionLock.Unlock()

	if ok {
		log.Infof("Synchronisation session %d deregistered with %v", id, session.node)
	}

	return nil
}

// Poll returns one or more synchronisation notifications to the caller,
// blocking until at least one notification becomes available or the poll
// timeout is reached.
func (s *SeesawSync) Poll(id SyncSessionID, sn *SyncNotes) error {
	if sn == nil {
		return errors.New("sync notes is nil")
	}

	s.sync.sessionLock.RLock()
	session, ok := s.sync.sessions[id]
	s.sync.sessionLock.RUnlock()
	if !ok {
		return errors.New("no session with ID %d")
	}

	// Reset expiry time and check for desynchronisation.
	session.Lock()
	session.expiryTime = time.Now().Add(sessionDeadtime)
	if session.desync {
		// TODO(jsing): Discard pending notes?
		sn.Notes = append(sn.Notes, SyncNote{Type: SNTDesync})
		session.desync = false
		session.Unlock()
		return nil
	}
	session.Unlock()

	// Block until a notification becomes available or our poll expires.
	select {
	case note := <-session.notes:
		sn.Notes = append(sn.Notes, *note)
	case <-time.After(syncPollTimeout):
		return errors.New("poll timeout")
	}
pollLoop:
	for i := 0; i < syncPollMsgLimit; i++ {
		select {
		case note := <-session.notes:
			sn.Notes = append(sn.Notes, *note)
		default:
			break pollLoop
		}
	}

	log.V(1).Infof("Sync server poll returning %d notifications", len(sn.Notes))

	return nil
}

// Config requests the current configuration from the peer Seesaw node.
func (s *SeesawSync) Config(arg int, config *config.Notification) error {
	return errors.New("unimplemented")
}

// Failover requests that we relinquish master state.
func (s *SeesawSync) Failover(arg int, reply *int) error {
	return s.sync.engine.haManager.requestFailover(true)
}

// Healthchecks requests the current healthchecks from the peer Seesaw node.
func (s *SeesawSync) Healthchecks(arg int, reply *int) error {
	return errors.New("unimplemented")
}

// syncSession contains the data needed for a synchronisation session.
type syncSession struct {
	id         SyncSessionID
	node       net.IP
	desync     bool
	startTime  time.Time
	expiryTime time.Time
	sync.RWMutex

	notes chan *SyncNote
}

// newSyncSession returns an initialised synchronisation session.
func newSyncSession(node net.IP, id SyncSessionID) *syncSession {
	return &syncSession{
		id:         id,
		node:       node,
		desync:     true,
		startTime:  time.Now(),
		expiryTime: time.Now().Add(sessionDeadtime),
		notes:      make(chan *SyncNote, sessionNotesQueueSize),
	}
}

// addNote adds a notification to the synchronisation session. If the notes
// channel is full the session is marked as desynchronised and the notification
// is discarded.
func (ss *syncSession) addNote(note *SyncNote) {
	select {
	case ss.notes <- note:
	default:
		ss.Lock()
		if !ss.desync {
			log.Warningf("Sync session with %v is desynchronised", ss.node)
			ss.desync = true
		}
		ss.Unlock()
	}
}

// syncServer encapsulates the data for a synchronisation server.
type syncServer struct {
	engine            *Engine
	heartbeatInterval time.Duration
	server            *rpc.Server

	sessionLock   sync.RWMutex
	nextSessionID SyncSessionID
	sessions      map[SyncSessionID]*syncSession
}

// newSyncServer returns an initalised synchronisation server.
func newSyncServer(e *Engine) *syncServer {
	return &syncServer{
		engine:            e,
		heartbeatInterval: syncHeartbeatInterval,
		sessions:          make(map[SyncSessionID]*syncSession),
	}
}

// serve accepts connections from the given TCP listener and dispatches each
// connection to the RPC server. Connections are only accepted from localhost
// and the seesaw node that we are configured to peer with.
func (s *syncServer) serve(l *net.TCPListener) error {
	defer l.Close()

	s.server = rpc.NewServer()
	s.server.Register(&SeesawSync{s})

	for {
		c, err := l.AcceptTCP()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		raddr := c.RemoteAddr().String()
		host, _, err := net.SplitHostPort(raddr)
		if err != nil {
			log.Errorf("Failed to parse remote address %q: %v", raddr, err)
			c.Close()
			continue
		}
		rip := net.ParseIP(host)
		if rip == nil || (!rip.IsLoopback() && !rip.Equal(s.engine.config.Peer.IPv4Addr) && !rip.Equal(s.engine.config.Peer.IPv6Addr)) {
			log.Warningf("Rejecting connection from non-peer (%s)...", rip)
			c.Close()
			continue
		}
		log.Infof("Sync connection established from %s", rip)
		go s.server.ServeConn(c)
	}
}

// notify queues a synchronisation notification with each of the active
// synchronisation sessions.
func (s *syncServer) notify(sn *SyncNote) {
	s.sessionLock.RLock()
	sessions := s.sessions
	s.sessionLock.RUnlock()
	for _, ss := range sessions {
		ss.addNote(sn)
	}
}

// run runs the synchronisation server, which is responsible for queueing
// heartbeat notifications and removing expired synchronisation sessions.
func (s *syncServer) run() error {
	heartbeat := time.NewTicker(s.heartbeatInterval)
	for {
		select {
		case <-heartbeat.C:
			now := time.Now()
			s.sessionLock.Lock()
			for id, ss := range s.sessions {
				ss.RLock()
				expiry := ss.expiryTime
				ss.RUnlock()
				if now.After(expiry) {
					log.Warningf("Sync session %d with %v has expired", id, ss.node)
					delete(s.sessions, id)
					continue
				}
				ss.addNote(&SyncNote{Type: SNTHeartbeat})
			}
			s.sessionLock.Unlock()
		}
	}
}

// syncClient contains the data needed by a synchronisation client.
type syncClient struct {
	engine   *Engine
	dispatch func(*SyncNote)

	conn    *net.TCPConn
	client  *rpc.Client
	enabled bool
	refs    uint
	lock    sync.Mutex

	quit    chan bool
	start   chan bool
	stopped chan bool
}

// newSyncClient returns an initialised synchronisation client.
func newSyncClient(e *Engine) *syncClient {
	sc := &syncClient{
		engine:  e,
		quit:    make(chan bool),
		start:   make(chan bool),
		stopped: make(chan bool, 1),
	}
	sc.dispatch = sc.handleNote
	sc.stopped <- true
	return sc
}

// dial establishes a connection to our peer Seesaw node.
func (sc *syncClient) dial() error {
	sc.lock.Lock()
	defer sc.lock.Unlock()
	if sc.client != nil {
		sc.refs++
		return nil
	}

	// TODO(jsing): Make this default to IPv6, if configured.
	peer := &net.TCPAddr{
		IP:   sc.engine.config.Peer.IPv4Addr,
		Port: sc.engine.config.SyncPort,
	}
	self := &net.TCPAddr{
		IP: sc.engine.config.Node.IPv4Addr,
	}
	conn, err := net.DialTCP("tcp", self, peer)
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	sc.client = rpc.NewClient(conn)
	sc.refs = 1
	return nil
}

// close closes an existing connection to our peer Seesaw node.
func (sc *syncClient) close() error {
	sc.lock.Lock()
	defer sc.lock.Unlock()
	if sc.client == nil {
		return nil
	}
	sc.refs--
	if sc.refs > 0 {
		return nil
	}
	if err := sc.client.Close(); err != nil {
		sc.client = nil
		return fmt.Errorf("client close failed: %v", err)
	}
	sc.client = nil
	return nil
}

// failover requests that the peer node initiate a failover.
func (sc *syncClient) failover() error {
	if err := sc.dial(); err != nil {
		return err
	}
	defer sc.close()
	if err := sc.client.Call("SeesawSync.Failover", 0, nil); err != nil {
		return err
	}
	return nil
}

// runOnce establishes a connection to the synchronisation server, registers
// for notifications, polls for notifications, then deregisters.
func (sc *syncClient) runOnce() {
	if err := sc.dial(); err != nil {
		log.Warningf("Sync client dial failed: %v", err)
		return
	}
	defer sc.close()

	var sid SyncSessionID
	self := sc.engine.config.Node.IPv4Addr

	// Register for synchronisation events.
	// TODO(jsing): Implement timeout on RPC?
	if err := sc.client.Call("SeesawSync.Register", self, &sid); err != nil {
		log.Warningf("Sync registration failed: %v", err)
		return
	}
	log.Infof("Registered for synchronisation notifications (ID %d)", sid)

	// TODO(jsing): Export synchronisation data to ECU/CLI.

	sc.poll(sid)

	// Attempt to deregister for notifications.
	// TODO(jsing): Implement timeout on RPC?
	if err := sc.client.Call("SeesawSync.Deregister", sid, nil); err != nil {
		log.Warningf("Sync deregistration failed: %v", err)
	}
}

// poll polls the synchronisation server for notifications, then dispatches
// them for processing.
func (sc *syncClient) poll(sid SyncSessionID) {
	for {
		var sn SyncNotes
		poll := sc.client.Go("SeesawSync.Poll", sid, &sn, nil)
		select {
		case <-poll.Done:
			if poll.Error != nil {
				log.Errorf("Synchronisation polling failed: %v", poll.Error)
				return
			}
			for _, note := range sn.Notes {
				sc.dispatch(&note)
			}

		case <-sc.quit:
			sc.stopped <- true
			return

		case <-time.After(syncPollTimeout):
			log.Warningf("Synchronisation polling timed out after %s", syncPollTimeout)
			return
		}
	}
}

// handleNote dispatches a synchronisation note to the appropriate handler.
func (sc *syncClient) handleNote(note *SyncNote) {
	switch note.Type {
	case SNTHeartbeat:
		log.V(1).Infoln("Sync client received heartbeat")

	case SNTDesync:
		sc.handleDesync()

	case SNTConfigUpdate:
		sc.handleConfigUpdate(note)

	case SNTHealthcheck:
		sc.handleHealthcheck(note)

	case SNTOverride:
		sc.handleOverride(note)

	default:
		log.Errorf("Unable to handle sync notification type %s (%d)", note.Type, note.Type)
	}
}

// handleDesync handles a desync notification.
func (sc *syncClient) handleDesync() {
	log.V(1).Infoln("Sync client desynchronised...")
	// TODO(jsing): Fetch all state - config, healthchecks, overrides...
}

// handleConfigUpdate handles a config update notification.
func (sc *syncClient) handleConfigUpdate(sn *SyncNote) {
	log.V(1).Infoln("Sync client received config update notification")
	// TODO(jsing): Implement.
}

// handleHealthcheck handles a healthcheck notification.
func (sc *syncClient) handleHealthcheck(sn *SyncNote) {
	log.V(1).Infoln("Sync client received healthcheck notification")
	// TODO(jsing): Implement.
}

// handleOverride handles an override notification.
func (sc *syncClient) handleOverride(sn *SyncNote) {
	log.V(1).Infoln("Sync client received override notification")

	if o := sn.VserverOverride; o != nil {
		sc.engine.queueOverride(o)
	}
	if o := sn.DestinationOverride; o != nil {
		sc.engine.queueOverride(o)
	}
	if o := sn.BackendOverride; o != nil {
		sc.engine.queueOverride(o)
	}
}

// run runs the synchronisation client.
func (sc *syncClient) run() error {
	for {
		select {
		case <-sc.stopped:
			<-sc.start
			log.Infof("Starting sync client...")
		default:
			sc.runOnce()
			select {
			case <-time.After(5 * time.Second):
			case <-sc.quit:
				sc.stopped <- true
			}
		}
	}
}

// enable enables synchronisation with our peer Seesaw node.
func (sc *syncClient) enable() {
	sc.lock.Lock()
	start := !sc.enabled
	sc.enabled = true
	sc.lock.Unlock()
	if start {
		sc.start <- true
	}
}

// disable disables synchronisation with our peer Seesaw node.
func (sc *syncClient) disable() {
	sc.lock.Lock()
	quit := sc.enabled
	sc.enabled = false
	sc.lock.Unlock()
	if quit {
		sc.quit <- true
	}
}
