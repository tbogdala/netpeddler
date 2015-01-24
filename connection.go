/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
	"bytes"
	"container/list"
	"fmt"
	"net"
	"time"
)

type Listener struct {
	Socket        *net.UDPConn
	ListenAddress *net.UDPAddr
	buffer        []byte
	isOpen        bool
	lastSeenSeq   uint32
	lastAckMask   uint32
}

type Sender struct {
	Socket        *net.UDPConn
	RemoteAddress *net.UDPAddr
	buffer        bytes.Buffer
	isOpen        bool
	acksNeeded    *list.List
	nextSeq       uint32
}

const (
	defaultBufferSize = 1500
)

func CreateListener(listenAddress string, bufferSize uint32) (*Listener, error) {
	address, err := net.ResolveUDPAddr("udp", listenAddress)
	if err != nil {
		return nil, fmt.Errorf("Failed to resolve the address to listen on: %s\n%v", listenAddress, err)
	}

	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		return nil, fmt.Errorf("Failed to listen on the address: %s\n%v", listenAddress, err)
	}

	var newConn Listener
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

func CreateSender(remoteAddress string) (*Sender, error) {
	address, err := net.ResolveUDPAddr("udp", remoteAddress)
	if err != nil {
		return nil, fmt.Errorf("Failed to resolve the address to send to: %s\n%v", remoteAddress, err)
	}

	// 'connect' via udp
	conn, err := net.DialUDP("udp", nil, address)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to the address: %s\n%v", remoteAddress, err)
	}

	var newConn Sender
	newConn.Socket = conn
	newConn.RemoteAddress = address
	newConn.isOpen = true
	newConn.acksNeeded = list.New()
	newConn.nextSeq = 1
	return &newConn, nil
}

func (lc *Listener) Close() {
	lc.isOpen = false
	lc.Socket.Close()
}

func (lc *Listener) IsOpen() bool {
	return lc.isOpen
}

func (lc *Listener) GetLastSeenSeq() uint32 {
	return lc.lastSeenSeq
}

func (lc *Listener) GetAckMask() uint32 {
	return lc.lastAckMask
}

func (sc *Sender) Close() {
	sc.isOpen = false
	sc.Socket.Close()
}

func (sc *Sender) IsOpen() bool {
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

func (lc *Listener) Read() (*Packet, *net.UDPAddr, error) {
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

func (sc *Sender) GetNextSeq() uint32 {
	seq := sc.nextSeq
	sc.nextSeq++
	return seq
}

func (sc *Sender) Send(p *Packet, generateNewSeq bool) error {
	// generate a new seq number for the packet if requested
	if generateNewSeq {
		p.Seq = sc.GetNextSeq()
	}

	// encode the packet to binary
	p.WriteTo(&sc.buffer)

	_, err := sc.Socket.Write(sc.buffer.Bytes())
	if err != nil {
		return fmt.Errorf("Failed to send bytes on connection.\n%v", err)
	}

	return nil
}

func (sc *Sender) SendReliable(p *Packet, generateNewSeq bool, retryInterval time.Duration, retryCount uint8) error {
	// construct the reliable packet structure
	rp := MakeReliable(p, retryInterval, retryCount)

	// try to send the packet
	err := sc.Send(rp.Packet, generateNewSeq)
	if err != nil {
		return err
	}

	// update the next ack check time
	rp.nextCheck = time.Now().Add(rp.RetryInterval)

	// add it to the list of packets to watch for acks
	sc.acksNeeded.PushBack(rp)

	return nil
}

func (sc *Sender) GetAcksNeededLen() int {
	return sc.acksNeeded.Len()
}

func (sc *Sender) Tick() error {
	// check for packets that need to be retried
	err := sc.RetryReliablePackets()
	return err
}

func (sc *Sender) ProccessAcks(p *Packet) {
	e := sc.acksNeeded.Front()
	for e != nil {
		nextElem := e.Next()

		rp := e.Value.(*ReliablePacket)

		// check to see if the incoming packet acks the monitored reliable packet.
		// if it does, remove it from the watch list and call the event
		if rp.Packet.IsAckBy(p) {
			sc.acksNeeded.Remove(e)
			if rp.OnAck != nil {
				rp.OnAck(sc, rp)
			}
		}

		e = nextElem
	}
}

func (sc *Sender) RetryReliablePackets() error {
	// loop through everything and retry if needed
	e := sc.acksNeeded.Front()
	for e != nil {
		nextElem := e.Next()

		rp := e.Value.(*ReliablePacket)
		_, maxed, err := sc.retryIfNeeded(rp)
		if err != nil {
			return err
		}

		// if max tries were reached, remove item from list
		if maxed {
			sc.acksNeeded.Remove(e)
		}

		e = nextElem
	}

	return nil
}

func (sc *Sender) retryIfNeeded(rp *ReliablePacket) (resent bool, maxErrors bool, err error) {
	// is it time for a resend?
	t := time.Now()
	if t.Before(rp.nextCheck) {
		return false, false, nil
	}

	// time for resend, so reset the timer and boost the fail count
	rp.nextCheck = rp.nextCheck.Add(rp.RetryInterval)
	rp.failCount++

	// if we have more retrys left, give it another shot
	if rp.failCount <= rp.RetryCount {
		resent = true
		err = sc.Send(rp.Packet, true)
		return true, false, err
	}

	// if we go here, it was time for a resend but we reached max fails,
	// so call the event for this
	if rp.OnFailToAck != nil {
		rp.OnFailToAck(sc, rp)
	}

	return false, true, nil
}
