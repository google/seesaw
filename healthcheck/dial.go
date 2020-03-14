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

package healthcheck

// This file contains helper routines for dialing connections.

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"
)

type conn struct {
	fd int
	f  *os.File
	net.Conn
}

func (c *conn) Close() error {
	if c.Conn != nil {
		c.Conn.Close()
	}
	if c.f != nil {
		err := c.f.Close()
		c.fd, c.f = -1, nil
		return err
	}
	if c.fd != -1 {
		err := syscall.Close(c.fd)
		c.fd = -1
		return err
	}
	return nil
}

func sockaddrToString(sa syscall.Sockaddr) string {
	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		return net.JoinHostPort(net.IP(sa.Addr[:]).String(), strconv.Itoa(sa.Port))
	case *syscall.SockaddrInet6:
		return net.JoinHostPort(net.IP(sa.Addr[:]).String(), strconv.Itoa(sa.Port))
	default:
		return fmt.Sprintf("(unknown - %T)", sa)
	}
}

// dialTCP dials a TCP connection to the specified host and sets marking on the
// socket. The host must be given as an IP address. A mark of zero results in a
// normal (non-marked) connection.
func dialTCP(network, addr string, timeout time.Duration, mark int) (nc net.Conn, err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address %q", host)
	}
	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid port number %q", port)
	}

	var domain int
	var rsa syscall.Sockaddr
	switch network {
	case "tcp4":
		domain = syscall.AF_INET
		if ip.To4() == nil {
			return nil, fmt.Errorf("invalid IPv4 address %q", host)
		}
		sa := &syscall.SockaddrInet4{Port: int(p)}
		copy(sa.Addr[:], ip.To4())
		rsa = sa

	case "tcp6":
		domain = syscall.AF_INET6
		if ip.To4() != nil {
			return nil, fmt.Errorf("invalid IPv6 address %q", host)
		}
		sa := &syscall.SockaddrInet6{Port: int(p)}
		copy(sa.Addr[:], ip.To16())
		rsa = sa

	default:
		return nil, fmt.Errorf("unsupported network %q", network)
	}

	c := &conn{}

	defer func() {
		if err != nil {
			c.Close()
		}
	}()

	c.fd, err = syscall.Socket(domain, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, os.NewSyscallError("socket", err)
	}

	if mark != 0 {
		if err := setSocketMark(c.fd, mark); err != nil {
			return nil, err
		}
	}

	if err := setSocketTimeout(c.fd, timeout); err != nil {
		return nil, err
	}
	for {
		err := syscall.Connect(c.fd, rsa)
		if err == nil {
			break
		}
		// Blocking socket connect may be interrupted with EINTR
		if err != syscall.EINTR {
			return nil, os.NewSyscallError("connect", err)
		}
	}
	if err := setSocketTimeout(c.fd, 0); err != nil {
		return nil, err
	}

	lsa, _ := syscall.Getsockname(c.fd)
	rsa, _ = syscall.Getpeername(c.fd)
	name := fmt.Sprintf("%s %s -> %s", network, sockaddrToString(lsa), sockaddrToString(rsa))
	c.f = os.NewFile(uintptr(c.fd), name)

	// When we call net.FileConn the socket will be made non-blocking and
	// we will get a *net.TCPConn in return. The *os.File needs to be
	// closed in addition to the *net.TCPConn when we're done (conn.Close
	// takes care of that for us).
	if c.Conn, err = net.FileConn(c.f); err != nil {
		return nil, err
	}
	if _, ok := c.Conn.(*net.TCPConn); !ok {
		return nil, fmt.Errorf("%T is not a *net.TCPConn", c.Conn)
	}

	return c, nil
}

// dialUDP dials a UDP connection to the specified host and sets marking on the
// socket. A mark of zero results in a normal (non-marked) connection.
func dialUDP(network, addr string, timeout time.Duration, mark int) (*net.UDPConn, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial(network, addr)
	if err != nil {
		return nil, err
	}

	udpConn, ok := conn.(*net.UDPConn)
	if !ok {
		conn.Close()
		return nil, errors.New("dial did not return a *net.UDPConn")
	}
	if mark == 0 {
		return udpConn, nil
	}

	f, err := udpConn.File()
	if err != nil {
		udpConn.Close()
		return nil, err
	}
	defer f.Close()

	fd := int(f.Fd())

	// Calling File results in the socket being set to blocking mode.
	// We need to undo this.
	if err := syscall.SetNonblock(fd, true); err != nil {
		udpConn.Close()
		return nil, err
	}

	if err := setSocketMark(fd, mark); err != nil {
		udpConn.Close()
		return nil, err
	}

	return udpConn, nil
}

// setSocketMark sets packet marking on the given socket.
func setSocketMark(fd, mark int) error {
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_MARK, mark); err != nil {
		return os.NewSyscallError("failed to set mark", err)
	}
	return nil
}

// setSocketTimeout sets the receive and send timeouts on the given socket.
func setSocketTimeout(fd int, timeout time.Duration) error {
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	for _, opt := range []int{syscall.SO_RCVTIMEO, syscall.SO_SNDTIMEO} {
		if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, opt, &tv); err != nil {
			return os.NewSyscallError("setsockopt", err)
		}
	}
	return nil
}
