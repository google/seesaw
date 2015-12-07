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

package quagga

// This file contains structures and functions for querying and manipulating
// the Quagga BGP daemon.

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const BGPSocketPath = "/var/run/quagga/bgpd.vty"

var bgpConfigLock sync.Mutex

// BGPState represents a BGP peering state.
type BGPState uint

const (
	BGPStateUnknown     BGPState = 0
	BGPStateIdle        BGPState = 1
	BGPStateConnect     BGPState = 2
	BGPStateActive      BGPState = 3
	BGPStateOpenSent    BGPState = 4
	BGPStateOpenConfirm BGPState = 5
	BGPStateEstablished BGPState = 6
)

var stateNames = map[BGPState]string{
	BGPStateUnknown:     "Unknown",
	BGPStateIdle:        "Idle",
	BGPStateConnect:     "Connect",
	BGPStateActive:      "Active",
	BGPStateOpenSent:    "OpenSent",
	BGPStateOpenConfirm: "OpenConfirm",
	BGPStateEstablished: "Established",
}

// String returns a string representation of the given BGP state.
func (s BGPState) String() string {
	if name, ok := stateNames[s]; ok {
		return name
	}
	return "Unknown"
}

// BGPStateByName returns the BGP state that corresponds to the given name.
func BGPStateByName(name string) BGPState {
	name = strings.ToLower(name)
	for s := range stateNames {
		if strings.ToLower(stateNames[s]) == name {
			return s
		}
	}
	return BGPStateUnknown
}

var (
	uptimeHourRE = regexp.MustCompile(`^(\d+):(\d+):(\d+)$`)
	uptimeDayRE  = regexp.MustCompile(`^(\d+)d(\d+)h(\d+)m$`)
	uptimeWeekRE = regexp.MustCompile(`^(\d+)w(\d+)d(\d+)h$`)
)

// ParseUptime parses an uptime string and returns the corresponding duration.
// Time can come in five formats:
//   07:05:56
//   2d23h12m
//   03w5d13h
//   never
//   <error>
// See the peer_uptime function in quagga/linc/bgpd/bgpd.c.
func ParseUptime(uptime string) time.Duration {
	var w, d, h, m, s int
	if uptime == "never" || uptime == "<error>" {
		return 0
	} else if um := uptimeHourRE.FindStringSubmatch(uptime); um != nil {
		h, _ = strconv.Atoi(um[1])
		m, _ = strconv.Atoi(um[2])
		s, _ = strconv.Atoi(um[3])
	} else if um := uptimeDayRE.FindStringSubmatch(uptime); um != nil {
		d, _ = strconv.Atoi(um[1])
		h, _ = strconv.Atoi(um[2])
		m, _ = strconv.Atoi(um[3])
	} else if um := uptimeWeekRE.FindStringSubmatch(uptime); um != nil {
		w, _ = strconv.Atoi(um[1])
		d, _ = strconv.Atoi(um[2])
		h, _ = strconv.Atoi(um[3])
	} else {
		return 0
	}
	return time.Duration(w)*168*time.Hour + time.Duration(d)*24*time.Hour +
		time.Duration(h)*time.Hour + time.Duration(m)*time.Minute +
		time.Duration(s)*time.Second
}

// MessageStat encapsulates the statistics for a given message type.
type MessageStat struct {
	Sent uint64
	Rcvd uint64
}

// MessageStats encapsulates messages statistics for a BGP neighbor.
type MessageStats struct {
	Opens         MessageStat
	Notifications MessageStat
	Updates       MessageStat
	Keepalives    MessageStat
	RouteRefresh  MessageStat
	Capability    MessageStat
	Total         MessageStat
}

// Neighbor represents a BGP neighbor.
type Neighbor struct {
	IP          net.IP
	RouterID    net.IP
	ASN         uint32
	Description string
	BGPState    BGPState
	Uptime      time.Duration
	MessageStats
}

// Neighbors represents a list of BGP neighbors.
type Neighbors struct {
	Neighbors []*Neighbor
}

// BGP contains the data needed to interface with the Quagga BGP daemon.
type BGP struct {
	vty *VTY
	asn uint32
}

// NewBGP returns an initialised BGP structure.
func NewBGP(socket string, asn uint32) *BGP {
	if socket == "" {
		socket = BGPSocketPath
	}
	return &BGP{
		vty: NewVTY(socket),
		asn: asn,
	}
}

// Dial establishes a connection to the VTY of the BGP daemon.
func (b *BGP) Dial() error {
	return b.vty.Dial()
}

// Close closes a connection to the VTY of the BGP daemon.
func (b *BGP) Close() error {
	return b.vty.Close()
}

// Enable issues an "enable" command to the BGP daemon.
func (b *BGP) Enable() error {
	_, err := b.vty.Command("enable")
	return err
}

// Configuration returns the current running configuration from the BGP daemon,
// as a slice of strings.
func (b *BGP) Configuration() ([]string, error) {
	cfg, err := b.vty.Command("write terminal")
	if err != nil {
		return nil, err
	}
	return strings.Split(cfg, "\n"), nil
}

// Neighbors returns a list of BGP neighbors that we are currently peering with.
func (b *BGP) Neighbors() ([]*Neighbor, error) {
	ni, err := b.vty.Command("show ip bgp neighbors")
	if err != nil {
		return nil, err
	}
	// TODO(jsing): Include details for advertised/received routes.
	return parseNeighbors(ni), nil
}

// network adds or removes a network statement from the BGP configuration.
func (b *BGP) network(n *net.IPNet, advertise bool) error {
	var prefix string
	if !advertise {
		prefix = "no "
	}
	family := "ipv4 unicast"
	if n.IP.To4() == nil {
		family = "ipv6"
	}
	prefixLen, _ := n.Mask.Size()
	cmds := []string{
		"configure terminal",
		fmt.Sprintf("router bgp %d", b.asn),
		fmt.Sprintf("address-family %s", family),
		fmt.Sprintf("%snetwork %s/%d", prefix, n.IP, prefixLen),
		"end",
	}
	bgpConfigLock.Lock()
	defer bgpConfigLock.Unlock()
	return b.vty.Commands(cmds)
}

// Advertise requests the BGP daemon to advertise the specified network.
func (b *BGP) Advertise(n *net.IPNet) error {
	return b.network(n, true)
}

// Withdraw requests the BGP daemon to withdraw advertisements for the
// specified network.
func (b *BGP) Withdraw(n *net.IPNet) error {
	return b.network(n, false)
}

var (
	neighborRE        = regexp.MustCompile(`^BGP neighbor is ([a-f0-9.:]+), remote AS (\d+), local AS (\d+),.*$`)
	neighborDescRE    = regexp.MustCompile(`^ Description: (.*)$`)
	neighborStateRE   = regexp.MustCompile(`^  BGP state = (\w+)(, up for ([0-9wdhm:]+))?$`)
	neighborStatsRE   = regexp.MustCompile(`^    (\w+): +(\d+) +(\d+)$`)
	neighborVersionRE = regexp.MustCompile(`^  BGP version (\d), remote router ID ([a-f0-9.:]+)`)
)

// parseNeighbors parses the "show ip bgp neighbors" output from the Quagga
// BGP daemon and returns a slice of Neighbor structs.
func parseNeighbors(sn string) []*Neighbor {
	neighbors := make([]*Neighbor, 0)
	var neighbor *Neighbor
	var msgStats bool
	for _, s := range strings.Split(sn, "\n") {
		if nm := neighborRE.FindStringSubmatch(s); nm != nil {
			asn, _ := strconv.ParseUint(nm[2], 10, 32)
			neighbor = &Neighbor{
				IP:  net.ParseIP(nm[1]),
				ASN: uint32(asn),
			}
			neighbors = append(neighbors, neighbor)
		}
		if neighbor == nil {
			continue
		}

		if msgStats {
			if nm := neighborStatsRE.FindStringSubmatch(s); nm != nil {
				var ms *MessageStat
				switch nm[1] {
				case "Opens":
					ms = &neighbor.Opens
				case "Notifications":
					ms = &neighbor.Notifications
				case "Updates":
					ms = &neighbor.Updates
				case "Keepalives":
					ms = &neighbor.Keepalives
				case "Route Refresh":
					ms = &neighbor.RouteRefresh
				case "Capability":
					ms = &neighbor.Capability
				case "Total":
					ms = &neighbor.Total
					msgStats = false
				}
				if ms != nil {
					sent, _ := strconv.ParseUint(nm[2], 10, 0)
					ms.Sent = sent
					rcvd, _ := strconv.ParseUint(nm[3], 10, 0)
					ms.Rcvd = rcvd
				}
			}
		}

		if nm := neighborDescRE.FindStringSubmatch(s); nm != nil {
			neighbor.Description = nm[1]
		} else if nm := neighborStateRE.FindStringSubmatch(s); nm != nil {
			neighbor.BGPState = BGPStateByName(nm[1])
			neighbor.Uptime = ParseUptime(nm[3])
		} else if nm := neighborVersionRE.FindStringSubmatch(s); nm != nil {
			neighbor.RouterID = net.ParseIP(nm[2])
		} else if s == "  Message statistics:" {
			msgStats = true
		}
	}
	return neighbors
}
