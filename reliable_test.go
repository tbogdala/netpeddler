/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
	"fmt"
	"testing"
	"time"
)

var (
	reliableTestPort = 42003
)

const (
	reliablePingPongCount = 10
)

func reliableServer(t *testing.T, ch chan int, onlyTestFinal bool) {
	npConn, err := NewConnection(testServerBufferSize, fmt.Sprintf("127.0.0.1:%d", reliableTestPort), "")
	if err != nil {
		ch <- serverListenFail
		t.Errorf("Failed to resolve the address to listen on for the server.\n%v", err)
		return
	}
	defer npConn.Close()

	// let the test know we're ready
	ch <- serverReady

	for pp := 1; pp <= reliablePingPongCount; pp++ {
		// attempt to read in a packet, block until it happens
		p, clientAddr, err := npConn.Read()
		if err != nil {
			ch <- serverFailedRead
			t.Errorf("Failed to read data from UDP.\n%v", err)
			return
		}

		// We got the packet
		t.Logf("Server got packet: %v\n", string(p.Payload[:p.PayloadSize]))
		t.Logf("Listener's last seq: %d ; ack mask: %x", npConn.lastSeenSeq, npConn.lastAckMask)

		// send a packet back to the client assuming they're listening in on the
		// same address they sent from
		if !onlyTestFinal || pp == reliablePingPongCount {
			// make a new packet that would be like a 'keep alive' packet
			testPayload := []byte("PONG")
			pong, err := NewPacket(0, 1, 7, npConn.lastSeenSeq, npConn.lastAckMask, uint32(len(testPayload)), testPayload)
			if err != nil {
				ch <- serverFailedSend
				t.Errorf("Server failed to create response packet.\n%v", err)
				return
			}

			// send the PING
			t.Logf("Server sending packet.\n")
			err = npConn.Send(pong, true, clientAddr)
			if err != nil {
				ch <- serverFailedSend
				t.Errorf("Client failed to send data.\n%v", err)
				return
			}
		}
	}
}

func reliableClient(t *testing.T, ch chan int, onlyTestFinal bool) {
	npConn, err := NewConnection(testServerBufferSize, "", fmt.Sprintf("127.0.0.1:%d", reliableTestPort))
	if err != nil {
		ch <- clientListenFail
		t.Errorf("Failed to setup the client connection.\n%v", err)
		return
	}

	for pp := 1; pp <= reliablePingPongCount; pp++ {
		// create a packet to send
		testPayload := []byte(fmt.Sprintf("PING%d", pp))
		packet, err := NewPacket(42, 0, 0, 0, 0, uint32(len(testPayload)), testPayload)
		if err != nil {
			ch <- clientSendFail
			t.Errorf("Failed to create client packet.\n%v", err)
			return
		}

		t.Logf("Client sending packet: %+v\n", string(packet.Payload[:packet.PayloadSize]))

		// send the PING
		err = npConn.SendReliable(packet, true, time.Second, 5, nil)
		if err != nil {
			ch <- clientSendFail
			t.Errorf("Client failed to send data.\n%v", err)
			return
		}

		if !onlyTestFinal || pp == reliablePingPongCount {
			// now wait for the PONG
			p, _, err := npConn.Read()
			if err != nil {
				ch <- clientListenFail
				t.Errorf("Client failed to read data on listener.\n%v", err)
				return
			}

			t.Logf("Client got packet: %+v\n", string(p.Payload[:p.PayloadSize]))

			// TEST: make sure we just have 1 packet being monitored for ack
			expectedAcksNeeded := 1
			if onlyTestFinal {
				expectedAcksNeeded = pp
			}
			ackLen := npConn.GetAcksNeededLen()
			t.Logf("Client looking for %d acks\n", ackLen)
			if ackLen != expectedAcksNeeded {
				ch <- clientListenFail
				t.Errorf("Client's sender ack needed count was incorrect (%d).\n", ackLen)
				return
			}

			// proccess acks from the incoming packet
			npConn.ProccessAcks(p)

			// TEST: make sure no more packets are being monitored
			ackLen = npConn.GetAcksNeededLen()
			if ackLen != 0 {
				ch <- clientListenFail
				t.Errorf("Client's sender ack needed count was incorrect after incoming packet (%d).\n", ackLen)
				return
			}
		}
	}

	ch <- clientSuccess
}

// TestReliablePackets tests for PING/PONG reliability one after the other
func TestReliablePackets(t *testing.T) {
	// communicate over a simple channel to coordinate the test
	serverChan := make(chan int)
	clientChan := make(chan int)

	// launch the server
	go reliableServer(t, serverChan, false)

	// wait until it's ready for connections
	signal := <-serverChan
	if signal == serverReady {
		t.Logf("Server is ready for connections.\n")
	} else {
		t.Errorf("Couldn't set up server correctly (%d).", signal)
		t.FailNow()
	}

	// launch the client
	go reliableClient(t, clientChan, false)

	// check for success
	if result := <-clientChan; result != clientSuccess {
		t.Error("Client failed to connect.")
		t.FailNow()
	}

	t.Logf("Client connection was successful.")

}

// TestReliablePackets tests for PING/PONG reliability after pooling up
// all of the test packets by having the server only reply at the end
func TestReliablePackets2(t *testing.T) {
	// communicate over a simple channel to coordinate the test
	serverChan := make(chan int)
	clientChan := make(chan int)

	// launch the server
	go reliableServer(t, serverChan, true)

	// wait until it's ready for connections
	signal := <-serverChan
	if signal == serverReady {
		t.Logf("Server is ready for connections.\n")
	} else {
		t.Errorf("Couldn't set up server correctly (%d).", signal)
		t.FailNow()
	}

	// launch the client
	go reliableClient(t, clientChan, true)

	// check for success
	if result := <-clientChan; result != clientSuccess {
		t.Error("Client failed to connect.")
		t.FailNow()
	}

	t.Logf("Client connection was successful.")

}
