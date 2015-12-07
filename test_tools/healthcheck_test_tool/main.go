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

// The healthcheck_test_tool binary is used to test healthcheckers.
package main

import (
	"flag"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/seesaw/healthcheck"
)

var (
	hcType       = flag.String("type", "ping", "healthcheck type")
	ip           = flag.String("ip", "127.0.0.1", "IP address to check")
	port         = flag.Int("port", 80, "port to check")
	mark         = flag.Int("mark", 0, "mark to use for network traffic")
	count        = flag.Int("count", 3, "number of packets to send for a ping healthcheck")
	receive      = flag.String("receive", "", "expected TCP or UDP response string")
	send         = flag.String("send", "", "string to send for a TCP or UDP healthcheck")
	method       = flag.String("method", "GET", "HTTP method")
	proxy        = flag.Bool("proxy", false, "HTTP(S) healthcheck is against a proxy")
	request      = flag.String("request", "/", "request URI for an HTTP(S) healthcheck")
	response     = flag.String("response", "", "expected HTTP(S) response")
	responseCode = flag.Int("response_code", 200, "expected HTTP(S) response code")
	tlsVerify    = flag.Bool("tls_verify", true, "enable TLS verification for HTTPS and TCP TLS")

	dnsAnswer    = flag.String("answer", "", "DNS answer expected from query")
	dnsQuery     = flag.String("query", "", "DNS query to perform")
	dnsQueryType = flag.String("query_type", "A", "DNS query type")

	radiusUser     = flag.String("radius_user", "", "RADIUS username")
	radiusPasswd   = flag.String("radius_password", "", "RADIUS password")
	radiusResponse = flag.String("radius_response", "accept", "RADIUS response type (any, accept, challenge or reject)")
	radiusSecret   = flag.String("radius_secret", "", "RADIUS secret")
	timeout        = flag.Duration("timeout", 0, "healthcheck timeout")
)

func check(hc healthcheck.Checker) {
	r := hc.Check(*timeout)
	s := "success"
	if !r.Success {
		s = "failure"
	}
	log.Printf("%v - %v (healthcheck %s)", hc, r, s)
}

func unquote(s string) string {
	if !(strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "`")) {
		return s
	}
	us, err := strconv.Unquote(s)
	if err != nil {
		log.Fatalf("Failed to unquote %q: %v", s, err)
	}
	return us
}

func doDNSCheck(target net.IP) {
	qt, err := healthcheck.DNSType(*dnsQueryType)
	if err != nil {
		log.Fatal(err)
	}
	hc := healthcheck.NewDNSChecker(target, *port)
	hc.Mark = *mark
	hc.Answer = *dnsAnswer
	hc.Question.Name = *dnsQuery
	hc.Question.Qtype = qt
	check(hc)
}

func doHTTPCheck(target net.IP, secure bool) {
	hc := healthcheck.NewHTTPChecker(target, *port)
	hc.Mark = *mark
	hc.Secure = secure
	hc.Request = unquote(*request)
	hc.Response = unquote(*response)
	hc.ResponseCode = *responseCode
	hc.Method = *method
	hc.Proxy = *proxy
	hc.TLSVerify = *tlsVerify
	check(hc)
}

func doPingCheck(target net.IP) {
	pc := healthcheck.NewPingChecker(target)
	pc.Mark = *mark
	received := 0
	for i := 0; i < *count; i++ {
		r := pc.Check(time.Duration(0))
		if !r.Success {
			log.Printf("Failed to ping %v: %v", target, r)
			continue
		}
		received++
		log.Printf("Received reply from %v in %v", target, r.Duration)
	}
	log.Printf("Sent %d packets, received %d replies", *count, received)
}

func doRADIUSCheck(target net.IP) {
	hc := healthcheck.NewRADIUSChecker(target, *port)
	hc.Mark = *mark
	hc.Username = *radiusUser
	hc.Password = *radiusPasswd
	hc.Response = *radiusResponse
	hc.Secret = *radiusSecret
	check(hc)
}

func doTCPCheck(target net.IP, secure bool) {
	hc := healthcheck.NewTCPChecker(target, *port)
	hc.Mark = *mark
	hc.Receive = unquote(*receive)
	hc.Send = unquote(*send)
	hc.Secure = secure
	hc.TLSVerify = *tlsVerify
	check(hc)
}

func doUDPCheck(target net.IP) {
	hc := healthcheck.NewUDPChecker(target, *port)
	hc.Mark = *mark
	hc.Receive = unquote(*receive)
	hc.Send = unquote(*send)
	check(hc)
}

func main() {
	flag.Parse()
	target := net.ParseIP(*ip)
	if target == nil {
		log.Fatalf("Invalid IP address: %v", *ip)
	}

	switch *hcType {
	case "dns":
		doDNSCheck(target)
	case "http":
		doHTTPCheck(target, false)
	case "https":
		doHTTPCheck(target, true)
	case "ping":
		doPingCheck(target)
	case "radius":
		doRADIUSCheck(target)
	case "tcp":
		doTCPCheck(target, false)
	case "tcp_tls":
		doTCPCheck(target, true)
	case "udp":
		doUDPCheck(target)
	default:
		log.Fatalf("Unsupported healthcheck type: %q", *hcType)
	}
}
