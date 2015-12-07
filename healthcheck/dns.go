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

// DNS healthcheck implementation.

package healthcheck

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/google/seesaw/common/seesaw"

	"github.com/miekg/dns"
)

const (
	defaultDNSTimeout = 3 * time.Second
)

// DNSType returns the dnsType that corresponds with the given name.
func DNSType(name string) (uint16, error) {
	dt, ok := dns.StringToType[strings.ToUpper(name)]
	if !ok {
		return 0, fmt.Errorf("unknown DNS type %q", name)
	}
	return dt, nil
}

// DNSChecker contains configuration specific to a DNS healthcheck.
type DNSChecker struct {
	Target
	Question dns.Question
	Answer   string
}

// NewDNSChecker returns an initialised DNSChecker.
func NewDNSChecker(ip net.IP, port int) *DNSChecker {
	return &DNSChecker{
		Target: Target{
			IP:    ip,
			Port:  port,
			Proto: seesaw.IPProtoUDP,
		},
		Question: dns.Question{
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeA,
		},
	}
}

func questionToString(q dns.Question) string {
	return fmt.Sprintf("%s %s %s", q.Name, dns.Class(q.Qclass), dns.Type(q.Qtype))
}

// String returns the string representation of a DNS healthcheck.
func (hc *DNSChecker) String() string {
	return fmt.Sprintf("DNS %s %s", questionToString(hc.Question), hc.Target)
}

// Check executes a DNS healthcheck.
func (hc *DNSChecker) Check(timeout time.Duration) *Result {
	if !strings.HasSuffix(hc.Question.Name, ".") {
		hc.Question.Name += "."
	}

	msg := fmt.Sprintf("DNS %s query to port %d", questionToString(hc.Question), hc.Port)
	start := time.Now()
	if timeout == time.Duration(0) {
		timeout = defaultDNSTimeout
	}
	deadline := start.Add(timeout)

	var aIP net.IP
	switch hc.Question.Qtype {
	case dns.TypeA:
		if aIP = net.ParseIP(hc.Answer); aIP == nil || aIP.To4() == nil {
			msg = fmt.Sprintf("%s; %q is not a valid IPv4 address", msg, hc.Answer)
			return complete(start, msg, false, nil)
		}
	case dns.TypeAAAA:
		if aIP = net.ParseIP(hc.Answer); aIP == nil {
			msg = fmt.Sprintf("%s; %q is not a valid IPv6 address", msg, hc.Answer)
			return complete(start, msg, false, nil)
		}
	}

	// Build DNS query.
	q := &dns.Msg{
		MsgHdr: dns.MsgHdr{
			Id:               dns.Id(),
			RecursionDesired: true,
		},
		Question: []dns.Question{hc.Question},
	}

	// TODO(mharo): don't assume UDP
	conn, err := dialUDP(hc.network(), hc.addr(), timeout, hc.Mark)
	if err != nil {
		return complete(start, msg, false, err)
	}
	defer conn.Close()

	err = conn.SetDeadline(deadline)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to set deadline", msg)
		return complete(start, msg, false, err)
	}

	dnsConn := &dns.Conn{Conn: conn}
	if err := dnsConn.WriteMsg(q); err != nil {
		msg = fmt.Sprintf("%s; failed to send request", msg)
		return complete(start, msg, false, err)
	}

	r, err := dnsConn.ReadMsg()
	if err != nil {
		msg = fmt.Sprintf("%s; failed to read response", msg)
		return complete(start, msg, false, err)
	}

	// Check reply.
	if !r.Response {
		msg = fmt.Sprintf("%s; not a query response", msg)
		return complete(start, msg, false, nil)
	}
	if rc := r.Rcode; rc != dns.RcodeSuccess {
		msg = fmt.Sprintf("%s; non-zero response code - %s", msg, rc)
		return complete(start, msg, false, nil)
	}
	if len(r.Answer) < 1 {
		msg = fmt.Sprintf("%s; no answers received for query %s", msg, questionToString(hc.Question))
		return complete(start, msg, false, nil)
	}

	// TODO(jsing): Compare query to original?
	// if q.Question != r.Question ...

	for _, rr := range r.Answer {
		if rr.Header().Name != hc.Question.Name {
			continue
		}
		if rr.Header().Class != hc.Question.Qclass {
			continue
		}
		if rr.Header().Rrtype != hc.Question.Qtype {
			// TODO(jsing): Follow CNAMEs for A/AAAA queries.
			continue
		}

		switch rr := rr.(type) {
		case *dns.A:
			if aIP.Equal(rr.A) {
				msg = fmt.Sprintf("%s; received answer %s", msg, rr.A)
				return complete(start, msg, true, err)
			}
		case *dns.AAAA:
			if aIP.Equal(rr.AAAA) {
				msg = fmt.Sprintf("%s; received answer %s", msg, rr.AAAA)
				return complete(start, msg, true, err)
			}
		case *dns.CNAME:
			// TODO(jsing): Implement comparision.
		case *dns.NS:
			// TODO(jsing): Implement comparision.
		case *dns.SOA:
			// TODO(jsing): Implement comparision.
		}
	}

	msg = fmt.Sprintf("%s; failed to match answer", msg)
	return complete(start, msg, false, err)
}
