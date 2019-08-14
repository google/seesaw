package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	pb "github.com/google/seesaw/pb/config"
)

const (
	configPort = 39390
)

func setupGetServerClient() (*GetServer, *tls.Config, error) {
	caKey, caCert, err := newCA("seesaw-ca")
	if err != nil {
		return nil, nil, fmt.Errorf("newCA failed: %s", err)
	}
	rootCACerts := x509.NewCertPool()
	rootCACerts.AddCert(caCert)

	serverCert, err := newSignedCert("config-server-cert", caKey, caCert, "", []string{"127.0.0.1"}, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create server cert: %v", err)
	}

	clientCert, err := newSignedCert("vserver-put-client", caKey, caCert, "", nil, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create client cert: %v", err)
	}
	tlsConfig := &tls.Config{
		RootCAs:      rootCACerts,
		Certificates: []tls.Certificate{clientCert},
	}

	p := NewProvider(initConfig)
	s := NewGetServer(p, serverCert, configPort)

	return s, tlsConfig, nil
}

func checkConfigGet(tlsConfig *tls.Config, expected *pb.Cluster) error {
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: clientTimeout,
	}

	succeed := false
	url := fmt.Sprintf("https://127.0.0.1:%d/config/test-cluster", configPort)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("http.NewRequest failed: %v", err)
	}
	var resp *http.Response
	// Need retry a bit until the server is up
	for i := 0; i < 5; i = i + 1 {
		resp, err = client.Do(req)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		succeed = true
		break
	}
	if !succeed {
		return fmt.Errorf("client.Do(%s) failed: %v", url, err)
	}
	defer resp.Body.Close()

	if got, want := resp.StatusCode, http.StatusOK; got != want {
		return fmt.Errorf("bad http status got %d but want %d", got, want)
	}

	if got, want := resp.Header.Get(contentType), pbContentType; got != want {
		return fmt.Errorf("content type mismatch. got %s but want %s", got, want)
	}

	raw, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ioutil.ReadAll(resp) failed: %v", err)
	}

	got := &pb.Cluster{}
	if err := proto.Unmarshal(raw, got); err != nil {
		return fmt.Errorf("proto.Unmarshal(raw) failed: %s", err)
	}

	if !proto.Equal(got, expected) {
		return fmt.Errorf("got config %s but want %s", got, initConfig)
	}
	return nil
}

func TestConfigGet(t *testing.T) {
	s, clientConfig, err := setupGetServerClient()
	if err != nil {
		t.Fatalf("setupGetServerClient failed: %v", err)
	}

	go s.Run()
	defer s.Shutdown()

	// we don't need to pass client cert for /config
	clientConfig.Certificates = nil
	if err := checkConfigGet(clientConfig, initConfig); err != nil {
		t.Fatal(err)
	}
}
