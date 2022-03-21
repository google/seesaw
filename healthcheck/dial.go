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
	"net"
	"os"
	"syscall"
	"time"
)

type conn struct {
	net.Conn
	mark int
}

func (c *conn) Close() error {
	if c.Conn != nil {
		return c.Conn.Close()
	}
	return nil
}

func (c *conn) control(network, address string, rawc syscall.RawConn) error {
	var fdErr error
	ctl := func(fd uintptr) {
		if c.mark != 0 {
			fdErr = setSocketMark(int(fd), c.mark)
		}
	}
	if err := rawc.Control(ctl); err != nil {
		return err
	}
	return fdErr
}

// dialTCP dials a TCP connection to the specified host and sets marking on the
// socket. The host must be given as an IP address. A mark of zero results in a
// normal (non-marked) connection.
func dialTCP(network, addr string, timeout time.Duration, mark int) (nc net.Conn, err error) {
	c := &conn{
		mark: mark,
	}
	dial := net.Dialer{
		Timeout: timeout,
		Control: c.control,
	}
	c.Conn, err = dial.Dial(network, addr)
	return c, err
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
