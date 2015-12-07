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

package ncc

// This file contains the IP and interface configuration related functions
// for the Seesaw Network Control component. At a later date this probably
// should be rewritten to use netlink directly.

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"

	"github.com/google/seesaw/common/seesaw"

	log "github.com/golang/glog"
)

var (
	ipCmd = "/sbin/ip"

	ifaceNameRegexp        = regexp.MustCompile(`^\w+\d+(\.\d+)?$`)
	ipv6AddrRegexp         = regexp.MustCompile(`^\s*inet6 ([a-f0-9:/]+) .*$`)
	routeDefaultIPv4Regexp = regexp.MustCompile(
		`^default via (\d+\.\d+\.\d+\.\d+) dev ([a-z]+[0-9]) `)
)

// validateInterface validates the name of a network interface.
func validateInterface(iface *net.Interface) error {
	if !ifaceNameRegexp.MatchString(iface.Name) {
		return fmt.Errorf("Invalid interface name %q", iface.Name)
	}
	return nil
}

// ipRunOutput runs the Linux ip(1) command with the specified arguments and
// returns the output.
func ipRunOutput(cmd string, args ...interface{}) (string, error) {
	cmdStr := fmt.Sprintf(cmd, args...)
	ipArgs := strings.Split(cmdStr, " ")
	log.Infof("%s %s", ipCmd, cmdStr)
	ip := exec.Command(ipCmd, ipArgs...)
	out, err := ip.Output()
	if err != nil {
		return "", fmt.Errorf("IP run %q: %v", cmdStr, err)
	}
	return string(out), nil
}

// ipRun runs the Linux ip(1) command with the specified arguments.
func ipRun(cmd string, args ...interface{}) error {
	_, err := ipRunOutput(cmd, args...)
	return err
}

// ipRunAFOutput runs the Linux ip(1) command with the given address family
// and specified arguments and returns the output.
func ipRunAFOutput(family seesaw.AF, cmd string, args ...interface{}) (string, error) {
	switch family {
	case seesaw.IPv4:
		cmd = fmt.Sprintf("-f inet %s", cmd)
	case seesaw.IPv6:
		cmd = fmt.Sprintf("-f inet6 %s", cmd)
	default:
		return "", fmt.Errorf("Unknown address family %s", family)
	}
	return ipRunOutput(cmd, args...)
}

// ipRunAF runs the Linux ip(1) command with the given address family and
// specified arguments.
func ipRunAF(family seesaw.AF, cmd string, args ...interface{}) error {
	_, err := ipRunAFOutput(family, cmd, args...)
	return err
}

// ipRunIface runs the Linux ip(1) command with the specified arguments, after
// validating the given network interface name. This function should be called
// if the interface name is included in the arguments.
func ipRunIface(iface *net.Interface, cmd string, args ...interface{}) error {
	if err := validateInterface(iface); err != nil {
		return err
	}
	_, err := ipRunOutput(cmd, args...)
	return err
}

// ifaceDown sets the interface link state to down and preserves IPv6 addresses
// for both the given interface and any associated VLAN interfaces.
func ifaceDown(pIface *net.Interface) error {
	out, err := ipRunOutput("link show dev %s", pIface.Name)
	if !strings.Contains(out, "state UP") {
		return nil
	}

	// Unlike IPv4, the kernel removes IPv6 addresses when the link goes down. We
	// don't want that behavior, so we have to preserve the addresses manually.
	// TODO(angusc): See if we can get a sysctl or something added to the kernel
	// so we can avoid doing this.
	ipv6Addrs := make(map[string]*net.Interface)
	ifaces, err := vlanInterfaces(pIface)
	if err != nil {
		return err
	}
	ifaces = append(ifaces, pIface)
	for _, iface := range ifaces {
		out, err = ipRunOutput("-6 addr show dev %s scope global", iface.Name)
		for _, line := range strings.Split(out, "\n") {
			if match := ipv6AddrRegexp.FindStringSubmatch(line); match != nil {
				ipv6Addrs[match[1]] = iface
			}
		}
	}
	if err := ifaceFastDown(pIface); err != nil {
		return err
	}
	for addr, iface := range ipv6Addrs {
		if err := ipRunIface(iface, "addr add %s dev %s", addr, iface.Name); err != nil {
			return err
		}
	}
	return nil
}

// ifaceFastDown sets the interface link state to down. If IPv6 addresses need
// to be preserved, use ifaceDown.
func ifaceFastDown(iface *net.Interface) error {
	return ipRunIface(iface, "link set %s down", iface.Name)
}

// ifaceUp sets the interface link state to up.
func ifaceUp(iface *net.Interface) error {
	return ipRunIface(iface, "link set %s up", iface.Name)
}

// ifaceSetMAC sets the interface MAC address.
func ifaceSetMAC(iface *net.Interface) error {
	return ipRunIface(iface, "link set %s address %s", iface.Name, iface.HardwareAddr)
}

// ifaceAddIPAddr adds the given IP address to the network interface.
func ifaceAddIPAddr(iface *net.Interface, ip net.IP, mask net.IPMask) error {
	if ip.To4() != nil {
		return ifaceAddIPv4Addr(iface, ip, mask)
	}
	return ifaceAddIPv6Addr(iface, ip, mask)
}

// ifaceAddIPv4Addr adds the given IPv4 address to the network interface.
func ifaceAddIPv4Addr(iface *net.Interface, ip net.IP, mask net.IPMask) error {
	if ip.To4() == nil {
		return fmt.Errorf("IP %v is not a valid IPv4 address", ip)
	}
	brd := make(net.IP, net.IPv4len)
	copy(brd, ip.To4())
	for i := 0; i < net.IPv4len; i++ {
		brd[i] |= ^mask[i]
	}
	prefixLen, _ := mask.Size()
	return ipRunIface(iface, "addr add %s/%d brd %s dev %s", ip, prefixLen, brd, iface.Name)
}

// ifaceAddIPv6Addr adds the given IPv6 address to the network interface.
func ifaceAddIPv6Addr(iface *net.Interface, ip net.IP, mask net.IPMask) error {
	prefixLen, _ := mask.Size()
	return ipRunIface(iface, "addr add %s/%d dev %s", ip, prefixLen, iface.Name)
}

// ifaceDelIPAddr deletes the given IP address from the network interface.
func ifaceDelIPAddr(iface *net.Interface, ip net.IP, mask net.IPMask) error {
	prefixLen, _ := mask.Size()
	return ipRunIface(iface, "addr del %s/%d dev %s", ip, prefixLen, iface.Name)
}

// ifaceFlushIPAddr flushes all IP addresses from the network interface.
func ifaceFlushIPAddr(iface *net.Interface) error {
	return ipRunIface(iface, "addr flush dev %s", iface.Name)
}

// ifaceAddVLAN creates a new VLAN interface on the given physical interface.
func ifaceAddVLAN(iface *net.Interface, vlan *seesaw.VLAN) error {
	name := fmt.Sprintf("%s.%d", iface.Name, vlan.ID)
	err := ipRunIface(iface, "link add link %s name %s type vlan id %d", iface.Name, name, vlan.ID)
	if err != nil {
		return fmt.Errorf("Failed to create VLAN interface %q: %v", name, err)
	}

	vlanIface, err := net.InterfaceByName(name)
	if err != nil {
		return fmt.Errorf("Failed to find newly created VLAN interface %q: %v", name, err)
	}

	if err := sysctlInitIface(vlanIface.Name); err != nil {
		return fmt.Errorf("Failed to initialise sysctls for VLAN interface %q: %v", name, err)
	}

	if vlan.IPv4Addr != nil {
		if err := ifaceAddIPv4Addr(vlanIface, vlan.IPv4Addr, vlan.IPv4Mask); err != nil {
			return fmt.Errorf("Failed to add %v to VLAN interface %q", vlan.IPv4Addr, name)
		}
	}
	if vlan.IPv6Addr != nil {
		if err := ifaceAddIPv6Addr(vlanIface, vlan.IPv6Addr, vlan.IPv6Mask); err != nil {
			return fmt.Errorf("Failed to add %v to VLAN interface %q", vlan.IPv6Addr, name)
		}
	}

	if iface.Flags&net.FlagUp == 0 {
		return nil
	}
	return ifaceUp(vlanIface)
}

// ifaceDelVLAN removes a VLAN interface from the given physical interface.
func ifaceDelVLAN(iface *net.Interface, vlan *seesaw.VLAN) error {
	name := fmt.Sprintf("%s.%d", iface.Name, vlan.ID)
	vlanIface, err := net.InterfaceByName(name)
	if err != nil {
		return fmt.Errorf("Failed to find VLAN interface %q: %v", name, err)
	}
	return ipRunIface(vlanIface, "link del dev %s", vlanIface.Name)
}

// ifaceFlushVLANs removes all VLAN interfaces from the given physical
// interface.
func ifaceFlushVLANs(iface *net.Interface) error {
	if err := validateInterface(iface); err != nil {
		return err
	}
	vlanIfaces, err := vlanInterfaces(iface)
	if err != nil {
		return err
	}
	for _, vlanIface := range vlanIfaces {
		if err := ipRunIface(vlanIface, "link del dev %s", vlanIface.Name); err != nil {
			return fmt.Errorf("Failed to remove %s from %s: %v", vlanIface.Name, iface.Name, err)
		}
	}
	return nil
}

// routeDefaultIPv4 returns the default route for IPv4 traffic.
func routeDefaultIPv4() (net.IP, error) {
	out, err := ipRunAFOutput(seesaw.IPv4, "route show default")
	if err != nil {
		return nil, err
	}
	if dr := routeDefaultIPv4Regexp.FindStringSubmatch(out); dr != nil {
		return net.ParseIP(dr[1]).To4(), nil
	}
	return nil, fmt.Errorf("Default route not found")
}

// removeRoutes deletes the routing table entries for a VIP, from the specified
// table.
func removeRoutes(table string, vip net.IP) error {
	af := seesaw.IPv6
	if vip.To4() != nil {
		af = seesaw.IPv4
	}
	return ipRunAF(af, "route flush table %s %s", table, vip)
}

// removeLocalRoutes deletes the local routing table entries for a VIP.
func removeLocalRoutes(vip net.IP) error {
	return removeRoutes("local", vip)
}

// removeMainRoutes deletes the main routing table entries for a VIP.
func removeMainRoutes(vip net.IP) error {
	return removeRoutes("main", vip)
}

// routeLocal removes all routes in the local routing table for the given VIP
// and adds new routes with the correct source address.
func routeLocal(iface *net.Interface, vip net.IP, node seesaw.Host) error {
	af := seesaw.IPv6
	src := node.IPv6Addr
	if vip.To4() != nil {
		af = seesaw.IPv4
		src = node.IPv4Addr
	}
	if err := removeLocalRoutes(vip); err != nil {
		return err
	}
	return ipRunAF(af, "route add table local local %s dev %s src %s", vip, iface.Name, src)
}

// RouteDefaultIPv4 returns the default route for IPv4 traffic.
func (ncc *SeesawNCC) RouteDefaultIPv4(unused int, gateway *net.IP) error {
	ip, err := routeDefaultIPv4()
	if err != nil {
		return err
	}
	if gateway != nil {
		*gateway = make(net.IP, len(ip))
		copy(*gateway, ip)
	}
	return nil
}
