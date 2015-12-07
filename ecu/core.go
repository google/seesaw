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
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/rpc"
	"path"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"

	log "github.com/golang/glog"
)

var defaultConfig = ECUConfig{
	CACertsFile:    path.Join(seesaw.ConfigPath, "ssl", "ca.crt"),
	ControlAddress: ":10256",
	ECUCertFile:    path.Join(seesaw.ConfigPath, "ssl", "seesaw.crt"),
	ECUKeyFile:     path.Join(seesaw.ConfigPath, "ssl", "seesaw.key"),
	EngineSocket:   seesaw.EngineSocket,
	MonitorAddress: ":10257",
	UpdateInterval: 10 * time.Second,
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

	stats := newECUStats(e)
	go stats.run()

	go e.control()
	go e.monitoring()

	<-e.shutdown
	e.shutdownControl <- true
	e.shutdownMonitor <- true
	<-e.shutdownControl
	<-e.shutdownMonitor
}

// Shutdown notifies the ECU to shutdown.
func (e *ECU) Shutdown() {
	e.shutdown <- true
}

// monitoring starts an HTTP server for monitoring purposes.
func (e *ECU) monitoring() {
	ln, err := net.Listen("tcp", e.cfg.MonitorAddress)
	if err != nil {
		log.Fatal("listen error:", err)
	}

	monitorHTTP := &http.Server{
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go monitorHTTP.Serve(ln)

	<-e.shutdownMonitor
	ln.Close()
	e.shutdownMonitor <- true
}

// controlTLSConfig returns a TLS configuration for use by the control server.
func (e *ECU) controlTLSConfig() (*tls.Config, error) {
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
	// TODO(jsing): Make the server name configurable.
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{certs},
		RootCAs:      rootCACerts,
		ServerName:   "seesaw.example.com",
	}
	return tlsConfig, nil
}

// control starts an HTTP server to handle control RPC via a TCP socket. This
// interface is used to control the Seesaw Node via the CLI or web console.
func (e *ECU) control() {
	tlsConfig, err := e.controlTLSConfig()
	if err != nil {
		log.Errorf("Disabling ECU control server: %v", err)
		<-e.shutdownControl
		e.shutdownControl <- true
		return
	}

	ln, err := net.Listen("tcp", e.cfg.ControlAddress)
	if err != nil {
		log.Fatal("listen error:", err)
	}

	seesawRPC := rpc.NewServer()
	seesawRPC.Register(&SeesawECU{e})
	tlsListener := tls.NewListener(ln, tlsConfig)
	go server.RPCAccept(tlsListener, seesawRPC)

	<-e.shutdownControl
	ln.Close()
	e.shutdownControl <- true
}
