package ecu

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	log "github.com/golang/glog"
	"github.com/google/seesaw/common/seesaw"
)

type healthzServer struct {
	stats  *statsCache
	server *http.Server
}

func newHealthzServer(address string, stats *statsCache) *healthzServer {
	s := &healthzServer{
		stats: stats,
	}
	handler := http.NewServeMux()
	handler.HandleFunc("/healthz", s.handle)
	s.server = &http.Server{
		Addr:         address,
		Handler:      handler,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}
	return s
}

func (s *healthzServer) isReady() (bool, error) {
	ha, err := s.stats.getHA()
	if err != nil {
		return false, err
	}
	return ha.State == seesaw.HABackup || ha.State == seesaw.HAMaster, nil
}

func (s *healthzServer) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	if r.Method != http.MethodGet {
		status := http.StatusMethodNotAllowed
		http.Error(w, fmt.Sprintf("%d %s", status, http.StatusText(status)), status)
		return
	}

	isHealth, err := s.isReady()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch readiness: %v", err), http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	text := "Ok"
	if !isHealth {
		status = http.StatusInternalServerError
		text = "Unhealthy"
	}

	w.WriteHeader(status)
	io.WriteString(w, text+"\n")
}

func (s *healthzServer) shutdown() {
	if err := s.server.Shutdown(context.Background()); err != nil {
		log.Errorf("healthzServer.Shutdown failed: %s", err)
	}
}

func (s *healthzServer) run() {
	log.Info("serving /healthz")
	if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
		log.Errorf("healthzServer ListenAndServe failed: %s", err)
	}
	log.Infof("/healthz shutdown")
}
