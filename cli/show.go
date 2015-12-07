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

package cli

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/quagga"
)

const (
	subIndent = 2
	valIndent = 22
	timeStamp = "Jan 2 15:04:05 MST"
)

func showBGPNeighbors(cli *SeesawCLI, args []string) error {
	neighbors, err := cli.seesaw.BGPNeighbors()
	if err != nil {
		return fmt.Errorf("Failed to get BGP neighbors: %v", err)
	}

	if len(args) == 0 {
		printHdr("BGP Neighbors")
		for i, n := range neighbors {
			fmt.Printf("[%3d] %s (%v, %v)\n", i+1, n.IP, n.BGPState, n.Uptime)
		}
	} else if len(args) == 1 {
		ip := net.ParseIP(args[0])
		if ip == nil {
			return fmt.Errorf("%q is not a valid IP address", args[0])
		}
		var n *quagga.Neighbor
		for i := range neighbors {
			if neighbors[i].IP.Equal(ip) {
				n = neighbors[i]
				break
			}
		}
		if n == nil {
			return fmt.Errorf("No such neighbor")
		}
		printHdr("BGP Neighbor")
		printVal("IP Address:", n.IP)
		printVal("Router ID:", n.RouterID)
		printVal("ASN:", n.ASN)
		printVal("Description:", n.Description)
		printVal("BGP State:", n.BGPState)
		printVal("Duration:", n.Uptime)
	} else {
		return fmt.Errorf("Too many arguments")
	}
	return nil
}

func showVLANs(cli *SeesawCLI, args []string) error {
	if len(args) > 1 {
		fmt.Println("show vlans [<id>|<ip>]")
		return nil
	}

	vlans, err := cli.seesaw.VLANs()
	if err != nil {
		return fmt.Errorf("failed to get VLANs: %v", err)
	}

	if len(args) == 0 {
		// Display all VLANs.
		sort.Sort(vlans)
		printHdr("VLANs")
		for i, v := range vlans.VLANs {
			fmt.Printf("[%3d] VLAN ID %d - %s\n", i+1, v.ID, v.Hostname)
		}
		return nil
	}

	// Display details for a single VLAN (by ID or IP address).
	var vlan *seesaw.VLAN
	if id, err := strconv.ParseUint(args[0], 10, 16); err == nil {
		// By ID
		for _, v := range vlans.VLANs {
			if uint(v.ID) == uint(id) {
				vlan = v
				break
			}
		}
		if vlan == nil {
			return fmt.Errorf("VLAN ID %d not found", id)
		}
	} else if ip := net.ParseIP(args[0]); ip != nil {
		// By IP
		for _, v := range vlans.VLANs {
			if v.IPv4Net().Contains(ip) || v.IPv6Net().Contains(ip) {
				vlan = v
				break
			}
		}
		if vlan == nil {
			return fmt.Errorf("VLAN not found for IP address %v", ip)
		}
	} else {
		return fmt.Errorf("unknown value %q - must be a VLAN ID or IP address", args[0])
	}

	printHdr("VLAN")
	printVal("ID:", vlan.ID)
	printVal("Hostname:", vlan.Hostname)
	printFmt("IPv4 Address:", vlan.IPv4Printable())
	printFmt("IPv6 Address:", vlan.IPv6Printable())
	printVal("IPv4 Backend Count:", vlan.BackendCount[seesaw.IPv4])
	printVal("IPv6 Backend Count:", vlan.BackendCount[seesaw.IPv6])
	printVal("IPv4 VIP Count:", vlan.VIPCount[seesaw.IPv4])
	printVal("IPv6 VIP Count:", vlan.VIPCount[seesaw.IPv6])

	return nil
}

func showHAStatus(cli *SeesawCLI, args []string) error {
	ha, err := cli.seesaw.HAStatus()
	if err != nil {
		return fmt.Errorf("HA status: %v\n", err)
	}

	durationStr := "N/A"
	if !ha.Since.IsZero() {
		duration := time.Duration(time.Now().Sub(ha.Since).Seconds()) * time.Second
		since := ha.Since.Format(timeStamp)
		durationStr = fmt.Sprintf("%s (since %s)", duration, since)
	}

	printHdr("HA Status")
	printVal("State:", ha.State)
	printVal("Duration:", durationStr)
	printVal("Transitions:", ha.Transitions)
	printVal("Advertisements Sent:", ha.Sent)
	printVal("Advertisements Rcvd:", ha.Received)
	printVal("Last Update:", ha.LastUpdate.Format(timeStamp))

	return nil
}

func configStatus(cli *SeesawCLI, args []string) error {
	cs, err := cli.seesaw.ConfigStatus()
	if err != nil {
		return fmt.Errorf("Failed to get config status: %v", err)
	}
	printHdr("Config Status")
	printVal("Last Update", cs.LastUpdate.Format(timeStamp))
	fmt.Println()
	fmt.Println("  Attributes:")
	for _, attr := range cs.Attributes {
		printVal(label(attr.Name, 4, 18), attr.Value)
	}

	return nil
}

func showNode(cli *SeesawCLI, args []string) error {
	if len(args) > 1 {
		fmt.Println("show node <node>")
		return nil
	}

	cs, err := cli.seesaw.ClusterStatus()
	if err != nil {
		return fmt.Errorf("Failed to get cluster status: %v", err)
	}
	sort.Sort(seesaw.NodesByIPv4{cs.Nodes})
	if len(args) == 1 {
		nodeName := args[0]
		var node *seesaw.Node
		for _, n := range cs.Nodes {
			// TODO(baptr): Write a shared matcher implementation for show*.
			if n.Hostname == nodeName {
				node = n
				break
			}
		}
		if node == nil {
			return fmt.Errorf("node %q not found", nodeName)
		}
		printHdr("Node")
		printVal("Hostname:", node.Hostname)
		printVal("Site:", cs.Site)
		printVal("IPv4 Address:", node.IPv4Printable())
		printVal("IPv6 Address:", node.IPv6Printable())
		printVal("HA Enabled:", node.State != seesaw.HADisabled)
		printVal("Anycast Enabled:", node.AnycastEnabled)
		printVal("BGP Enabled:", node.BGPEnabled)
		printVal("Vservers Enabled:", node.VserversEnabled)
		return nil
	}
	printHdr("Nodes")
	for i, node := range cs.Nodes {
		enabled := "enabled"
		if node.State == seesaw.HADisabled {
			enabled = "disabled"
		}
		// TODO(baptr): Figure out how to identify/mark local node.
		fmt.Printf("[%d] %s %s\n", i+1, node.Hostname, enabled)
	}
	return nil
}

func showBackend(cli *SeesawCLI, args []string) error {
	if len(args) > 1 {
		fmt.Println("show backend <backend>")
		return nil
	}

	vservers, err := cli.seesaw.Vservers()
	if err != nil {
		return fmt.Errorf("Failed to get vservers: %v", err)
	}

	// If no args given, list all backends.
	showAll := len(args) == 0

	backendsMap := make(map[string]seesaw.Destinations)
	for _, v := range vservers {
		for _, s := range v.Services {
			for _, d := range s.Destinations {
				if showAll || strings.HasPrefix(d.Backend.Hostname, args[0]) {
					list, ok := backendsMap[d.Backend.Hostname]
					if !ok {
						list = make([]*seesaw.Destination, 0)
					}
					list = append(list, d)
					backendsMap[d.Backend.Hostname] = list
				}
			}
		}
	}

	if len(backendsMap) == 0 {
		if len(args) > 0 {
			return fmt.Errorf("backend '%v...' not found", args[0])
		}
		return fmt.Errorf("no backends found")
	}

	backends := make([]string, 0)
	for backend := range backendsMap {
		backends = append(backends, backend)
	}

	if len(backends) == 1 {
		// Exactly 1 backend found, print details.
		printHdr("Backend")
		fmt.Printf("  Hostname: %v\n", backends[0])
		fmt.Printf("  Destinations:\n")
		dests := backendsMap[backends[0]]
		sort.Sort(dests)
		for i, d := range dests {
			fmt.Printf("  [%3d] %v\n", i+1, destSummary(d, vservers))
		}
		return nil
	}

	// Multiple matching backends, just list their names.
	sort.Strings(backends)
	printHdr("Backends")
	for i, backend := range backends {
		fmt.Printf("[%4d] %v\n", i+1, backendSummary(backend, backendsMap[backend], vservers))
	}
	return nil
}

func backendSummary(host string, dests []*seesaw.Destination, vservers map[string]*seesaw.Vserver) string {
	disabledDests := 0
	disabledVservers := make(map[string]bool)
	allVservers := make(map[string]bool)
	for _, d := range dests {
		if !d.Enabled {
			disabledDests++
		}
		if v, ok := vservers[d.VserverName]; ok {
			if !v.Enabled {
				disabledVservers[d.VserverName] = true
			}
			allVservers[d.VserverName] = true
		}
	}

	status := ""
	if len(disabledVservers) == len(allVservers) || disabledDests == len(dests) {
		status = " (disabled)"
	} else if len(disabledVservers) > 0 || disabledDests > 0 {
		status = " (partially disabled)"
	}

	return fmt.Sprintf("%v%v", host, status)
}

func showDestination(cli *SeesawCLI, args []string) error {
	if len(args) > 1 {
		fmt.Println("show destinations <vserver|destination>")
		return nil
	}

	vservers, err := cli.seesaw.Vservers()
	if err != nil {
		return fmt.Errorf("Failed to get vservers: %v", err)
	}

	// If no args given, list all destinations.
	showAll := len(args) == 0

	var dests seesaw.Destinations = make([]*seesaw.Destination, 0)
	for _, v := range vservers {
		for _, s := range v.Services {
			for _, d := range s.Destinations {
				if showAll || strings.HasPrefix(d.Name, args[0]) {
					dests = append(dests, d)
				}
			}
		}
	}

	switch len(dests) {
	case 0:
		if len(args) > 0 {
			return fmt.Errorf("destination '%v...' not found", args[0])
		}
		return fmt.Errorf("no destinations found")
	case 1:
		// Exactly one destination found, print destination details.
		d := dests[0]
		vserverName := d.VserverName
		if v, ok := vservers[d.VserverName]; ok && !v.Enabled {
			vserverName = vserverName + " (disabled)"
		}
		printHdr("Destination")
		printVal("Name:", d.Name)
		printVal("Vserver:", vserverName)
		printVal("Backend:", d.Backend.Hostname)
		printVal("Enabled:", d.Enabled)
		printVal("Healthy:", d.Healthy)
		printVal("Active:", d.Active)
		// TODO(angusc): Show healthcheck history and status details.
		return nil
	}

	// Multiple matching destinations, just print a summary.
	printHdr("Destinations")
	sort.Sort(dests)
	for i, d := range dests {
		fmt.Printf("[%4d] %v\n", i+1, destSummary(d, vservers))
	}
	return nil
}

func destSummary(d *seesaw.Destination, vservers map[string]*seesaw.Vserver) string {
	status := statusSummary(d.Enabled, d.Healthy, d.Active)
	if v, ok := vservers[d.VserverName]; ok && !v.Enabled {
		status = "vserver disabled"
	}
	return fmt.Sprintf("%v (%v)", d.Name, status)
}

func filterVservers(filter string, vservers map[string]*seesaw.Vserver) map[string]*seesaw.Vserver {
	if filter == "" {
		return vservers
	}
	filtered := make(map[string]*seesaw.Vserver)

	// Full match.
	vserver, ok := vservers[filter]
	if ok {
		filtered[vserver.Name] = vserver
		return filtered
	}

	// Prefix match.
	for _, vs := range vservers {
		if strings.HasPrefix(vs.Name, filter) {
			filtered[vs.Name] = vs
		}
	}

	// TODO(jsing): Consider implementing partial matching based on
	// globbing or regexes.

	return filtered
}

func showVserver(cli *SeesawCLI, args []string) error {
	if len(args) > 1 {
		fmt.Println("show vserver [<vserver>]")
		return nil
	}

	vservers, err := cli.seesaw.Vservers()
	if err != nil {
		return fmt.Errorf("Failed to get vservers: %v\n", err)
	}

	filter := ""
	if len(args) == 1 {
		filter = args[0]
	}

	vservers = filterVservers(filter, vservers)
	switch len(vservers) {
	case 0:
		msg := "No vservers found"
		if filter != "" {
			msg = "No matching vservers"
		}
		fmt.Printf("%s\n", msg)

	case 1:
		for _, vs := range vservers {
			printVserver(vs)
		}

	default:
		// List vserver names.
		printHdr("Vservers")
		names := make([]string, 0)
		for name := range vservers {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			v := vservers[name]
			status := ""
			if !v.Enabled {
				status = " (disabled)"
			}
			fmt.Printf("[%3d] %s%s\n", i+1, name, status)
		}
	}

	return nil
}

func printVserver(vserver *seesaw.Vserver) {
	vserverStatus := "enabled"
	if !vserver.Enabled {
		vserverStatus = "disabled"
	}

	configStatus := "enabled"
	if !vserver.ConfigEnabled {
		configStatus = "disabled"
	}

	status := fmt.Sprintf("%s (override state %s; config state %s)",
		vserverStatus, vserver.OverrideState, configStatus)

	printHdr("Vserver")
	printVal("Name:", vserver.Name)
	printVal("Hostname:", vserver.Host.Hostname)
	printVal("Status:", status)
	printFmt("IPv4 Address:", vserver.Host.IPv4Printable())
	printFmt("IPv6 Address:", vserver.Host.IPv6Printable())
	fmt.Println()
	fmt.Printf("  Services:\n")

	var serviceKeys seesaw.ServiceKeys
	for _, svc := range vserver.Services {
		serviceKeys = append(serviceKeys, &svc.ServiceKey)
	}
	sort.Sort(serviceKeys)
	for _, sk := range serviceKeys {
		svc := vserver.Services[*sk]

		config := []string{svc.Mode.String(), fmt.Sprintf("%s scheduler", svc.Scheduler)}
		if svc.OnePacket {
			config = append(config, "one-packet mode")
		}
		if svc.Persistence > 0 {
			config = append(config, fmt.Sprintf("%ds persistence", svc.Persistence))
		}
		l := label(fmt.Sprintf("%s %s/%d", svc.AF, svc.Proto, svc.Port), 4, 18)
		fmt.Printf("\n%s (%s)\n", l, strings.Join(config, ", "))

		fmt.Printf("%s %s\n", label("State:", 8, 20),
			statusSummary(svc.Enabled, svc.Healthy, svc.Active))

		watermarkStatus := fmt.Sprintf("Low %.2f, High %.2f, Currently %.2f",
			svc.LowWatermark, svc.HighWatermark, svc.CurrentWatermark)
		fmt.Printf("%s %s\n", label("Watermarks:", 8, 20), watermarkStatus)
	}

	if len(vserver.Warnings) > 0 {
		fmt.Println()
		fmt.Printf("  Warnings:\n")
		for _, warning := range vserver.Warnings {
			fmt.Printf("    - %s\n", warning)
		}
	}
}

func showVersion(cli *SeesawCLI, args []string) error {
	return errors.New("unimplemented")
}

func showWarning(cli *SeesawCLI, args []string) error {
	cs, err := cli.seesaw.ConfigStatus()
	if err != nil {
		return fmt.Errorf("Failed to get config status: %v", err)
	}
	if len(cs.Warnings) == 0 {
		fmt.Println("No warnings.")
		return nil
	}
	fmt.Printf("There are %d warnings:\n\n", len(cs.Warnings))
	for i, warning := range cs.Warnings {
		fmt.Printf("[%3d] %s\n", i+1, warning)
	}
	return nil
}

func statusSummary(enabled, healthy, active bool) string {
	status := "disabled"
	if enabled {
		health := "unhealthy"
		if healthy {
			health = "healthy"
		}
		act := "inactive"
		if active {
			act = "active"
		}
		status = fmt.Sprintf("enabled, %s, %s", health, act)
	}
	return status
}

// label returns a string containing the label with indentation and padding.
func label(l string, indent, width int) string {
	pad := width - indent - len(l)
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("%s%s%s", strings.Repeat(" ", indent), l,
		strings.Repeat(" ", pad))
}

// printHdr prints a given header label.
func printHdr(h string, args ...interface{}) {
	fmt.Printf(h+"\n", args...)
}

// printFmt formats and prints a given value with the specified label.
func printFmt(l, v string, args ...interface{}) {
	l = label(l, subIndent, valIndent)
	fmt.Printf("%s %s\n", l, fmt.Sprintf(v, args...))
}

// printVal prints the given value with the specified label.
func printVal(l string, v interface{}) {
	switch f := v.(type) {
	case uint, uint64:
		printFmt(l, "%d", f)
	case string:
		printFmt(l, "%s", f)
	default:
		printFmt(l, "%v", f)
	}
}
