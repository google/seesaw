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
	"path/filepath"
	"testing"

	"github.com/google/seesaw/engine/config"
)

func TestVserverAccess(t *testing.T) {
	const testVserver = "dns.resolver.anycast@au-syd"
	testUsers := []string{"user1", "user2", "user3", "user4"}

	n, err := config.ReadConfig(filepath.Join(testDataDir, "vserver_access_1.pb"), "tst")
	if err != nil {
		t.Fatalf("Failed to read test configuration: %v", err)
	}
	vua, err := newVserverUserAccess(n.Cluster)
	if err != nil {
		t.Fatalf("Failed to create vserver user access: %v", err)
	}

	va := newVserverAccess()

	// Should fail closed.
	for _, user := range testUsers {
		if access, reason := va.hasAccess(testVserver, user); access {
			t.Errorf("User %q has access to vserver %q (reason %q), but should not since no access grants exist", user, testVserver, reason)
		}
	}

	// Test users that should have access via access grants.
	va.update(vua)
	for _, user := range testUsers {
		if access, _ := va.hasAccess(testVserver, user); !access {
			t.Errorf("User %q should have access to vserver %q, but does not", user, testVserver)
		}
	}

	// And one that should not...
	user := "user5"
	if access, _ := va.hasAccess(testVserver, user); access {
		t.Errorf("User %q has access to vserver %q, but should not", user, testVserver)
	}
}
