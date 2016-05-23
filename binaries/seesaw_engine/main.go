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

// The seesaw_engine binary implements the Seesaw Engine component, which is
// responsible for maintaining configuration, handling state transitions and
// communicating with other Seesaw v2 components.
package main

import (
	"flag"
	"fmt"
	"net"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/engine/config"
	"github.com/google/seesaw/engine"

	conf "github.com/dlintw/goconf"
	log "github.com/golang/glog"
)

var (
	configFile = flag.String("conf", config.DefaultEngineConfig().ConfigFile,
		"Seesaw configuration file")
	clusterFile = flag.String("cluster", config.DefaultEngineConfig().ClusterFile,
		"Seesaw cluster configuration file")
	nccSocket = flag.String("ncc_socket", config.DefaultEngineConfig().NCCSocket,
		"Seesaw NCC socket")
	socketPath = flag.String("socket", config.DefaultEngineConfig().SocketPath,
		"Seesaw Engine socket")
)

// cfgOpt returns the configuration option from the specified section. If the
// option does not exist an empty string is returned.
func cfgOpt(cfg *conf.ConfigFile, section, option string) string {
	if !cfg.HasOption(section, option) {
		return ""
	}
	s, err := cfg.GetString(section, option)
	if err != nil {
		log.Exitf("Failed to get %s for %s: %v", option, section, err)
	}
	return s
}

// cfgIP returns configuration option from the specified section, as an IP
// address. If the option does not exist or is blank, a nil IP is returned.
func cfgIP(cfg *conf.ConfigFile, section, option string) (net.IP, error) {
	ipStr := cfgOpt(cfg, section, option)
	if ipStr == "" {
		return nil, nil
	}
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, fmt.Errorf("%s: %q is not a valid IP address", option, ipStr)
	}
	return ip, nil
}

func main() {
	flag.Parse()

	cfg, err := conf.ReadConfigFile(*configFile)
	if err != nil {
		log.Exitf("Failed to read configuration file: %v", err)
	}
	clusterName := cfgOpt(cfg, "cluster", "name")
	if clusterName == "" {
		log.Exit("Unable to get cluster name")
	}

	anycastEnabled := config.DefaultEngineConfig().AnycastEnabled
	if opt := cfgOpt(cfg, "cluster", "anycast_enabled"); opt != "" {
		if anycastEnabled, err = cfg.GetBool("cluster", "anycast_enabled"); err != nil {
			log.Exitf("Unable to parse cluster anycast_enabled: %v", err)
		}
	}
	clusterVIPv4, err := cfgIP(cfg, "cluster", "vip_ipv4")
	if err != nil {
		log.Exitf("Unable to get cluster vip_ipv4: %v", err)
	}
	clusterVIPv6, err := cfgIP(cfg, "cluster", "vip_ipv6")
	if err != nil {
		log.Exitf("Unable to get cluster vip_ipv6: %v", err)
	}
	nodeIPv4, err := cfgIP(cfg, "cluster", "node_ipv4")
	if err != nil {
		log.Exitf("Unable to get cluster node_ipv4: %v", err)
	}
	nodeIPv6, err := cfgIP(cfg, "cluster", "node_ipv6")
	if err != nil {
		log.Exitf("Unable to get cluster node_ipv6: %v", err)
	}
	peerIPv4, err := cfgIP(cfg, "cluster", "peer_ipv4")
	if err != nil {
		log.Exitf("Unable to get cluster peer_ipv4: %v", err)
	}
	peerIPv6, err := cfgIP(cfg, "cluster", "peer_ipv6")
	if err != nil {
		log.Exitf("Unable to get cluster peer_ipv6: %v", err)
	}

	// The default VRID may be overridden via the config file.
	vrid := config.DefaultEngineConfig().VRID
	if cfg.HasOption("cluster", "vrid") {
		id, err := cfg.GetInt("cluster", "vrid")
		if err != nil {
			log.Exitf("Unable to get VRID: %v", err)
		}
		if id < 1 || id > 255 {
			log.Exitf("Invalid VRID %d - must be between 1 and 255 inclusive", id)
		}
		vrid = uint8(id)
	}

	// Optional primary, secondary and tertiary configuration servers.
	configServers := make([]string, 0)
	for _, level := range []string{"primary", "secondary", "tertiary"} {
		if server := cfgOpt(cfg, "config_server", level); server != "" {
			configServers = append(configServers, server)
		}
	}
	if len(configServers) == 0 {
		configServers = config.DefaultEngineConfig().ConfigServers
	}

	nodeInterface := config.DefaultEngineConfig().NodeInterface
	if opt := cfgOpt(cfg, "interface", "node"); opt != "" {
		nodeInterface = opt
	}
	lbInterface := config.DefaultEngineConfig().LBInterface
	if opt := cfgOpt(cfg, "interface", "lb"); opt != "" {
		lbInterface = opt
	}

	// Additional anycast addresses.
	serviceAnycastIPv4 := config.DefaultEngineConfig().ServiceAnycastIPv4
	serviceAnycastIPv6 := config.DefaultEngineConfig().ServiceAnycastIPv6
	if cfg.HasSection("extra_service_anycast") {
		opts, err := cfg.GetOptions("extra_service_anycast")
		if err != nil {
			log.Exitf("Unable to get extra_serivce_anycast options: %v", err)
		}
		for _, opt := range opts {
			ip, err := cfgIP(cfg, "extra_service_anycast", opt)
			if err != nil {
				log.Exitf("Unable to get extra_service_anycast option %q: %v", opt, err)
			}
			if !seesaw.IsAnycast(ip) {
				log.Exitf("%q is not an anycast address", ip)
			}
			if ip.To4() != nil {
				serviceAnycastIPv4 = append(serviceAnycastIPv4, ip)
			} else {
				serviceAnycastIPv6 = append(serviceAnycastIPv6, ip)
			}
		}
	}

	// Override some of the defaults.
	engineCfg := config.DefaultEngineConfig()
	engineCfg.AnycastEnabled = anycastEnabled
	engineCfg.ConfigFile = *configFile
	engineCfg.ConfigServers = configServers
	engineCfg.ClusterFile = *clusterFile
	engineCfg.ClusterName = clusterName
	engineCfg.ClusterVIP.IPv4Addr = clusterVIPv4
	engineCfg.ClusterVIP.IPv6Addr = clusterVIPv6
	engineCfg.LBInterface = lbInterface
	engineCfg.NCCSocket = *nccSocket
	engineCfg.Node.IPv4Addr = nodeIPv4
	engineCfg.Node.IPv6Addr = nodeIPv6
	engineCfg.NodeInterface = nodeInterface
	engineCfg.Peer.IPv4Addr = peerIPv4
	engineCfg.Peer.IPv6Addr = peerIPv6
	engineCfg.ServiceAnycastIPv4 = serviceAnycastIPv4
	engineCfg.ServiceAnycastIPv6 = serviceAnycastIPv6
	engineCfg.SocketPath = *socketPath
	engineCfg.VRID = vrid

	// Gentlemen, start your engines...
	engine := engine.NewEngine(&engineCfg)
	server.ShutdownHandler(engine)
	server.ServerRunDirectory("engine", 0, 0)
	// TODO(jsing): Drop privileges before starting engine.
	engine.Run()
}
