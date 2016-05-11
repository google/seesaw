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

package ncc

// This file contains ARP related functions for the Seesaw Network Control
// component.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	ncctypes "github.com/google/seesaw/ncc/types"

	log "github.com/golang/glog"
)

const (
	opARPRequest = 1
	opARPReply   = 2
	hwLen        = 6
)

var (
	ethernetBroadcast = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
)

func htons(p uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], p)
	return *(*uint16)(unsafe.Pointer(&b))
}

// arpHeader specifies the header for an ARP message.
type arpHeader struct {
	hardwareType          uint16
	protocolType          uint16
	hardwareAddressLength uint8
	protocolAddressLength uint8
	opcode                uint16
}

// arpMessage represents an ARP message.
type arpMessage struct {
	arpHeader
	senderHardwareAddress []byte
	senderProtocolAddress []byte
	targetHardwareAddress []byte
	targetProtocolAddress []byte
}

// bytes returns the wire representation of the ARP message.
func (m *arpMessage) bytes() ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.BigEndian, m.arpHeader); err != nil {
		return nil, fmt.Errorf("binary write failed: %v", err)
	}
	buf.Write(m.senderHardwareAddress)
	buf.Write(m.senderProtocolAddress)
	buf.Write(m.targetHardwareAddress)
	buf.Write(m.targetProtocolAddress)

	return buf.Bytes(), nil
}

// gratuitousARPReply returns an ARP message that contains a gratuitous ARP
// reply from the specified sender.
func gratuitousARPReply(ip net.IP, mac net.HardwareAddr) (*arpMessage, error) {
	if ip.To4() == nil {
		return nil, fmt.Errorf("%q is not an IPv4 address", ip)
	}
	if len(mac) != hwLen {
		return nil, fmt.Errorf("%q is not an Ethernet MAC address", mac)
	}

	m := &arpMessage{
		arpHeader{
			1,           // Ethernet
			0x0800,      // IPv4
			hwLen,       // 48-bit MAC Address
			net.IPv4len, // 32-bit IPv4 Address
			opARPReply,  // ARP Reply
		},
		mac,
		ip.To4(),
		ethernetBroadcast,
		net.IPv4bcast,
	}

	return m, nil
}

// sendARP sends the given ARP message via the specified interface.
func sendARP(iface *net.Interface, m *arpMessage) error {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(syscall.ETH_P_ARP)))
	if err != nil {
		return fmt.Errorf("failed to get raw socket: %v", err)
	}
	defer syscall.Close(fd)

	if err := syscall.BindToDevice(fd, iface.Name); err != nil {
		return fmt.Errorf("failed to bind to device: %v", err)
	}

	ll := syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_ARP),
		Ifindex:  iface.Index,
		Pkttype:  0, // syscall.PACKET_HOST
		Hatype:   m.hardwareType,
		Halen:    m.hardwareAddressLength,
	}
	target := ethernetBroadcast
	if m.opcode == opARPReply {
		target = m.targetHardwareAddress
	}
	for i := 0; i < len(target); i++ {
		ll.Addr[i] = target[i]
	}

	b, err := m.bytes()
	if err != nil {
		return fmt.Errorf("failed to convert ARP message: %v", err)
	}

	if err := syscall.Bind(fd, &ll); err != nil {
		return fmt.Errorf("failed to bind: %v", err)
	}
	if err := syscall.Sendto(fd, b, 0, &ll); err != nil {
		return fmt.Errorf("failed to send: %v", err)
	}

	return nil
}

// ARPSendGratuitous sends a gratuitous ARP message via the specified interface.
func (ncc *SeesawNCC) ARPSendGratuitous(arp *ncctypes.ARPGratuitous, out *int) error {
	iface, err := net.InterfaceByName(arp.IfaceName)
	if err != nil {
		return fmt.Errorf("failed to get interface %q: %v", arp.IfaceName, err)
	}
	log.V(2).Infof("Sending gratuitous ARP for %s (%s) via %s", arp.IP, iface.HardwareAddr, iface.Name)
	m, err := gratuitousARPReply(arp.IP, iface.HardwareAddr)
	if err != nil {
		return err
	}
	return sendARP(iface, m)
}
