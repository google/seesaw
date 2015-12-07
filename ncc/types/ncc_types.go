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

// Package types contains types used to exchange data between the NCC client
// and the NCC server.
package types

import (
	"net"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ipvs"
	"github.com/google/seesaw/quagga"
)

// ARPGratuitous encapsulates a request to send a gratuitous ARP message.
type ARPGratuitous struct {
	IfaceName string
	IP        net.IP
}

// BGPNeighbors encapsulates a list of BGP neighbors.
type BGPNeighbors struct {
	Neighbors []*quagga.Neighbor
}

// BGPConfig encapsulates the configuration for a BGP daemon.
type BGPConfig struct {
	Config []string
}

// IPVSServices contains an array of IPVS services.
type IPVSServices struct {
	Services []*ipvs.Service
}

// IPVSDestination specifies an IPVS destination and its associated service.
type IPVSDestination struct {
	Service     *ipvs.Service
	Destination *ipvs.Destination
}

// LBConfig represents the configuration for a load balancing network interface.
type LBConfig struct {
	ClusterVIP     seesaw.Host
	DummyInterface string
	NodeInterface  string
	Node           seesaw.Host
	RoutingTableID uint8
	VRID           uint8
}

// LBInterface represents the load balancing network interface on a Seesaw Node.
type LBInterface struct {
	Name string // The name of the network device, e.g. "eth1"
	LBConfig
}

// Interface returns the network interface associated with the LBInterface.
func (lb *LBInterface) Interface() (*net.Interface, error) {
	iface, err := net.InterfaceByName(lb.Name)
	if err != nil {
		return nil, err
	}
	return iface, nil
}

// LBInterfaceVIP represents a VIP address that is configured on a load
// balancing interface.
type LBInterfaceVIP struct {
	Iface *LBInterface
	*seesaw.VIP
}

// LBInterfaceVLAN represents a VLAN interface that is configured on a load
// balancing interface.
type LBInterfaceVLAN struct {
	Iface *LBInterface
	*seesaw.VLAN
}

// LBInterfaceVserver represents a Vserver to be configured on a load balancing
// interface.
type LBInterfaceVserver struct {
	Iface   *LBInterface
	Vserver *seesaw.Vserver
	AF      seesaw.AF
}
