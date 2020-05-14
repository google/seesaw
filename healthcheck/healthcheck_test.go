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

package healthcheck

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const timeout = 1 * time.Second

func newLocalTCPListener(n string) (*net.TCPListener, *net.TCPAddr, error) {
	var addr string
	switch n {
	case "tcp4":
		addr = "127.0.0.1:0"
	case "tcp6":
		addr = "[::1]:0"
	default:
		return nil, nil, fmt.Errorf("Unknown network %q", n)
	}

	tcpAddr, err := net.ResolveTCPAddr(n, addr)
	if err != nil {
		return nil, nil, err
	}
	l, err := net.ListenTCP(n, tcpAddr)
	if err != nil {
		return nil, nil, err
	}
	tcpAddr = l.Addr().(*net.TCPAddr)

	return l, tcpAddr, nil
}

func newLocalUDPConn(n string) (*net.UDPConn, *net.UDPAddr, error) {
	var addr string
	switch n {
	case "udp4":
		addr = "127.0.0.1:0"
	case "udp6":
		addr = "[::1]:0"
	default:
		return nil, nil, fmt.Errorf("Unknown network %q", n)
	}

	udpAddr, err := net.ResolveUDPAddr(n, addr)
	if err != nil {
		return nil, nil, err
	}
	c, err := net.ListenUDP(n, udpAddr)
	if err != nil {
		return nil, nil, err
	}
	udpAddr = c.LocalAddr().(*net.UDPAddr)

	return c, udpAddr, nil
}

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/http")
	w.WriteHeader(200)
	fmt.Fprintln(w, "<html><body>Test Server</body></html>")
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(200)
	fmt.Fprintf(w, "ok\n")
}

func newLocalHTTPServer(l net.Listener) *httptest.Server {
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(defaultHandler))
	mux.Handle("/healthz", http.HandlerFunc(healthzHandler))
	mux.Handle("/notfound", http.NotFoundHandler())
	return &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: mux},
	}
}

type httpTest struct {
	method       string
	request      string
	response     string
	responseCode int
	expected     bool
}

func (ht httpTest) configure(hc *HTTPChecker) {
	hc.Method = ht.method
	hc.Request = ht.request
	hc.Response = ht.response
	hc.ResponseCode = ht.responseCode
}

func (ht httpTest) String() string {
	return fmt.Sprintf("{%s %s, %q, %d}",
		ht.method, ht.request, ht.response, ht.responseCode)
}

var httpTests = []httpTest{
	{"GET", "/", "", 0, true},
	{"GET", "/", "", 200, true},
	{"GET", "/", "", 404, false},
	{"GET", "/healthz", "", 0, true},
	{"GET", "/healthz", "", 200, true},
	{"GET", "/healthz", "ok\n", 200, true},
	{"GET", "/healthz", "notok", 200, false},
	{"GET", "/healthz", "ok\n", 503, false},
	{"GET", "/notfound", "", 0, true},
	{"GET", "/notfound", "", 200, false},
	{"GET", "/notfound", "", 404, true},
	{"HEAD", "/healthz", "", 0, true},
	{"HEAD", "/healthz", "", 200, true},
	{"HEAD", "/notfound", "", 0, true},
	{"HEAD", "/notfound", "", 200, false},
	{"HEAD", "/notfound", "", 404, true},
}

func testHTTPChecker(t *testing.T, secure bool) {
	for _, n := range []string{"tcp4", "tcp6"} {
		l, a, err := newLocalTCPListener(n)
		if err != nil {
			t.Fatalf("Failed to get TCP listener: %v", err)
		}

		srv := newLocalHTTPServer(l)
		if secure {
			srv.StartTLS()
		} else {
			srv.Start()
		}

		hc := NewHTTPChecker(a.IP, a.Port)
		hc.Secure = secure
		hc.TLSVerify = false

		for _, ht := range httpTests {
			ht.configure(hc)
			if result := hc.Check(timeout); result.Success != ht.expected {
				t.Errorf("HTTP healthcheck %v to %v failed: %v", ht, a, result)
			}
		}

		// Test with TLS inverted.
		// Expect to get 400 code
		httpTests[1].configure(hc)
		hc.Secure = !secure
		if result := hc.Check(timeout); result.Success {
			t.Errorf("HTTP healthcheck %v to %v succeeded with secure=%t: %v",
				httpTests[0], a, !secure, result)
		}

		// Test with shutdown/closed server.
		httpTests[0].configure(hc)
		hc.Secure = secure
		srv.Close()
		// srv may still handle one more request immediately after Close.
		// Throw away one check to ensure the server has actually shut down,
		// one extra check doesn't matter in practice.
		hc.Check(timeout)
		if result := hc.Check(timeout); result.Success {
			t.Errorf("HTTP healthcheck %v to %v succeeded after close: %v",
				httpTests[0], a, result)
		}
	}
}

func TestHTTPChecker(t *testing.T) {
	testHTTPChecker(t, false)
}

func TestHTTPCheckerSecure(t *testing.T) {
	testHTTPChecker(t, true)
}

type tcpTest struct {
	send     string
	receive  string
	expected bool
}

func (tt tcpTest) configure(hc *TCPChecker) {
	hc.Send = tt.send
	hc.Receive = tt.receive
}

func (tt tcpTest) String() string {
	return fmt.Sprintf("{%q, %q}", tt.send, tt.receive)
}

var tcpTests = []tcpTest{
	{"", "", true},
	{"foo", "foo", true},
	{"foo\n", "foo", true},
	{"foo", "foo\n", false},
	{"foo", "foooo", false},
	{"", "foo", false},
}

func tcpEchoHandler(l *net.TCPListener) {
	for {
		c, err := l.AcceptTCP()
		if err != nil {
			break
		}
		go func() {
			defer c.Close()
			buf := make([]byte, 64)
			for {
				// This could potentially return a partial read but we'll pick up
				// any remaining bytes in the next iteration(s).
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				if _, err = c.Write(buf[0:n]); err != nil {
					return
				}
			}
		}()
	}
}

func TestTCPChecker(t *testing.T) {
	for _, n := range []string{"tcp4", "tcp6"} {
		l, a, err := newLocalTCPListener(n)
		if err != nil {
			t.Fatalf("Failed to get TCP listener: %v", err)
		}
		go tcpEchoHandler(l)

		for _, tt := range tcpTests {
			hc := NewTCPChecker(a.IP, a.Port)
			tt.configure(hc)
			if result := hc.Check(timeout); result.Success != tt.expected {
				t.Errorf("TCP healthcheck %v to %v failed: %v", tt, a, result)
			}
		}

		hc := NewTCPChecker(a.IP, a.Port)
		l.Close()
		time.Sleep(100 * time.Millisecond)
		if result := hc.Check(timeout); result.Success {
			t.Errorf("TCP healthcheck %v to %v succeeded: %v", hc, a, result)
		}
	}
}

type udpTest struct {
	send     string
	receive  string
	expected bool
}

func (ut udpTest) configure(hc *UDPChecker) {
	hc.Send = ut.send
	hc.Receive = ut.receive
}

func (ut udpTest) String() string {
	return fmt.Sprintf("{%q, %q}", ut.send, ut.receive)
}

func udpEchoHandler(c *net.UDPConn) {
	buf := make([]byte, 64)
	for {
		n, addr, err := c.ReadFrom(buf)
		if err != nil {
			return
		}
		if _, err = c.WriteTo(buf[0:n], addr); err != nil {
			return
		}
	}
}

var udpTests = []udpTest{
	{"", "", true},
	{"foo", "foo", true},
	{"foo\n", "foo", true},
	{"foo", "foo\n", false},
	{"foo", "foooo", false},
	{"", "foo", false},
	{"\x00\x01\x02\x03", "\x00\x01\x02\x03", true},
	{"\x00\x01", "\x00\x01\x02\x03", false},
	{"\x00\x01", "\x02", false},
	{"\x00\x01", "", true},
}

func TestUDPChecker(t *testing.T) {
	for _, n := range []string{"udp4", "udp6"} {
		c, a, err := newLocalUDPConn(n)
		if err != nil {
			t.Fatalf("Failed to get UDPConn: %v", err)
		}
		go udpEchoHandler(c)

		for _, ut := range udpTests {
			hc := NewUDPChecker(a.IP, a.Port)
			ut.configure(hc)
			if result := hc.Check(timeout); result.Success != ut.expected {
				t.Errorf("UDP healthcheck %v to %v failed: %v", ut, a, result)
			}
		}

		hc := NewUDPChecker(a.IP, a.Port)
		c.Close()
		if result := hc.Check(timeout); result.Success {
			t.Errorf("UDP healthcheck %v to %v succeeded: %v", hc, a, result)
		}
	}
}

type fakeChecker struct {
	succeed bool
	sleepy  bool
}

func (hc *fakeChecker) String() string {
	return "FAKE"
}

func (hc *fakeChecker) Check(timeout time.Duration) *Result {
	if hc.sleepy {
		time.Sleep(500 * time.Millisecond)
	}
	return &Result{Success: hc.succeed}
}

func TestCheckRetries(t *testing.T) {
	notify := make(chan *Notification, 10)
	checker := &fakeChecker{}
	config := NewConfig(1, checker)
	hc := NewCheck(notify)
	hc.Config = *config
	hc.Config.Retries = 5

	// Check should start in state unknown.
	if hc.state != StateUnknown {
		t.Errorf("Unexpected new healthcheck state - got %v, want %v", hc.state, StateUnknown)
	}

	// Check should immediately transition from unknown to unhealthy.
	hc.healthcheck()
	if hc.state != StateUnhealthy {
		t.Errorf("Unexpected healthcheck state - got %v, want %v", hc.state, StateUnhealthy)
	}

	// Check should immediately transition from unhealthy to healthy.
	checker.succeed = true
	hc.healthcheck()
	if hc.state != StateHealthy {
		t.Errorf("Unexpected healthcheck state - got %v, want %v", hc.state, StateHealthy)
	}

	// A single healthcheck failure should not result in a transition.
	checker.succeed = false
	hc.healthcheck()
	if hc.state != StateHealthy {
		t.Errorf("Unexpected healthcheck state - got %v, want %v", hc.state, StateHealthy)
	}

	// Retries + 1 consecutive failures should result in a transition.
	checker.succeed = true
	hc.healthcheck()
	checker.succeed = false
	for i := 0; i < hc.Config.Retries; i++ {
		hc.healthcheck()
	}
	if hc.state != StateHealthy {
		t.Errorf("Unexpected healthcheck state - got %v, want %v", hc.state, StateHealthy)
	}
	hc.healthcheck()
	if hc.state != StateUnhealthy {
		t.Errorf("Unexpected healthcheck state - got %v, want %v", hc.state, StateUnhealthy)
	}

	// We should have a notification for StateUnhealthy, followed by a
	// notification for StateHealthy and another StateUnhealthy.
	for i, state := range []State{StateUnhealthy, StateHealthy, StateUnhealthy} {
		select {
		case n := <-notify:
			if n.State != state {
				t.Errorf("Notification %d got unexpected state %v, want %v", i+1, n.State, state)
			}
		default:
			t.Errorf("Expected state change notification not received")
		}
	}
	select {
	case n := <-notify:
		t.Errorf("Received unexpected state change notification: %v", n.State)
	default:
	}
}

func TestCheckRun(t *testing.T) {
	notify := make(chan *Notification, 10)
	hc := NewCheck(notify)
	hc.Blocking(true)
	go hc.Run(nil)

	config := NewConfig(1, &fakeChecker{succeed: true})
	config.Interval = 250 * time.Millisecond
	hc.Update(config)

	time.Sleep(800 * time.Millisecond)
	config.Checker = &fakeChecker{succeed: false}
	config.Interval = 500 * time.Millisecond
	hc.Update(config)

	time.Sleep(1200 * time.Millisecond)
	hc.Stop()

	s := hc.Status()
	t.Logf("Healthchecks resulted in %d failure(s) and %d success(es)",
		s.Failures, s.Successes)

	// We expect 4 successes and 2 failures - allow a tolerance of -1/+1.
	if s.Successes < 3 || s.Successes > 5 {
		t.Errorf("Unexpected number of successes - got %d, want 4",
			s.Successes)
	}
	if s.Failures < 1 || s.Failures > 3 {
		t.Errorf("Unexpected number of failures - got %d, want 2",
			s.Failures)
	}

	// We should have a notification for StateHealthy, followed by a
	// notification for StateUnhealthy.
	select {
	case n := <-notify:
		if n.State != StateHealthy {
			t.Errorf("Unexpected state - got %v, want %v",
				n.State, StateHealthy)
		}
	default:
		t.Errorf("Expected state change notification not received")
	}
	select {
	case n := <-notify:
		if n.State != StateUnhealthy {
			t.Errorf("Unexpected state - got %v, want %v",
				n.State, StateUnhealthy)
		}
	default:
		t.Errorf("Expected state change notification not received")
	}
}

func TestCheckTimeout(t *testing.T) {
	notify := make(chan *Notification, 10)
	hc := NewCheck(notify)
	hc.Blocking(true)
	go hc.Run(nil)

	// Increase timeout from 200ms to 1s, after 800ms. This should result
	// in the config update being applied at the end of four failures
	// (1000ms). We then wait for 1200ms before sending a stop notification,
	// which should allow for two successful checks to complete.
	config := NewConfig(1, &fakeChecker{succeed: true, sleepy: true})
	config.Interval = 250 * time.Millisecond
	config.Timeout = 200 * time.Millisecond
	hc.Update(config)
	time.Sleep(800 * time.Millisecond)
	config.Timeout = 1 * time.Second
	hc.Update(config)
	time.Sleep(1200 * time.Millisecond)
	hc.Stop()

	s := hc.Status()
	t.Logf("Healthchecks resulted in %d failure(s) and %d success(es)",
		s.Failures, s.Successes)

	// We expect 4 failures and 2 successes - allow a tolerance of -1/+1.
	if s.Failures < 3 || s.Failures > 5 {
		t.Errorf("Unexpected number of failures - got %d, want 4",
			s.Failures)
	}
	if s.Successes < 1 || s.Successes > 3 {
		t.Errorf("Unexpected number of successes - got %d, want 2",
			s.Successes)
	}

	// We should have a notification for StateUnhealthy, followed by a
	// notification for StateHealthy.
	select {
	case n := <-notify:
		if n.State != StateUnhealthy {
			t.Errorf("Unexpected state - got %v, want %v",
				n.State, StateUnhealthy)
		}
	default:
		t.Errorf("Expected state change notification not received")
	}
	select {
	case n := <-notify:
		if n.State != StateHealthy {
			t.Errorf("Unexpected state - got %v, want %v",
				n.State, StateHealthy)
		}
	default:
		t.Errorf("Expected state change notification not received")
	}
}

func TestCheckDryrun(t *testing.T) {
	notify := make(chan *Notification, 10)
	hc := NewCheck(notify)
	hc.Blocking(true)
	hc.Dryrun(true)
	go hc.Run(nil)
	defer hc.Stop()

	config := NewConfig(1, &fakeChecker{succeed: false})
	config.Interval = 250 * time.Millisecond
	hc.Update(config)

	// We should have a notification for StateHealthy
	select {
	case n := <-notify:
		if n.State != StateHealthy {
			t.Errorf("Unexpected state - got %v, want %v",
				n.State, StateHealthy)
		}
	default:
		t.Errorf("Expected state change notification not received")
	}
}
