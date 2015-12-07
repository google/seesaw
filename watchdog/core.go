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

package watchdog

import (
	"fmt"
	"time"

	log "github.com/golang/glog"
)

var restartBackoff = 5 * time.Second
var restartBackoffMax = 60 * time.Second
var restartDelay = 2 * time.Second

// Watchdog contains the data needed to run a watchdog.
type Watchdog struct {
	services map[string]*Service
	shutdown chan bool
}

// NewWatchdog returns an initialised watchdog.
func NewWatchdog() *Watchdog {
	return &Watchdog{
		services: make(map[string]*Service),
		shutdown: make(chan bool),
	}
}

// Shutdown requests the watchdog to shutdown.
func (w *Watchdog) Shutdown() {
	select {
	case w.shutdown <- true:
	default:
	}
}

// AddService adds a service that is to be run by the watchdog.
func (w *Watchdog) AddService(name, binary string) (*Service, error) {
	if _, ok := w.services[name]; ok {
		return nil, fmt.Errorf("Service %q already exists", name)
	}

	svc := newService(name, binary)
	w.services[name] = svc

	return svc, nil
}

// Walk takes the watchdog component for a walk so that it can run the
// configured services.
func (w *Watchdog) Walk() {
	log.Info("Seesaw watchdog starting...")

	w.mapDependencies()

	for _, svc := range w.services {
		go svc.run()
	}
	<-w.shutdown
	for _, svc := range w.services {
		go svc.stop()
	}
	for _, svc := range w.services {
		stopped := <-svc.stopped
		svc.stopped <- stopped
	}
}

// mapDependencies maps service dependency names to configured services.
func (w *Watchdog) mapDependencies() {
	for name := range w.services {
		svc := w.services[name]
		for depName := range svc.dependencies {
			dep, ok := w.services[depName]
			if !ok {
				log.Fatalf("Failed to find dependency %q for service %q", depName, name)
			}
			svc.dependencies[depName] = dep
			dep.dependents[svc.name] = svc
		}
	}
}
