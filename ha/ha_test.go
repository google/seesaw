// Copyright 2012 Google Inc.  All Rights Reserved.
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

package ha

// This file contains the unit tests for the ha package.

import (
	"testing"
	"time"

	"github.com/google/seesaw/common/seesaw"
	spb "github.com/google/seesaw/pb/seesaw"
)

type dummyHAConn struct{}

var vrrpTestAdvert = advertisement{
	VersionType: vrrpVersionType,
	Priority:    1,
	VRID:        1,
}

func (h *dummyHAConn) receive() (*advertisement, error) {
	return nil, nil
}

func (h *dummyHAConn) send(advert *advertisement, timeout time.Duration) error {
	return nil
}

func newTestNode() *Node {
	haConn := &dummyHAConn{}
	engine := &DummyEngine{}
	nc := NodeConfig{
		HAConfig: seesaw.HAConfig{
			Enabled:  true,
			Priority: 100,
			VRID:     1,
		},
		MasterAdvertInterval: 60 * time.Second,
	}
	n := NewNode(nc, haConn, engine, "/dev/null")
	// force runOnce() to timeout immediately
	n.masterDownInterval = 1
	return n
}

func TestNoPeer(t *testing.T) {
	node := newTestNode()
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()
}

func TestHighPriorityPeer(t *testing.T) {
	node := newTestNode()
	// no incoming advertisements - we should become master
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// incoming advertisement from higher priority peer
	advert := vrrpTestAdvert
	advert.Priority = 255
	node.queueAdvertisement(&advert)
	node.runOnce()
	if node.state() != spb.HaState_BACKUP {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_BACKUP, node.state())
	}

	// no incoming advertisements (i.e. peer is dead)
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()
}

func TestLowPriorityPeer(t *testing.T) {
	node := newTestNode()
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// incoming advertisement from lower priority peer
	node.queueAdvertisement(&vrrpTestAdvert)
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()
}

func TestWrongVRRPVersion(t *testing.T) {
	node := newTestNode()
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}
	alien := vrrpTestAdvert
	alien.VersionType = (vrrpVersion + 1) << 4
	alien.Priority = 255
	node.queueAdvertisement(&alien)
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()
}

func TestWrongVRID(t *testing.T) {
	node := newTestNode()
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}
	alien := vrrpTestAdvert
	alien.Priority = 255
	alien.VRID = 2
	node.queueAdvertisement(&alien)
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()
}

func TestPreempt(t *testing.T) {
	node := newTestNode()
	node.Preempt = true
	node.queueAdvertisement(&vrrpTestAdvert)
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()

	node = newTestNode()
	node.queueAdvertisement(&vrrpTestAdvert)
	node.runOnce()
	if node.state() != spb.HaState_BACKUP {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_BACKUP, node.state())
	}
}

func TestShutdown(t *testing.T) {
	node := newTestNode()
	advert := vrrpTestAdvert
	advert.Priority = 0
	node.queueAdvertisement(&advert)
	node.runOnce()
	if node.state() != spb.HaState_LEADER {
		t.Errorf("Expected state to be %v but was %v", spb.HaState_LEADER, node.state())
	}

	// clean up
	node.becomeBackup()
}

func TestIPChecksum(t *testing.T) {
	// test data from RFC1071
	b := []byte{0x0, 0x1, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7}
	chksum := ipChecksum(b)
	want := ^uint16(0xddf2)
	if chksum != want {
		t.Errorf("Want checksum %x but was %x", want, chksum)
	}

	// test with odd len(b)
	b = []byte{0x0, 0x1, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7, 0x01}
	chksum = ipChecksum(b)
	want = ^uint16(0xdef2)
	if chksum != want {
		t.Errorf("Want checksum %x but was %x", want, chksum)
	}
}
