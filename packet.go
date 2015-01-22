/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
  "fmt"
  "bytes"
  "encoding/binary"
)

type Packet struct {
  ClientId uint32
  Seq uint32
  Chan uint8
  PayloadSize uint32
  Payload []byte
}

var (
  byteOrder = binary.BigEndian
  payloadOffset = binary.Size(uint32(1))*3 + binary.Size(uint8(1))
)

func NewPacket(id uint32, seq uint32, ch uint8, size uint32, b []byte) (*Packet, error) {
  p := new(Packet)
  p.ClientId = id
  p.Seq = seq
  p.Chan = ch

  p.PayloadSize = size
  p.Payload = make([]byte, size)
  copy(p.Payload, b)

  return p, nil
}

func (p *Packet)WriteTo(b *bytes.Buffer) error {
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
  binary.Read(buf, byteOrder, &p.PayloadSize)

  // resize if necessary
  byteLength := n+1
  if cap(p.Payload) <= byteLength - payloadOffset {
    //fmt.Printf("\nPACKET: adjusting payload size from %d to %d.\n", cap(p.Payload), byteLength)
    p.Payload = make([]byte, byteLength)
  }

  // copy the payload slice
  copy(p.Payload, b[payloadOffset:])

  return p, nil
}
