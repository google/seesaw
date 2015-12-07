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

// This file contains the load balancing interface configuration related
// functions for the Seesaw Network Control component.

import (
	"fmt"
	"net"
	"strings"

	"github.com/google/seesaw/common/seesaw"
	ncctypes "github.com/google/seesaw/ncc/types"

	log "github.com/golang/glog"
)

const vrrpMAC = "00:00:5E:00:01:00"

// LBInterfaceInit initialises the load balancing interface for a Seesaw Node.
func (ncc *SeesawNCC) LBInterfaceInit(iface *ncctypes.LBInterface, out *int) error {
	netIface, err := iface.Interface()
	if err != nil {
		return fmt.Errorf("Failed to get network interface: %v", err)
	}
	nodeIface, err := net.InterfaceByName(iface.NodeInterface)
	if err != nil {
		return fmt.Errorf("Failed to get node interface: %v", err)
	}

	if iface.RoutingTableID < 1 || iface.RoutingTableID > 250 {
		return fmt.Errorf("Invalid routing table ID: %d", iface.RoutingTableID)
	}

	vmac, err := net.ParseMAC(vrrpMAC)
	if err != nil {
		return fmt.Errorf("Failed to parse VRRP MAC %q: %v", vrrpMAC, err)
	}

	// The last byte of the VRRP MAC is determined by the VRID.
	vmac[len(vmac)-1] = iface.VRID
	netIface.HardwareAddr = vmac

	log.Infof("Initialising load balancing interface %s - VRID %d (VMAC %s)", iface.Name, iface.VRID, vmac)

	// Ensure interface is down and set VMAC address.
	if err := ifaceFastDown(netIface); err != nil {
		return fmt.Errorf("Failed to down interface: %v", err)
	}
	if err := ifaceSetMAC(netIface); err != nil {
		return fmt.Errorf("Failed to set MAC: %v", err)
	}

	// Remove VLAN interfaces associated with the load balancing interface.
	if err := ifaceFlushVLANs(netIface); err != nil {
		return fmt.Errorf("Failed to flush VLAN interfaces: %v", err)
	}

	// Configure sysctls for load balancing.
	if err := sysctlInitLB(nodeIface, netIface); err != nil {
		return fmt.Errorf("Failed to initialise sysctls: %v", err)
	}

	// Flush existing IP addresses and add cluster VIPs.
	if err := ifaceFlushIPAddr(netIface); err != nil {
		return fmt.Errorf("Failed to flush IP addresses: %v", err)
	}

	if iface.ClusterVIP.IPv4Addr != nil {
		if err := addClusterVIP(iface, netIface, nodeIface, iface.ClusterVIP.IPv4Addr); err != nil {
			return fmt.Errorf("Failed to add IPv4 cluster VIP: %v", err)
		}
	}
	if iface.ClusterVIP.IPv6Addr != nil {
		if err := addClusterVIP(iface, netIface, nodeIface, iface.ClusterVIP.IPv6Addr); err != nil {
			return fmt.Errorf("Failed to add IPv6 cluster VIP: %v", err)
		}
	}

	// Initialise iptables rules.
	if err := iptablesInit(iface.ClusterVIP); err != nil {
		return err
	}

	// Setup dummy interface.
	dummyIface, err := net.InterfaceByName(iface.DummyInterface)
	if err != nil {
		return fmt.Errorf("Failed to get dummy interface: %v", err)
	}
	if err := ifaceFastDown(dummyIface); err != nil {
		return fmt.Errorf("Failed to down dummy interface: %v", err)
	}
	if err := ifaceFlushIPAddr(dummyIface); err != nil {
		return fmt.Errorf("Failed to flush dummy interface: %v", err)
	}
	if err := ifaceUp(dummyIface); err != nil {
		return fmt.Errorf("Failed to up dummy interface: %v", err)
	}

	return nil
}

// addClusterVIP adds a cluster VIP to the load balancing interface and
// performs additional network configuration.
func addClusterVIP(iface *ncctypes.LBInterface, netIface, nodeIface *net.Interface, clusterVIP net.IP) error {
	nodeIP := iface.Node.IPv4Addr
	family := seesaw.IPv4
	if clusterVIP.To4() == nil {
		nodeIP = iface.Node.IPv6Addr
		family = seesaw.IPv6
	}
	if nodeIP == nil {
		return fmt.Errorf("Node does not have an %s address", family)
	}

	log.Infof("Adding %s cluster VIP %s on %s", family, clusterVIP, iface.Name)

	// Determine network for the node IP and ensure that the cluster VIP is
	// contained within this network.
	nodeNet, err := findNetwork(nodeIface, nodeIP)
	if err != nil {
		return fmt.Errorf("Failed to get node network: %v", err)
	}
	if !nodeNet.Contains(clusterVIP) {
		return fmt.Errorf("Node network %s does not contain cluster VIP %s", nodeNet, clusterVIP)
	}

	// Set up routing policy for IPv4.  These rules and the associated routes
	// added in LBInterfaceUp tell the seesaw to:
	// 1) Respond to ARP requests for the seesaw VIP
	// 2) Send seesaw VIP reply traffic out eth1
	//
	// For IPv6, policy routing won't work on physical machines until
	// b/10061534 is resolved. VIP reply traffic will go out eth0 which is fine,
	// and neighbor discovery for the seesaw VIP works fine without policy routing
	// anyway.
	//
	// TODO(angusc): Figure out how whether we can get rid of this.
	if family == seesaw.IPv4 {
		ipRunAF(family, "rule del to %s", clusterVIP)
		ipRunAF(family, "rule del from %s", clusterVIP)
		if err := ipRunAF(family, "rule add from all to %s lookup %d", clusterVIP, iface.RoutingTableID); err != nil {
			return fmt.Errorf("Failed to set up routing policy: %v", err)
		}
		if err := ipRunAF(family, "rule add from %s lookup %d", clusterVIP, iface.RoutingTableID); err != nil {
			return fmt.Errorf("Failed to set up routing policy: %v", err)
		}
	}

	// Add cluster VIP to interface.
	if err := ifaceAddIPAddr(netIface, clusterVIP, nodeNet.Mask); err != nil {
		return fmt.Errorf("Failed to add cluster VIP to interface: %v", err)
	}

	return nil
}

// LBInterfaceDown brings the load balancing interface down.
func (ncc *SeesawNCC) LBInterfaceDown(iface *ncctypes.LBInterface, out *int) error {
	netIface, err := iface.Interface()
	if err != nil {
		return err
	}
	log.Infof("Bringing down LB interface %s", netIface.Name)
	return ifaceDown(netIface)
}

// LBInterfaceUp brings the load balancing interface up.
func (ncc *SeesawNCC) LBInterfaceUp(iface *ncctypes.LBInterface, out *int) error {
	// TODO(jsing): Handle IPv6-only, and improve IPv6 route setup.
	netIface, err := iface.Interface()
	if err != nil {
		return err
	}
	nodeIface, err := net.InterfaceByName(iface.NodeInterface)
	if err != nil {
		return fmt.Errorf("Failed to get node interface: %v", err)
	}
	nodeNet, err := findNetwork(nodeIface, iface.Node.IPv4Addr)
	if err != nil {
		return fmt.Errorf("Failed to get node network: %v", err)
	}

	log.Infof("Bringing up LB interface %s on %s", nodeNet, iface.Name)

	if !nodeNet.Contains(iface.ClusterVIP.IPv4Addr) {
		return fmt.Errorf("Node network %s does not contain cluster VIP %s", nodeNet, iface.ClusterVIP.IPv4Addr)
	}

	gateway, err := routeDefaultIPv4()
	if err != nil {
		return fmt.Errorf("Failed to get IPv4 default route: %v", err)
	}

	if err := ifaceUp(netIface); err != nil {
		return fmt.Errorf("Failed to bring interface up: %v", err)
	}

	// Configure routing.
	if err := ipRunIface(netIface, "route add %s dev %s table %d", nodeNet, iface.Name, iface.RoutingTableID); err != nil {
		return fmt.Errorf("Failed to configure routing: %v", err)
	}
	if err := ipRunIface(netIface, "route add 0/0 via %s dev %s table %d", gateway, iface.Name, iface.RoutingTableID); err != nil {
		return fmt.Errorf("Failed to configure routing: %v", err)
	}

	return nil
}

// LBInterfaceAddVserver adds the specified Vserver to the load balancing interface.
func (ncc *SeesawNCC) LBInterfaceAddVserver(lbVserver *ncctypes.LBInterfaceVserver, out *int) error {
	return iptablesAddRules(lbVserver.Vserver, lbVserver.Iface.ClusterVIP, lbVserver.AF)
}

// LBInterfaceDeleteVserver removes the specified Vserver from the load balancing interface.
func (ncc *SeesawNCC) LBInterfaceDeleteVserver(lbVserver *ncctypes.LBInterfaceVserver, out *int) error {
	return iptablesDeleteRules(lbVserver.Vserver, lbVserver.Iface.ClusterVIP, lbVserver.AF)
}

// LBInterfaceAddVIP adds the specified VIP to the load balancing interface.
func (ncc *SeesawNCC) LBInterfaceAddVIP(vip *ncctypes.LBInterfaceVIP, out *int) error {
	switch vip.Type {
	case seesaw.UnicastVIP:
		netIface, err := vip.Iface.Interface()
		if err != nil {
			return err
		}
		iface, network, err := selectInterfaceNetwork(netIface, vip.IP.IP())
		if err != nil {
			return fmt.Errorf("Failed to select interface and network for %v: %v", vip.VIP, err)
		}
		log.Infof("Adding VIP %s to %s", vip.IP.IP(), iface.Name)
		if err := ifaceAddIPAddr(iface, vip.IP.IP(), network.Mask); err != nil {
			return err
		}
		return routeLocal(iface, vip.IP.IP(), vip.Iface.Node)
	case seesaw.AnycastVIP, seesaw.DedicatedVIP:
		dummyIface, err := net.InterfaceByName(vip.Iface.DummyInterface)
		if err != nil {
			return fmt.Errorf("Failed to find dummy interface: %v", err)
		}
		prefixLen := net.IPv6len * 8
		if vip.IP.IP().To4() != nil {
			prefixLen = net.IPv4len * 8
		}
		mask := net.CIDRMask(prefixLen, prefixLen)
		log.Infof("Adding VIP %s to %s", vip.IP.IP(), dummyIface.Name)
		if err := ifaceAddIPAddr(dummyIface, vip.IP.IP(), mask); err != nil {
			return err
		}
		return routeLocal(dummyIface, vip.IP.IP(), vip.Iface.Node)
	default:
		return fmt.Errorf("Unknown VIPType for %v: %v", vip.VIP, vip.Type)
	}
}

// LBInterfaceDeleteVIP removes the specified VIP from the load balancing
// interface.
func (ncc *SeesawNCC) LBInterfaceDeleteVIP(vip *ncctypes.LBInterfaceVIP, out *int) error {
	switch vip.Type {
	case seesaw.UnicastVIP:
		netIface, err := vip.Iface.Interface()
		if err != nil {
			return err
		}
		iface, network, err := findInterfaceNetwork(netIface, vip.IP.IP())
		if err != nil {
			return fmt.Errorf("Failed to find interface and network for %v: %v", vip.VIP, err)
		}
		log.Infof("Removing VIP %s from %s", vip.IP.IP(), iface.Name)
		if err := ifaceDelIPAddr(iface, vip.IP.IP(), network.Mask); err != nil {
			return fmt.Errorf("Failed to delete VIP %s: %v", vip.VIP, err)
		}
		if err := removeLocalRoutes(vip.IP.IP()); err != nil {
			log.Infof("Failed to remove local routes for VIP %s: %v", vip.VIP, err)
		}
		if err := removeMainRoutes(vip.VIP.IP.IP()); err != nil {
			log.Infof("Failed to remove main routes for VIP %s: %v", vip.VIP, err)
		}
		return nil
	case seesaw.AnycastVIP, seesaw.DedicatedVIP:
		dummyIface, err := net.InterfaceByName(vip.Iface.DummyInterface)
		if err != nil {
			return fmt.Errorf("Failed to find dummy interface: %v", err)
		}
		prefixLen := net.IPv6len * 8
		if vip.IP.IP().To4() != nil {
			prefixLen = net.IPv4len * 8
		}
		mask := net.CIDRMask(prefixLen, prefixLen)

		log.Infof("Removing VIP %s from %s", vip.IP.IP(), dummyIface.Name)
		if err = ifaceDelIPAddr(dummyIface, vip.IP.IP(), mask); err != nil {
			return fmt.Errorf("Failed to delete VIP %s: %v", vip.VIP, err)
		}

		// Workaround for kernel bug(?). The route should have been removed when
		// address was removed, but this doesn't always happen.  An error is
		// non-fatal since it probably means the route doesn't exist.
		if err := removeLocalRoutes(vip.VIP.IP.IP()); err != nil {
			log.Infof("Failed to remove local routes for VIP %s: %v", vip.VIP, err)
		}
		if err := removeMainRoutes(vip.VIP.IP.IP()); err != nil {
			log.Infof("Failed to remove main routes for VIP %s: %v", vip.VIP, err)
		}
		return nil
	default:
		return fmt.Errorf("Unknown VIPType for %v: %v", vip.VIP, vip.VIP.Type)
	}

}

// LBInterfaceAddVLAN creates a VLAN interface on the load balancing interface.
func (ncc *SeesawNCC) LBInterfaceAddVLAN(vlan *ncctypes.LBInterfaceVLAN, out *int) error {
	netIface, err := vlan.Iface.Interface()
	if err != nil {
		return err
	}
	return ifaceAddVLAN(netIface, vlan.VLAN)
}

// LBInterfaceDeleteVLAN deletes a VLAN interface from the load balancing interface.
func (ncc *SeesawNCC) LBInterfaceDeleteVLAN(vlan *ncctypes.LBInterfaceVLAN, out *int) error {
	netIface, err := vlan.Iface.Interface()
	if err != nil {
		return err
	}
	return ifaceDelVLAN(netIface, vlan.VLAN)
}

// vlanInterfaces returns a slice containing the VLAN interfaces associated
// with a physical interface.
func vlanInterfaces(pIface *net.Interface) ([]*net.Interface, error) {
	allIfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	ifaces := make([]*net.Interface, 0, len(allIfaces))
	prefix := fmt.Sprintf("%s.", pIface.Name)
	for i, iface := range allIfaces {
		if strings.HasPrefix(iface.Name, prefix) {
			ifaces = append(ifaces, &allIfaces[i])
		}
	}
	return ifaces, nil
}

// findInterfaceNetwork searches for the given IP address on the given physical
// interface and its associated VLAN interfaces. If the address is not
// configured on any interface, an error is returned.
func findInterfaceNetwork(pIface *net.Interface, ip net.IP) (*net.Interface, *net.IPNet, error) {
	ifaces, err := vlanInterfaces(pIface)
	ifaces = append(ifaces, pIface)
	if err != nil {
		return nil, nil, err
	}
	for _, iface := range ifaces {
		if network, _ := findNetwork(iface, ip); network != nil {
			return iface, network, nil
		}
	}
	return nil, nil, fmt.Errorf("Failed to find IP %v on any interface", ip)
}

// findNetwork returns the network for the given IP address on the given
// interface. If the address is not configured on the interface, an error is
// returned.
func findNetwork(iface *net.Interface, ip net.IP) (*net.IPNet, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("Failed to get addresses for interface %q: %v", iface.Name, err)
	}
	for _, addr := range addrs {
		ipStr := addr.String()
		ipAddr, ipNet, err := net.ParseCIDR(ipStr)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse interface address %q - %v: %v", iface.Name, ipStr, err)
		}
		if ipAddr.Equal(ip) {
			return ipNet, nil
		}
	}
	return nil, fmt.Errorf("Failed to find IP %v on interface %q", ip, iface.Name)
}

// selectInterfaceNetwork attempts to find the interface that is configured
// with a network that contains the given IP address.  The set of interfaces
// searched includes the given physical interface and its associated VLAN
// interfaces.
func selectInterfaceNetwork(pIface *net.Interface, ip net.IP) (*net.Interface, *net.IPNet, error) {
	ifaces, err := vlanInterfaces(pIface)
	ifaces = append(ifaces, pIface)
	if err != nil {
		return nil, nil, err
	}
	for _, iface := range ifaces {
		network, err := selectNetwork(iface, ip)
		if err != nil {
			return nil, nil, err
		}
		if network != nil {
			return iface, network, nil
		}
	}
	return nil, nil, fmt.Errorf("Failed to find a suitable interface for IP %v", ip)
}

// selectNetwork attempts to find a network that contains the given IP address
// on the given interface. If an appropriate network is not found, and there
// no other errors, (nil, nil) is returned.
func selectNetwork(iface *net.Interface, ip net.IP) (*net.IPNet, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("Failed to get addresses for interface %q: %v", iface.Name, err)
	}
	for _, addr := range addrs {
		ipStr := addr.String()
		_, ipNet, err := net.ParseCIDR(ipStr)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse interface address %q - %v: %v", iface.Name, ipStr, err)
		}
		if ipNet.Contains(ip) {
			return ipNet, nil
		}
	}
	return nil, nil
}
