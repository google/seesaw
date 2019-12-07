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
		return fmt.Errorf("failed to get HA status: %v", err)
	}
	s.ha = ha

	cs, err := seesawConn.ConfigStatus()
	if err != nil {
		return fmt.Errorf("failed to get config status: %v", err)
	}
	s.cs = cs

	return nil
}

// getHAStatus returns seesaw HAStatus. May refresh it based if it's older than staleThreashold.
func (s *statsCache) getHAStatus() (*seesaw.HAStatus, error) {
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

// getConfigStatus returns ConfigStatus. May refresh it based if it's older than staleThreashold.
func (s *statsCache) getConfigStatus() (*seesaw.ConfigStatus, error) {
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
