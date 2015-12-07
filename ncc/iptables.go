// Copyright 2013 Google Inc. All Rights Reserved.
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

package ncc

// This file contains the iptables related functions for the Seesaw Network
// Control component. At a later date this probably should be rewritten to use
// netfilter netlink sockets directly.

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"text/template"

	"github.com/google/seesaw/common/seesaw"

	log "github.com/golang/glog"
)

var (
	vipRulesBegin []*iptRuleTemplate // Rules for each VIP address, added before service rules.
	svcRules      []*iptRuleTemplate // Rules for each VserverEntry.
	fwmRules      []*iptRuleTemplate // FWM rules for each VserverEntry (FWM services only).
	natRules      []*iptRuleTemplate // NAT rules for each VserverEntry (NAT services only).
	vipRulesEnd   []*iptRuleTemplate // Rules for each VIP address, added after service rules.

	iptMutex sync.Mutex // For serializing iptables calls.
)

type iptAction string

const (
	ipt4Cmd = "/sbin/iptables"
	ipt6Cmd = "/sbin/ip6tables"

	iptAppend iptAction = "-A"
	iptCheck            = "-C"
	iptDelete           = "-D"
	iptInsert           = "-I"

	iptMaxTries      = 5
	iptResourceError = 4
)

// iptRule represents an iptables rule.
type iptRule struct {
	seesaw.AF
	action iptAction
	rule   string
}

// iptRuleTemplate is a template for creating an iptRules.
type iptRuleTemplate struct {
	action iptAction
	*template.Template
}

// newIPTRuleTemplate creates a new iptRuleTemplate.
func newIPTRuleTemplate(action iptAction, tmpl string) *iptRuleTemplate {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse iptables template: %q: %v", tmpl, err))
	}
	return &iptRuleTemplate{action, t}
}

// iptTemplateData specifies the values to use to create an iptRule from an
// iptRuleTemplate.
type iptTemplateData struct {
	ClusterVIP net.IP
	ServiceVIP net.IP
	FWM        uint32
	Proto      seesaw.IPProto
	Port       uint16
}

// iptTemplateExecuter creates a list of iptRules from a list of
// iptRuleTemplates and an iptTemplateData.
type iptTemplateExecuter struct {
	templates []*iptRuleTemplate
	data      *iptTemplateData
}

// execute generates the list of iptRules for this iptTemplateExecuter.
func (te *iptTemplateExecuter) execute() ([]*iptRule, error) {
	rules := make([]*iptRule, 0, len(te.templates))
	for _, t := range te.templates {
		w := new(bytes.Buffer)
		if err := t.Template.Execute(w, te.data); err != nil {
			return nil, err
		}
		af := seesaw.IPv6
		if te.data.ServiceVIP.To4() != nil {
			af = seesaw.IPv4
		}
		rule := &iptRule{
			af,
			t.action,
			w.String(),
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func newIPTTemplateExecuter(t []*iptRuleTemplate, data *iptTemplateData) *iptTemplateExecuter {
	return &iptTemplateExecuter{
		templates: t,
		data:      data,
	}
}

// initIPTRuleTemplates initialises the iptRuleTemplates.
func initIPTRuleTemplates() {
	// Allow traffic for VIP services.
	svcRules = append(svcRules, newIPTRuleTemplate(iptAppend,
		"INPUT -p {{.Proto}} -d {{.ServiceVIP}}{{with .Port}} --dport {{.}}{{end}} -j ACCEPT"))

	// Mark packets for firemark based VIPs.
	fwmRule := "PREROUTING -t mangle -p {{.Proto}} -d {{.ServiceVIP}}" +
		"{{with .Port}} --dport {{.}}{{end}} -j MARK --set-mark {{.FWM}}"
	fwmRules = append(fwmRules, newIPTRuleTemplate(iptAppend, fwmRule))

	// Enable conntrack for NAT connections.
	natConntrack := "PREROUTING -t raw -p {{.Proto}} -d {{.ServiceVIP}}" +
		"{{with .Port}} --dport {{.}}{{end}} -j ACCEPT"
	natRules = append(natRules, newIPTRuleTemplate(iptInsert, natConntrack))

	// Rewrite source address for NAT'd packets so backend reply traffic comes
	// back to the load balancer.
	natPostrouting := "POSTROUTING -t nat -m ipvs -p {{.Proto}}{{with .Port}} --vport {{.}}{{end}} " +
		"--vaddr {{.ServiceVIP}} -j SNAT --to-source {{.ClusterVIP}} --random"
	natRules = append(natRules, newIPTRuleTemplate(iptAppend, natPostrouting))

	// Allow ICMP traffic to this VIP.
	vipRulesEnd = append(vipRulesEnd, newIPTRuleTemplate(iptAppend,
		"INPUT -p icmp -d {{.ServiceVIP}} -j ACCEPT"))
	vipRulesEnd = append(vipRulesEnd, newIPTRuleTemplate(iptAppend,
		"INPUT -p ipv6-icmp -d {{.ServiceVIP}} -j ACCEPT"))

	// Block all other traffic to this VIP.
	vipRulesEnd = append(vipRulesEnd, newIPTRuleTemplate(iptAppend,
		"INPUT -d {{.ServiceVIP}} -j REJECT"))
}

// iptablesRunOutput runs the Linux ip(6?)tables command with the specified
// arguments and returns the output.
func iptablesRunOutput(af seesaw.AF, argsStr string) (string, error) {
	var cmd string
	switch af {
	case seesaw.IPv4:
		cmd = ipt4Cmd
	case seesaw.IPv6:
		cmd = ipt6Cmd
	default:
		return "", fmt.Errorf("iptablesRunOutput: Unsupported address family: %v", af)
	}

	var out []byte
	var err error
	log.Infof("%s %s", cmd, argsStr)
	args := strings.Split(argsStr, " ")
	failed := false
	for tries := 1; !failed && tries <= iptMaxTries; tries++ {
		proc := exec.Command(cmd, args...)
		iptMutex.Lock()
		out, err = proc.Output()
		iptMutex.Unlock()
		if err == nil {
			break
		}
		// If someone else is running iptables, our command will fail with a
		// resource error, so retry. Also retry if we can't determine the exit
		// status at all.
		switch status := proc.ProcessState.Sys().(type) {
		case syscall.WaitStatus:
			if status.ExitStatus() != iptResourceError {
				failed = true
			}
		}
		log.Infof("'%s %s' try %d of %d failed: %v (%v)", cmd, argsStr, tries, iptMaxTries, err, out)
	}
	if err != nil {
		msg := fmt.Sprintf("iptablesRunOutput: '%s %s': %v", cmd, argsStr, err)
		log.Infof(msg)
		return "", fmt.Errorf(msg)
	}
	return string(out), nil
}

// iptablesRun runs the Linux ip(6?)tables command with the specified arguments.
func iptablesRun(af seesaw.AF, argsStr string) error {
	_, err := iptablesRunOutput(af, argsStr)
	return err
}

// iptablesInit initialises the iptables rules for a Seesaw Node.
func iptablesInit(clusterVIP seesaw.Host) error {
	if err := iptablesFlush(); err != nil {
		return err
	}
	for _, af := range seesaw.AFs() {
		// Allow connections to port 10257 for all VIPs.
		if err := iptablesRun(af, "-I INPUT -p tcp --dport 10257 -j ACCEPT"); err != nil {
			return err
		}

		// conntrack is only required for NAT. Disable it for everything else.
		if err := iptablesRun(af, "-t raw -A PREROUTING -j NOTRACK"); err != nil {
			return err
		}
		if err := iptablesRun(af, "-t raw -A OUTPUT -j NOTRACK"); err != nil {
			return err
		}
	}

	// Enable conntrack for incoming traffic on the seesaw cluster IPv4 VIP. This
	// is required for NAT return traffic.
	return iptablesRun(seesaw.IPv4,
		fmt.Sprintf("-t raw -I PREROUTING -d %v/32 -j ACCEPT", clusterVIP.IPv4Addr))
}

// iptablesFlush removes all IPv4 and IPv6 iptables rules.
func iptablesFlush() error {
	for _, af := range seesaw.AFs() {
		for _, table := range []string{"filter", "mangle", "raw"} {
			if err := iptablesRun(af, fmt.Sprintf("--flush -t %v", table)); err != nil {
				return err
			}
		}
	}
	// NAT is IPv4-only until linux kernel version 3.7.
	// TODO(angusc): Support IPv6 NAT.
	if err := iptablesRun(seesaw.IPv4, "--flush -t nat"); err != nil {
		return err
	}
	return nil
}

// iptablesRules returns the list of iptRules for a Vserver.
func iptablesRules(v *seesaw.Vserver, clusterVIP seesaw.Host, af seesaw.AF) ([]*iptRule, error) {
	var clusterIP, serviceIP net.IP
	switch af {
	case seesaw.IPv4:
		clusterIP = clusterVIP.IPv4Addr
		serviceIP = v.IPv4Addr
	case seesaw.IPv6:
		clusterIP = clusterVIP.IPv6Addr
		serviceIP = v.IPv6Addr
	}
	if clusterIP == nil {
		return nil, fmt.Errorf("Seesaw Cluster VIP does not have an %s address", af)
	}
	if serviceIP == nil {
		return nil, fmt.Errorf("Service VIP does not have an %s address", af)
	}

	executers := make([]*iptTemplateExecuter, 0)
	vipData := &iptTemplateData{
		ClusterVIP: clusterIP,
		ServiceVIP: serviceIP,
	}
	executers = append(executers, &iptTemplateExecuter{vipRulesBegin, vipData})

	for _, ve := range v.Entries {
		svcData := &iptTemplateData{
			ClusterVIP: clusterIP,
			ServiceVIP: serviceIP,
			FWM:        v.FWM[af],
			Port:       ve.Port,
			Proto:      ve.Proto,
		}
		executers = append(executers, newIPTTemplateExecuter(svcRules, svcData))

		if v.FWM[af] > 0 {
			executers = append(executers, newIPTTemplateExecuter(fwmRules, svcData))
		}

		if ve.Mode == seesaw.LBModeNAT {
			executers = append(executers, newIPTTemplateExecuter(natRules, svcData))
		}
	}

	executers = append(executers, &iptTemplateExecuter{vipRulesEnd, vipData})

	allRules := make([]*iptRule, 0)
	for _, ex := range executers {
		rules, err := ex.execute()
		if err != nil {
			return nil, err
		}
		allRules = append(allRules, rules...)
	}
	return allRules, nil
}

// iptablesAddRules installs the iptables rules for a given Vserver and Seesaw
// Cluster VIP.
func iptablesAddRules(v *seesaw.Vserver, clusterVIP seesaw.Host, af seesaw.AF) error {
	rules, err := iptablesRules(v, clusterVIP, af)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if err := iptablesRun(r.AF, string(r.action)+" "+r.rule); err != nil {
			return err
		}
	}
	return nil
}

// iptablesDeleteRules deletes the iptables rules for a given Vserver and
// Seesaw Cluster VIP.
func iptablesDeleteRules(v *seesaw.Vserver, clusterVIP seesaw.Host, af seesaw.AF) error {
	rules, err := iptablesRules(v, clusterVIP, af)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if err := iptablesRun(r.AF, string(iptDelete)+" "+r.rule); err != nil {
			return err
		}
	}
	return nil
}
