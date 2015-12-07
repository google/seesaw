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

// This file implements the IPVS interface for the Seesaw Network Control
// component.

import (
	"sync"

	"github.com/google/seesaw/ipvs"
	ncctypes "github.com/google/seesaw/ncc/types"

	log "github.com/golang/glog"
)

var ipvsMutex sync.Mutex

// initIPVS initialises the IPVS sub-component.
func initIPVS() {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	log.Infof("Initialising IPVS...")
	if err := ipvs.Init(); err != nil {
		// TODO(jsing): modprobe ip_vs and try again.
		log.Fatalf("IPVS initialisation failed: %v", err)
	}
	log.Infof("IPVS version %s", ipvs.Version())
}

// IPVSFlush flushes all services and destinations from the IPVS table.
func (ncc *SeesawNCC) IPVSFlush(in int, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.Flush()
}

// IPVSGetServices gets the currently configured services from the IPVS table.
func (ncc *SeesawNCC) IPVSGetServices(in int, s *ncctypes.IPVSServices) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	svcs, err := ipvs.GetServices()
	if err != nil {
		return err
	}
	s.Services = nil
	for _, svc := range svcs {
		s.Services = append(s.Services, svc)
	}
	return nil
}

// IPVSGetService gets the currently configured service from the IPVS table,
// which matches the specified service.
func (ncc *SeesawNCC) IPVSGetService(si *ipvs.Service, s *ncctypes.IPVSServices) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	so, err := ipvs.GetService(si)
	if err != nil {
		return err
	}
	s.Services = []*ipvs.Service{so}
	return nil
}

// IPVSAddService adds the specified service to the IPVS table.
func (ncc *SeesawNCC) IPVSAddService(svc *ipvs.Service, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.AddService(*svc)
}

// IPVSUpdateService updates the specified service in the IPVS table.
func (ncc *SeesawNCC) IPVSUpdateService(svc *ipvs.Service, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.UpdateService(*svc)
}

// IPVSDeleteService deletes the specified service from the IPVS table.
func (ncc *SeesawNCC) IPVSDeleteService(svc *ipvs.Service, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.DeleteService(*svc)
}

// IPVSAddDestination adds the specified destination to the IPVS table.
func (ncc *SeesawNCC) IPVSAddDestination(dst *ncctypes.IPVSDestination, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.AddDestination(*dst.Service, *dst.Destination)
}

// IPVSUpdateDestination updates the specified destination in the IPVS table.
func (ncc *SeesawNCC) IPVSUpdateDestination(dst *ncctypes.IPVSDestination, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.UpdateDestination(*dst.Service, *dst.Destination)
}

// IPVSDeleteDestination deletes the specified destination from the IPVS table.
func (ncc *SeesawNCC) IPVSDeleteDestination(dst *ncctypes.IPVSDestination, out *int) error {
	ipvsMutex.Lock()
	defer ipvsMutex.Unlock()
	return ipvs.DeleteDestination(*dst.Service, *dst.Destination)
}
