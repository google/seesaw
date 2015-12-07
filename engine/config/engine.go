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

// Author: angusc@google.com (Angus Cameron)

package config

// This file contains structs and functions to set up the basic configuration
// for a seesaw engine.

import (
	"net"
	"path"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

var defaultEngineConfig = EngineConfig{
	AnycastEnabled:          true,
	BGPUpdateInterval:       15 * time.Second,
	CACertFile:              path.Join(seesaw.ConfigPath, "ssl", "ca.crt"),
	ConfigFile:              path.Join(seesaw.ConfigPath, "seesaw.cfg"),
	ConfigInterval:          1 * time.Minute,
	ConfigServers:           []string{"seesaw-config.example.com"},
	ConfigServerPort:        10255,
	ConfigServerTimeout:     20 * time.Second,
	ClusterFile:             path.Join(seesaw.ConfigPath, "cluster.pb"),
	DummyInterface:          "dummy0",
	GratuitousARPInterval:   10 * time.Second,
	HAStateTimeout:          30 * time.Second,
	LBInterface:             "eth1",
	MaxPeerConfigSyncErrors: 3,
	NCCSocket:               seesaw.NCCSocket,
	NodeInterface:           "eth0",
	RoutingTableID:          2,
	ServiceAnycastIPv4:      []net.IP{seesaw.TestAnycastHost().IPv4Addr},
	ServiceAnycastIPv6:      []net.IP{seesaw.TestAnycastHost().IPv6Addr},
	SocketPath:              seesaw.EngineSocket,
	StatsInterval:           15 * time.Second,
	SyncPort:                10258,
	VRID:                    60,
	VRRPDestIP:              net.ParseIP("224.0.0.18"),
}

// DefaultEngineConfig returns the default engine configuration.
func DefaultEngineConfig() EngineConfig {
	return defaultEngineConfig
}

// EngineConfig provides configuration details for an Engine.
type EngineConfig struct {
	AnycastEnabled          bool          // Flag to enable or disable anycast.
	BGPUpdateInterval       time.Duration // The BGP update interval.
	CACertFile              string        // The path to the SSL/TLS CA cert file.
	ClusterFile             string        // The path to the cluster protobuf file.
	ClusterName             string        // The name of the cluster the engine is running in.
	ClusterVIP              seesaw.Host   // The VIP for this Seesaw Cluster.
	ConfigInterval          time.Duration // The cluster configuration update interval.
	ConfigFile              string        // The path to the engine config file.
	ConfigServers           []string      // The list of configuration servers (hostnames) in priority order.
	ConfigServerPort        int           // The configuration server port number.
	ConfigServerTimeout     time.Duration // The configuration server client timeout (per TCP connection).
	DummyInterface          string        // The dummy network interface.
	GratuitousARPInterval   time.Duration // The interval for gratuitous ARP messages.
	HAStateTimeout          time.Duration // The timeout for receiving HAState updates.
	LBInterface             string        // The network interface to use for load balancing.
	MaxPeerConfigSyncErrors int           // The number of allowable peer config sync errors.
	NCCSocket               string        // The Network Control Center socket.
	NodeInterface           string        // The primary network interface for this node.
	Node                    seesaw.Host   // The node the engine is running on.
	Peer                    seesaw.Host   // The node's peer.
	RoutingTableID          uint8         // The routing table ID to use for load balanced traffic.
	ServiceAnycastIPv4      []net.IP      // IPv4 anycast addresses that are always advertised.
	ServiceAnycastIPv6      []net.IP      // IPv6 anycast addresses that are always advertised.
	SocketPath              string        // The path to the engine socket.
	StatsInterval           time.Duration // The statistics update interval.
	SyncPort                int           // The port for sync'ing with this node's peer.
	VMAC                    string        // The VMAC address to use for the load balancing network interface.
	VRID                    uint8         // The VRRP virtual router ID for the cluster.
	VRRPDestIP              net.IP        // The destination IP for VRRP advertisements.
}
