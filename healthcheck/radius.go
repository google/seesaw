// Copyright 2014 Google Inc. All Rights Reserved.
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

// RADIUS healthcheck implementation.

package healthcheck

import (
	"bytes"
	"crypto/md5"
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"time"

	"github.com/google/seesaw/common/seesaw"
)

const (
	defaultRADIUSTimeout = 3 * time.Second

	radiusHeaderSize  = 20
	radiusMaximumSize = 4096
)

var radiusRand rand.Source

func init() {
	radiusRand = rand.NewSource(int64(os.Getpid()) + time.Now().UnixNano())
}

type radiusIdentifier uint8

func newRADIUSIdentifier() radiusIdentifier {
	return radiusIdentifier(radiusRand.Int63())
}

type radiusCode uint8

const (
	rcAccessRequest      radiusCode = 1
	rcAccessAccept       radiusCode = 2
	rcAccessReject       radiusCode = 3
	rcAccountingRequest  radiusCode = 4
	rcAccountingResponse radiusCode = 5
	rcAccessChallenge    radiusCode = 11
	rcStatusServer       radiusCode = 12
	rcStatusClient       radiusCode = 13
	rcReserved           radiusCode = 255
)

var rcNames = map[radiusCode]string{
	rcAccessRequest:      "Access-Request",
	rcAccessAccept:       "Access-Accept",
	rcAccessReject:       "Access-Reject",
	rcAccountingRequest:  "Accounting-Request",
	rcAccountingResponse: "Accounting-Response",
	rcAccessChallenge:    "Access-Challenge",
	rcStatusServer:       "Status-Server",
	rcStatusClient:       "Status-Client",
	rcReserved:           "Reserved",
}

// String returns the string representation of a RADIUS code.
func (rc radiusCode) String() string {
	if name, ok := rcNames[rc]; ok {
		return name
	}
	return fmt.Sprintf("(unknown %d)", rc)
}

type radiusAuthenticator [16]byte

func newRADIUSAuthenticator() (*radiusAuthenticator, error) {
	var authenticator radiusAuthenticator
	n, err := cryptorand.Read(authenticator[:])
	if err != nil {
		return nil, err
	}
	if n != len(authenticator) {
		return nil, fmt.Errorf("short random read: %d", n)
	}
	return &authenticator, nil
}

type radiusHeader struct {
	Code          radiusCode
	Identifier    radiusIdentifier
	Length        uint16
	Authenticator radiusAuthenticator
}

func (rh *radiusHeader) decode(b *bytes.Reader) error {
	return binary.Read(b, binary.BigEndian, rh)
}

func (rh *radiusHeader) encode() []byte {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, rh); err != nil {
		return nil
	}
	return buf.Bytes()
}

type radiusAttributeType uint8

const (
	ratUserName      radiusAttributeType = 1
	ratUserPassword  radiusAttributeType = 2
	ratNASIPAddress  radiusAttributeType = 4
	ratNASPort       radiusAttributeType = 5
	ratServiceType   radiusAttributeType = 6
	ratNASIdentifier radiusAttributeType = 32
	ratNASPortType   radiusAttributeType = 61
)

var ratNames = map[radiusAttributeType]string{
	ratUserName:      "User-Name",
	ratUserPassword:  "User-Password",
	ratNASIPAddress:  "NAS-IP-Address",
	ratNASPort:       "NAS-Port",
	ratServiceType:   "Service-Type",
	ratNASIdentifier: "NAS-Identifier",
	ratNASPortType:   "NAS-Port-Type",
}

// String returns the string representation of a RADIUS attribute type.
func (rat radiusAttributeType) String() string {
	if name, ok := ratNames[rat]; ok {
		return name
	}
	return fmt.Sprintf("(unknown %d)", rat)
}

type radiusAttribute struct {
	raType radiusAttributeType
	length uint8
	value  []byte
}

func (ra *radiusAttribute) decode(b *bytes.Reader) error {
	raType, err := b.ReadByte()
	if err != nil {
		return err
	}
	ra.raType = radiusAttributeType(raType)
	length, err := b.ReadByte()
	if err != nil {
		return err
	}
	if length < 2 {
		return fmt.Errorf("invalid attribute length: %d", length)
	}
	ra.length = length
	if length == 2 {
		return nil
	}
	ra.value = make([]byte, length-2)
	n, err := b.Read(ra.value)
	if err != nil {
		return err
	}
	if n != int(length)-2 {
		return fmt.Errorf("attribute value short read: %d", n)
	}
	return nil
}

func (ra *radiusAttribute) encode() []byte {
	ra.length = uint8(2 + len(ra.value))
	buf := new(bytes.Buffer)
	buf.WriteByte(byte(ra.raType))
	buf.WriteByte(byte(ra.length))
	buf.Write(ra.value)
	return buf.Bytes()
}

type radiusPacket struct {
	radiusHeader
	attributes []*radiusAttribute
}

func (rp *radiusPacket) addAttribute(ra *radiusAttribute) {
	rp.attributes = append(rp.attributes, ra)
}

func (rp *radiusPacket) decode(b *bytes.Reader) error {
	if err := rp.radiusHeader.decode(b); err != nil {
		return err
	}
	if rp.Length < radiusHeaderSize || rp.Length > radiusMaximumSize {
		return fmt.Errorf("invalid length %d", rp.Length)
	}
	len := rp.Length - radiusHeaderSize
	rp.attributes = make([]*radiusAttribute, 0)
	for {
		if len < 2 {
			break
		}
		attribute := &radiusAttribute{}
		if err := attribute.decode(b); err != nil {
			return err
		}
		rp.attributes = append(rp.attributes, attribute)
		len -= uint16(attribute.length)
	}
	return nil
}

func (rp *radiusPacket) encode() []byte {
	attributes := make([]byte, 0, radiusMaximumSize)
	rp.Length = radiusHeaderSize
	for _, attribute := range rp.attributes {
		attributes = append(attributes, attribute.encode()...)
		rp.Length += uint16(attribute.length)
	}
	buf := new(bytes.Buffer)
	buf.Write(rp.radiusHeader.encode())
	buf.Write(attributes)
	return buf.Bytes()
}

// radiusPassword encodes a password using the algorithm described in RFC2865
// section 5.2.
func radiusPassword(passwd, secret string, authenticator *radiusAuthenticator) []byte {
	const blockSize = 16
	length := (len(passwd) + 0xf) &^ 0xf
	if length > 128 {
		length = 128
	}
	blocks := make([]byte, length)
	copy(blocks, []byte(passwd))
	for i := 0; i < length; i += blockSize {
		hash := md5.New()
		hash.Write([]byte(secret))
		if i >= blockSize {
			hash.Write(blocks[i-blockSize : i])
		} else {
			hash.Write(authenticator[:])
		}
		h := hash.Sum(nil)
		for j := 0; j < blockSize; j++ {
			blocks[i+j] ^= h[j]
		}
	}
	return blocks
}

// responseAuthenticator calculates the response authenticator for a RADIUS
// response packet, as per RFC2865 section 3.
func responseAuthenticator(rp *radiusPacket, requestAuthenticator *radiusAuthenticator, secret string) (*radiusAuthenticator, error) {
	hash := md5.New()
	hash.Write([]byte{byte(rp.Code), byte(rp.Identifier)})
	if err := binary.Write(hash, binary.BigEndian, rp.Length); err != nil {
		return nil, err
	}
	hash.Write(requestAuthenticator[:])
	for _, ra := range rp.attributes {
		hash.Write(ra.encode())
	}
	hash.Write([]byte(secret))
	h := hash.Sum(nil)
	var authenticator radiusAuthenticator
	copy(authenticator[:], h[:])
	return &authenticator, nil
}

// RADIUSChecker contains configuration specific to a RADIUS healthcheck.
type RADIUSChecker struct {
	Target
	Username string
	Password string
	Secret   string
	Response string
}

// NewRADIUSChecker returns an initialised RADIUSChecker.
func NewRADIUSChecker(ip net.IP, port int) *RADIUSChecker {
	return &RADIUSChecker{
		Target: Target{
			IP:    ip,
			Port:  port,
			Proto: seesaw.IPProtoUDP,
		},
		Response: "accept",
	}
}

// String returns the string representation of a RADIUS healthcheck.
func (hc *RADIUSChecker) String() string {
	return fmt.Sprintf("RADIUS %s", hc.Target)
}

// Check executes a RADIUS healthcheck.
func (hc *RADIUSChecker) Check(timeout time.Duration) *Result {
	msg := fmt.Sprintf("RADIUS %s to port %d", rcAccessRequest, hc.Port)
	start := time.Now()
	if timeout == time.Duration(0) {
		timeout = defaultRADIUSTimeout
	}
	deadline := start.Add(timeout)

	conn, err := dialUDP(hc.network(), hc.addr(), timeout, hc.Mark)
	if err != nil {
		return complete(start, msg, false, err)
	}
	defer conn.Close()

	// Build a RADIUS Access-Request packet.
	authenticator, err := newRADIUSAuthenticator()
	if err != nil {
		return complete(start, msg, false, err)
	}
	identifier := newRADIUSIdentifier()
	rp := &radiusPacket{
		radiusHeader: radiusHeader{
			Code:       rcAccessRequest,
			Identifier: identifier,
		},
	}
	copy(rp.Authenticator[:], authenticator[:])

	// NAS Identifier.
	hostname, err := os.Hostname()
	if err != nil {
		return complete(start, msg, false, err)
	}
	ra := &radiusAttribute{raType: ratNASIdentifier}
	ra.value = []byte(hostname)
	rp.addAttribute(ra)

	// Username.
	ra = &radiusAttribute{raType: ratUserName}
	ra.value = []byte(hc.Username)
	rp.addAttribute(ra)

	// User Password.
	ra = &radiusAttribute{raType: ratUserPassword}
	ra.value = radiusPassword(hc.Password, hc.Secret, authenticator)
	rp.addAttribute(ra)

	// NAS IP Address.
	nasIP := net.ParseIP(conn.LocalAddr().String())
	if ip := nasIP.To4(); ip != nil {
		ra := &radiusAttribute{raType: ratNASIPAddress}
		ra.value = ip
		rp.addAttribute(ra)
	}

	// NAS Port Type (virtual).
	ra = &radiusAttribute{raType: ratNASPortType}
	ra.value = []byte{0x0, 0x0, 0x0, 0x5}
	rp.addAttribute(ra)

	// Service Type (login).
	ra = &radiusAttribute{raType: ratServiceType}
	ra.value = []byte{0x0, 0x0, 0x0, 0x1}
	rp.addAttribute(ra)

	// Send a RADIUS request.
	rpb := rp.encode()

	err = conn.SetDeadline(deadline)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to set deadline", msg)
		return complete(start, msg, false, err)
	}

	if _, err := conn.Write(rpb); err != nil {
		msg = fmt.Sprintf("%s; failed to send request", msg)
		return complete(start, msg, false, err)
	}

	// Read and decode the RADIUS response.
	reply := make([]byte, radiusMaximumSize)
	n, _, err := conn.ReadFrom(reply)
	if err != nil {
		msg = fmt.Sprintf("%s; failed to read response", msg)
		return complete(start, msg, false, err)
	}

	reader := bytes.NewReader(reply[0:n])
	if err := rp.decode(reader); err != nil {
		msg = fmt.Sprintf("%s; failed to decode response", msg)
		return complete(start, msg, false, err)
	}

	// The Access-Request should result in an Access-Accept,
	// Access-Reject or Access-Challenge response.

	if rp.Identifier != identifier {
		msg = fmt.Sprintf("%s; identifier mismatch", msg)
		return complete(start, msg, false, err)
	}

	// TODO(jsing): Consider adding a flag to disable the response
	// authenticator check. This would allow healthchecks to be performed
	// without needing a valid secret (although the host still needs to be
	// configured as a RADIUS client).
	respAuth, err := responseAuthenticator(rp, authenticator, hc.Secret)
	if err != nil {
		return complete(start, msg, false, err)
	}
	if !bytes.Equal(rp.Authenticator[:], respAuth[:]) {
		msg = fmt.Sprintf("%s; response authenticator mismatch (incorrect secret?)", msg)
		return complete(start, msg, false, err)
	}

	msg = fmt.Sprintf("%s; got RADIUS %s response", msg, rp.Code)

	switch {
	case hc.Response == "any":
		return complete(start, msg, true, err)
	case hc.Response == "accept" && rp.Code == rcAccessAccept:
		return complete(start, msg, true, err)
	case hc.Response == "challenge" && rp.Code == rcAccessChallenge:
		return complete(start, msg, true, err)
	case hc.Response == "reject" && rp.Code == rcAccessReject:
		return complete(start, msg, true, err)
	}

	switch rp.Code {
	case rcAccessAccept, rcAccessChallenge, rcAccessReject:
		msg = fmt.Sprintf("%s; want %s response", msg, hc.Response)
	default:
		msg = fmt.Sprintf("%s; unknown RADIUS response %d", msg, rp.Code)
	}
	return complete(start, msg, false, err)
}
