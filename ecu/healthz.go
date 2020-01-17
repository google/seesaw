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
	sc     *statsCache
	server *http.Server
}

func newHealthzServer(address string, sc *statsCache) *healthzServer {
	s := &healthzServer{
		sc: sc,
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

func (s *healthzServer) isAlive() (bool, error) {
	ha, err := s.sc.GetHAStatus()
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

	isHealth, err := s.isAlive()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to fetch readiness: %v", err), http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	text := "Ok"
	if !isHealth {
		status = http.StatusServiceUnavailable
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
