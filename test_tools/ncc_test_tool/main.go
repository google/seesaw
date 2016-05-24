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
Perform end-to-end tests of the Seesaw NCC component and client.

Note: This needs to talk to the NCC, hence it being a manual testing tool.
*/

package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ipvs"
	"github.com/google/seesaw/ncc/client"
	"github.com/google/seesaw/ncc/types"
)

var (
	ipCmd       = "/sbin/ip"
	testAnycast = net.ParseIP("172.16.255.14")

	nccSocket     = flag.String("ncc", seesaw.NCCSocket, "Seesaw NCC socket")
	testClass     = flag.String("test", "", "Class of tests to run")
	testIface     = flag.String("iface", "dummy1", "Interface to use for tests")
	nodeIPStr     = flag.String("nodeip_v4", "", "IPv4 address of the seesaw node")
	nodeIPv6Str   = flag.String("nodeip_v6", "", "IPv6 address of the seesaw node")
	vlanNodeIPStr = flag.String("vlan_nodeip_v4", "10.0.0.1/24",
		"IPv4 address and prefix length of the seesaw on the VLAN in CIDR format")
	vlanNodeIPv6Str = flag.String("vlan_nodeip_v6", "fe80::1/64",
		"IPv6 address and prefix length of the seesaw on the VLAN in CIDR format")
	clusterVIPStr   = flag.String("vip_v4", "10.10.10.10", "Cluster IPv4 VIP address")
	unicastVIPStr   = flag.String("unicast_vip_v4", "10.10.10.11", "Unicast IPv4 VIP Address")
	unicastVIPv6Str = flag.String("unicast_vip_v6", "2401:fa00:c:03FF::1",
		"IPv6 Unicast Address")
	vlanID         = flag.Int("vlan_id", 1234, "VLAN ID of the VLAN interface")
	vlanVIPStr     = flag.String("vlan_vip_v4", "10.0.0.10", "VIP IPv4 address on the VLAN")
	routingTableID = flag.Int("rtid", 2, "Routing table ID")
	vrID           = flag.Int("vrid", 61, "VRRP ID")
)

func arpTests(ncc client.NCC) {
	// Send a gratuitous ARP message.
	vip := net.ParseIP(*clusterVIPStr)
	if vip == nil {
		log.Fatalf("Invalid cluster VIP - %q", *clusterVIPStr)
	}
	log.Print("Sending gratuitous ARP...")
	if err := ncc.ARPSendGratuitous(*testIface, vip); err != nil {
		log.Fatalf("Failed to send gratuitous ARP: %v", err)
	}
	log.Print("Done.")
}

func bgpTests(ncc client.NCC) {
	// Display the BGP neighbors.
	log.Print("Getting BGP neighbors...")
	neighbors, err := ncc.BGPNeighbors()
	if err != nil {
		log.Fatalf("Failed to get BGP neighbors")
	}
	for _, n := range neighbors {
		log.Printf("- BGP neighbor %v, remote AS %d, state %v, uptime %v seconds",
			n.IP, n.ASN, n.BGPState, n.Uptime.Seconds())
	}

	// Withdraw all existing network advertisements.
	log.Printf("Withdrawing all network advertisements")
	if err := ncc.BGPWithdrawAll(); err != nil {
		log.Fatalf("Failed to withdraw BGP advertisements: %v", err)
	}

	// Advertise and withdraw the test-anycast VIP.
	log.Printf("Advertising and withdrawing VIP %v", testAnycast)
	if err := ncc.BGPAdvertiseVIP(testAnycast); err != nil {
		log.Fatalf("Failed to advertise VIP %v: %v", testAnycast, err)
	}
	cfg, err := ncc.BGPConfig()
	if err != nil {
		log.Fatal("Failed to get BGP configuration")
	}
	found := false
	ns := fmt.Sprintf(" network %v/32", testAnycast)
	for _, l := range cfg {
		if l == ns {
			found = true
			break
		}
	}
	if !found {
		log.Fatal("Failed to find network statement")
	}
	if err := ncc.BGPWithdrawVIP(testAnycast); err != nil {
		log.Fatalf("Failed to withdraw VIP %v: %v", testAnycast, err)
	}
}

func lbInterface(ncc client.NCC) client.LBInterface {
	iface := *testIface
	nodeIP := net.ParseIP(*nodeIPStr)
	nodeIPv6 := net.ParseIP(*nodeIPv6Str)
	if nodeIP == nil && nodeIPv6 == nil {
		log.Fatalf("Invalid node IP/IPv6 - %q/%q", *nodeIPStr, *nodeIPv6Str)
	}

	unicastVIP := net.ParseIP(*unicastVIPStr)
	if unicastVIP == nil {
		log.Fatalf("Invalid unicast VIP - %q", *unicastVIPStr)
	}
	vip := net.ParseIP(*clusterVIPStr)
	if vip == nil {
		log.Fatalf("Invalid cluster VIP - %q", *clusterVIPStr)
	}
	if *vrID < 1 || *vrID > 255 {
		log.Fatalf("Invalid VRID - %q", *vrID)
	}
	rtID := uint8(*routingTableID)
	// TODO(jsing): Change this to allow both IPv4 and IPv6 addresses.
	lbCfg := &types.LBConfig{
		ClusterVIP:     seesaw.Host{IPv4Addr: vip},
		Node:           seesaw.Host{IPv4Addr: nodeIP, IPv6Addr: nodeIPv6},
		DummyInterface: "dummy0",
		NodeInterface:  "eth0",
		RoutingTableID: rtID,
		VRID:           uint8(*vrID),
	}
	lb := ncc.NewLBInterface(iface, lbCfg)
	if err := lb.Init(); err != nil {
		log.Fatalf("Failed to initialise the LB interface: %v", err)
	}
	return lb
}

func interfaceTests(ncc client.NCC) {
	iface := *testIface
	vip := net.ParseIP(*clusterVIPStr)
	rtID := uint8(*routingTableID)

	out, err := exec.Command(ipCmd, "link", "show", "dev", iface).Output()
	if err != nil {
		log.Fatalf("Failed to get link state for %v: %v", iface, err)
	}
	if !strings.Contains(string(out), " state DOWN ") {
		log.Fatalf("Link state for %v is not DOWN: %v", iface, string(out))
	}

	out, err = exec.Command(ipCmd, "addr", "show", "dev", iface).Output()
	if err != nil {
		log.Fatalf("Failed to get addresses for %v: %v", iface, err)
	}
	if !strings.Contains(string(out), fmt.Sprintf(" inet %s/", vip)) {
		log.Fatalf("VIP address is not configured on interface: %v", string(out))
	}

	out, err = exec.Command(ipCmd, "rule", "show").Output()
	if err != nil {
		log.Fatalf("Failed to get routing policy: %v", err)
	}
	rpFrom := fmt.Sprintf("from %s lookup %d", vip, rtID)
	if !strings.Contains(string(out), rpFrom) {
		log.Fatalf("Routing policy (from) is not correctly configured for %s: %v", vip, string(out))
	}
	rpTo := fmt.Sprintf("from all to %s lookup %d", vip, rtID)
	if !strings.Contains(string(out), rpTo) {
		log.Fatalf("Routing policy (to) is not correctly configured for %s: %v", vip, string(out))
	}

	lb := lbInterface(ncc)

	// Add a unicast VIP.
	unicastVIP := seesaw.NewVIP(net.ParseIP(*unicastVIPStr), nil)
	if err := lb.AddVIP(unicastVIP); err != nil {
		log.Fatalf("Failed to add VIP %v: %v", unicastVIP, err)
	}

	out, err = exec.Command(ipCmd, "addr", "show", "dev", iface).Output()
	if err != nil {
		log.Fatalf("Failed to get addresses for %v: %v", iface, err)
	}
	if !strings.Contains(string(out), fmt.Sprintf(" inet %s/", unicastVIP)) {
		log.Fatalf("Unicast VIP address is not configured on interface: %v", string(out))
	}

	// Going up...
	if err := lb.Up(); err != nil {
		log.Fatalf("Failed to bring LB interface up: %v", err)
	}

	out, err = exec.Command(ipCmd, "link", "show", "dev", iface).Output()
	if err != nil {
		log.Fatalf("Failed to get link state for %v: %v", iface, err)
	}
	if !strings.Contains(string(out), ",UP,") {
		log.Fatalf("Interface %v is not UP: %v", iface, string(out))
	}

	// Remove unicast VIP.
	if err := lb.DeleteVIP(unicastVIP); err != nil {
		log.Fatalf("Failed to remove unicast VIP %v: %v", unicastVIP, err)
	}

	out, err = exec.Command(ipCmd, "addr", "show", "dev", iface).Output()
	if err != nil {
		log.Fatalf("Failed to get addresses for %v: %v", iface, err)
	}
	if strings.Contains(string(out), fmt.Sprintf(" inet %s/", unicastVIP)) {
		log.Fatalf("Unicast VIP address is still configured on interface: %v", string(out))
	}

	// Add VLAN
	ipv4Addr, ipv4Net, err := net.ParseCIDR(*vlanNodeIPStr)
	if err != nil {
		log.Fatalf("Failed to parse VLAN node IPv4 address: %v", err)
	}
	ipv6Addr, ipv6Net, err := net.ParseCIDR(*vlanNodeIPv6Str)
	if err != nil {
		log.Fatalf("Failed to parse VLAN node IPv6 address: %v", err)
	}
	vlan := &seesaw.VLAN{
		ID: uint16(*vlanID),
		Host: seesaw.Host{
			IPv4Addr: ipv4Addr,
			IPv4Mask: ipv4Net.Mask,
			IPv6Addr: ipv6Addr,
			IPv6Mask: ipv6Net.Mask,
		},
	}
	if err := lb.AddVLAN(vlan); err != nil {
		log.Fatalf("Failed to create VLAN %d: %v", vlan.ID, err)
	}
	vlanIface := fmt.Sprintf("%s.%d", iface, vlan.ID)
	out, err = exec.Command(ipCmd, "link", "show", "dev", vlanIface).Output()
	if err != nil {
		log.Fatalf("Failed to get link state for %v: %v", vlanIface, err)
	}
	if !strings.Contains(string(out), ",UP,") {
		log.Fatalf("Interface %v is not UP: %v", vlanIface, string(out))
	}

	// Add a VIP on the VLAN
	vlanVIP := seesaw.NewVIP(net.ParseIP(*vlanVIPStr), nil)
	if err := lb.AddVIP(vlanVIP); err != nil {
		log.Fatalf("Failed to add VIP %v: %v", vlanVIP, err)
	}
	out, err = exec.Command(ipCmd, "addr", "show", "dev", vlanIface).Output()
	if err != nil {
		log.Fatalf("Failed to get addresses for %v: %v", vlanIface, err)
	}
	if !strings.Contains(string(out), fmt.Sprintf(" inet %s/", vlanVIP)) {
		log.Fatalf("VLAN VIP address is not configured on interface: %v", string(out))
	}

	// Remove the VIP from the VLAN
	if err := lb.DeleteVIP(vlanVIP); err != nil {
		log.Fatalf("Failed to delete VIP %v: %v", vlanVIP, err)
	}
	out, err = exec.Command(ipCmd, "addr", "show", "dev", vlanIface).Output()
	if err != nil {
		log.Fatalf("Failed to get addresses for %v: %v", vlanIface, err)
	}
	if strings.Contains(string(out), fmt.Sprintf("%s", vlanVIP)) {
		log.Fatalf("VLAN VIP address is still configured on interface: %v", string(out))
	}

	// Remove VLAN
	if err := lb.DeleteVLAN(vlan); err != nil {
		log.Fatalf("Failed to delete VLAN %d: %v", vlan.ID, err)
	}

	// Going down...
	if err := lb.Down(); err != nil {
		log.Fatalf("Failed to bring LB interface down: %v", err)
	}

	out, err = exec.Command(ipCmd, "link", "show", "dev", iface).Output()
	if err != nil {
		log.Fatalf("Failed to get link state for %v: %v", iface, err)
	}
	if !strings.Contains(string(out), " state DOWN ") {
		log.Fatalf("Link state for %v is not DOWN: %v", iface, string(out))
	}
}

func iptablesRuleExists(af seesaw.AF, rule string) bool {
	var iptCmd string
	switch af {
	case seesaw.IPv4:
		iptCmd = "/sbin/iptables"
	case seesaw.IPv6:
		iptCmd = "/sbin/ip6tables"
	default:
		log.Fatalf("Unsupported AF for rule %#v", rule)
	}
	args := []string{"-C"}
	args = append(args, strings.Split(rule, " ")...)
	cmd := exec.Command(iptCmd, args...)
	err := cmd.Run()
	return err == nil
}

func vserverTest(lb client.LBInterface, af seesaw.AF, v *seesaw.Vserver, rules []string) {
	for _, rule := range rules {
		if iptablesRuleExists(af, rule) {
			log.Fatalf("iptables rule %q already exists", rule)
		}
	}
	if err := lb.AddVserver(v, af); err != nil {
		log.Fatalf("Failed to install iptables rules: %v", err)
	}
	for _, rule := range rules {
		if !iptablesRuleExists(af, rule) {
			log.Fatalf("iptables rule %q not installed", rule)
		}
	}
	if err := lb.DeleteVserver(v, af); err != nil {
		log.Fatalf("Failed to delete iptables rules: %v", err)
	}
	for _, rule := range rules {
		if iptablesRuleExists(af, rule) {
			log.Fatalf("iptables rule %q still exists", rule)
		}
	}
}

func vserverTests(ncc client.NCC) {
	lbIface := lbInterface(ncc)

	// Non FWMark, IPv4 only, DSR
	vip := net.ParseIP(*unicastVIPStr)
	node := net.ParseIP(*nodeIPStr)
	ve1 := &seesaw.VserverEntry{
		Proto: seesaw.IPProtoTCP,
		Port:  53,
	}
	ve2 := &seesaw.VserverEntry{
		Proto: seesaw.IPProtoUDP,
		Port:  53,
	}
	v := &seesaw.Vserver{
		Host:    seesaw.Host{IPv4Addr: vip},
		Entries: []*seesaw.VserverEntry{ve1, ve2},
		FWM:     make(map[seesaw.AF]uint32),
	}
	rules := []string{
		fmt.Sprintf("INPUT -p tcp -d %v --dport 53 -j ACCEPT", vip),
		fmt.Sprintf("INPUT -p udp -d %v --dport 53 -j ACCEPT", vip),
		fmt.Sprintf("INPUT -p icmp -d %v -j ACCEPT", vip),
		fmt.Sprintf("INPUT -p ipv6-icmp -d %v -j ACCEPT", vip),
		fmt.Sprintf("INPUT -d %v -j REJECT", vip),
	}
	vserverTest(lbIface, seesaw.IPv4, v, rules)

	// Non FWMark, IPv4 only, NAT
	ve1.Mode = seesaw.LBModeNAT
	ve2.Mode = seesaw.LBModeNAT
	natRules := []string{
		fmt.Sprintf("POSTROUTING -t nat -m ipvs -p tcp --vport 53 --vaddr %v -j SNAT --to-source %v --random", vip, node),
		fmt.Sprintf("POSTROUTING -t nat -m ipvs -p udp --vport 53 --vaddr %v -j SNAT --to-source %v --random", vip, node),
	}
	rules = append(rules, natRules...)
	vserverTest(lbIface, seesaw.IPv4, v, rules)

	// FWMark, IPv4 only, DSR
	ve1.Mode = seesaw.LBModeDSR
	ve2.Mode = seesaw.LBModeDSR
	v.FWM[seesaw.IPv4] = 1
	rules = []string{
		fmt.Sprintf("INPUT -p tcp -d %v --dport 53 -j ACCEPT", vip),
		fmt.Sprintf("INPUT -p udp -d %v --dport 53 -j ACCEPT", vip),
		fmt.Sprintf("INPUT -p icmp -d %v -j ACCEPT", vip),
		fmt.Sprintf("INPUT -p ipv6-icmp -d %v -j ACCEPT", vip),
		fmt.Sprintf("INPUT -d %v -j REJECT", vip),
		fmt.Sprintf("PREROUTING -t mangle -p tcp -d %v --dport 53 -j MARK --set-mark 1", vip),
		fmt.Sprintf("PREROUTING -t mangle -p udp -d %v --dport 53 -j MARK --set-mark 1", vip),
	}
	vserverTest(lbIface, seesaw.IPv4, v, rules)

	// FWMark, IPv6 only, DSR
	vip6 := net.ParseIP(*unicastVIPv6Str)
	v.Host.IPv6Addr = vip6
	v.Host.IPv4Addr = nil
	v.FWM[seesaw.IPv6] = 2
	rules6 := []string{
		fmt.Sprintf("INPUT -p tcp -d %v --dport 53 -j ACCEPT", vip6),
		fmt.Sprintf("INPUT -p udp -d %v --dport 53 -j ACCEPT", vip6),
		fmt.Sprintf("INPUT -p icmp -d %v -j ACCEPT", vip6),
		fmt.Sprintf("INPUT -p ipv6-icmp -d %v -j ACCEPT", vip6),
		fmt.Sprintf("INPUT -d %v -j REJECT", vip6),
		fmt.Sprintf("PREROUTING -t mangle -p tcp -d %v --dport 53 -j MARK --set-mark 2", vip6),
		fmt.Sprintf("PREROUTING -t mangle -p udp -d %v --dport 53 -j MARK --set-mark 2", vip6),
	}
	vserverTest(lbIface, seesaw.IPv6, v, rules6)

	// FWMark, IPv4 and IPv6, DSR
	v.Host.IPv4Addr = vip
	vserverTest(lbIface, seesaw.IPv4, v, rules)
	vserverTest(lbIface, seesaw.IPv6, v, rules6)
}

var testSvc = &ipvs.Service{
	Address:   net.ParseIP("1.1.1.1"),
	Protocol:  syscall.IPPROTO_TCP,
	Port:      80,
	Scheduler: "wlc",
	Destinations: []*ipvs.Destination{
		{Address: net.ParseIP("1.1.1.2"), Port: 80, Weight: 1},
		{Address: net.ParseIP("1.1.1.3"), Port: 80, Weight: 1},
		{Address: net.ParseIP("1.1.1.4"), Port: 80, Weight: 1},
		{Address: net.ParseIP("1.1.1.5"), Port: 80, Weight: 1},
	},
}

func ipvsGetServices(quit chan bool, count chan int) {
	ncc := client.NewNCC(*nccSocket)
	if err := ncc.Dial(); err != nil {
		log.Fatalf("Failed to connect to NCC: %v", err)
	}
	defer ncc.Close()

	i := 0
	for {
		select {
		case <-quit:
			count <- i
			return
		default:
		}
		if _, err := ncc.IPVSGetServices(); err != nil {
			log.Fatalf("Failed to get IPVS services: %v", err)
		}
		i++
	}
}

func ipvsGetLoadTest(clients int, duration time.Duration) {
	log.Printf("Starting IPVS get load test - %v duration with %d clients", duration, clients)

	// Hammer IPVS for a while...
	quit := make(chan bool, clients)
	count := make(chan int, clients)
	for i := 0; i < clients; i++ {
		go ipvsGetServices(quit, count)
	}
	time.Sleep(duration)
	for i := 0; i < clients; i++ {
		quit <- true
	}

	total := 0
	counts := make([]string, 0)
	for i := 0; i < clients; i++ {
		n := <-count
		counts = append(counts, strconv.Itoa(n))
		total += n
	}

	log.Printf("Retrieved IPVS services %d times (%v)", total, strings.Join(counts, " + "))
}

func ipvsAddService(done chan bool, service, dests int) {
	ncc := client.NewNCC(*nccSocket)
	if err := ncc.Dial(); err != nil {
		log.Fatalf("Failed to connect to NCC: %v", err)
	}
	defer ncc.Close()

	svc := &ipvs.Service{
		Address:   net.IPv4(1, 1, 1, 1),
		Protocol:  syscall.IPPROTO_TCP,
		Port:      uint16(10000 + service),
		Scheduler: "wlc",
	}
	if err := ncc.IPVSAddService(svc); err != nil {
		log.Fatalf("Failed to add IPVS service: %v", err)
	}

	for i := 0; i < dests; i++ {
		dst := &ipvs.Destination{
			Address: net.IPv4(1, 1, 1, byte(i+2)),
			Port:    uint16(10000 + service),
			Flags:   ipvs.DFForwardRoute,
			Weight:  1,
		}
		if err := ncc.IPVSAddDestination(svc, dst); err != nil {
			log.Fatalf("Failed to add IPVS destination: %v", err)
		}
	}

	done <- true
}

func ipvsAddLoadTest(ncc client.NCC, services, dests int) {
	log.Printf("Starting IPVS add load test - %d services, each with %d destinations", services, dests)

	if err := ncc.IPVSFlush(); err != nil {
		log.Fatalf("Failed to flush the IPVS table: %v", err)
	}

	done := make(chan bool)
	for i := 0; i < services; i++ {
		go ipvsAddService(done, i, dests)
	}
	for i := 0; i < services; i++ {
		<-done
	}

	if err := ncc.IPVSFlush(); err != nil {
		log.Fatalf("Failed to flush the IPVS table: %v", err)
	}

	log.Printf("Completed IPVS add load test")
}

func ipvsTests(ncc client.NCC) {

	// Manipulate the IPVS table.
	if err := ncc.IPVSFlush(); err != nil {
		log.Fatalf("Failed to flush the IPVS table: %v", err)
	}

	log.Printf("Adding IPVS service %v with %d destinations", testSvc, len(testSvc.Destinations))
	if err := ncc.IPVSAddService(testSvc); err != nil {
		log.Fatalf("Failed to add IPVS service: %v", err)
	}

	svcs, err := ncc.IPVSGetServices()
	if err != nil {
		log.Fatalf("Failed to get IPVS services: %v", err)
	}
	for _, svc := range svcs {
		log.Printf("Got IPVS service %v with %d destinations",
			svc, len(svc.Destinations))
	}
	if len(svcs) != 1 {
		log.Fatalf("IPVSGetServices returned %d services, expected 1", len(svcs))
	}
	if len(svcs[0].Destinations) != len(testSvc.Destinations) {
		log.Fatalf("IPVSGetServices returned %d destinations, expected %d",
			len(svcs[0].Destinations), len(testSvc.Destinations))
	}

	dst := testSvc.Destinations[0]
	if err := ncc.IPVSDeleteDestination(testSvc, dst); err != nil {
		log.Fatalf("Failed to delete destination: %v", err)
	}

	svc, err := ncc.IPVSGetService(testSvc)
	if err != nil {
		log.Fatalf("Failed to get IPVS service: %v", err)
	}
	if len(svc.Destinations) != len(testSvc.Destinations)-1 {
		log.Fatalf("IPVSGetService returned %d destinations, expected %d",
			len(svc.Destinations), len(testSvc.Destinations)-1)
	}

	ipvsGetLoadTest(4, 10*time.Second)

	if err := ncc.IPVSDeleteService(testSvc); err != nil {
		log.Fatalf("Failed to delete IPVS service: %v", err)
	}

	ipvsAddLoadTest(ncc, 20, 50)

	// Clean up after ourselves...
	if err := ncc.IPVSFlush(); err != nil {
		log.Fatalf("Failed to flush the IPVS table: %v", err)
	}
}

func routeTests(ncc client.NCC) {
	gateway, err := ncc.RouteDefaultIPv4()
	if err != nil {
		log.Fatalf("Failed to get IPv4 default route: %v", err)
	}
	log.Printf("Got IPv4 default route - %v", gateway)
}

func main() {
	flag.Parse()

	// Connect to the NCC component.
	ncc := client.NewNCC(*nccSocket)
	if err := ncc.Dial(); err != nil {
		log.Fatalf("Failed to connect to NCC: %v", err)
	}

	switch strings.ToLower(*testClass) {
	case "arp":
		log.Print("Starting ARP tests...")
		arpTests(ncc)
	case "bgp":
		log.Print("Starting BGP tests...")
		bgpTests(ncc)
	case "interface":
		log.Print("Starting interface tests...")
		interfaceTests(ncc)
	case "ipvs":
		log.Print("Starting IPVS tests...")
		ipvsTests(ncc)
	case "route":
		log.Print("Starting route tests...")
		routeTests(ncc)
	case "vserver":
		log.Print("Starting vserver tests...")
		vserverTests(ncc)
	default:
		log.Fatal("Test class must be one of arp, bgp, interface, ipvs, route or vserver")
	}

	ncc.Close()
	log.Print("Done!")
}
