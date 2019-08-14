package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"
	"time"

	log "github.com/golang/glog"
)

// GetServer implements a config server that allow get of configs.
type GetServer struct {
	p          *Provider
	shutdownWG sync.WaitGroup
	getServer  *http.Server
}

// NewGetServer returns a GetServer
func NewGetServer(p *Provider, serverCerts tls.Certificate, configPort int) *GetServer {
	s := &GetServer{
		p: p,
	}

	getHandler := http.NewServeMux()
	getHandler.HandleFunc("/config/", func(w http.ResponseWriter, req *http.Request) {
		s.serveGet(w, req)
	})
	// getServer binds to localhost
	s.getServer = &http.Server{
		Addr:         fmt.Sprintf("127.0.0.1:%d", configPort),
		Handler:      getHandler,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 3 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{serverCerts},
			ClientAuth:   tls.NoClientCert,
		},
	}

	return s
}

const (
	contentType   = "Content-Type"
	pbContentType = "application/x-protobuffer"
)

func (s *GetServer) serveGet(w http.ResponseWriter, req *http.Request) {
	log.V(1).Infof("received /config request")
	if req.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}

	raw, err := s.p.get()
	if err != nil {
		http.Error(w, fmt.Sprintf("Unable to marshal config: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set(contentType, pbContentType)
	if _, err := w.Write(raw); err != nil {
		log.Warningf("w.Write errored: %v", err)
		return
	}
}

// Shutdown turns off the server
func (s *GetServer) Shutdown() {
	if err := s.getServer.Shutdown(context.Background()); err != nil {
		log.Errorf("getServer.Shutdown failed: %s", err)
	}
	s.shutdownWG.Wait()
}

func (s *GetServer) Run() {
	s.shutdownWG.Add(1)
	defer s.shutdownWG.Done()
	log.Infof("serving /config")
	if err := s.getServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
		log.Errorf("GetServer ListenAndServe failed: %s", err)
	}
	log.Infof("/config shutdown")
}
