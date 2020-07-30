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

package engine

// This file contains types and functions that are used across multiple
// engine tests.

import (
	ncclient "github.com/google/seesaw/ncc/client"
)

func newTestEngine() *Engine {
	e := newEngineWithNCC(nil, ncclient.NewDummyNCC())
	e.lbInterface = ncclient.NewDummyLBInterface()
	return e
}

func newTestVserver(engine *Engine) *vserver {
	if engine == nil {
		engine = newTestEngine()
	}
	v := newVserver(engine)
	return v
}
