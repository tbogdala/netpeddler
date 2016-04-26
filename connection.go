/* Copyright 2016, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
	"bytes"
	"container/list"
	"fmt"
	"net"
	"time"
)

type ConnectionReadEvent func(c *Connection, p *Packet)

// Connection is the main structure for a network connection. It is designed around
// a 1:1 relationship between client and server, but members can be tweaked to
// more readily support 1:* relationship between server and clients.
// NOTE: Per golang's net documentation, UDPConn can be accessed from multiple
// threads safely.
type Connection struct {
	Socket        *net.UDPConn
	ListenAddress *net.UDPAddr
	RemoteAddress *net.UDPAddr

	// UpdateAcksOnRead indicates if Read() should update the lastAckMask and lastSeenSeq
	// fields. When a connection is used to read from many clients this may turn out
	// to not be ideal and therefore can be turned off.
	UpdateAcksOnRead bool

	// OnPacketRead is called from Tick() when a packet is successfully read
	// in from the network connection.
	OnPacketRead ConnectionReadEvent

	buffer       []byte
	packetBuffer bytes.Buffer
	isOpen       bool
	lastSeenSeq  uint32
	lastAckMask  uint32
	acksNeeded   *list.List
	nextSeq      uint32
	ReadTimeout  time.Duration
}

const (
	defaultBufferSize = 1500
)

func New(bufferSize uint32) *Connection {
	var newConn Connection
	newConn.buffer = make([]byte, bufferSize)
	newConn.UpdateAcksOnRead = true
	newConn.isOpen = false
	newConn.lastSeenSeq = 0
	newConn.lastAckMask = 0
	newConn.acksNeeded = list.New()
	newConn.nextSeq = 1
	newConn.OnPacketRead = nil

	// It appears that some platforms are sensitive to the value that's added here.
	// For example, on Linux, 1 ns results in no packets being read, but 1 ms works.
	// On Windows, 1 ns works okay.
	newConn.ReadTimeout = time.Millisecond

	return &newConn
}

func NewConnection(bufferSize uint32, localAddress string, remoteAddress string) (*Connection, error) {
	newConn := New(bufferSize)

	// resolve the local address to use for listening
	localAddressOpt := localAddress
	if localAddressOpt == "" {
		localAddressOpt = "127.0.0.1:0"
	}
	addr, err := net.ResolveUDPAddr("udp", localAddressOpt)
	if err != nil {
		return nil, fmt.Errorf("Failed to resolve the address to listen on: %s\n%v", localAddressOpt, err)
	}
	newConn.ListenAddress = addr

	// if provided, resolve a remote address to use as a default for sending
	if remoteAddress != "" {
		raddr, err := net.ResolveUDPAddr("udp", remoteAddress)
		if err != nil {
			return nil, fmt.Errorf("Failed to resolve the remote address: %s\n%v", remoteAddress, err)
		} else {
			newConn.RemoteAddress = raddr
		}
	}

	// Go's net library still needs a UDPConn connection to access a lot of methods
	// so we setup a listener for each connection.
	conn, err := net.ListenUDP("udp", newConn.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("Failed to listen on the address: %s\n%v", localAddressOpt, err)
	}

	newConn.Socket = conn
	if bufferSize > 0 {
		// Setting the buffer size on the connection is important, but more important
		// it seems in Linux, where you might blast through the buffer quickly.
		conn.SetReadBuffer(int(bufferSize))
		conn.SetWriteBuffer(int(bufferSize))
	}
	newConn.isOpen = true

	return newConn, nil
}

func (c *Connection) Close() {
	c.isOpen = false
	c.Socket.Close()
}

func (c *Connection) ResizeBuffer(bufferSize uint32) {
	c.buffer = make([]byte, bufferSize)
}

func (c *Connection) IsOpen() bool {
	return c.isOpen
}

func (c *Connection) SetIsOpen(o bool) {
	c.isOpen = o
}

func (c *Connection) GetLastSeenSeq() uint32 {
	return c.lastSeenSeq
}

func (c *Connection) GetAckMask() uint32 {
	return c.lastAckMask
}

func (c *Connection) CalcAckMask(currentSeq uint32) (mask, seq uint32) {
	const maskDepth = 32
	if c.lastSeenSeq < currentSeq { // New SEQ
		// update the last seen data for new packets
		seqDiff := currentSeq - c.lastSeenSeq
		if seqDiff < maskDepth && seqDiff > 0 {
			// shift the old acks down appropriately
			c.lastAckMask = c.lastAckMask << seqDiff
		} else {
			// nothing is close enough to remember
			c.lastAckMask = 0x0000
		}

		// update the last seen seq and flag itself in the mask.
		c.lastSeenSeq = currentSeq
		c.lastAckMask = c.lastAckMask | 0x0001
	} else { // Old SEQ
		// see if the older packet needs an ack set
		seqDiff := c.lastSeenSeq - currentSeq
		if seqDiff < maskDepth {
			c.lastAckMask = c.lastAckMask | (0x0001 << seqDiff)
		}

		// else if it's too old, just forget about it ... and keep the old last seen seq
		// c.lastSeenSeq = c.lastSeenSeq
	}
	return
}

// Read attempts to read a UDP packet from the connection in a synchronous way.
// If data was read, it constructs a new packet object, updates the ack masks
// if desired and then returns it.
// NOTE: this function
func (c *Connection) Read() (*Packet, error) {
	// read the raw data in from the UDP connection
	n, addr, err := c.Socket.ReadFromUDP(c.buffer)
	if err != nil {
		return nil, fmt.Errorf("Failed to read bytes from UDP: %v\n", err)
	}

	// construct the packet
	p, err := NewPacketFrom(n, c.buffer)
	if err != nil {
		return nil, fmt.Errorf("Failed to read packet from UDP: %v\n", err)
	}

	// fill in the address the packet was received from
	p.RemoteAddress = addr

	if c.UpdateAcksOnRead {
		// calculate new ack masks and last seen seq numbers
		//c.lastAckMask, c.lastSeenSeq = c.CalcAckMask(c.lastSeenSeq, p.Seq, c.lastAckMask)
		c.CalcAckMask(p.Seq)
	}

	// if the OnPacketRead event is defined, fire that
	if c.OnPacketRead != nil {
		c.OnPacketRead(c, p)
	}

	// update any packets that are awaiting their ACK
	c.ProccessAcks(p)

	return p, nil
}

func (c *Connection) GetNextSeq() uint32 {
	seq := c.nextSeq
	c.nextSeq++
	return seq
}

func (c *Connection) Send(p *Packet, generateNewSeq bool, remote *net.UDPAddr) error {
	// generate a new seq number for the packet if requested
	if generateNewSeq {
		p.Seq = c.GetNextSeq()
	}

	// update the ack data
	p.AckSeq = c.GetLastSeenSeq()
	p.AckMask = c.GetAckMask()

	// encode the packet to binary
	p.WriteTo(&c.packetBuffer)

	// use the remote address passed in to the function, but if one was not
	// supplied, try to use the remote address setup in the connection.
	sendAddr := remote
	if sendAddr == nil {
		sendAddr = c.RemoteAddress
		if sendAddr == nil {
			return fmt.Errorf("No remote address specified to send to.")
		}
	}

	_, err := c.Socket.WriteToUDP(c.packetBuffer.Bytes(), sendAddr)
	if err != nil {
		return fmt.Errorf("Failed to send bytes on connection.\n%v", err)
	}

	return nil
}

func (c *Connection) SendReliable(rp *ReliablePacket, generateNewSeq bool, remote *net.UDPAddr) error {
	rp.Packet.RemoteAddress = remote

	// try to send the packet
	err := c.Send(rp.Packet, generateNewSeq, remote)
	if err != nil {
		return err
	}

	// update the next ack check time
	rp.nextCheck = time.Now().Add(rp.RetryInterval)

	// add it to the list of packets to watch for acks
	c.acksNeeded.PushBack(rp)

	return nil
}

func (c *Connection) GetAcksNeededLen() int {
	return c.acksNeeded.Len()
}

// Tick triest to read a packet -- if it finds one it will update the acks --
// and then it tries to send out any reliable packets as necessary. Returns
// a bool indicating if a packet was read and a possible error
func (c *Connection) Tick() (bool, error) {
	// listen for a packet
	c.Socket.SetReadDeadline(time.Now().Add(c.ReadTimeout))
	p, err := c.Read()
	if err == nil && p != nil {
		return true, err
	}

	// check for packets that need to be retried
	err = c.RetryReliablePackets()
	if p != nil {
		return true, err
	}
	return false, err
}

func (c *Connection) ProccessAcks(p *Packet) {
	e := c.acksNeeded.Front()
	for e != nil {
		nextElem := e.Next()

		rp := e.Value.(*ReliablePacket)

		// check to see if the incoming packet acks the monitored reliable packet.
		// if it does, remove it from the watch list and call the event
		if rp.Packet.IsAckBy(p) {
			c.acksNeeded.Remove(e)
			if rp.OnAck != nil {
				rp.OnAck(c, rp)
			}
		}

		e = nextElem
	}
}

func (c *Connection) RetryReliablePackets() error {
	// loop through everything and retry if needed
	e := c.acksNeeded.Front()
	for e != nil {
		nextElem := e.Next()

		rp := e.Value.(*ReliablePacket)
		_, maxed, err := c.retryIfNeeded(rp)
		if err != nil {
			return err
		}

		// if max tries were reached, remove item from list
		if maxed {
			c.acksNeeded.Remove(e)
		}

		e = nextElem
	}

	return nil
}

func (c *Connection) retryIfNeeded(rp *ReliablePacket) (resent bool, maxErrors bool, err error) {
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
		err = c.Send(rp.Packet, true, rp.Packet.RemoteAddress)
		return true, false, err
	}

	// if we go here, it was time for a resend but we reached max fails,
	// so call the event for this
	if rp.OnFailToAck != nil {
		rp.OnFailToAck(c, rp)
	}

	return false, true, nil
}
