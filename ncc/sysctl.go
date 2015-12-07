// Copyright 2013 Google Inc. All Rights Reserved.
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

package ncc

// This file contains sysctl related functions for the Seesaw Network Control
// component.

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"strings"

	"github.com/google/seesaw/common/seesaw"
)

const sysctlPath = "/proc/sys"

var seesawSysctls = []struct {
	name  string
	value string
}{
	// Increase the size of the ARP cache.
	{"net.ipv4.neigh.default.gc_thresh1", "1024"},
	{"net.ipv4.neigh.default.gc_thresh2", "2048"},
	{"net.ipv4.neigh.default.gc_thresh3", "4096"},

	// ip_forward and conntrack are required for NAT.
	{"net.ipv4.ip_forward", "1"},
	{"net.ipv4.vs.conntrack", "1"},

	// Set the timeout for conntrack TCP ESTABLISHED connections to
	// 15 minutes (the default is five days), to match the IPVS TCP
	// connection timeout.
	{"net.netfilter.nf_conntrack_tcp_timeout_established", "900"},

	// When a packet arrives for connection belonging to a dead backend,
	// remove the connection from the IPVS table.
	{"net.ipv4.vs.expire_nodest_conn", "1"},
}

var seesawIfaceSysctls = []struct {
	af    seesaw.AF
	name  string
	value string
}{
	// Accept incoming traffic which has a local source address in order
	// to allow connections to self-hosted VIPs.
	{seesaw.IPv4, "accept_local", "1"},

	// Only respond to ARP requests via the interface the IP belongs to.
	{seesaw.IPv4, "arp_filter", "1"},

	// Generate gratuitous ARP messages when interface is brought up.
	{seesaw.IPv4, "arp_notify", "1"},

	// Enable loose reverse path filtering.
	{seesaw.IPv4, "rp_filter", "2"},

	// ip_forward and conntrack are required for NAT, however ip_forward
	// changes the default for send_redirects to 'enabled', so explicitly
	// disable it.
	{seesaw.IPv4, "send_redirects", "0"},

	// Disable SLAAC since we want Seesaw IPv6 addresses to be at fixed
	// offsets within netblocks.
	{seesaw.IPv6, "autoconf", "0"},

	// Disable IPv6 duplicate address detection, we do not need it.
	{seesaw.IPv6, "accept_dad", "0"},
	{seesaw.IPv6, "dad_transmits", "0"},
}

// sysctlInitLB initialises sysctls required for load balancing.
func sysctlInitLB(nodeIface, lbIface *net.Interface) error {

	// System level sysctls.
	for _, ctl := range seesawSysctls {
		if _, err := sysctl(ctl.name, ctl.value); err != nil {
			return err
		}
	}

	// Interface level sysctls.
	ifaces := []string{"all", "default", nodeIface.Name, lbIface.Name}
	for _, iface := range ifaces {
		if err := sysctlInitIface(iface); err != nil {
			return err
		}
	}
	return nil
}

// sysctlInitIface initialises sysctls required for a load balancing interface.
func sysctlInitIface(iface string) error {
	for _, ctl := range seesawIfaceSysctls {
		if err := sysctlIface(iface, ctl.af, ctl.name, ctl.value); err != nil {
			return err
		}
	}
	return nil
}

// sysctlIface sets a sysctl for a given address family and network interface.
func sysctlIface(iface string, af seesaw.AF, name, value string) error {
	components := []string{
		"net", strings.ToLower(af.String()), "conf", iface, name,
	}
	_, err := sysctlByComponents(components, value)
	return err
}

// sysctl sets the named sysctl to the value specified and returns its
// original value as a string. Note that this cannot be used if a sysctl
// component includes a period it its name - in that case use
// sysctlByComponents instead.
func sysctl(name, value string) (string, error) {
	return sysctlByComponents(strings.Split(name, "."), value)
}

// sysctlByComponents sets the sysctl specified by the individual components
// to the value specified and returns its original value as a string.
func sysctlByComponents(components []string, value string) (string, error) {
	components = append([]string{sysctlPath}, components...)
	f, err := os.OpenFile(path.Join(components...), os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(f, "%s\n", value); err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}
