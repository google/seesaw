package ecu

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
)

// statsCache caches seesaw stats and limits QPS to backends.
type statsCache struct {
	lock sync.RWMutex

	staleThreashold time.Duration
	engineSocket    string

	lastRefresh time.Time

	ha *seesaw.HAStatus
	cs *seesaw.ConfigStatus
	vs map[string]*seesaw.Vserver
	es *seesaw.EngineStatus
}

func newStatsCache(engineSocket string, staleThreashold time.Duration) *statsCache {
	return &statsCache{
		engineSocket:    engineSocket,
		staleThreashold: staleThreashold,
	}
}

// Caller must hold s.lock
func (s *statsCache) refreshIfNeeded() error {
	age := time.Now().Sub(s.lastRefresh)
	if age <= s.staleThreashold {
		return nil
	}

	s.ha = nil
	s.cs = nil
	s.vs = nil
	s.es = nil

	defer func() { s.lastRefresh = time.Now() }()
	ctx := ipc.NewTrustedContext(seesaw.SCECU)
	seesawConn, err := conn.NewSeesawIPC(ctx)
	if err != nil {
		return fmt.Errorf("Failed to connect to engine: %v", err)
	}
	if err := seesawConn.Dial(s.engineSocket); err != nil {
		return fmt.Errorf("Failed to connect to engine: %v", err)
	}
	defer seesawConn.Close()

	ha, err := seesawConn.HAStatus()
	if err != nil {
		return fmt.Errorf("failed to get ha status: %v", err)
	}
	s.ha = ha

	cs, err := seesawConn.ConfigStatus()
	if err != nil {
		return fmt.Errorf("failed to get config status: %v", err)
	}
	s.cs = cs

	vs, err := seesawConn.Vservers()
	if err != nil {
		return fmt.Errorf("failed to get vservers: %v", err)
	}
	s.vs = vs

	es, err := seesawConn.EngineStatus()
	if err != nil {
		return fmt.Errorf("failed to get engine status: %v", err)
	}
	s.es = es

	return nil
}

// GetHAStatus returns seesaw HAStatus. May refresh it based if it's older than staleThreashold.
func (s *statsCache) GetHAStatus() (*seesaw.HAStatus, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.refreshIfNeeded(); err != nil {
		return nil, err
	}

	if s.ha == nil {
		return nil, errors.New("last refresh failed. Retry after stale")
	}
	return s.ha, nil
}

// GetConfigStatus returns ConfigStatus. May refresh it based if it's older than staleThreashold.
func (s *statsCache) GetConfigStatus() (*seesaw.ConfigStatus, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.refreshIfNeeded(); err != nil {
		return nil, err
	}

	if s.cs == nil {
		return nil, errors.New("last refresh failed. Retry after stale")
	}
	return s.cs, nil
}

// GetVservers returns all Vservers. May refresh it based if it's older than staleThreashold.
func (s *statsCache) GetVservers() (map[string]*seesaw.Vserver, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.refreshIfNeeded(); err != nil {
		return nil, err
	}

	if s.vs == nil {
		return nil, errors.New("last refresh failed. Retry after stale")
	}
	return s.vs, nil
}

// GetEngineStatus returns seesaw EngineStatus. May refresh it based if it's older than staleThreashold.
func (s *statsCache) GetEngineStatus() (*seesaw.EngineStatus, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	if err := s.refreshIfNeeded(); err != nil {
		return nil, err
	}

	if s.es == nil {
		return nil, errors.New("last refresh failed. Retry after stale")
	}
	return s.es, nil
}
