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

package main

import (
	"flag"
	"path"
	"strconv"
	"time"

	"github.com/google/seesaw/common/seesaw"
	"github.com/google/seesaw/common/server"
	"github.com/google/seesaw/watchdog"

	conf "github.com/dlintw/goconf"
	log "github.com/golang/glog"
)

var (
	configFile = flag.String("config",
		path.Join(seesaw.ConfigPath, "watchdog.cfg"),
		"Watchdog configuration file")
)

// cfgOpt returns the configuration option from the specified section. If the
// option does not exist an empty string is returned.
func cfgOpt(cfg *conf.ConfigFile, section, option string) string {
	if !cfg.HasOption(section, option) {
		return ""
	}
	s, err := cfg.GetString(section, option)
	if err != nil {
		log.Fatalf("Failed to get %s for %s: %v", option, section, err)
	}
	return s
}

// svcOpt returns the specified configuration option for a service.
func svcOpt(cfg *conf.ConfigFile, service, option string, required bool) string {
	// TODO(jsing): Add support for defaults.
	opt := cfgOpt(cfg, service, option)
	if opt == "" && required {
		log.Fatalf("Service %s has missing %s option", service, option)
	}
	return opt
}

func main() {
	flag.Parse()

	cfg, err := conf.ReadConfigFile(*configFile)
	if err != nil {
		log.Fatalf("Failed to read config file %q: %v", *configFile, err)
	}

	fido := watchdog.NewWatchdog()
	server.ShutdownHandler(fido)

	for _, name := range cfg.GetSections() {
		if name == "default" {
			continue
		}

		binary := svcOpt(cfg, name, "binary", true)
		args := svcOpt(cfg, name, "args", false)

		svc, err := fido.AddService(name, binary)
		if err != nil {
			log.Fatalf("Failed to add service %q: %v", name, err)
		}
		svc.AddArgs(args)
		if dep := svcOpt(cfg, name, "dependency", false); dep != "" {
			svc.AddDependency(dep)
		}
		if opt := svcOpt(cfg, name, "priority", false); opt != "" {
			prio, err := strconv.Atoi(opt)
			if err != nil {
				log.Fatalf("Service %s has invalid priority %q: %v", name, opt, err)
			}
			if err := svc.SetPriority(prio); err != nil {
				log.Fatalf("Failed to set priority for service %s: %v", name, err)
			}
		}
		if opt := svcOpt(cfg, name, "term_timeout", false); opt != "" {
			tt, err := time.ParseDuration(opt)
			if err != nil {
				log.Fatalf("Service %s has invalid term_timeout %q: %v", name, opt, err)
			}
			svc.SetTermTimeout(tt)
		}
		// TODO(angusc): Add support for a "group" option.
		if user := svcOpt(cfg, name, "user", false); user != "" {
			if err := svc.SetUser(user); err != nil {
				log.Fatalf("Failed to set user for service %s: %v", name, err)
			}
		}
	}

	fido.Walk()
}
