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

// Author: angusc@google.com (Angus Cameron)

package config

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"time"

	pb "github.com/google/seesaw/pb/config"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func certPool(certfile string) (*x509.CertPool, error) {
	f, err := os.Open(certfile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("failed to load certificates from %q", certfile)
	}
	return pool, nil
}

type fetcher struct {
	certs   *x509.CertPool
	port    int
	cluster string
	servers []string
	timeout time.Duration
}

func newFetcher(cfg *EngineConfig) (*fetcher, error) {
	if len(cfg.ConfigServers) == 0 {
		return nil, fmt.Errorf("no config servers")
	}

	certs, err := certPool(cfg.CACertFile)
	if err != nil {
		return nil, err
	}
	f := &fetcher{
		certs:   certs,
		cluster: cfg.ClusterName,
		port:    cfg.ConfigServerPort,
		servers: cfg.ConfigServers,
		timeout: cfg.ConfigServerTimeout,
	}
	return f, nil
}

// fetchFromHost attempts to fetch the specified URL from a specific host.
func (f *fetcher) fetchFromHost(ip net.IP, url, contentType string) ([]byte, error) {
	// TODO(angusc): connection timeout?
	tcpAddr := &net.TCPAddr{IP: ip, Port: f.port}
	tcpConn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return nil, err
	}
	defer tcpConn.Close()
	tcpConn.SetDeadline(time.Now().Add(f.timeout))
	dialer := func(net string, addr string) (net.Conn, error) {
		return tcpConn, nil
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("HTTP redirect prohibited")
		},
		Transport: &http.Transport{
			Dial:              dialer,
			DisableKeepAlives: true,
			TLSClientConfig: &tls.Config{
				RootCAs: f.certs,
			},
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Connection", "close")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received HTTP status %s", resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); ct != contentType {
		return nil, fmt.Errorf("unexpected Content-Type: %q", ct)
	}
	return ioutil.ReadAll(resp.Body)
}

type fetchHandler func(f *fetcher, host string, ip net.IP) (string, []byte, error)

func fetchConfig(f *fetcher, host string, ip net.IP) (string, []byte, error) {
	url := fmt.Sprintf("https://%v:%d/config/%v", host, f.port, f.cluster)
	body, err := f.fetchFromHost(ip, url, "application/x-protobuffer")
	if err != nil {
		return url, nil, fmt.Errorf("fetch failed from %v (%v): %v", url, ip, err)
	}
	// TODO(jsing): Consider moving the verification to a callback function.
	p := &pb.Cluster{}
	if err := proto.Unmarshal(body, p); err != nil {
		return url, nil, fmt.Errorf("invalid configuration from %v (%v): %v", url, ip, err)
	}
	return url, body, nil
}

func (f *fetcher) fetch(handler fetchHandler) (string, []byte, error) {
	for _, server := range f.servers {
		// TODO(angusc): Resolve and cache the IP address for each server so we have
		// a fallback in case DNS is down.
		addrs, err := net.LookupIP(server)
		if err != nil {
			log.Errorf("DNS lookup failed for config server %q: %v", server, err)
			continue
		}

		// Randomise the list of addresses, putting all IPv6 addresses before IPv4.
		var ipv4Addrs, ipv6Addrs []net.IP
		for _, ip := range addrs {
			switch {
			case ip.To4() != nil:
				ipv4Addrs = append(ipv4Addrs, ip)
			default:
				ipv6Addrs = append(ipv6Addrs, ip)
			}
		}
		// In-place Fisher–Yates shuffle.
		for i := len(ipv4Addrs) - 1; i > 0; i-- {
			j := rand.Intn(i + 1)
			ipv4Addrs[i], ipv4Addrs[j] = ipv4Addrs[j], ipv4Addrs[i]
		}
		for i := len(ipv6Addrs) - 1; i > 0; i-- {
			j := rand.Intn(i + 1)
			ipv6Addrs[i], ipv6Addrs[j] = ipv6Addrs[j], ipv6Addrs[i]
		}
		addrs = append(ipv6Addrs, ipv4Addrs...)

		for _, ip := range addrs {
			url, body, err := handler(f, server, ip)
			if err != nil {
				log.Warningf("Fetch failed: %v", err)
				continue
			}
			source := fmt.Sprintf("%v (%v)", url, ip)
			log.Infof("Successful fetch from config server %v", source)
			return source, body, nil
		}
	}
	return "", nil, fmt.Errorf("all config server requests failed")
}

// config attempts to fetch and return a configuration protobuf from any valid
// configuration server.
func (f *fetcher) config() (string, []byte, error) {
	return f.fetch(fetchConfig)
}
