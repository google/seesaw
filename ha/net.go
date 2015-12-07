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

/*
This file contains the networking layer of the ha package. It sends and receives VRRP v3
advertisements per RFC 5798.

TODO(angusc): Include a list of IPvX Addrs in the VRRP advertisements.
*/

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"

	log "github.com/golang/glog"
)

type ipv4PseudoHeader struct {
	Src      [net.IPv4len]byte
	Dst      [net.IPv4len]byte
	Zero     uint8
	Protocol uint8
	VRRPLen  uint16
}

type ipv6PseudoHeader struct {
	Src        [net.IPv6len]byte
	Dst        [net.IPv6len]byte
	VRRPLen    uint32
	Zeros      [3]byte
	NextHeader uint8
}

// IPConn creates a net.IPConn using the given local and remote addresses using IP protocol
// 112 (VRRP).
func IPConn(localAddr, remoteAddr net.IP) (*net.IPConn, error) {
	c, err := net.ListenIP("ip:112", &net.IPAddr{IP: localAddr})
	if err != nil {
		return nil, err
	}

	f, err := c.File()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ip4 := localAddr.To4()
	switch {
	case ip4 != nil && !remoteAddr.IsMulticast():
		// IPv4 unicast
		// TTL = 255 per VRRP spec
		if err := setsockopt(f, syscall.IPPROTO_IP, syscall.IP_TTL, 255); err != nil {
			return nil, err
		}

	case ip4 != nil && remoteAddr.IsMulticast():
		// IPv4 multicast
		// TTL = 255 per VRRP spec
		if err := setsockopt(f, syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 255); err != nil {
			return nil, err
		}
		// We don't want to receive our own messages.
		if err := setsockopt(f, syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 0); err != nil {
			return nil, err
		}

	case ip4 == nil && !remoteAddr.IsMulticast():
		// IPv6 unicast
		// HOPLIMIT = 255 per VRRP spec
		if err := setsockopt(f, syscall.IPPROTO_IPV6, syscall.IPV6_UNICAST_HOPS, 255); err != nil {
			return nil, err
		}

	case ip4 == nil && remoteAddr.IsMulticast():
		// IPv6 multicast
		// HOPLIMIT = 255 per VRRP spec
		if err := setsockopt(f, syscall.IPPROTO_IPV6, syscall.IPV6_MULTICAST_HOPS, 255); err != nil {
			return nil, err
		}
		// We don't want to receive our own messages.
		if err := setsockopt(f, syscall.IPPROTO_IPV6, syscall.IPV6_MULTICAST_LOOP, 0); err != nil {
			return nil, err
		}

	}

	if ip4 == nil {
		// IPv6 unicast and multicast
		// Request that the ancillary data for received packets include the hop limit and the
		// destination address.

		// TODO(angusc): syscall.IPV6_RECVHOPLIMIT and syscall.IPV6_RECVPKTINFO are prefered but they
		// don't work on lucid.
		if err := setsockopt(f, syscall.IPPROTO_IPV6, syscall.IPV6_2292HOPLIMIT, 1); err != nil {
			return nil, err
		}
		if err := setsockopt(f, syscall.IPPROTO_IPV6, syscall.IPV6_2292PKTINFO, 1); err != nil {
			return nil, err
		}
	}

	log.Infof("ha.IPConn: laddr=%v, raddr=%v", localAddr, remoteAddr)
	return c, nil
}

func setsockopt(f *os.File, level, opt, value int) error {
	if err := syscall.SetsockoptInt(int(f.Fd()), level, opt, value); err != nil {
		return fmt.Errorf("ha.setsockopt(level=%v, opt=%v, value=%v): %v", level, opt, value, err)
	}
	return nil
}

// IPHAConn implements the HAConn interface.
type IPHAConn struct {
	sendConn *net.IPConn
	recvConn *net.IPConn
	laddr    net.IP
	raddr    net.IP
}

// NewIPHAConn creates a new IPHAConn.
func NewIPHAConn(laddr, raddr net.IP) (HAConn, error) {
	sendConn, err := IPConn(laddr, raddr)
	if err != nil {
		return nil, err
	}

	// For IPv6 unicast and multicast, and for IPv4 unicast, we can use the same IPConn for both
	// sending and receiving. For IPv4 multicast, we need a separate listener for receiving.
	recvConn := sendConn
	if raddr.IsMulticast() {
		if raddr.To4() != nil {
			log.Infof("Using IPv4 multicast")
			if recvConn, err = ListenMulticastIPv4(raddr, laddr); err != nil {
				return nil, err
			}
		} else {
			log.Infof("Using IPv6 multicast")
			if err = JoinMulticastIPv6(recvConn, raddr, laddr); err != nil {
				return nil, err
			}
		}
	}

	return &IPHAConn{
		sendConn: sendConn,
		recvConn: recvConn,
		laddr:    laddr,
		raddr:    raddr,
	}, nil
}

// ListenMulticastIPv4 creates a net.IPConn to receive multicast messages for the given group
// address. laddr specifies which network interface to use when joining the group.
func ListenMulticastIPv4(gaddr, laddr net.IP) (*net.IPConn, error) {
	gaddr = gaddr.To4()
	laddr = laddr.To4()
	c, err := net.ListenIP("ip4:112", &net.IPAddr{IP: gaddr})
	if err != nil {
		return nil, err
	}

	f, err := c.File()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	mreq := &syscall.IPMreq{
		Multiaddr: [4]byte{gaddr[0], gaddr[1], gaddr[2], gaddr[3]},
		Interface: [4]byte{laddr[0], laddr[1], laddr[2], laddr[3]},
	}

	err = syscall.SetsockoptIPMreq(int(f.Fd()), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq)
	if err != nil {
		return nil, err
	}

	return c, nil
}

// JoinMulticastIPv6 joins the multicast group address given by gaddr. laddr specifies which
// network interface to use when joining the group.
func JoinMulticastIPv6(c *net.IPConn, gaddr, laddr net.IP) error {
	f, err := c.File()
	if err != nil {
		return err
	}
	defer f.Close()

	mreq := &syscall.IPv6Mreq{}
	copy(mreq.Multiaddr[:], gaddr.To16())
	iface, err := findInterface(laddr)
	if err != nil {
		return err
	}
	mreq.Interface = uint32(iface.Index)

	err = syscall.SetsockoptIPv6Mreq(int(f.Fd()), syscall.IPPROTO_IPV6, syscall.IPV6_JOIN_GROUP, mreq)
	if err != nil {
		return err
	}
	return nil
}

func findInterface(laddr net.IP) (*net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			return nil, err
		}
		for _, addr := range addrs {
			ip, _, _ := net.ParseCIDR(addr.String())
			if laddr.Equal(ip) {
				return &iface, nil
			}
		}
	}
	return nil, fmt.Errorf("ha.findInterface(%q): No interface found", laddr)
}

// af returns the address family for an IPHAConn.
func (c *IPHAConn) af() int {
	if c.laddr.To4() != nil {
		return syscall.AF_INET
	}
	return syscall.AF_INET6
}

// receive reads an IP packet from the IP layer and translates it into an advertisement.
// receive blocks until either an advertisement is received or an error occurs.  If the
// error is a recoverable/ignorable error, receive will return (nil, nil).
func (c *IPHAConn) receive() (*advertisement, error) {
	p, err := c.readPacket()
	if err != nil {
		switch err := err.(type) {
		case *net.OpError:
			// Ignore "protocol not available" ICMP messages. These occur in response to advertisements
			// sent from this node when it is master and the ha process on the peer is down.
			if errno, ok := err.Err.(syscall.Errno); ok {
				if errno == syscall.ENOPROTOOPT || errno == syscall.EPROTO {
					log.Infof("IPHAConn.receive: Ignoring ENOPROTOOPT/EPROTO")
					return nil, nil
				}
			}
		}
		return nil, err
	} else if len(p.payload) != vrrpAdvertSize {
		// Ignore
		return nil, nil
	}

	advert := &advertisement{}
	reader := bytes.NewReader(p.payload)
	if err := binary.Read(reader, binary.BigEndian, advert); err != nil {
		return nil, err
	}

	// Drop packets from ourselves.
	if p.src.Equal(c.laddr) {
		log.Warningf("IPHAConn.receive: Received packet from localhost (%v)", p.src)
		return nil, nil
	}

	// Drop packets that don't have a TTL/HOPLIMIT.
	if p.ttl != 255 {
		log.Warningf("IPHAConn.receive: Invalid TTL/HOPLIMIT %d from %v", p.ttl, p.src)
		return nil, nil
	}

	// Validate the VRRP checksum.
	chksum, err := checksum(advert, p.src, p.dst)
	if err != nil {
		log.Errorf("IPHAConn.receive: Failed to compute checksum from %v", p.src)
		return nil, nil
	}

	if chksum != 0 {
		log.Warningf("IPHAConn.receive: Invalid VRRP checksum (%x) from %v", advert.Checksum, p.src)
		return nil, nil
	}

	return advert, nil
}

// packet encapsulates information about a received IP packet.
type packet struct {
	src     net.IP
	dst     net.IP
	ttl     uint8
	payload []byte
}

var (
	// Up to 60 bytes for the IPv4 header + 8 bytes for the VRRP payload, rounded
	// to the next power of 2.
	recvBuffer = make([]byte, 96)

	// Per RFC 3542 10240 bytes should "always be large enough".
	oobBuffer = make([]byte, 10240)
)

// readPacket reads a packet from this IPHAConn's recvConn.
func (c *IPHAConn) readPacket() (*packet, error) {
	switch c.af() {
	case syscall.AF_INET:
		return c.readIPv4Packet()
	case syscall.AF_INET6:
		return c.readIPv6Packet()
	}
	panic("unreachable")
}

// readIPv4Packet reads an IPv4 packet. For IPv4, Read() includes the IP
// header, so the TTL, source and destination addresses directly are read
// directly from the header.
func (c *IPHAConn) readIPv4Packet() (*packet, error) {
	b := recvBuffer
	n, err := c.recvConn.Read(b)
	if err != nil {
		return nil, err
	}
	if n < 20 {
		return nil, fmt.Errorf("IPHAConn.readIPv4Packet: Packet len %d is too small", n)
	} else if int(b[0])>>4 != 4 {
		return nil, fmt.Errorf("IPHAConn.readIPv4Packet: Expected an IPv4 packet")
	}
	hdrLen := (int(b[0]) & 0x0f) << 2
	if hdrLen > n {
		return nil, fmt.Errorf("IPHAConn.readIPv4Packet: Header len %d > total len %d", hdrLen, n)
	}
	return &packet{
		src:     net.IP{b[12], b[13], b[14], b[15]},
		dst:     net.IP{b[16], b[17], b[18], b[19]},
		ttl:     b[8],
		payload: b[hdrLen:n],
	}, nil
}

// readIPv6Packet reads an IPv6 packet. For IPv6, the Read* functions do not
// include the IP header, so the HOPLIMIT and destination address are read from
// the control message data (see RFCs 3542 and 2292). The source address is
// taken from the ReadMsgIP return values.
func (c *IPHAConn) readIPv6Packet() (*packet, error) {
	b := recvBuffer
	oob := oobBuffer
	n, oobn, _, raddr, err := c.recvConn.ReadMsgIP(b, oob)
	if err != nil {
		return nil, err
	}
	scm, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return nil, err
	}
	var dst net.IP
	var ttl uint8
	haveTTL := false
	for _, sc := range scm {
		if sc.Header.Level != syscall.IPPROTO_IPV6 {
			continue
		}
		switch sc.Header.Type {
		case syscall.IPV6_2292HOPLIMIT:
			if len(sc.Data) == 0 {
				return nil, fmt.Errorf("IPHAConn.readIPv6Packet: Invalid HOPLIMIT")
			}
			ttl = sc.Data[0]
			haveTTL = true
		case syscall.IPV6_2292PKTINFO:
			if len(sc.Data) < 16 {
				return nil, fmt.Errorf("IPHAConn.readIPv6Packet: Invalid destination address")
			}
			dst = net.IP(sc.Data[:16])
		}
	}

	if !haveTTL {
		return nil, fmt.Errorf("IPHAConn.readIPv6Packet: HOPLIMIT not found")
	}
	if dst == nil {
		return nil, fmt.Errorf("IPHAConn.readIPv6Packet: Destination address not found")
	}
	return &packet{
		src:     raddr.IP,
		dst:     dst,
		ttl:     ttl,
		payload: b[:n],
	}, nil
}

// send translates an advertisement into a []byte and passes it to the IP layer for delivery.
func (c *IPHAConn) send(advert *advertisement, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if err := c.sendConn.SetWriteDeadline(deadline); err != nil {
		return err
	}

	if advert.Checksum == 0 {
		chksum, err := checksum(advert, c.laddr, c.raddr)
		if err != nil {
			return fmt.Errorf("IPHAConn.send: checksum failed: %v", err)
		}
		advert.Checksum = chksum
	}

	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, advert); err != nil {
		return err
	}

	if _, err := c.sendConn.WriteToIP(buf.Bytes(), &net.IPAddr{IP: c.raddr}); err != nil {
		return err
	}

	return nil
}

func checksum(advert *advertisement, srcIP, dstIP net.IP) (uint16, error) {
	buf := new(bytes.Buffer)
	if src, dst := srcIP.To4(), dstIP.To4(); src != nil && dst != nil {
		// IPv4
		hdr := &ipv4PseudoHeader{
			Protocol: 112,
			VRRPLen:  vrrpAdvertSize,
		}
		copy(hdr.Src[:], src)
		copy(hdr.Dst[:], dst)

		if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
			return 0, err
		}
	} else if src, dst := srcIP.To16(), dstIP.To16(); src != nil && dst != nil {
		// IPv6
		hdr := &ipv6PseudoHeader{
			VRRPLen:    vrrpAdvertSize,
			NextHeader: 112,
		}
		copy(hdr.Src[:], src)
		copy(hdr.Dst[:], dst)

		if err := binary.Write(buf, binary.BigEndian, hdr); err != nil {
			return 0, err
		}
	} else {
		return 0, fmt.Errorf("ha.checksum(%q, %q): Need two IPv4 or IPv6 addresses", srcIP, dstIP)
	}

	if err := binary.Write(buf, binary.BigEndian, advert); err != nil {
		return 0, err
	}
	return ipChecksum(buf.Bytes()), nil
}

// ipChecksum calculates the IP checksum of a []byte per RFC1071
func ipChecksum(b []byte) uint16 {
	var sum uint32
	for ; len(b) >= 2; b = b[2:] {
		sum += uint32(b[0])<<8 | uint32(b[1])
	}
	if len(b) == 1 {
		sum += uint32(b[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return uint16(^sum)
}
