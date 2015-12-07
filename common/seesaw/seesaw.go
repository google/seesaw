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

// Package seesaw contains structures, interfaces and utility functions that
// are used by Seesaw v2 components.
package seesaw

import (
	"fmt"
	"net"
	"syscall"
	"time"

	"github.com/google/seesaw/ipvs"
)

const (
	SeesawVersion = 2

	BinaryPath = "/usr/local/seesaw"
	ConfigPath = "/etc/seesaw"
	RunPath    = "/var/run/seesaw"
)

var (
	EngineSocket = socketPath("engine")
	NCCSocket    = socketPath("ncc")
)

// AF represents a network address family.
type AF int

const (
	IPv4 AF = syscall.AF_INET
	IPv6 AF = syscall.AF_INET6
)

// String returns the string representation of an AF.
func (af AF) String() string {
	switch af {
	case IPv4:
		return "IPv4"
	case IPv6:
		return "IPv6"
	}
	return "(unknown)"
}

// AFs returns the Seesaw-supported address families.
func AFs() []AF {
	return []AF{IPv4, IPv6}
}

// Component identifies a particular Seesaw component.
type Component int

const (
	SCNone Component = iota
	SCEngine
	SCECU
	SCHA
	SCHealthcheck
	SCLocalCLI
	SCNCC
	SCRemoteCLI
)

var componentNames = map[Component]string{
	SCNone:        "none",
	SCEngine:      "engine",
	SCECU:         "ecu",
	SCHA:          "ha",
	SCHealthcheck: "healthcheck",
	SCLocalCLI:    "local-cli",
	SCNCC:         "ncc",
	SCRemoteCLI:   "remote-cli",
}

// String returns the string representation of a Component.
func (sc Component) String() string {
	if name, ok := componentNames[sc]; ok {
		return name
	}
	return "(unknown)"
}

// HAState indicates the High-Availability state of a Seesaw Node.
type HAState int

const (
	HAUnknown HAState = iota
	HABackup
	HADisabled
	HAError
	HAMaster
	HAShutdown
)

// HAStatus indicates the High-Availability status for a Seesaw Node.
type HAStatus struct {
	LastUpdate     time.Time
	State          HAState
	Since          time.Time
	Sent           uint64
	Received       uint64
	ReceivedQueued uint64
	Transitions    uint64
}

// HealthcheckMode specifies the mode for a Healthcheck.
type HealthcheckMode int

const (
	HCModePlain HealthcheckMode = iota
	HCModeDSR
)

// String returns the name for a given HealthcheckMode.
func (h HealthcheckMode) String() string {
	switch h {
	case HCModePlain:
		return "PLAIN"
	case HCModeDSR:
		return "DSR"
	default:
		return "(unknown)"
	}
}

// HealthcheckType specifies the type for a Healthcheck.
type HealthcheckType int

const (
	HCTypeNone HealthcheckType = iota
	HCTypeDNS
	HCTypeHTTP
	HCTypeHTTPS
	HCTypeICMP
	HCTypeRADIUS
	HCTypeTCP
	HCTypeTCPTLS
	HCTypeUDP
)

// String returns the name for the given HealthcheckType.
func (h HealthcheckType) String() string {
	switch h {
	case HCTypeDNS:
		return "DNS"
	case HCTypeHTTP:
		return "HTTP"
	// TODO(angusc): Drop HTTPS as a separate type.
	case HCTypeHTTPS:
		return "HTTP" // NB: Not HTTPS
	case HCTypeICMP:
		return "ICMP"
	case HCTypeRADIUS:
		return "RADIUS"
	case HCTypeTCP:
		return "TCP"
	// TODO(angusc): Drop TCPTLS as a separate type.
	case HCTypeTCPTLS:
		return "TCP" // NB: Not TCPTLS
	case HCTypeUDP:
		return "UDP"
	}
	return "(unknown)"
}

// VIPType indicates whether a VIP is in a normal, dedicated, or anycast subnet.
type VIPType int

const (
	AnycastVIP VIPType = iota
	DedicatedVIP
	UnicastVIP
)

// String returns the string representation of a VIPType.
func (vt VIPType) String() string {
	switch vt {
	case AnycastVIP:
		return "Anycast"
	case DedicatedVIP:
		return "Dedicated"
	case UnicastVIP:
		return "Unicast"
	default:
		return "Unknown"
	}
}

// VIP represents a Seesaw VIP.
type VIP struct {
	IP
	Type VIPType
}

// String returns the string representation of a VIP.
func (v VIP) String() string {
	return fmt.Sprintf("%v (%v)", v.IP, v.Type)
}

// NewVIP returns a seesaw VIP.
func NewVIP(ip net.IP, vipSubnets map[string]*net.IPNet) *VIP {
	vipType := UnicastVIP
	switch {
	case IsAnycast(ip):
		vipType = AnycastVIP
	case InSubnets(ip, vipSubnets):
		vipType = DedicatedVIP
	}
	return &VIP{
		IP:   NewIP(ip),
		Type: vipType,
	}
}

// IP specifies an IP address.
type IP [net.IPv6len]byte

// NewIP returns a seesaw IP initialised from a net.IP address.
func NewIP(nip net.IP) IP {
	var ip IP
	copy(ip[:], nip.To16())
	return ip
}

// ParseIP parses the given string and returns a seesaw IP initialised with the
// resulting IP address.
func ParseIP(ip string) IP {
	return NewIP(net.ParseIP(ip))
}

// Equal returns true of the given seesaw.IP addresses are equal, as
// determined by net.IP.Equal().
func (ip IP) Equal(eip IP) bool {
	return ip.IP().Equal(eip.IP())
}

// IP returns the net.IP representation of a seesaw IP address.
func (ip IP) IP() net.IP {
	return net.IP(ip[:])
}

// AF returns the address family of a seesaw IP address.
func (ip IP) AF() AF {
	if ip.IP().To4() != nil {
		return IPv4
	}
	return IPv6
}

// String returns the string representation of an IP address.
func (ip IP) String() string {
	return fmt.Sprintf("%v", ip.IP())
}

// IPProto specifies an IP protocol.
type IPProto uint16

const (
	IPProtoICMP   IPProto = syscall.IPPROTO_ICMP
	IPProtoICMPv6 IPProto = syscall.IPPROTO_ICMPV6
	IPProtoTCP    IPProto = syscall.IPPROTO_TCP
	IPProtoUDP    IPProto = syscall.IPPROTO_UDP
)

// String returns the name for the given protocol value.
func (proto IPProto) String() string {
	switch proto {
	case IPProtoICMP:
		return "ICMP"
	case IPProtoICMPv6:
		return "ICMPv6"
	case IPProtoTCP:
		return "TCP"
	case IPProtoUDP:
		return "UDP"
	}
	return fmt.Sprintf("IP(%d)", proto)
}

// LBMode specifies the load balancing mode for a Vserver.
type LBMode int

const (
	LBModeNone LBMode = iota
	LBModeDSR
	LBModeNAT
)

var modeNames = map[LBMode]string{
	LBModeNone: "None",
	LBModeDSR:  "DSR",
	LBModeNAT:  "NAT",
}

// String returns the string representation of a LBMode.
func (lbm LBMode) String() string {
	if name, ok := modeNames[lbm]; ok {
		return name
	}
	return "(unknown)"
}

// LBScheduler specifies the load balancer scheduling algorithm for a Vserver.
type LBScheduler int

const (
	LBSchedulerNone LBScheduler = iota
	LBSchedulerRR
	LBSchedulerWRR
	LBSchedulerLC
	LBSchedulerWLC
	LBSchedulerSH
)

var schedulerNames = map[LBScheduler]string{
	LBSchedulerNone: "none",
	LBSchedulerRR:   "rr",
	LBSchedulerWRR:  "wrr",
	LBSchedulerLC:   "lc",
	LBSchedulerWLC:  "wlc",
	LBSchedulerSH:   "sh",
}

// String returns the string representation of a LBScheduler.
func (lbs LBScheduler) String() string {
	if name, ok := schedulerNames[lbs]; ok {
		return name
	}
	return "(unknown)"
}

// OverrideState specifies an override state for a vserver, backend, or
// destination.
type OverrideState int

const (
	OverrideDefault OverrideState = iota
	OverrideDisable
	OverrideEnable
)

// String returns the string representation of an OverrideState.
func (o OverrideState) String() string {
	switch o {
	case OverrideDefault:
		return "default"
	case OverrideDisable:
		return "disabled"
	case OverrideEnable:
		return "enabled"
	}
	return "(unknown)"
}

type Override interface {
	Target() string
	State() OverrideState
}

type VserverOverride struct {
	VserverName string
	OverrideState
}

type BackendOverride struct {
	Hostname string
	OverrideState
}

type DestinationOverride struct {
	VserverName     string
	DestinationName string
	OverrideState
}

// Host contains the hostname, IP addresses, and IP masks for a host.
type Host struct {
	Hostname string
	IPv4Addr net.IP
	IPv4Mask net.IPMask
	IPv6Addr net.IP
	IPv6Mask net.IPMask
}

// Node represents a node that is part of a Seesaw cluster.
type Node struct {
	Host
	Priority        uint8
	State           HAState
	AnycastEnabled  bool
	BGPEnabled      bool
	VserversEnabled bool
}

// Nodes represents a list of Nodes
type Nodes []*Node

// ConfigMetadata describes part of the state of the currently-loaded config.
type ConfigMetadata struct {
	Name  string
	Value string
}

// ConfigStatus describes the status of the currently-loaded config.
type ConfigStatus struct {
	Attributes []ConfigMetadata
	LastUpdate time.Time
	Warnings   []string
}

// ClusterStatus specifies the status of a Seesaw cluster.
type ClusterStatus struct {
	Version int
	Site    string
	Nodes
}

// HAConfig represents the high availability configuration for a node in a
// Seesaw cluster.
type HAConfig struct {
	Enabled    bool
	LocalAddr  net.IP
	RemoteAddr net.IP
	Priority   uint8
	VRID       uint8
}

// VLAN represents a VLAN interface configuration.
type VLAN struct {
	ID uint16
	Host
	BackendCount map[AF]uint
	VIPCount     map[AF]uint
}

// VLANs provides a slice of VLANs.
type VLANs struct {
	VLANs []*VLAN
}

// Vserver represents a virtual server configured for load balancing.
type Vserver struct {
	Name    string
	Entries []*VserverEntry
	FWM     map[AF]uint32
	Host
	Services map[ServiceKey]*Service
	OverrideState
	Enabled       bool
	ConfigEnabled bool
	Warnings      []string
}

// VserverEntry represents a port and protocol combination for a Vserver.
type VserverEntry struct {
	Port          uint16
	Proto         IPProto
	Scheduler     LBScheduler
	Mode          LBMode
	Persistence   int
	OnePacket     bool
	HighWatermark float32
	LowWatermark  float32
	// TODO(angusc): Rename these:
	LThreshold int
	UThreshold int
}

// VserverMap provides a map of vservers keyed by vserver name.
type VserverMap struct {
	Vservers map[string]*Vserver
}

// ServiceKey provides a unique identifier for a load balancing service.
type ServiceKey struct {
	AF
	Proto IPProto
	Port  uint16
}

// ServiceKeys represents a list of ServiceKey.
type ServiceKeys []*ServiceKey

// Service represents a load balancing service.
type Service struct {
	ServiceKey
	IP               net.IP
	Mode             LBMode
	Scheduler        LBScheduler
	OnePacket        bool
	Persistence      int
	Stats            *ServiceStats
	Destinations     map[string]*Destination // keyed by backend hostname
	Enabled          bool
	Healthy          bool
	Active           bool
	HighWatermark    float32
	LowWatermark     float32
	CurrentWatermark float32
}

// ServiceStats contains statistics for a Service.
type ServiceStats struct {
	*ipvs.ServiceStats
}

// Destination represents a load balancing destination.
type Destination struct {
	Name        string
	VserverName string
	Weight      int32
	Stats       *DestinationStats
	Backend     *Backend
	Enabled     bool
	Healthy     bool
	Active      bool
}

// DestinationStats contains statistics for a Destination.
type DestinationStats struct {
	*ipvs.DestinationStats
}

// Destinations represents a list of Destination.
type Destinations []*Destination

// Backend represents a backend server that can be used as a load balancing
// destination.
type Backend struct {
	Host
	Weight    int32
	Enabled   bool
	InService bool
}

// BackendMap provides a map of backends keyed by backend hostname.
type BackendMap struct {
	Backends map[string]*Backend
}
