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
	Perform end-to-end tests between the Go IPVS implementation and
	the Linux kernel by flushing the IPVS table, adding specified
	services and destinations, then getting the services and destinations
	back from the kernel. If things worked correctly we should get the
	same services and destinations that we added.

	Note: This must be run as root hence it being a manual testing tool.
*/

package main

import (
	"log"
	"net"
	"reflect"
	"syscall"

	"github.com/google/seesaw/ipvs"
)

var ipvsTests = []struct {
	desc         string
	service      ipvs.Service
	destinations []ipvs.Destination
}{

	{
		"IPv4 1.1.1.1 TCP/80 using wlc with four destinations",
		ipvs.Service{
			Address:   net.ParseIP("1.1.1.1"),
			Protocol:  syscall.IPPROTO_TCP,
			Port:      80,
			Scheduler: "wlc",
		},
		[]ipvs.Destination{
			{Address: net.ParseIP("1.1.1.2"), Port: 80, Weight: 1},
			{Address: net.ParseIP("1.1.1.3"), Port: 80, Weight: 1},
			{Address: net.ParseIP("1.1.1.4"), Port: 80, Weight: 1},
			{Address: net.ParseIP("1.1.1.5"), Port: 80, Weight: 1},
		},
	},

	{
		"IPv6 2012::1 UDP/53 using wrr with four destinations",
		ipvs.Service{
			Address:   net.ParseIP("2012::1"),
			Protocol:  syscall.IPPROTO_UDP,
			Port:      53,
			Scheduler: "wrr",
		},
		[]ipvs.Destination{
			{Address: net.ParseIP("2012::2"), Port: 53, Weight: 1},
			{Address: net.ParseIP("2012::3"), Port: 53, Weight: 1},
			{Address: net.ParseIP("2012::4"), Port: 53, Weight: 1},
			{Address: net.ParseIP("2012::5"), Port: 53, Weight: 1},
		},
	},

	{
		"IPv4 FWM 1 using wlc with four destinations",
		ipvs.Service{
			Address:      net.ParseIP("0.0.0.0"),
			FirewallMark: 1,
			Port:         0,
			Scheduler:    "wlc",
		},
		[]ipvs.Destination{
			{Address: net.ParseIP("1.1.1.2"), Port: 0, Weight: 1},
			{Address: net.ParseIP("1.1.1.3"), Port: 0, Weight: 1},
			{Address: net.ParseIP("1.1.1.4"), Port: 0, Weight: 1},
			{Address: net.ParseIP("1.1.1.5"), Port: 0, Weight: 1},
		},
	},

	{
		"IPv6 FWM 2 using wrr with four destinations",
		ipvs.Service{
			Address:      net.ParseIP("::0"),
			FirewallMark: 2,
			Port:         0,
			Scheduler:    "wrr",
		},
		[]ipvs.Destination{
			{Address: net.ParseIP("2012::2"), Port: 0, Weight: 1},
			{Address: net.ParseIP("2012::3"), Port: 0, Weight: 1},
			{Address: net.ParseIP("2012::4"), Port: 0, Weight: 1},
			{Address: net.ParseIP("2012::5"), Port: 0, Weight: 1},
		},
	},
}

// compareSvc attempts to locate a matching service within the given list of
// services. If a match is found a deep comparision is performed and the
// service is returned to the caller.
func compareSvc(want *ipvs.Service, have []*ipvs.Service) *ipvs.Service {
	log.Printf("=> Looking for service %s\n", want)
	var svc *ipvs.Service
	for _, s := range have {
		if want.Address.Equal(s.Address) && want.Port == s.Port && want.FirewallMark == s.FirewallMark {
			svc = s
			break
		}
	}
	if svc == nil {
		log.Println("ERROR: Failed to find service!")
		return nil
	}
	log.Printf("=> Got matching service %s", svc)

	// Service.Flags comes back with additional flags set.
	svc.Flags &= ^ipvs.SFHashed

	// If want does not have Destinations or Statistics then do not try
	// to compare them.
	dsts := svc.Destinations
	if want.Destinations == nil {
		svc.Destinations = nil
	}
	stats := svc.Statistics
	if want.Statistics == nil {
		svc.Statistics = nil
	}
	if !reflect.DeepEqual(svc, want) {
		log.Printf("ERROR: Services do not match - got %#v, want %#v\n",
			svc, want)
	}
	svc.Destinations = dsts
	svc.Statistics = stats

	return svc
}

// compareDst attempts to locate a matching destination within the given list
// of destinations. If a match is found a deep comparision is performed and the
// destination is returned to the caller.
func compareDst(want *ipvs.Destination, have []*ipvs.Destination) *ipvs.Destination {
	log.Printf("--> Looking for destination %s\n", want)
	var dst *ipvs.Destination
	for _, d := range have {
		if want.Address.Equal(d.Address) && want.Port == d.Port {
			dst = d
			break
		}
	}
	if dst == nil {
		log.Println("ERROR: Failed to find destination!")
		return nil
	}
	log.Printf("--> Got matching destination %s", dst)

	// If want does not have statistics then do not try to compare them.
	stats := dst.Statistics
	if want.Statistics == nil {
		dst.Statistics = nil
	}
	if !reflect.DeepEqual(dst, want) {
		log.Printf("ERROR: Destinations do not match - got %#v, want %#v\n",
			dst, want)
	}
	dst.Statistics = stats

	return dst
}

// testIPVS adds all of the services and destinations provided via the testing
// table, then gets all of the services and destinations from IPVS before
// comparing and reporting on the result. If everything worked correctly we
// should end up with an exact match between what we added and what we got back
// from IPVS.
func testIPVS() {
	log.Println("Testing IPVS...")

	// Make sure we have a clean slate.
	if err := ipvs.Flush(); err != nil {
		log.Fatalf("Failed to flush IPVS table: %v\n", err)
	}

	// Add the test services and destinations to IPVS.
	log.Println("Adding services and destinations...")
	for _, test := range ipvsTests {
		svc := test.service
		log.Println(test.desc)
		log.Printf("=> Adding service %s\n", svc)
		svc.Destinations = nil
		for i := range test.destinations {
			dst := &test.destinations[i]
			log.Printf("--> Destination %s\n", dst)
			svc.Destinations = append(svc.Destinations, dst)
		}
		if err := ipvs.AddService(svc); err != nil {
			log.Fatalf("ipvs.AddService() failed: %v\n", err)
		}
	}

	// Get the services from IPVS and compare to what we added. Note that
	// the order of the returned services and destinations are not
	// guaranteed to match the order in which they were added.
	log.Println("Getting services from IPVS...")
	svcs, err := ipvs.GetServices()
	if err != nil {
		log.Fatalf("ipvs.GetServices() failed: %v\n", err)
	}
	if len(ipvsTests) != len(svcs) {
		log.Printf("ERROR: Added %d services but got %d!\n",
			len(ipvsTests), len(svcs))
	}
	for _, test := range ipvsTests {
		svc := compareSvc(&test.service, svcs)
		if svc == nil {
			continue
		}
		if len(test.destinations) != len(svc.Destinations) {
			log.Printf("ERROR: Added %d destinations by got %d!\n",
				len(test.destinations), len(svc.Destinations))
		}
		for _, testDst := range test.destinations {
			compareDst(&testDst, svc.Destinations)
		}
	}
}

// testIPVSModification adds, updates and deletes services and destinations
// from the test configurations. At each step the service or destination is
// retrieved from IPVS and compared against what should be configured.
func testIPVSModification() {
	log.Println("Testing IPVS modifications...")

	// Make sure we have a clean slate.
	if err := ipvs.Flush(); err != nil {
		log.Fatalf("Failed to flush IPVS table: %v\n", err)
	}

	for _, test := range ipvsTests {
		var svc *ipvs.Service
		var err error
		testSvc := test.service
		testDst := test.destinations[0]

		// Add service.
		log.Printf("=> Adding service %s\n", testSvc)
		if err = ipvs.AddService(testSvc); err != nil {
			log.Fatalf("ipvs.AddService() failed: %v\n", err)
		}
		if svc, err = ipvs.GetService(&testSvc); err != nil {
			log.Fatalf("ipvs.GetService() failed: %v\n", err)
		}
		compareSvc(&testSvc, []*ipvs.Service{svc})

		// Add destination.
		log.Printf("--> Adding destination %s\n", testDst)
		if err = ipvs.AddDestination(testSvc, testDst); err != nil {
			log.Fatalf("ipvs.AddDestination() failed: %v\n", err)
		}
		if svc, err = ipvs.GetService(&testSvc); err != nil {
			log.Fatalf("ipvs.GetService() failed: %v\n", err)
		}
		compareSvc(&testSvc, []*ipvs.Service{svc})
		compareDst(&testDst, svc.Destinations)

		// Update service.
		testSvc.Scheduler = "lc"
		if err = ipvs.UpdateService(testSvc); err != nil {
			log.Fatalf("ipvs.UpdateService() failed: %v\n", err)
		}
		if svc, err = ipvs.GetService(&testSvc); err != nil {
			log.Fatalf("ipvs.GetService() failed: %v\n", err)
		}
		compareSvc(&testSvc, []*ipvs.Service{svc})
		compareDst(&testDst, svc.Destinations)

		// Update destination.
		testDst.Weight = 1000
		if err = ipvs.UpdateDestination(testSvc, testDst); err != nil {
			log.Fatalf("ipvs.UpdateDestination() failed: %v\n", err)
		}
		if svc, err = ipvs.GetService(&testSvc); err != nil {
			log.Fatalf("ipvs.GetService() failed: %v\n", err)
		}
		compareSvc(&testSvc, []*ipvs.Service{svc})
		compareDst(&testDst, svc.Destinations)

		// Delete destination.
		if err = ipvs.DeleteDestination(testSvc, testDst); err != nil {
			log.Fatalf("ipvs.DeleteDestination() failed: %v\n", err)
		}
		if svc, err = ipvs.GetService(&testSvc); err != nil {
			log.Fatalf("ipvs.GetService() failed: %v\n", err)
		}
		if len(svc.Destinations) != 0 {
			log.Printf("ERROR: Service still has destinations\n")
		}

		// Delete service.
		if err = ipvs.DeleteService(testSvc); err != nil {
			log.Fatalf("ipvs.DeleteService() failed: %v\n", err)
		}

		// Make sure there is nothing left behind.
		svcs, err := ipvs.GetServices()
		if err != nil {
			log.Fatalf("ipvs.GetServices() failed: %v\n", err)
		}
		if len(svcs) != 0 {
			log.Printf("ERROR: IPVS services still exist!\n")
			for _, svc = range svcs {
				log.Printf("=> Got service %s\n", svc)
			}
		}
	}
}

func main() {
	if err := ipvs.Init(); err != nil {
		log.Fatalf("IPVS initialisation failed: %v\n", err)
	}
	log.Printf("IPVS version %s\n", ipvs.Version())

	testIPVS()
	testIPVSModification()

	// Clean up after ourselves...
	if err := ipvs.Flush(); err != nil {
		log.Fatalf("Failed to flush IPVS table: %v\n", err)
	}
}
