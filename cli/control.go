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

// Author: jsing@google.com (Joel Sing)

package cli

import (
	"fmt"
)

func configReload(cli *SeesawCLI, args []string) error {
	if err := cli.seesaw.ConfigReload(); err != nil {
		return fmt.Errorf("Config reload failed: %v", err)
	}
	fmt.Println("Configuration reload requested.")
	return nil
}

func configSource(cli *SeesawCLI, args []string) error {
	// TODO(jsing): Move this up to the command handling level.
	if len(args) > 1 {
		return fmt.Errorf("Unexpected arguments")
	}
	source := ""
	if len(args) == 1 {
		source = args[0]
	}
	oldSource, err := cli.seesaw.ConfigSource(source)
	if err != nil {
		return fmt.Errorf("Failed to change config source: %v", err)
	}
	if source == "" {
		fmt.Printf("Config source is %s\n", oldSource)
	} else {
		fmt.Printf("Config source is %s (was %s)\n", source, oldSource)
	}
	return nil
}

func failover(cli *SeesawCLI, args []string) error {
	if err := cli.seesaw.Failover(); err != nil {
		return fmt.Errorf("Failover request failed: %v", err)
	}
	fmt.Println("Failover requested.")
	return nil
}
