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

// This file contains the BGP related configuration and control functions for
// the Seesaw v2 Network Control Center. These functions interface with
// Quagga's bgpd via its VTY interface.

import (
	"fmt"
	"net"
	"strings"

	ncctypes "github.com/google/seesaw/ncc/types"
	"github.com/google/seesaw/quagga"
)

// TODO(jsing): Make this configurable.
const seesawASN = uint32(64512)

// quaggaBGP establishes a connection with the Quagga BGP daemon.
func quaggaBGP(asn uint32) (*quagga.BGP, error) {
	bgp := quagga.NewBGP("", asn)
	if err := bgp.Dial(); err != nil {
		return nil, err
	}
	if err := bgp.Enable(); err != nil {
		bgp.Close()
		return nil, err
	}
	return bgp, nil
}

// BGPConfig returns the current configuration for the Quagga BGP daemon.
func (ncc *SeesawNCC) BGPConfig(unused int, cfg *ncctypes.BGPConfig) error {
	bgp, err := quaggaBGP(seesawASN)
	if err != nil {
		return err
	}
	defer bgp.Close()
	bgpCfg, err := bgp.Configuration()
	if err != nil {
		return err
	}
	if cfg != nil {
		cfg.Config = bgpCfg
	}
	return nil
}

// BGPNeighbors returns the current BGP neighbors for the Quagga BGP daemon.
func (ncc *SeesawNCC) BGPNeighbors(unused int, neighbors *ncctypes.BGPNeighbors) error {
	bgp, err := quaggaBGP(seesawASN)
	if err != nil {
		return err
	}
	defer bgp.Close()
	bgpNeighbors, err := bgp.Neighbors()
	if err != nil {
		return err
	}
	if neighbors != nil {
		neighbors.Neighbors = bgpNeighbors
	}
	return nil
}

// hostMask returns an IP mask that corresponds with a host prefix.
func hostMask(ip net.IP) net.IPMask {
	var hl int
	if ip.To4() != nil {
		hl = net.IPv4len * 8
	} else {
		hl = net.IPv6len * 8
	}
	return net.CIDRMask(hl, hl)
}

// BGPWithdrawAll removes all network advertisements from the Quagga BGP daemon.
func (ncc *SeesawNCC) BGPWithdrawAll(unused int, reply *int) error {
	bgp, err := quaggaBGP(seesawASN)
	if err != nil {
		return err
	}

	defer bgp.Close()
	bgpCfg, err := bgp.Configuration()
	if err != nil {
		return err
	}

	// Find the network statements within the router bgp section.
	bgpStr := fmt.Sprintf("router bgp %d", seesawASN)
	found := false
	for _, line := range bgpCfg {
		if line == bgpStr {
			found = true
			continue
		}
		if !found || line == "!" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			break
		}
		if strings.HasPrefix(line, " network ") {
			n := strings.Replace(line, " network ", "", 1)
			vip, ipNet, err := net.ParseCIDR(n)
			if err != nil {
				return err
			}
			err = bgp.Withdraw(&net.IPNet{IP: vip, Mask: ipNet.Mask})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// BGPAdvertiseVIP requests the Quagga BGP daemon to advertise the given VIP.
func (ncc *SeesawNCC) BGPAdvertiseVIP(vip net.IP, unused *int) error {
	bgp, err := quaggaBGP(seesawASN)
	if err != nil {
		return err
	}
	defer bgp.Close()
	return bgp.Advertise(&net.IPNet{IP: vip, Mask: hostMask(vip)})
}

// BGPWithdrawVIP requests the Quagga BGP daemon to withdraw the given VIP.
func (ncc *SeesawNCC) BGPWithdrawVIP(vip net.IP, unused *int) error {
	bgp, err := quaggaBGP(seesawASN)
	if err != nil {
		return err
	}
	defer bgp.Close()
	return bgp.Withdraw(&net.IPNet{IP: vip, Mask: hostMask(vip)})
}
