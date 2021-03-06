/* Copyright 2016, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"time"
)

// SendablePacket is an interface for packets that can be sent on a connection which
// allows client code to make collections of packets that can be sent that contain
// both reliable and non-reliable packets.
type SendablePacket interface {
	// Send the packet on `c` Connection, possibly generating a new sequence number,
	// to the remote address specified.
	Send(c *Connection, generateNewSeq bool, remote *net.UDPAddr) error

	// SetRemoteAddress will set the remote address property of the packet.
	SetRemoteAddress(remote *net.UDPAddr)
}

type PacketEvent func(c *Connection, rp *ReliablePacket)

type ReliablePacket struct {
	*Packet
	RetryInterval time.Duration
	OnAck         PacketEvent
	OnFailToAck   PacketEvent
	RetryCount    uint8
	nextCheck     time.Time
	failCount     uint8
}

type Packet struct {
	RemoteAddress *net.UDPAddr
	ClientId      uint32
	Seq           uint32
	Chan          uint8
	AckSeq        uint32
	AckMask       uint32
	PayloadSize   uint32
	Payload       []byte
}

var (
	byteOrder     = binary.BigEndian
	payloadOffset = binary.Size(uint32(1))*5 + binary.Size(uint8(1))
)

const (
	ackMaskDepth = 32
)

func NewPacket(id uint32, seq uint32, ch uint8, ack uint32, m uint32, size uint32, b []byte) *Packet {
	p := new(Packet)
	p.ClientId = id
	p.Seq = seq
	p.Chan = ch
	p.AckSeq = ack
	p.AckMask = m

	p.PayloadSize = size
	p.Payload = make([]byte, size)
	copy(p.Payload, b)

	return p
}

func (p *Packet) WriteTo(b *bytes.Buffer) error {
	b.Reset()

	// client id
	err := binary.Write(b, byteOrder, p.ClientId)
	if err != nil {
		return fmt.Errorf("Error while writing the client id from packet to buffer.\n%v", err)
	}

	// sequence
	err = binary.Write(b, byteOrder, p.Seq)
	if err != nil {
		return fmt.Errorf("Error while writing the sequence from packet to buffer.\n%v", err)
	}

	// channel
	err = binary.Write(b, byteOrder, p.Chan)
	if err != nil {
		return fmt.Errorf("Error while writing the channel from packet to buffer.\n%v", err)
	}

	// ack sequence
	err = binary.Write(b, byteOrder, p.AckSeq)
	if err != nil {
		return fmt.Errorf("Error while writing the ACK sequence from packet to buffer.\n%v", err)
	}

	// ack mask
	err = binary.Write(b, byteOrder, p.AckMask)
	if err != nil {
		return fmt.Errorf("Error while writing the ACK bitmask from packet to buffer.\n%v", err)
	}

	// payload size
	err = binary.Write(b, byteOrder, p.PayloadSize)
	if err != nil {
		return fmt.Errorf("Error while writing the payload size from packet to buffer.\n%v", err)
	}

	// payload
	err = binary.Write(b, byteOrder, p.Payload[:p.PayloadSize])
	if err != nil {
		return fmt.Errorf("Error while writing the payload from packet to buffer.\n%v", err)
	}

	return nil
}

func NewPacketFrom(n int, b []byte) (*Packet, error) {
	// make sure we at least have enough bytes for the packet 'header'
	if n < payloadOffset {
		return nil, fmt.Errorf("Not enough bytes (%d) read to form a packet.", n)
	}

	p := new(Packet)
	buf := bytes.NewBuffer(b)

	// read in the packet 'header' information
	binary.Read(buf, byteOrder, &p.ClientId)
	binary.Read(buf, byteOrder, &p.Seq)
	binary.Read(buf, byteOrder, &p.Chan)
	binary.Read(buf, byteOrder, &p.AckSeq)
	binary.Read(buf, byteOrder, &p.AckMask)
	binary.Read(buf, byteOrder, &p.PayloadSize)

	// resize if necessary
	byteLength := n + 1
	if cap(p.Payload) <= byteLength-payloadOffset {
		//fmt.Printf("\nPACKET: adjusting payload size from %d to %d.\n", cap(p.Payload), byteLength)
		p.Payload = make([]byte, byteLength)
	}

	// copy the payload slice
	copy(p.Payload, b[payloadOffset:])

	return p, nil
}

func (p *Packet) MakeReliable(retryInterval time.Duration, retryCount uint8) *ReliablePacket {
	rp := new(ReliablePacket)
	rp.Packet = p
	rp.RetryInterval = retryInterval
	rp.RetryCount = retryCount
	rp.OnAck = nil
	rp.OnFailToAck = nil
	rp.nextCheck = time.Now().Add(retryInterval)
	rp.failCount = 0
	return rp
}

func (p *Packet) IsAckBy(ackPacket *Packet) bool {
	// if ack packet's seq is below the packets, then it can't possibly ack it
	if ackPacket.AckSeq < p.Seq {
		return false
	}

	// if the packet's seq is not within the bitfield depth of the ack packet,
	// then it can't possibly ack it
	seqDiff := ackPacket.AckSeq - p.Seq
	if seqDiff >= ackMaskDepth {
		return false
	}

	var mask uint32 = (0x0001 << seqDiff)
	if ackPacket.AckMask&mask > 0x00 {
		return true
	} else {
		return false
	}
}

// Send sends a non-reliable packet on the connection specified.
func (p *Packet) Send(c *Connection, generateNewSeq bool, remote *net.UDPAddr) error {
	ra := remote
	if ra == nil {
		ra = p.RemoteAddress
	}
	return c.Send(p, generateNewSeq, ra)
}

// Send sends a reliable packet on the connection specified.
func (rp *ReliablePacket) Send(c *Connection, generateNewSeq bool, remote *net.UDPAddr) error {
	ra := remote
	if ra == nil {
		ra = rp.RemoteAddress
	}
	if ra == nil {
		ra = rp.Packet.RemoteAddress
	}
	return c.SendReliable(rp, generateNewSeq, ra)
}

// SetRemoteAddress will set the remote address property of the packet.
func (p *Packet) SetRemoteAddress(remote *net.UDPAddr) {
	p.RemoteAddress = remote
}

// SetRemoteAddress will set the remote address property of the packet.
func (rp *ReliablePacket) SetRemoteAddress(remote *net.UDPAddr) {
	rp.Packet.RemoteAddress = remote
	rp.RemoteAddress = remote
}
