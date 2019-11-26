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

/*
	Package ecu implements the Seesaw v2 ECU component, which provides
	an externally accessible interface to monitor and control the
	Seesaw Node.
*/
package ecu

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ecu/prom"

	log "github.com/golang/glog"
)

var defaultConfig = ECUConfig{
	EngineSocket:   seesaw.EngineSocket,
	HealthzAddress: ":20256",
	MonitorAddress: ":20257",
	CACertsFile:    "/etc/seesaw/ssl/ca/cert.pem",
	ECUCertFile:    "/etc/seesaw/ssl/ecu/cert.pem",
	ECUKeyFile:     "/etc/seesaw/ssl/ecu/key.pem",
}

// ECUConfig provides configuration details for a Seesaw ECU.
type ECUConfig struct {
	CACertsFile    string
	ControlAddress string
	ECUCertFile    string
	ECUKeyFile     string
	EngineSocket   string
	MonitorAddress string
	UpdateInterval time.Duration
	HealthzAddress string
}

// DefaultECUConfig returns the default ECU configuration.
func DefaultECUConfig() ECUConfig {
	return defaultConfig
}

// ECU contains the data necessary to run the Seesaw v2 ECU.
type ECU struct {
	cfg             *ECUConfig
	shutdown        chan bool
	shutdownControl chan bool
	shutdownMonitor chan bool
}

// NewECU returns an initialised ECU struct.
func NewECU(cfg *ECUConfig) *ECU {
	if cfg == nil {
		defaultCfg := DefaultECUConfig()
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
	if err := e.authInit(); err != nil {
		log.Warningf("Failed to initialise authentication, remote control will likely fail: %v", err)
	}

	tlsConfig, err := e.tlsConfig()
	if err != nil {
		log.Fatalf("failed to load tls config: %v", err)
	}

	cache := newStatsCache(e.cfg.EngineSocket, time.Second)

	httpsServer := e.httpsServer(tlsConfig)

	healthz := newHealthzServer(e.cfg.HealthzAddress, cache)
	go healthz.run()

	<-e.shutdown

	if httpsServer != nil {
		if err := httpsServer.Shutdown(context.Background()); err != nil {
			log.Errorf("HTTPs server Shutdown failed: %v", err)
		}
	}
	healthz.shutdown()

}

// Shutdown notifies the ECU to shutdown.
func (e *ECU) Shutdown() {
	e.shutdown <- true
}

// monitoring starts an HTTP server for monitoring purposes.
func (e *ECU) httpsServer(tlsConfig *tls.Config) *http.Server {
	handler, err := prom.NewHandler()
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
		if err := s.ListenAndServe(); err != http.ErrServerClosed {
			log.Errorf("httpsServer ListenAndServe failed: %v", err)
		}
	}()
	return s
}

// tlsConfig returns a TLS configuration for use by the ecu server.
func (e *ECU) tlsConfig() (*tls.Config, error) {
	rootCACerts := x509.NewCertPool()
	data, err := ioutil.ReadFile(e.cfg.CACertsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert file: %v", err)
	}
	if ok := rootCACerts.AppendCertsFromPEM(data); !ok {
		return nil, errors.New("failed to load CA certificates")
	}
	certs, err := tls.LoadX509KeyPair(e.cfg.ECUCertFile, e.cfg.ECUKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load X.509 key pair: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certs},
		RootCAs:      rootCACerts,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	return tlsConfig, nil
}
