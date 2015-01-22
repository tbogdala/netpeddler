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
	IsOpen        bool
}

type SendConnection struct {
	Socket        *net.UDPConn
	RemoteAddress *net.UDPAddr
	buffer        bytes.Buffer
	IsOpen        bool
}

const (
	defaultBufferSize = 1500
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
	newConn.IsOpen = true
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
	newConn.IsOpen = true
	return &newConn, nil
}

func (lc *ListenConnection) Close() {
	lc.IsOpen = false
	lc.Socket.Close()
}

func (sc *SendConnection) Close() {
	sc.IsOpen = false
	sc.Socket.Close()
}

func (lc *ListenConnection) Read() (*Packet, *net.UDPAddr, error) {
	// read the raw data in from the UDP connection
	n, addr, err := lc.Socket.ReadFromUDP(lc.buffer)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to read bytes from UDP: %v\n", err)
	}

	p, err := NewPacketFrom(n, lc.buffer)
	if err != nil {
		return nil, nil, fmt.Errorf("Failed to read packet from UDP: %v\n", err)
	}

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
