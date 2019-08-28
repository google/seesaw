package ecu

import (
	"errors"
	"fmt"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/google/seesaw/common/conn"
	"github.com/google/seesaw/common/ipc"
	"github.com/google/seesaw/common/seesaw"
)

// statsCache caches seesaw stats and limits QPS to backends.
type statsCache struct {
	lock sync.RWMutex

	staleThreashold time.Duration
	engineSocket    string

	haTime time.Time
	ha     *seesaw.HAStatus
}

func newStatsCache(engineSocket string, staleThreashold time.Duration) *statsCache {
	return &statsCache{
		engineSocket:    engineSocket,
		staleThreashold: staleThreashold,
	}
}

// Caller must hold s.lock
func (s *statsCache) refreshHA() error {
	s.ha = nil
	defer func() { s.haTime = time.Now() }()
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
	log.V(1).Infof("refreshed HAStatus: %#v", ha)
	return nil
}

// getHA returns seesaw HAStatus. May refresh it based if it's older than staleThreashold.
func (s *statsCache) getHA() (*seesaw.HAStatus, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	age := time.Now().Sub(s.haTime)
	if age > s.staleThreashold {
		if err := s.refreshHA(); err != nil {
			return nil, err
		}
	}
	if s.ha == nil {
		return nil, errors.New("last refresh failed. Retry after stale")
	}
	return s.ha, nil
}
