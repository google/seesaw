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

// UDP healthcheck implementation.

package healthcheck

import (
	"fmt"
	"net"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

const (
	defaultUDPTimeout = 5 * time.Second
)

// UDPChecker contains configuration specific to a UDP healthcheck.
type UDPChecker struct {
	Target
	Receive string
	Send    string
}

// NewUDPChecker returns an initialised UDPChecker.
func NewUDPChecker(ip net.IP, port int) *UDPChecker {
	return &UDPChecker{
		Target: Target{
			IP:    ip,
			Port:  port,
			Proto: seesaw.IPProtoUDP,
		},
	}
}

// String returns the string representation of a UDP healthcheck.
func (hc *UDPChecker) String() string {
	return fmt.Sprintf("UDP %s", hc.Target)
}

// Check executes a UDP healthcheck.
func (hc *UDPChecker) Check(timeout time.Duration) *Result {
	msg := fmt.Sprintf("UDP check to %s", hc.addr())
	start := time.Now()
	if timeout == time.Duration(0) {
		timeout = defaultUDPTimeout
	}
	deadline := start.Add(timeout)

	conn, err := dialUDP(hc.network(), hc.addr(), timeout, hc.Mark)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to create socket", msg)
		return complete(start, msg, false, err)
	}
	defer conn.Close()

	err = conn.SetDeadline(deadline)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to set deadline", msg)
		return complete(start, msg, false, err)
	}

	if _, err = conn.Write([]byte(hc.Send)); err != nil {
		msg = fmt.Sprintf("%s; failed to send request", msg)
		return complete(start, msg, false, err)
	}

	buf := make([]byte, len(hc.Receive))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to read response", msg)
		return complete(start, msg, false, err)
	}

	got := string(buf[0:n])
	if got != hc.Receive {
		msg = fmt.Sprintf("%s; unexpected response - %q", msg, got)
		return complete(start, msg, false, err)
	}
	return complete(start, msg, true, err)
}
