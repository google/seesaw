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

package client

// This file contains dummy implementation of interfaces for testing purposes

import (
	"fmt"
	"net"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/ipvs"
	ncctypes "github.com/google/seesaw/ncc/types"
	"github.com/google/seesaw/quagga"

	log "github.com/golang/glog"
)

type dummyNCC struct{}

// NewDummyNCC returns a dummy NCC client for testing purpose.
// It implements all methods NCC interface requires with no-op.
func NewDummyNCC() NCC {
	return &dummyNCC{}
}

func (nc *dummyNCC) NewLBInterface(name string, cfg *ncctypes.LBConfig) LBInterface {
	return nil
}
func (nc *dummyNCC) Dial() error                                                          { return nil }
func (nc *dummyNCC) Close() error                                                         { return nil }
func (nc *dummyNCC) ARPSendGratuitous(iface string, ip net.IP) error                      { return nil }
func (nc *dummyNCC) BGPConfig() ([]string, error)                                         { return nil, nil }
func (nc *dummyNCC) BGPNeighbors() ([]*quagga.Neighbor, error)                            { return nil, nil }
func (nc *dummyNCC) BGPWithdrawAll() error                                                { return nil }
func (nc *dummyNCC) BGPAdvertiseVIP(ip net.IP) error                                      { return nil }
func (nc *dummyNCC) BGPWithdrawVIP(ip net.IP) error                                       { return nil }
func (nc *dummyNCC) IPVSFlush() error                                                     { return nil }
func (nc *dummyNCC) IPVSGetServices() ([]*ipvs.Service, error)                            { return nil, nil }
func (nc *dummyNCC) IPVSGetService(svc *ipvs.Service) (*ipvs.Service, error)              { return nil, nil }
func (nc *dummyNCC) IPVSAddService(svc *ipvs.Service) error                               { return nil }
func (nc *dummyNCC) IPVSUpdateService(svc *ipvs.Service) error                            { return nil }
func (nc *dummyNCC) IPVSDeleteService(svc *ipvs.Service) error                            { return nil }
func (nc *dummyNCC) IPVSAddDestination(svc *ipvs.Service, dst *ipvs.Destination) error    { return nil }
func (nc *dummyNCC) IPVSUpdateDestination(svc *ipvs.Service, dst *ipvs.Destination) error { return nil }
func (nc *dummyNCC) IPVSDeleteDestination(svc *ipvs.Service, dst *ipvs.Destination) error { return nil }
func (nc *dummyNCC) RouteDefaultIPv4() (net.IP, error)                                    { return nil, nil }

type DummyLBInterface struct {
	Vips     map[seesaw.VIP]bool
	Vlans    map[uint16]bool
	Vservers map[string]map[seesaw.AF]bool
}

// NewDummyLBInterface returns a dummy LBInterface for testing purpose.
// Internally it tracks the vips, vlans and vservers resources it manages and
// can be read back through public fields.
func NewDummyLBInterface() *DummyLBInterface {
	return &DummyLBInterface{
		make(map[seesaw.VIP]bool),
		make(map[uint16]bool),
		make(map[string]map[seesaw.AF]bool),
	}
}

func (lb *DummyLBInterface) Init() error { return nil }
func (lb *DummyLBInterface) Up() error   { return nil }
func (lb *DummyLBInterface) Down() error { return nil }
func (lb *DummyLBInterface) AddVIP(vip *seesaw.VIP) error {
	log.Infof("Adding vip %v", vip)
	lb.Vips[*vip] = true
	return nil
}
func (lb *DummyLBInterface) DeleteVIP(vip *seesaw.VIP) error {
	log.Infof("Deleting vip %v", vip)
	if _, ok := lb.Vips[*vip]; !ok {
		return fmt.Errorf("deleting non-existent VIP: %v", vip)
	}
	delete(lb.Vips, *vip)
	return nil
}
func (lb *DummyLBInterface) AddVLAN(vlan *seesaw.VLAN) error {
	lb.Vlans[vlan.Key()] = true
	return nil
}
func (lb *DummyLBInterface) DeleteVLAN(vlan *seesaw.VLAN) error {
	if _, ok := lb.Vlans[vlan.Key()]; !ok {
		return fmt.Errorf("deleting non-existent VLAN: %v", vlan)
	}
	delete(lb.Vlans, vlan.Key())
	return nil
}
func (lb *DummyLBInterface) AddVserver(v *seesaw.Vserver, af seesaw.AF) error {
	log.Infof("Adding vserver %v", v)
	afMap, ok := lb.Vservers[v.Name]
	if !ok {
		afMap = make(map[seesaw.AF]bool)
		lb.Vservers[v.Name] = afMap
	}
	afMap[af] = true
	return nil
}
func (lb *DummyLBInterface) DeleteVserver(v *seesaw.Vserver, af seesaw.AF) error {
	log.Infof("Deleting vserver %v", v)
	if afMap, ok := lb.Vservers[v.Name]; !ok {
		return fmt.Errorf("deleting non-existent Vserver: %v", v)
	} else if _, ok := afMap[af]; !ok {
		return fmt.Errorf("deleting wrong AF for Vserver %v: %v", v, af)
	} else {
		delete(afMap, af)
		if len(afMap) == 0 {
			delete(lb.Vservers, v.Name)
		}
	}
	return nil
}
