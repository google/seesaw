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
	Perform end-to-end tests between the Go Quagga library and the Quagga
	BGP daemon, by advertising and withdrawing a network. If things work
	correctly the returned configuration should include (or not include)
	the given network statements. We also request the current BGP neighbors
	and display their particulars.

	Note: This must be run as root hence it being a manual testing tool.
*/

package main

import (
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/google/seesaw/quagga"
)

const testAnycastStr = "172.16.255.14/32"

func main() {
	network := fmt.Sprintf("network %s", testAnycastStr)
	ip, testAnycast, err := net.ParseCIDR(testAnycastStr)
	if err != nil {
		log.Fatal(err)
	}
	testAnycast.IP = ip

	log.Print("Connecting to Quagga BGP daemon...")
	bgp := quagga.NewBGP("", 64512)
	if err := bgp.Dial(); err != nil {
		log.Fatalf("BGP dial failed: %v", err)
	}
	if err := bgp.Enable(); err != nil {
		log.Fatalf("BGP enabled failed: %v", err)
	}

	neighbors, err := bgp.Neighbors()
	if err != nil {
		log.Fatalf("Failed to get neighbors: %v", err)
	}
	log.Printf("Got %d neighbor(s)", len(neighbors))
	for _, n := range neighbors {
		log.Printf("Neighbor %v, remote AS %v", n.IP, n.ASN)
		log.Printf(" State %v, uptime %v", n.BGPState, n.Uptime)
	}

	// Get the current configuration and ensure that the network statement
	// does not already exist.
	cfg, err := bgp.Configuration()
	if err != nil {
		log.Fatalf("Failed to get BGP configuration: %v", err)
	}
	for _, s := range cfg {
		if strings.Contains(s, network) {
			log.Printf("Network statement already exists - %q", s)
		}
	}
	ocfg := cfg

	// Advertise and withdraw the test anycast address.
	log.Printf("Advertising network %s", testAnycastStr)
	if err := bgp.Advertise(testAnycast); err != nil {
		log.Fatalf("Failed to advertise network: %v", err)
	}
	cfg, err = bgp.Configuration()
	if err != nil {
		log.Fatalf("Failed to get BGP configuration: %v", err)
	}
	found := false
	for _, s := range cfg {
		if strings.Contains(s, network) {
			found = true
		}
	}
	if !found {
		log.Printf("Failed to find network statement - %s", network)
	}
	log.Printf("Withdrawing network %s", testAnycastStr)
	if bgp.Withdraw(testAnycast); err != nil {
		log.Fatalf("Failed to withdraw network: %v", err)
	}

	// The configuration should now match what we started with...
	cfg, err = bgp.Configuration()
	if err != nil {
		log.Fatalf("Failed to get BGP configuration: %v", err)
	}
	if len(ocfg) != len(cfg) {
		log.Printf("Number of lines in configuration changed: %d != %d",
			len(ocfg), len(cfg))
	}
	for i := range ocfg {
		if ocfg[i] != cfg[i] {
			log.Printf("Configuration lines mismatched: %q != %q",
				ocfg[i], cfg[i])
			break
		}
	}

	log.Print("Disconnecting from Quagga BGP daemon")
	if err := bgp.Close(); err != nil {
		log.Fatalf("Failed to close BGP connection: %v", err)
	}
	log.Print("Tests completed!")
}
