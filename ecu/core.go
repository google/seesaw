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

// Package ecu implements the Seesaw v2 ECU component, which provides
// an externally accessible interface to monitor and control the
// Seesaw Node.
package ecu

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ecu/prom"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	log "github.com/golang/glog"
	ecupb "github.com/google/seesaw/pb/ecu"
)

var defaultConfig = Config{
	Authenticator:  DefaultAuthenticator{},
	EngineSocket:   seesaw.EngineSocket,
	HealthzAddress: ":20256",
	MonitorAddress: ":20257",
	ControlAddress: ":20258",
	CACertsFile:    "/etc/seesaw/ssl/ca/cert.pem",
	ECUCertFile:    "/etc/seesaw/ssl/ecu/cert.pem",
	ECUKeyFile:     "/etc/seesaw/ssl/ecu/key.pem",
}

// Config provides configuration details for a Seesaw ECU.
type Config struct {
	Authenticator  Authenticator
	CACertsFile    string
	ControlAddress string
	ECUCertFile    string
	ECUKeyFile     string
	EngineSocket   string
	MonitorAddress string
	UpdateInterval time.Duration
	HealthzAddress string
}

// DefaultConfig returns the default ECU configuration.
func DefaultConfig() Config {
	return defaultConfig
}

// ECU contains the data necessary to run the Seesaw v2 ECU.
type ECU struct {
	cfg             *Config
	shutdown        chan bool
	shutdownControl chan bool
	shutdownMonitor chan bool
}

// New returns an initialised ECU struct.
func New(cfg *Config) *ECU {
	if cfg == nil {
		defaultCfg := DefaultConfig()
		cfg = &defaultCfg
	}
	return &ECU{
		cfg:             cfg,
		shutdown:        make(chan bool),
		shutdownControl: make(chan bool),
		shutdownMonitor: make(chan bool),
	}
}

// Run starts the ECU.
func (e *ECU) Run() {
	if err := e.cfg.Authenticator.AuthInit(); err != nil {
		log.Warningf("Failed to initialise authentication, remote control will likely fail: %v", err)
	}

	tlsConfig, err := e.tlsConfig()
	if err != nil {
		log.Fatalf("failed to load tls config: %v", err)
	}

	cache := newStatsCache(e.cfg.EngineSocket, time.Second)

	httpsServer := e.httpsServer(tlsConfig, cache)

	healthz := newHealthzServer(e.cfg.HealthzAddress, cache)
	go healthz.run()

	cs := e.controlServer(cache, tlsConfig)

	<-e.shutdown

	if err := httpsServer.Shutdown(context.Background()); err != nil {
		log.Errorf("HTTPs server Shutdown failed: %v", err)
	}
	healthz.shutdown()
	cs.Stop()
}

// Shutdown notifies the ECU to shutdown.
func (e *ECU) Shutdown() {
	e.shutdown <- true
}

// httpsServer starts an HTTPs server.
func (e *ECU) httpsServer(tlsConfig *tls.Config, cache *statsCache) *http.Server {
	handler, err := prom.NewHandler(cache)
	if err != nil {
		log.Fatalf("failed to create prometheus handler: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle(prom.MetricsPath, handler)

	s := &http.Server{
		Addr:         e.cfg.MonitorAddress,
		Handler:      mux,
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
		TLSConfig:    tlsConfig,
	}
	go func() {
		if err := s.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
			log.Fatalf("httpsServer ListenAndServe failed: %v", err)
		}
	}()
	return s
}

// tlsConfig returns a TLS configuration for use by the ecu server.
func (e *ECU) tlsConfig() (*tls.Config, error) {
	caCerts := x509.NewCertPool()
	data, err := ioutil.ReadFile(e.cfg.CACertsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert file: %v", err)
	}
	if ok := caCerts.AppendCertsFromPEM(data); !ok {
		return nil, errors.New("failed to load CA certificates")
	}
	certs, err := tls.LoadX509KeyPair(e.cfg.ECUCertFile, e.cfg.ECUKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load X.509 key pair: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certs},
		ClientCAs:    caCerts,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	return tlsConfig, nil
}

func (e *ECU) controlServer(cache *statsCache, tlsConfig *tls.Config) *grpc.Server {
	lis, err := net.Listen("tcp", e.cfg.ControlAddress)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	creds := credentials.NewTLS(tlsConfig)
	s := grpc.NewServer(grpc.Creds(creds))
	ecupb.RegisterSeesawECUServer(s, newControlServer(e.cfg.EngineSocket, cache))
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()
	return s
}
