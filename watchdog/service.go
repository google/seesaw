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
	"io"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/golang/glog"
)

const logDir = "/var/log/seesaw"

const prioProcess = 0

// Service contains the data needed to manage a service.
type Service struct {
	name   string
	binary string
	path   string
	args   []string

	uid      uint32
	gid      uint32
	priority int

	dependencies map[string]*Service
	dependents   map[string]*Service

	termTimeout time.Duration

	lock    sync.Mutex
	process *os.Process

	done     chan bool
	shutdown chan bool
	started  chan bool
	stopped  chan bool

	failures uint64
	restarts uint64

	lastFailure time.Time
	lastRestart time.Time
}

// newService returns an initialised service.
func newService(name, binary string) *Service {
	return &Service{
		name:         name,
		binary:       binary,
		args:         make([]string, 0),
		dependencies: make(map[string]*Service),
		dependents:   make(map[string]*Service),

		done:     make(chan bool),
		shutdown: make(chan bool, 1),
		started:  make(chan bool, 1),
		stopped:  make(chan bool, 1),

		termTimeout: 5 * time.Second,
	}
}

// AddDependency registers a dependency for this service.
func (svc *Service) AddDependency(name string) {
	svc.dependencies[name] = nil
}

// AddArgs adds the given string as arguments.
func (svc *Service) AddArgs(args string) {
	svc.args = strings.Fields(args)
}

// SetPriority sets the process priority for a service.
func (svc *Service) SetPriority(priority int) error {
	if priority < -20 || priority > 19 {
		return fmt.Errorf("Invalid priority %d - must be between -20 and 19", priority)
	}
	svc.priority = priority
	return nil
}

// SetTermTimeout sets the termination timeout for a service.
func (svc *Service) SetTermTimeout(tt time.Duration) {
	svc.termTimeout = tt
}

// SetUser sets the user for a service.
func (svc *Service) SetUser(username string) error {
	u, err := user.Lookup(username)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	svc.uid = uint32(uid)
	svc.gid = uint32(gid)
	return nil
}

// run runs a service and restarts it upon termination, unless a shutdown
// notification has been received.
func (svc *Service) run() {

	// Wait for dependencies to start.
	for _, dep := range svc.dependencies {
		log.Infof("Service %s waiting for %s to start", svc.name, dep.name)
		select {
		case started := <-dep.started:
			dep.started <- started
		case <-svc.shutdown:
			goto done
		}
	}

	for {
		if svc.failures > 0 {
			delay := time.Duration(svc.failures) * restartBackoff
			if delay > restartBackoffMax {
				delay = restartBackoffMax
			}
			log.Infof("Service %s has failed %d times - delaying %s before restart",
				svc.name, svc.failures, delay)

			select {
			case <-time.After(delay):
			case <-svc.shutdown:
				goto done
			}
		}

		svc.restarts++
		svc.lastRestart = time.Now()
		svc.runOnce()

		select {
		case <-time.After(restartDelay):
		case <-svc.shutdown:
			goto done
		}
	}
done:
	svc.done <- true
}

// logFile creates a log file for this service.
func (svc *Service) logFile() (*os.File, error) {
	name := "seesaw_" + svc.name
	t := time.Now()
	logName := fmt.Sprintf("%s.log.%04d%02d%02d-%02d%02d%02d", name,
		t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second())

	f, err := os.Create(path.Join(logDir, logName))
	if err != nil {
		return nil, err
	}

	logLink := path.Join(logDir, name+".log")
	os.Remove(logLink)
	os.Symlink(logName, logLink)

	fmt.Fprintf(f, "Log file for %s (stdout/stderr)\n", name)
	fmt.Fprintf(f, "Created at: %s\n", t.Format("2006/01/02 15:04:05"))

	return f, nil
}

// logSink copies output from the given reader to a log file, before closing
// both the reader and the log file.
func (svc *Service) logSink(f *os.File, r io.ReadCloser) {
	_, err := io.Copy(f, r)
	if err != nil {
		log.Warningf("Service %s - log sink failed: %v", svc.name, err)
	}
	f.Close()
	r.Close()
}

// runOnce runs a service once, returning once an error occurs or the process
// has exited.
func (svc *Service) runOnce() {
	args := make([]string, len(svc.args)+1)
	args[0] = "seesaw_" + svc.name
	copy(args[1:], svc.args)

	null, err := os.Open(os.DevNull)
	if err != nil {
		log.Warningf("Service %s - failed to open %s: %v", svc.name, os.DevNull, err)
		return
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		log.Warningf("Service %s - failed to create pipes: %v", svc.name, err)
		null.Close()
		return
	}

	f, err := svc.logFile()
	if err != nil {
		log.Warningf("Service %s - failed to create log file: %v", svc.name, err)
		null.Close()
		pr.Close()
		pw.Close()
		return
	}

	go svc.logSink(f, pr)

	attr := &os.ProcAttr{
		Dir:   svc.path,
		Files: []*os.File{null, pw, pw},
		Sys: &syscall.SysProcAttr{
			Credential: &syscall.Credential{
				Uid: svc.uid,
				Gid: svc.gid,
			},
			Setpgid: true,
		},
	}

	log.Infof("Starting service %s...", svc.name)
	proc, err := os.StartProcess(svc.binary, args, attr)
	if err != nil {
		log.Warningf("Service %s failed to start: %v", svc.name, err)
		svc.lastFailure = time.Now()
		svc.failures++
		null.Close()
		pw.Close()
		return
	}
	null.Close()
	pw.Close()
	svc.lock.Lock()
	svc.process = proc
	svc.lock.Unlock()

	if _, _, err := syscall.Syscall(syscall.SYS_SETPRIORITY, uintptr(prioProcess), uintptr(proc.Pid), uintptr(svc.priority)); err != 0 {
		log.Warningf("Failed to set priority to %d for service %s: %v", svc.priority, svc.name, err)
	}

	select {
	case svc.started <- true:
	default:
	}

	state, err := svc.process.Wait()
	if err != nil {
		log.Warningf("Service %s wait failed with %v", svc.name, err)
		svc.lastFailure = time.Now()
		svc.failures++
		return
	}
	if !state.Success() {
		log.Warningf("Service %s exited with %v", svc.name, state)
		svc.lastFailure = time.Now()
		svc.failures++
		return
	}
	// TODO(jsing): Reset failures after process has been running for some
	// given duration, so that failures with large intervals do not result
	// in backoff. However, we also want to count the total number of
	// failures and export it for monitoring purposes.
	svc.failures = 0
	log.Infof("Service %s exited normally.", svc.name)
}

// signal sends a signal to the service.
func (svc *Service) signal(sig os.Signal) error {
	svc.lock.Lock()
	defer svc.lock.Unlock()
	if svc.process == nil {
		return nil
	}
	return svc.process.Signal(sig)
}

// stop stops a running service.
func (svc *Service) stop() {
	// TODO(jsing): Check if it is actually running?
	log.Infof("Stopping service %s...", svc.name)

	// Wait for dependents to shutdown.
	for _, dep := range svc.dependents {
		log.Infof("Service %s waiting for %s to stop", svc.name, dep.name)
		stopped := <-dep.stopped
		dep.stopped <- stopped
	}

	svc.shutdown <- true
	svc.signal(syscall.SIGTERM)
	select {
	case <-svc.done:
	case <-time.After(svc.termTimeout):
		svc.signal(syscall.SIGKILL)
		<-svc.done
	}
	log.Infof("Service %s stopped", svc.name)
	svc.stopped <- true
}
