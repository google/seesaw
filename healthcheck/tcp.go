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

// TCP healthcheck implementation.

package healthcheck

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

const (
	defaultTCPTimeout = 10 * time.Second
)

// TCPChecker contains configuration specific to a TCP healthcheck.
type TCPChecker struct {
	Target
	Receive   string
	Send      string
	Secure    bool
	TLSVerify bool
}

// NewTCPChecker returns an initialised TCPChecker.
func NewTCPChecker(ip net.IP, port int) *TCPChecker {
	return &TCPChecker{
		Target: Target{
			IP:    ip,
			Port:  port,
			Proto: seesaw.IPProtoTCP,
		},
	}
}

// String returns the string representation of a TCP healthcheck.
func (hc *TCPChecker) String() string {
	attr := []string{}
	if hc.Secure {
		attr = append(attr, "secure")
		if hc.TLSVerify {
			attr = append(attr, "verify")
		}
	}
	var s string
	if len(attr) > 0 {
		s = fmt.Sprintf(" [%s]", strings.Join(attr, "; "))
	}
	return fmt.Sprintf("TCP%s %s", s, hc.Target)
}

// Check executes a TCP healthcheck.
func (hc *TCPChecker) Check(timeout time.Duration) *Result {
	msg := fmt.Sprintf("TCP connect to %s", hc.addr())
	start := time.Now()
	if timeout == time.Duration(0) {
		timeout = defaultTCPTimeout
	}
	deadline := start.Add(timeout)

	tcpConn, err := dialTCP(hc.network(), hc.addr(), timeout, hc.Mark)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to connect", msg)
		return complete(start, msg, false, err)
	}
	conn := net.Conn(tcpConn)
	defer conn.Close()

	// Negotiate TLS if this is required.
	if hc.Secure {
		// TODO(jsing): We probably should allow the server name to
		// be specified via configuration...
		host, _, err := net.SplitHostPort(hc.addr())
		if err != nil {
			msg = msg + "; failed to split host"
			return complete(start, msg, false, err)
		}
		tlsConfig := &tls.Config{
			InsecureSkipVerify: !hc.TLSVerify,
			ServerName:         host,
		}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.Handshake(); err != nil {
			return complete(start, msg, false, err)
		}
		conn = tlsConn
	}

	if hc.Send == "" && hc.Receive == "" {
		return complete(start, msg, true, err)
	}

	err = conn.SetDeadline(deadline)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to set deadline", msg)
		return complete(start, msg, false, err)
	}

	if hc.Send != "" {
		err = writeFull(conn, []byte(hc.Send))
		if err != nil {
			msg = fmt.Sprintf("%s; failed to send request", msg)
			return complete(start, msg, false, err)
		}
	}

	if hc.Receive != "" {
		buf := make([]byte, len(hc.Receive))
		n, err := io.ReadFull(conn, buf)
		if err != nil {
			msg = fmt.Sprintf("%s; failed to read response", msg)
			return complete(start, msg, false, err)
		}
		got := string(buf[0:n])
		if got != hc.Receive {
			msg = fmt.Sprintf("%s; unexpected response - %q", msg, got)
			return complete(start, msg, false, err)
		}
	}
	return complete(start, msg, true, err)
}

func writeFull(conn net.Conn, b []byte) error {
	for len(b) > 0 {
		n, err := conn.Write(b)
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}
