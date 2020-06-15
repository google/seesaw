// Copyright 2020 Google Inc. All Rights Reserved.
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

package engine

import (
	"sync"

	log "github.com/golang/glog"
	"github.com/google/seesaw/engine/config"
)

// vserverUserAccess specifies vserver access on a per user basis. This is
// specified as vserver names, usernames and the reason that access was granted.
type vserverUserAccess map[string]map[string]string // (vserver -> user -> reason)

// grantAccess grants a user access to a vserver.
func (vua vserverUserAccess) grantAccess(vserver, username, reason string) {
	vs, ok := vua[vserver]
	if !ok {
		vs = make(map[string]string)
		vua[vserver] = vs
	}
	vs[username] = reason
}

// hasAccess determines whether the user has access to the given vserver.
func (vua vserverUserAccess) hasAccess(vserver, username string) (bool, string) {
	if reason, ok := vua[vserver][username]; ok {
		return true, reason
	}
	return false, "no access granted"
}

// newVserverUserAccess builds a vserver user access map based on access grants.
func newVserverUserAccess(cluster *config.Cluster) (vserverUserAccess, error) {
	vua := make(vserverUserAccess)
	for _, vs := range cluster.Vservers {
		for _, ag := range vs.AccessGrants {
			if !ag.IsGroup {
				vua.grantAccess(vs.Name, ag.Grantee, ag.Key())
				continue
			}
			group, ok := cluster.AccessGroups[ag.Grantee]
			if !ok {
				log.Warningf("vserver %v references non-existent access group %q, ignoring", vs.Name, ag.Grantee)
				continue
			}
			for _, username := range group.Members {
				vua.grantAccess(vs.Name, username, ag.Key())
			}
		}
	}
	return vua, nil
}

// vserverAccess handles access control evaluation for vservers.
type vserverAccess struct {
	access vserverUserAccess
	mu     sync.RWMutex
}

// newVserverAccess returns an initialised vserver access control.
func newVserverAccess() *vserverAccess {
	return &vserverAccess{
		access: make(vserverUserAccess),
	}
}

// hasAccess determines whether the user has access to the given vserver.
func (va *vserverAccess) hasAccess(vserver, username string) (bool, string) {
	va.mu.RLock()
	vua := va.access
	va.mu.RUnlock()

	return vua.hasAccess(vserver, username)
}

// update updates the vserver user access map.
func (va *vserverAccess) update(vua vserverUserAccess) {
	va.mu.Lock()
	va.access = vua
	va.mu.Unlock()
}
