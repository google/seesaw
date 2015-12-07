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
Package client implements the client interface to the Seesaw v2
Network Control Centre component, which provides an interface for the
Seesaw engine to manipulate and control network related configuration.
*/
package client

import (
	"fmt"
	"net"
	"net/rpc"
	"sync"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ipvs"
	ncctypes "github.com/google/seesaw/ncc/types"
	"github.com/google/seesaw/quagga"
)

const nccMaxRetries = 3
const nccRetryDelay = 500 * time.Millisecond
const nccRPCTimeout = 10 * time.Second

// NCC provides a client interface to the network control component.
type NCC interface {
	// NewLBInterface returns an initialised NCC network LB interface.
	NewLBInterface(name string, cfg *ncctypes.LBConfig) LBInterface

	// Dial establishes a connection to the Seesaw NCC.
	Dial() error

	// Close closes an existing connection to the Seesaw NCC.
	Close() error

	// ARPSendGratuitous sends a gratuitious ARP message.
	ARPSendGratuitous(iface string, ip net.IP) error

	// BGPConfig returns the configuration for the Quagga BGP daemon.
	BGPConfig() ([]string, error)

	// BGPNeighbors returns the neighbors that the Quagga BGP daemon
	// is peering with.
	BGPNeighbors() ([]*quagga.Neighbor, error)

	// BGPWithdrawAll requests the Quagga BGP daemon to withdraw all
	// configured network advertisements.
	BGPWithdrawAll() error

	// BGPAdvertiseVIP requests the Quagga BGP daemon to advertise the
	// specified VIP.
	BGPAdvertiseVIP(vip net.IP) error

	// BGPWithdrawVIP requests the Quagga BGP daemon to withdraw the
	// specified VIP.
	BGPWithdrawVIP(vip net.IP) error

	// IPVSFlush flushes all services and destinations from the IPVS table.
	IPVSFlush() error

	// IPVSGetServices returns all services configured in the IPVS table.
	IPVSGetServices() ([]*ipvs.Service, error)

	// IPVSGetService returns the service entry currently configured in
	// the kernel IPVS table, which matches the specified service.
	IPVSGetService(svc *ipvs.Service) (*ipvs.Service, error)

	// IPVSAddService adds the specified service to the IPVS table.
	IPVSAddService(svc *ipvs.Service) error

	// IPVSUpdateService updates the specified service in the IPVS table.
	IPVSUpdateService(svc *ipvs.Service) error

	// IPVSDeleteService deletes the specified service from the IPVS table.
	IPVSDeleteService(svc *ipvs.Service) error

	// IPVSAddDestination adds the specified destination to the IPVS table.
	IPVSAddDestination(svc *ipvs.Service, dst *ipvs.Destination) error

	// IPVSUpdateDestination updates the specified destination in
	// the IPVS table.
	IPVSUpdateDestination(svc *ipvs.Service, dst *ipvs.Destination) error

	// IPVSDeleteDestination deletes the specified destination from
	// the IPVS table.
	IPVSDeleteDestination(svc *ipvs.Service, dst *ipvs.Destination) error

	// RouteDefaultIPv4 returns the default route for IPv4 traffic.
	RouteDefaultIPv4() (net.IP, error)
}

// LBInterface provides an interface for manipulating a load balancing
// network interface.
type LBInterface interface {
	// Init initialises the load balancing network interface.
	Init() error

	// Up attempts to bring the network interface up.
	Up() error

	// Down attempts to bring the network interface down.
	Down() error

	// AddVserver adds a Vserver to the load balancing interface.
	AddVserver(*seesaw.Vserver, seesaw.AF) error

	// Delete Vserver removes a Vserver from the load balancing interface.
	DeleteVserver(*seesaw.Vserver, seesaw.AF) error

	// AddVIP adds a VIP to the load balancing interface.
	AddVIP(*seesaw.VIP) error

	// DeleteVIP removes a VIP from the load balancing interface.
	DeleteVIP(*seesaw.VIP) error

	// AddVLAN adds a VLAN interface to the load balancing interface.
	AddVLAN(vlan *seesaw.VLAN) error

	// DeleteVLAN removes a VLAN interface from the load balancing interface.
	DeleteVLAN(vlan *seesaw.VLAN) error
}

// nccClient contains the data needed by a NCC client.
type nccClient struct {
	client    *rpc.Client
	lock      sync.RWMutex
	nccSocket string
	refs      uint
}

// NewNCC returns an initialised NCC client.
func NewNCC(socket string) *nccClient {
	return &nccClient{nccSocket: socket}
}

func (nc *nccClient) NewLBInterface(name string, cfg *ncctypes.LBConfig) LBInterface {
	iface := &ncctypes.LBInterface{LBConfig: *cfg}
	iface.Name = name
	return &nccLBIface{nc: nc, nccLBInterface: iface}
}

// call performs an RPC call to the Seesaw v2 nccClient.
func (nc *nccClient) call(name string, in interface{}, out interface{}) error {
	nc.lock.RLock()
	client := nc.client
	nc.lock.RUnlock()
	if client == nil {
		return fmt.Errorf("Not connected")
	}
	ch := make(chan error, 1)
	go func() {
		ch <- client.Call(name, in, out)
	}()
	select {
	case err := <-ch:
		return err
	case <-time.After(nccRPCTimeout):
		return fmt.Errorf("RPC call timed out after %v", nccRPCTimeout)
	}
}

func (nc *nccClient) Dial() error {
	nc.lock.Lock()
	defer nc.lock.Unlock()
	if nc.client != nil {
		nc.refs++
		return nil
	}
	var err error
	for i := 0; i < nccMaxRetries; i++ {
		nc.client, err = rpc.Dial("unix", nc.nccSocket)
		if err == nil {
			nc.refs = 1
			return nil
		}
		time.Sleep(time.Duration(i) * nccRetryDelay)
	}
	return fmt.Errorf("Failed to establish connection: %v", err)
}

func (nc *nccClient) Close() error {
	nc.lock.Lock()
	defer nc.lock.Unlock()
	if nc.client == nil {
		return nil
	}
	nc.refs--
	if nc.refs > 0 {
		return nil
	}
	if err := nc.client.Close(); err != nil {
		return fmt.Errorf("Failed to close connection: %v", err)
	}
	nc.client = nil
	return nil
}

func (nc *nccClient) ARPSendGratuitous(iface string, ip net.IP) error {
	arp := ncctypes.ARPGratuitous{
		IfaceName: iface,
		IP:        ip,
	}
	return nc.call("SeesawNCC.ARPSendGratuitous", &arp, nil)
}

func (nc *nccClient) BGPConfig() ([]string, error) {
	bc := &ncctypes.BGPConfig{}
	if err := nc.call("SeesawNCC.BGPConfig", 0, bc); err != nil {
		return nil, err
	}
	return bc.Config, nil
}

func (nc *nccClient) BGPNeighbors() ([]*quagga.Neighbor, error) {
	bn := &ncctypes.BGPNeighbors{}
	if err := nc.call("SeesawNCC.BGPNeighbors", 0, bn); err != nil {
		return nil, err
	}
	return bn.Neighbors, nil
}

func (nc *nccClient) BGPWithdrawAll() error {
	return nc.call("SeesawNCC.BGPWithdrawAll", 0, nil)
}

// TODO(ncope): Use seesaw.VIP here for consistency
func (nc *nccClient) BGPAdvertiseVIP(vip net.IP) error {
	return nc.call("SeesawNCC.BGPAdvertiseVIP", vip, nil)
}

// TODO(ncope): Use seesaw.VIP here for consistency
func (nc *nccClient) BGPWithdrawVIP(vip net.IP) error {
	return nc.call("SeesawNCC.BGPWithdrawVIP", vip, nil)
}

func (nc *nccClient) IPVSFlush() error {
	return nc.call("SeesawNCC.IPVSFlush", 0, nil)
}

func (nc *nccClient) IPVSGetServices() ([]*ipvs.Service, error) {
	s := &ncctypes.IPVSServices{}
	if err := nc.call("SeesawNCC.IPVSGetServices", 0, s); err != nil {
		return nil, err
	}
	return s.Services, nil
}

func (nc *nccClient) IPVSGetService(svc *ipvs.Service) (*ipvs.Service, error) {
	s := &ncctypes.IPVSServices{}
	if err := nc.call("SeesawNCC.IPVSGetService", svc, s); err != nil {
		return nil, err
	}
	return s.Services[0], nil
}

func (nc *nccClient) IPVSAddService(svc *ipvs.Service) error {
	return nc.call("SeesawNCC.IPVSAddService", svc, nil)
}

func (nc *nccClient) IPVSUpdateService(svc *ipvs.Service) error {
	return nc.call("SeesawNCC.IPVSUpdateService", svc, nil)
}

func (nc *nccClient) IPVSDeleteService(svc *ipvs.Service) error {
	return nc.call("SeesawNCC.IPVSDeleteService", svc, nil)
}

func (nc *nccClient) IPVSAddDestination(svc *ipvs.Service, dst *ipvs.Destination) error {
	ipvsDst := ncctypes.IPVSDestination{Service: svc, Destination: dst}
	return nc.call("SeesawNCC.IPVSAddDestination", ipvsDst, nil)
}

func (nc *nccClient) IPVSUpdateDestination(svc *ipvs.Service, dst *ipvs.Destination) error {
	ipvsDst := ncctypes.IPVSDestination{Service: svc, Destination: dst}
	return nc.call("SeesawNCC.IPVSUpdateDestination", ipvsDst, nil)
}

func (nc *nccClient) IPVSDeleteDestination(svc *ipvs.Service, dst *ipvs.Destination) error {
	ipvsDst := ncctypes.IPVSDestination{Service: svc, Destination: dst}
	return nc.call("SeesawNCC.IPVSDeleteDestination", ipvsDst, nil)
}

func (nc *nccClient) RouteDefaultIPv4() (net.IP, error) {
	var ip net.IP
	err := nc.call("SeesawNCC.RouteDefaultIPv4", 0, &ip)
	return ip, err
}

// nccLBIface contains the data needed to manipulate a LB network interface.
type nccLBIface struct {
	nc             *nccClient
	nccLBInterface *ncctypes.LBInterface
}

func (iface *nccLBIface) Init() error {
	return iface.nc.call("SeesawNCC.LBInterfaceInit", iface.nccLBInterface, nil)
}

func (iface *nccLBIface) Up() error {
	return iface.nc.call("SeesawNCC.LBInterfaceUp", iface.nccLBInterface, nil)
}

func (iface *nccLBIface) Down() error {
	return iface.nc.call("SeesawNCC.LBInterfaceDown", iface.nccLBInterface, nil)
}

func (iface *nccLBIface) AddVserver(v *seesaw.Vserver, af seesaw.AF) error {
	lbVserver := ncctypes.LBInterfaceVserver{
		Iface:   iface.nccLBInterface,
		Vserver: v,
		AF:      af,
	}
	return iface.nc.call("SeesawNCC.LBInterfaceAddVserver", &lbVserver, nil)
}

func (iface *nccLBIface) DeleteVserver(v *seesaw.Vserver, af seesaw.AF) error {
	lbVserver := ncctypes.LBInterfaceVserver{
		Iface:   iface.nccLBInterface,
		Vserver: v,
		AF:      af,
	}
	return iface.nc.call("SeesawNCC.LBInterfaceDeleteVserver", &lbVserver, nil)
}

func (iface *nccLBIface) AddVIP(vip *seesaw.VIP) error {
	lbVIP := ncctypes.LBInterfaceVIP{
		Iface: iface.nccLBInterface,
		VIP:   vip,
	}
	return iface.nc.call("SeesawNCC.LBInterfaceAddVIP", &lbVIP, nil)
}

func (iface *nccLBIface) DeleteVIP(vip *seesaw.VIP) error {
	lbVIP := ncctypes.LBInterfaceVIP{
		Iface: iface.nccLBInterface,
		VIP:   vip,
	}
	return iface.nc.call("SeesawNCC.LBInterfaceDeleteVIP", &lbVIP, nil)
}

func (iface *nccLBIface) AddVLAN(vlan *seesaw.VLAN) error {
	lbVLAN := ncctypes.LBInterfaceVLAN{Iface: iface.nccLBInterface, VLAN: vlan}
	return iface.nc.call("SeesawNCC.LBInterfaceAddVLAN", &lbVLAN, nil)
}

func (iface *nccLBIface) DeleteVLAN(vlan *seesaw.VLAN) error {
	lbVLAN := ncctypes.LBInterfaceVLAN{Iface: iface.nccLBInterface, VLAN: vlan}
	return iface.nc.call("SeesawNCC.LBInterfaceDeleteVLAN", &lbVLAN, nil)
}
