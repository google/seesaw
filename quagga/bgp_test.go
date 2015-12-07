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

package quagga

import (
	"io/ioutil"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const testDataDir = "testdata"

var uptimeTests = []struct {
	uptime   string
	expected time.Duration
}{
	{"", 0},
	{"<error>", 0},
	{"never", 0},
	{"2:45:15", 2*time.Hour + 45*time.Minute + 15*time.Second},
	{"2d23h12m", 2*24*time.Hour + 23*time.Hour + 12*time.Minute},
	{"2w3d14h", 2*168*time.Hour + 3*24*time.Hour + 14*time.Hour},
}

func TestParseUptime(t *testing.T) {
	for _, test := range uptimeTests {
		if uptime := ParseUptime(test.uptime); uptime != test.expected {
			t.Errorf("Incorrect uptime %v - got %v, want %v",
				test.uptime, uptime, test.expected)
		}
	}
}

var stateTests = []struct {
	stateName string
	expected  BGPState
}{
	{"Idle", BGPStateIdle},
	{"Active", BGPStateActive},
	{"Established", BGPStateEstablished},
}

func TestBGPState(t *testing.T) {
	for _, test := range stateTests {
		s := BGPStateByName(test.stateName)
		if s != test.expected {
			t.Errorf("Unexpected state - got %d, want %d",
				s, test.expected)
		}
		if s.String() != test.stateName {
			t.Errorf("Unexpected state name - got %q, want %q",
				s.String(), test.stateName)
		}
	}
}

var testNeighbors = []*Neighbor{
	{
		IP:          net.ParseIP("192.168.0.252"),
		ASN:         64513,
		Description: "au-syd-router1.example.com.",
		RouterID:    net.ParseIP("192.168.1.254"),
		BGPState:    BGPStateEstablished,
		Uptime:      ParseUptime("03w2d10h"),
		MessageStats: MessageStats{
			MessageStat{1, 1},
			MessageStat{0, 0},
			MessageStat{15, 12},
			MessageStat{202300, 197590},
			MessageStat{0, 0},
			MessageStat{0, 0},
			MessageStat{202316, 197603},
		},
	},
	{
		IP:          net.ParseIP("192.168.0.253"),
		ASN:         64513,
		Description: "au-syd-router2.example.com.",
		RouterID:    net.ParseIP("192.168.2.254"),
		BGPState:    BGPStateActive,
		Uptime:      ParseUptime(""),
		MessageStats: MessageStats{
			MessageStat{1, 1},
			MessageStat{0, 0},
			MessageStat{15, 12},
			MessageStat{202301, 197590},
			MessageStat{0, 0},
			MessageStat{0, 0},
			MessageStat{202317, 197603},
		},
	},
}

func TestParseNeighbors(t *testing.T) {
	b, err := ioutil.ReadFile(filepath.Join(testDataDir, "neighbors"))
	if err != nil {
		t.Fatalf("Failed to open %v", err)
	}
	neighbors := parseNeighbors(string(b))
	t.Log(neighbors)
	if len(testNeighbors) != len(neighbors) {
		t.Errorf("Got %d neighbors, want %d",
			len(neighbors), len(testNeighbors))
		return
	}
	for i := range testNeighbors {
		if !reflect.DeepEqual(neighbors[i], testNeighbors[i]) {
			t.Errorf("Neighbor does not match - got %#v, want %#v",
				neighbors[i], testNeighbors[i])
		}
	}
}
