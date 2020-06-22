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

package ipc

import (
	"testing"

	"github.com/google/seesaw/common/seesaw"
)

func TestUserRights(t *testing.T) {
	// Restore previous state after test
	defer func(a, o, r string) {
		authGroupAdmin, authGroupOperator, authGroupReader = a, o, r
	}(authGroupAdmin, authGroupOperator, authGroupReader)

	authGroupAdmin, authGroupOperator, authGroupReader = "admins", "operators", "readers"

	tests := []struct {
		user       User
		isAdmin    bool
		isOperator bool
		isReader   bool
		canRead    bool
		canWrite   bool
	}{
		{
			user:       User{},
			isAdmin:    false,
			isOperator: false,
			isReader:   false,
			canRead:    false,
			canWrite:   false,
		},
		{
			user:       User{Groups: []string{}},
			isAdmin:    false,
			isOperator: false,
			isReader:   false,
			canRead:    false,
			canWrite:   false,
		},
		{
			user:       User{Groups: AuthGroups()},
			isAdmin:    true,
			isOperator: true,
			isReader:   true,
			canRead:    true,
			canWrite:   true,
		},
		{
			user:       User{Groups: []string{"readers"}},
			isAdmin:    false,
			isOperator: false,
			isReader:   true,
			canRead:    true,
			canWrite:   false,
		},
		{
			user:       User{Groups: []string{"operators"}},
			isAdmin:    false,
			isOperator: true,
			isReader:   true,
			canRead:    true,
			canWrite:   false,
		},
		{
			user:       User{Groups: []string{"admins"}},
			isAdmin:    true,
			isOperator: true,
			isReader:   true,
			canRead:    true,
			canWrite:   true,
		},
	}
	for _, test := range tests {
		if got, want := test.user.IsAdmin(), test.isAdmin; got != want {
			t.Errorf("(%#v).IsAdmin() = %v, want %v", test.user, got, want)
		}
		if got, want := test.user.IsOperator(), test.isOperator; got != want {
			t.Errorf("(%#v).IsOperator() = %v, want %v", test.user, got, want)
		}
		if got, want := test.user.IsReader(), test.isReader; got != want {
			t.Errorf("(%#v).IsReader() = %v, want %v", test.user, got, want)
		}
		ctx := NewAuthContext(seesaw.SCECU, "token")
		ctx.AuthType = ATSSO
		ctx.User = test.user
		if got, want := ctx.CanRead(), test.canRead; got != want {
			t.Errorf("(%#v).CanRead() = %v, want %v", ctx, got, want)
		}
		if got, want := ctx.CanWrite(), test.canWrite; got != want {
			t.Errorf("(%#v).CanWrite() = %v, want %v", ctx, got, want)
		}
	}
}
