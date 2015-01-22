/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
	"bytes"
	"fmt"
	"net"
)

type ListenConnection struct {
	Socket        *net.UDPConn
	ListenAddress *net.UDPAddr
	buffer        []byte
	isOpen        bool
	lastSeenSeq   uint32
	lastAckMask   uint32
}

type SendConnection struct {
	Socket        *net.UDPConn
	RemoteAddress *net.UDPAddr
	buffer        bytes.Buffer
	isOpen        bool
}

const (
	defaultBufferSize = 1500
	ackMaskDepth      = 32
)

func CreateListener(listenAddress string, bufferSize uint32) (*ListenConnection, error) {
	address, err := net.ResolveUDPAddr("udp", listenAddress)
	if err != nil {
		return nil, fmt.Errorf("Failed to resolve the address to listen on: %s\n%v", listenAddress, err)
	}

	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		return nil, fmt.Errorf("Failed to listen on the address: %s\n%v", listenAddress, err)
	}

	var newConn ListenConnection
	newConn.Socket = conn
	newConn.ListenAddress = address
	if bufferSize > 0 {
		newConn.buffer = make([]byte, bufferSize)
	} else {
		newConn.buffer = make([]byte, defaultBufferSize)
	}
	newConn.isOpen = true
	newConn.lastSeenSeq = 0
	newConn.lastAckMask = 0
	return &newConn, nil
}

func CreateSender(remoteAddress string) (*SendConnection, error) {
	address, err := net.ResolveUDPAddr("udp", remoteAddress)
	if err != nil {
		return nil, fmt.Errorf("Failed to resolve the address to send to: %s\n%v", remoteAddress, err)
	}

	// 'connect' via udp
	conn, err := net.DialUDP("udp", nil, address)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to the address: %s\n%v", remoteAddress, err)
	}

	var newConn SendConnection
	newConn.Socket = conn
	newConn.RemoteAddress = address
	newConn.isOpen = true
	return &newConn, nil
}

func (lc *ListenConnection) Close() {
	lc.isOpen = false
	lc.Socket.Close()
}

func (lc *ListenConnection) IsOpen() bool {
	return lc.isOpen
}

func (lc *ListenConnection) GetLastSeenSeq() uint32 {
	return lc.lastSeenSeq
}

func (lc *ListenConnection) GetAckMask() uint32 {
	return lc.lastAckMask
}

func (sc *SendConnection) Close() {
	sc.isOpen = false
	sc.Socket.Close()
}

func (sc *SendConnection) IsOpen() bool {
	return sc.isOpen
}

func calcNewAckMask(lastSeen, currentSeq, currentMask uint32) (mask, seq uint32) {
	const maskDepth = 32
	if lastSeen < currentSeq { // New SEQ
		// update the last seen data for new packets
		seqDiff := currentSeq - lastSeen
		if seqDiff < maskDepth && seqDiff > 0 {
			// shift the old acks down appropriately
			mask = currentMask << seqDiff
		} else {
			// nothing is close enough to remember
			mask = 0x0000
		}

		// update the last seen seq and flag itself in the mask.
		seq = currentSeq
		mask = mask | 0x0001
	} else { // Old SEQ
		// see if the older packet needs an ack set
		seqDiff := lastSeen - currentSeq
		if seqDiff < maskDepth {
			mask = currentMask | (0x0001 << seqDiff)
		}

		// else if it's too old, just forget about it ... and keep the old last seen seq
		seq = lastSeen
	}
	return
}

func (lc *ListenConnection) Read() (*Packet, *net.UDPAddr, error) {
	// read the raw data in from the UDP connection
	n, addr, err := lc.Socket.ReadFromUDP(lc.buffer)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to read bytes from UDP: %v\n", err)
	}

	// construct the packet
	p, err := NewPacketFrom(n, lc.buffer)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to read packet from UDP: %v\n", err)
	}

	// calculate new ack masks and last seen seq numbers
	lc.lastAckMask, lc.lastSeenSeq = calcNewAckMask(lc.lastSeenSeq, p.Seq, lc.lastAckMask)

	return p, addr, nil
}

func (sc *SendConnection) Send(p *Packet) error {
	// encode the packet to binary
	p.WriteTo(&sc.buffer)

	_, err := sc.Socket.Write(sc.buffer.Bytes())
	if err != nil {
		return fmt.Errorf("Failed to send bytes on connection.\n%v", err)
	}

	return nil
}
