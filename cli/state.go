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

package cli

import (
	"errors"
	"fmt"

	"github.com/google/seesaw/common/seesaw"
)

func overrideVserverStateDefault(cli *SeesawCLI, args []string) error {
	return overrideVserver(cli, args, seesaw.OverrideDefault)
}

func overrideVserverStateDisabled(cli *SeesawCLI, args []string) error {
	return overrideVserver(cli, args, seesaw.OverrideDisable)
}

func overrideVserverStateEnabled(cli *SeesawCLI, args []string) error {
	return overrideVserver(cli, args, seesaw.OverrideEnable)
}

func overrideVserver(cli *SeesawCLI, args []string, state seesaw.OverrideState) error {
	if len(args) != 1 {
		fmt.Println("override vserver state <default|disabled|enabled> <vserver>")
		return errors.New("Incorrect arguments given.")
	}
	vservers, err := cli.seesaw.Vservers()
	if err != nil {
		return fmt.Errorf("Failed to retrieve list of vservers: %v", err)
	}
	if _, ok := vservers[args[0]]; !ok {
		return fmt.Errorf("No such vserver - %s", args[0])
	}
	if err := cli.seesaw.OverrideVserver(&seesaw.VserverOverride{args[0], state}); err != nil {
		return fmt.Errorf("Override vserver state failed - %s", err)
	}
	return nil
}
