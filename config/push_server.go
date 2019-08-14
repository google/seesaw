// config provides a config server implementation
package config

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	pb "github.com/google/seesaw/pb/config"
)

// PushServer implements a config server that allow push of configs.
type PushServer struct {
	p          *Provider
	pushServer *http.Server
	shutdownWG sync.WaitGroup
}

// NewPushServer returns a PushServer
func NewPushServer(p *Provider, caCerts *x509.CertPool, serverCerts tls.Certificate, vserverPort int) *PushServer {
	s := &PushServer{
		p: p,
	}

	putHandler := http.NewServeMux()
	putHandler.HandleFunc("/vservers", func(w http.ResponseWriter, req *http.Request) {
		s.servePut(w, req)
	})
	// pushServer binds to all IPs
	s.pushServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", vserverPort),
		Handler:      putHandler,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 2 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{serverCerts},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    caCerts,
		},
	}
	return s
}

func checkContentType(req *http.Request) error {
	switch t := req.Header.Get(contentType); t {
	case pbContentType:
		return nil
	case "":
		return fmt.Errorf("must specify Content-Type header")
	default:
		return fmt.Errorf("Content-Type: %s not supported", t)
	}
}

func (s *PushServer) servePut(w http.ResponseWriter, req *http.Request) {
	log.V(1).Infof("received /vservers request")
	if req.Method != http.MethodPut {
		http.Error(w, "PUT only", http.StatusMethodNotAllowed)
		return
	}

	if err := checkContentType(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	raw, err := ioutil.ReadAll(req.Body)
	defer req.Body.Close()
	if err != nil {
		http.Error(w, "Failed to read request", http.StatusBadRequest)
		return
	}

	updated := &pb.Cluster{}
	if err := proto.Unmarshal(raw, updated); err != nil {
		http.Error(w, fmt.Sprintf("Unable to marshal request: %v", err), http.StatusBadRequest)
		return
	}

	s.p.updateVservers(updated.Vserver)
}

// Shutdown turns off the server
func (s *PushServer) Shutdown() {
	if err := s.pushServer.Shutdown(context.Background()); err != nil {
		log.Errorf("pushServer.Shutdown failed: %s", err)
	}
	s.shutdownWG.Wait()
}

// Run starts the server and returns until it's shutdown.
func (s *PushServer) Run() {
	s.shutdownWG.Add(1)
	defer s.shutdownWG.Done()
	log.Infof("serving /vservers")
	if err := s.pushServer.ListenAndServeTLS("", ""); err != http.ErrServerClosed {
		log.Errorf("PushServer ListenAndServe failed: %s", err)
	}
	log.Infof("/vservers shutdown")
}
