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

// HTTP healthcheck implementation.

package healthcheck

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

const (
	defaultHTTPTimeout = 5 * time.Second
)

// HTTPChecker contains configuration specific to a HTTP healthcheck.
type HTTPChecker struct {
	Target
	Secure       bool
	TLSVerify    bool
	Method       string
	Proxy        bool
	Request      string
	Response     string
	ResponseCode int
}

// NewHTTPChecker returns an initialised HTTPChecker.
func NewHTTPChecker(ip net.IP, port int) *HTTPChecker {
	return &HTTPChecker{
		Target: Target{
			IP:    ip,
			Port:  port,
			Proto: seesaw.IPProtoTCP,
		},
		Secure:       false,
		TLSVerify:    true,
		Method:       "GET",
		Proxy:        false,
		Request:      "/",
		Response:     "",
		ResponseCode: 200,
	}
}

// String returns the string representation of an HTTP healthcheck.
func (hc *HTTPChecker) String() string {
	attr := []string{fmt.Sprintf("code %d", hc.ResponseCode)}
	if hc.Proxy {
		attr = append(attr, "proxy")
	}
	if hc.Secure {
		attr = append(attr, "secure")
		if hc.TLSVerify {
			attr = append(attr, "verify")
		}
	}
	s := strings.Join(attr, "; ")
	return fmt.Sprintf("HTTP %s %s [%s] %s", hc.Method, hc.Request, s, hc.Target)
}

// Check executes a HTTP healthcheck.
func (hc *HTTPChecker) Check(timeout time.Duration) *Result {
	msg := fmt.Sprintf("HTTP %s to %s", hc.Method, hc.addr())
	start := time.Now()
	if timeout == time.Duration(0) {
		timeout = defaultHTTPTimeout
	}
	deadline := start.Add(timeout)

	u, err := url.Parse(hc.Request)
	if err != nil {
		return complete(start, "", false, err)
	}
	if hc.Secure {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	if u.Host == "" {
		u.Host = hc.IP.String()
	}

	proxy := (func(*http.Request) (*url.URL, error))(nil)
	if hc.Proxy {
		proxy = http.ProxyURL(u)
	}

	conn, err := dialTCP(hc.network(), hc.addr(), timeout, hc.Mark)
	if err != nil {
		return complete(start, "", false, err)
	}
	defer conn.Close()

	dialer := func(net string, addr string) (net.Conn, error) {
		return conn, nil
	}
	tlsConfig := &tls.Config{
		InsecureSkipVerify: !hc.TLSVerify,
	}
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return errors.New("redirect not permitted")
		},
		Transport: &http.Transport{
			Dial:            dialer,
			Proxy:           proxy,
			TLSClientConfig: tlsConfig,
		},
	}
	req, err := http.NewRequest(hc.Method, hc.Request, nil)
	req.URL = u

	// If we received a response we want to process it, even in the
	// presence of an error - a redirect 3xx will result in both the
	// response and an error being returned.
	conn.SetDeadline(deadline)
	resp, err := client.Do(req)
	if resp == nil {
		return complete(start, "", false, err)
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	err = nil

	// Check response code.
	var codeOk bool
	if hc.ResponseCode == 0 {
		codeOk = true
	} else if resp.StatusCode == hc.ResponseCode {
		codeOk = true
	}

	// Check response body.
	var bodyOk bool
	msg = fmt.Sprintf("%s; got %s", msg, resp.Status)
	if hc.Response == "" {
		bodyOk = true
	} else if resp.Body != nil {
		buf := make([]byte, len(hc.Response))
		n, err := io.ReadFull(resp.Body, buf)
		if err != nil && err != io.ErrUnexpectedEOF {
			msg = fmt.Sprintf("%s; failed to read HTTP response", msg)
		} else if string(buf) != hc.Response {
			msg = fmt.Sprintf("%s; unexpected response - %q", msg, string(buf[0:n]))
		} else {
			bodyOk = true
		}
	}

	return complete(start, msg, codeOk && bodyOk, err)
}
