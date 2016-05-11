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

// Package config implements functions to manage the configuration for a
// Seesaw v2 engine.
package config

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/google/seesaw/common/seesaw"
	pb "github.com/google/seesaw/pb/config"

	log "github.com/golang/glog"
	"github.com/golang/protobuf/proto"
)

var defaultArchiveConfig = archiveConfig{
	age:   60 * 24 * time.Hour,
	bytes: 10 * 1 << 30,
	count: 1500,
}

// Source specifies a source of configuration information.
type Source int

const (
	SourceNone Source = iota
	SourceDisk
	SourcePeer
	SourceServer
)

var sourceNames = map[Source]string{
	SourceNone:   "none",
	SourceDisk:   "disk",
	SourcePeer:   "peer",
	SourceServer: "server",
}

// SourceByName returns the source that has the given name.
func SourceByName(name string) (Source, error) {
	for s, n := range sourceNames {
		if n == name {
			return s, nil
		}
	}
	return -1, fmt.Errorf("unknown source %q", name)
}

// String returns the string representation of a source.
func (s Source) String() string {
	if name, ok := sourceNames[s]; ok {
		return name
	}
	return "(unknown)"
}

// Notification represents a configuration change notification.
type Notification struct {
	Cluster      *Cluster
	MetadataOnly bool
	protobuf     *pb.Cluster
	Source       Source
	SourceDetail string
	Time         time.Time
}

func (n *Notification) String() string {
	return fmt.Sprintf("config from %v (%v) at %v", n.Source, n.SourceDetail, n.Time)
}

// ReadConfig reads a cluster configuration file.
func ReadConfig(filename, clusterName string) (*Notification, error) {
	p := &pb.Cluster{}
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if err = proto.UnmarshalText(string(b), p); err != nil {
		return nil, err
	}

	c, err := protoToCluster(p, clusterName)
	if err != nil {
		return nil, err
	}
	return &Notification{c, false, p, SourceDisk, filename, time.Now()}, nil
}

// ConfigFromServer fetches the cluster configuration for the given cluster.
func ConfigFromServer(cluster string) (*Notification, error) {
	cfg := DefaultEngineConfig()
	cfg.ClusterName = cluster
	n := &Notifier{engineCfg: &cfg}
	return n.configFromServer()
}

func saveConfig(p *pb.Cluster, file string, backup bool) error {
	if backup {
		if err := backupConfig(file); err != nil {
			log.Warningf("Failed to back up existing file %s: %v", file, err)
		}
	}
	tmpFile := fmt.Sprintf("%s.%d", file, os.Getpid())
	content := proto.MarshalTextString(p)
	defer os.Remove(tmpFile)
	if err := ioutil.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("saveConfig(%q): Write to %q failed: %v", file, tmpFile, err)
	}
	if err := os.Rename(tmpFile, file); err != nil {
		return fmt.Errorf("saveConfig(%q): Rename(%q, %q) failed: %v", file, tmpFile, file, err)
	}
	return nil
}

func backupConfig(file string) error {
	fi, err := os.Stat(file)
	if os.IsNotExist(err) {
		// Source file doesn't exist - nothing to do.
		return nil
	}
	src, err := os.Open(file)
	if err != nil {
		return err
	}
	defer src.Close()

	backupDir := filepath.Join(filepath.Dir(file), "archive")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	stats, err := pruneArchive(backupDir, time.Now(), defaultArchiveConfig)
	if err != nil {
		log.Errorf("Error while trying to prune archive: %v", err)
	} else {
		log.Infof("Pruned %d files from archive; %d files (%d bytes) skipped, oldest %v; %d files failed to be removed",
			stats.filesRemoved, stats.count, stats.bytes, stats.age, stats.filesErrored)
	}

	backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.%d", filepath.Base(file), time.Now().Unix()))

	dst, err := os.OpenFile(backupFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fi.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func protoToCluster(p *pb.Cluster, clusterName string) (*Cluster, error) {
	c := NewCluster(clusterName)
	c.VIP = protoToHost(p.SeesawVip)
	c.BGPLocalASN = uint32(p.GetBgpLocalAsn())
	c.BGPRemoteASN = uint32(p.GetBgpRemoteAsn())

	addBGPPeers(c, p)
	addMetadata(c, p)
	addNodes(c, p)
	addVIPSubnets(c, p)
	addVLANs(c, p)
	addVservers(c, p)
	addWarnings(c, p)

	return c, nil
}

func protosToHealthchecks(pbs []*pb.Healthcheck, defaultPort uint16) []*Healthcheck {
	var checks Healthchecks
	checks = make([]*Healthcheck, 0, len(pbs))
	for _, p := range pbs {
		checks = append(checks, protoToHealthcheck(p, defaultPort))
	}
	sort.Sort(checks)

	// Assign names to each Healthcheck based on the type and port. If there are
	// multiple healthchecks with the same type and port, append a counter
	// suffix. This code requires that the list of Healthchecks are sorted by
	// type and port, then by the remaining Healthcheck fields to provide a
	// stable sort.
	lastType := seesaw.HCTypeNone
	lastPort := uint16(0)
	counter := 0
	for _, hc := range checks {
		if hc.Type == lastType && hc.Port == lastPort {
			counter++
		} else {
			counter = 0
			lastType = hc.Type
			lastPort = hc.Port
		}
		hc.Name = fmt.Sprintf("%s/%d_%d", hc.Type, hc.Port, counter)
	}
	return checks
}

func protoToHealthcheck(p *pb.Healthcheck, defaultPort uint16) *Healthcheck {
	var hcMode seesaw.HealthcheckMode
	switch p.GetMode() {
	case pb.Healthcheck_PLAIN:
		hcMode = seesaw.HCModePlain
	case pb.Healthcheck_DSR:
		hcMode = seesaw.HCModeDSR
	}
	var hcType seesaw.HealthcheckType
	switch p.GetType() {
	case pb.Healthcheck_ICMP_PING:
		hcType = seesaw.HCTypeICMP
	case pb.Healthcheck_UDP:
		hcType = seesaw.HCTypeUDP
	case pb.Healthcheck_TCP:
		hcType = seesaw.HCTypeTCP
	case pb.Healthcheck_HTTP:
		hcType = seesaw.HCTypeHTTP
	case pb.Healthcheck_HTTPS:
		hcType = seesaw.HCTypeHTTPS
	case pb.Healthcheck_DNS:
		hcType = seesaw.HCTypeDNS
	case pb.Healthcheck_TCP_TLS:
		hcType = seesaw.HCTypeTCPTLS
	case pb.Healthcheck_RADIUS:
		hcType = seesaw.HCTypeRADIUS
	}
	port := uint16(p.GetPort())
	if port == 0 {
		port = defaultPort
	}
	hc := NewHealthcheck(hcMode, hcType, port)
	hc.Interval = time.Duration(p.GetInterval()) * time.Second
	hc.Timeout = time.Duration(p.GetTimeout()) * time.Second
	hc.Retries = int(p.GetRetries())
	hc.Send = p.GetSend()
	hc.Receive = p.GetReceive()
	hc.Code = int(p.GetCode())
	hc.Proxy = p.GetProxy()
	hc.Method = p.GetMethod()
	hc.TLSVerify = p.GetTlsVerify()
	return hc
}

func protoToHost(p *pb.Host) seesaw.Host {
	ipv4, mask4 := parseCIDR(p.GetIpv4())
	ipv6, mask6 := parseCIDR(p.GetIpv6())
	return seesaw.Host{
		Hostname: p.GetFqdn(),
		IPv4Addr: ipv4,
		IPv4Mask: mask4,
		IPv6Addr: ipv6,
		IPv6Mask: mask6,
	}
}

func addBGPPeers(c *Cluster, p *pb.Cluster) {
	for _, p := range p.BgpPeer {
		peer := protoToHost(p)
		c.AddBGPPeer(&peer)
	}
}

func addMetadata(c *Cluster, p *pb.Cluster) {
	m := p.GetMetadata()
	if m == nil {
		return
	}

	c.Status.LastUpdate = time.Unix(*m.LastUpdated, 0)
	for _, attr := range m.Attribute {
		c.Status.Attributes = append(c.Status.Attributes, seesaw.ConfigMetadata{Name: *attr.Name, Value: *attr.Value})
	}
}

func addNodes(c *Cluster, p *pb.Cluster) {
	var haEnabled bool
	switch *p.SeesawVip.Status {
	case pb.Host_PRODUCTION, pb.Host_TESTING, pb.Host_BUILDING:
		haEnabled = true
	}

	for _, n := range p.Node {
		haState := seesaw.HAUnknown
		if !haEnabled || p.SeesawVip.GetStatus() != n.GetStatus() {
			haState = seesaw.HADisabled
		}

		h := protoToHost(n)
		node := &seesaw.Node{Host: h, State: haState}
		switch n.GetStatus() {
		case pb.Host_BUILDING:
			node.BGPEnabled = true
		case pb.Host_TESTING, pb.Host_PRODUCTION:
			node.AnycastEnabled = true
			node.BGPEnabled = true
			node.VserversEnabled = true
		}
		c.AddNode(node)
	}

	// Prioritise nodes by their IPv4 addresses.
	nodes := make([]*seesaw.Node, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		nodes = append(nodes, n)
	}
	sort.Sort(seesaw.NodesByIPv4{nodes})
	for i, n := range nodes {
		switch i {
		case 0:
			n.Priority = 255
		case 1:
			n.Priority = 1
		default:
			break
		}
	}
}

func addVLANs(c *Cluster, p *pb.Cluster) {
	for _, v := range p.Vlan {
		h := protoToHost(v.Host)
		c.AddVLAN(&seesaw.VLAN{
			ID:           uint16(*v.VlanId),
			Host:         h,
			BackendCount: make(map[seesaw.AF]uint),
			VIPCount:     make(map[seesaw.AF]uint),
		})
	}

	// Determine number of backend and VIP addresses in each VLAN.
	backends := make(map[seesaw.IP]bool)
	vips := make(map[seesaw.IP]bool)
	for _, vs := range p.Vserver {
		host := protoToHost(vs.GetEntryAddress())
		if host.IPv4Addr != nil {
			vips[seesaw.NewIP(host.IPv4Addr)] = true
		}
		if host.IPv6Addr != nil {
			vips[seesaw.NewIP(host.IPv6Addr)] = true
		}
		for _, backend := range vs.Backend {
			host := protoToHost(backend.GetHost())
			if host.IPv4Addr != nil {
				backends[seesaw.NewIP(host.IPv4Addr)] = true
			}
			if host.IPv6Addr != nil {
				backends[seesaw.NewIP(host.IPv6Addr)] = true
			}
		}
	}
	for _, v := range c.VLANs {
		for bip := range backends {
			if v.IPv4Net().Contains(bip.IP()) {
				v.BackendCount[seesaw.IPv4]++
			}
			if v.IPv6Net().Contains(bip.IP()) {
				v.BackendCount[seesaw.IPv6]++
			}
		}
		for vip := range vips {
			if v.IPv4Net().Contains(vip.IP()) {
				v.VIPCount[seesaw.IPv4]++
			}
			if v.IPv6Net().Contains(vip.IP()) {
				v.VIPCount[seesaw.IPv6]++
			}
		}
	}
}

func addVIPSubnets(c *Cluster, p *pb.Cluster) {
	for _, cidr := range p.DedicatedVipSubnet {
		_, vipSubnet, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Errorf("%v: Unable to parse VIP subnet %v: %v", c.Site, cidr, err)
			continue
		}
		if err := c.AddVIPSubnet(vipSubnet); err != nil {
			log.Errorf("%v: Unable to add VIP subnet %v: %v", c.Site, cidr, err)
		}
	}
}

func addVservers(c *Cluster, p *pb.Cluster) {
	for _, vs := range p.Vserver {
		host := vs.GetEntryAddress()
		v := NewVserver(vs.GetName(), protoToHost(host))
		v.Enabled = host.GetStatus() == pb.Host_PRODUCTION || host.GetStatus() == pb.Host_TESTING
		v.UseFWM = vs.GetUseFwm()
		v.Warnings = vs.GetWarning()
		sort.Strings(v.Warnings)

		for _, ip := range []net.IP{v.Host.IPv4Addr, v.Host.IPv6Addr} {
			if ip != nil {
				v.AddVIP(seesaw.NewVIP(ip, c.VIPSubnets))
			}
		}

		for _, ve := range vs.VserverEntry {
			var proto seesaw.IPProto
			switch ve.GetProtocol() {
			case pb.Protocol_TCP:
				proto = seesaw.IPProtoTCP
			case pb.Protocol_UDP:
				proto = seesaw.IPProtoUDP
			default:
				// TODO(angusc): Consider this VServer broken.
				log.Errorf("%v: Unsupported IP protocol %v", vs.GetName(), ve.GetProtocol())
				continue
			}
			e := NewVserverEntry(uint16(ve.GetPort()), proto)

			var scheduler seesaw.LBScheduler
			switch ve.GetScheduler() {
			case pb.VserverEntry_RR:
				scheduler = seesaw.LBSchedulerRR
			case pb.VserverEntry_WRR:
				scheduler = seesaw.LBSchedulerWRR
			case pb.VserverEntry_LC:
				scheduler = seesaw.LBSchedulerLC
			case pb.VserverEntry_WLC:
				scheduler = seesaw.LBSchedulerWLC
			case pb.VserverEntry_SH:
				scheduler = seesaw.LBSchedulerSH
			default:
				// TODO(angusc): Consider this VServer broken.
				log.Errorf("%v: Unsupported scheduler %v", vs.GetName(), ve.GetScheduler())
				continue
			}
			e.Scheduler = scheduler

			var mode seesaw.LBMode
			switch ve.GetMode() {
			case pb.VserverEntry_DSR:
				mode = seesaw.LBModeDSR
			case pb.VserverEntry_NAT:
				mode = seesaw.LBModeNAT
			default:
				// TODO(angusc): Consider this VServer broken.
				log.Errorf("%v: Unsupported mode %v", vs.GetName(), ve.GetMode())
				continue
			}
			e.Mode = mode

			e.Persistence = int(ve.GetPersistence())
			e.OnePacket = ve.GetOnePacket()
			e.HighWatermark = ve.GetServerHighWatermark()
			e.LowWatermark = ve.GetServerLowWatermark()
			if e.HighWatermark < e.LowWatermark {
				e.HighWatermark = e.LowWatermark
			}
			e.LThreshold = int(ve.GetLthreshold())
			e.UThreshold = int(ve.GetUthreshold())
			for _, hc := range protosToHealthchecks(ve.Healthcheck, e.Port) {
				if err := e.AddHealthcheck(hc); err != nil {
					log.Warning(err)
				}
			}
			if err := v.AddVserverEntry(e); err != nil {
				log.Warning(err)
			}
		}
		for _, backend := range vs.Backend {
			status := backend.GetHost().GetStatus()
			b := &seesaw.Backend{
				Host:      protoToHost(backend.GetHost()),
				Weight:    backend.GetWeight(),
				Enabled:   status == pb.Host_PRODUCTION || status == pb.Host_TESTING,
				InService: status != pb.Host_PROPOSED && status != pb.Host_BUILDING,
			}
			if err := v.AddBackend(b); err != nil {
				log.Warning(err)
			}
		}
		for _, hc := range protosToHealthchecks(vs.Healthcheck, 0) {
			if err := v.AddHealthcheck(hc); err != nil {
				log.Warning(err)
			}
		}
		if err := c.AddVserver(v); err != nil {
			log.Warning(err)
		}
	}
}

func addWarnings(c *Cluster, p *pb.Cluster) {
	for _, mvs := range p.GetMisconfiguredVserver() {
		warning := fmt.Sprintf("%s: %s", mvs.GetName(), mvs.GetErrorMessage())
		c.Status.Warnings = append(c.Status.Warnings, warning)
	}
	sort.Strings(c.Status.Warnings)
}

func parseCIDR(s string) (net.IP, net.IPMask) {
	var mask net.IPMask
	ip, ipNet, _ := net.ParseCIDR(s)
	if ipNet != nil {
		mask = ipNet.Mask
	}
	ip4 := ip.To4()
	if ip4 != nil {
		return ip4, mask
	}
	return ip, mask
}

type byModTime []os.FileInfo

func (x byModTime) Len() int           { return len(x) }
func (x byModTime) Swap(i, j int)      { x[i], x[j] = x[j], x[i] }
func (x byModTime) Less(i, j int) bool { return x[i].ModTime().Before(x[j].ModTime()) }

type archiveConfig struct {
	age   time.Duration
	bytes int64
	count int
}

type pruneStats struct {
	archiveConfig
	filesRemoved int
	filesErrored int
}

// pruneArchive removes files from the given directory to ensure that the specified
// maximums are not exceeded. It returns the number of the files removed along with
// the maximums observed in the current archive.
func pruneArchive(archiveDir string, refTime time.Time, max archiveConfig) (*pruneStats, error) {
	d, err := os.Open(archiveDir)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	files, err := d.Readdir(0)
	if err != nil {
		return nil, err
	}

	sort.Sort(sort.Reverse(byModTime(files)))

	var seen, curr pruneStats
	for i := 0; i < len(files); i++ {
		curr.age = refTime.Sub(files[i].ModTime())
		curr.bytes = seen.bytes + files[i].Size()
		curr.count = seen.count + 1
		if curr.count > max.count || curr.bytes > max.bytes || curr.age > max.age {
			seen.filesRemoved, seen.filesErrored = removeFiles(archiveDir, files[i:])
			return &seen, nil
		}
		seen = curr
	}

	return &seen, nil
}

func removeFiles(dirname string, list []os.FileInfo) (int, int) {
	var removed, failed int
	for _, file := range list {
		filename := filepath.Join(dirname, file.Name())
		if err := os.Remove(filename); err != nil {
			log.Warningf("Error removing %s: %v", filename, err)
			failed++
			continue
		}
		removed++
	}

	return removed, failed
}
