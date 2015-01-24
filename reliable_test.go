/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */


package netpeddler

import (
	"fmt"
	"testing"
	"time"
)

var (
	reliableTestListenAddress = "127.0.0.1:42003"
)

const (
	reliablePingPongCount = 10
)


func reliableServer(t *testing.T, ch chan int) {
	listener, err := CreateListener(reliableTestListenAddress, testServerBufferSize)
	if err != nil {
		ch <- serverListenFail
		t.Errorf("Failed to resolve the address to listen on for the server.\n%v", err)
		return
	}
	defer listener.Close()

	// let the test know we're ready
	ch <- serverReady

	var sender *Sender = nil
	for pp:=1; pp<=reliablePingPongCount; pp++ {
		// attempt to read in a packet, block until it happens
		p, clientAddr, err := listener.Read()
		if err != nil {
			ch <- serverFailedRead
			t.Errorf("Failed to read data from UDP.\n%v", err)
			return
		}

		// We got the packet
		t.Logf("Server got packet: %v\n", string(p.Payload[:p.PayloadSize]))
		t.Logf("Listener's last seq: %d ; ack mask: %x", listener.lastSeenSeq, listener.lastAckMask)

		// send a packet back to the client assuming they're listening in on the
		// same address they sent from
		if sender == nil {
		sender, err = CreateSender(clientAddr.String())
			if err != nil {
				ch <- serverFailedRead
				t.Errorf("Server failed to resolve the address to send to.\n%v", err)
				return
			}
			defer sender.Close()
			t.Logf("Server created sender connection.\n")
		}

		// make a new packet that would be like a 'keep alive' packet
		testPayload := []byte("PONG")
		pong, err := NewPacket(0, 1, 7, listener.lastSeenSeq, listener.lastAckMask, uint32(len(testPayload)), testPayload)
		if err != nil {
			ch <- serverFailedSend
			t.Errorf("Server failed to create response packet.\n%v", err)
			return
		}

		// send the PING
		t.Logf("Server sending packet.\n")
		err = sender.Send(pong, true)
		if err != nil {
			ch <- serverFailedSend
			t.Errorf("Client failed to send data.\n%v", err)
			return
		}
	}
}



func reliableClient(t *testing.T, ch chan int) {
	sender, err := CreateSender(reliableTestListenAddress)
	if err != nil {
		t.Errorf("Client failed to resolve the address to send to.\n%v", err)
		return
	}
	defer sender.Close()

	listenerAddr := sender.Socket.LocalAddr()
	t.Logf("Creating client listener on local addr: %v\n", listenerAddr)
	listener, err := CreateListener(listenerAddr.String(), testServerBufferSize)
	if err != nil {
		ch <- clientSendFail
		t.Errorf("Failed to resolve the address to listen on for the client.\n%v", err)
		return
	}
	defer listener.Close()
	t.Logf("Client listener created.\n")

	for pp:=1; pp<=reliablePingPongCount; pp++ {
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
		err = sender.SendReliable(packet, true, time.Second, 5)
		if err != nil {
			ch <- clientSendFail
			t.Errorf("Client failed to send data.\n%v", err)
			return
		}

		// now wait for the PONG
		p, _, err := listener.Read()
		if err != nil {
			ch <- clientListenFail
			t.Errorf("Client failed to read data on listener.\n%v", err)
			return
		}

		t.Logf("Client got packet: %+v\n", string(p.Payload[:p.PayloadSize]))

		// TEST: make sure we just have 1 packet being monitored for ack
		ackLen := sender.GetAcksNeededLen()
		if ackLen != 1 {
			ch <- clientListenFail
			t.Errorf("Client's sender ack needed count was incorrect (%d).\n", ackLen)
			return
		}

		// proccess acks from the incoming packet
		sender.ProccessAcks(p)

		// TEST: make sure no more packets are being monitored
		ackLen = sender.GetAcksNeededLen()
		if ackLen != 0 {
			ch <- clientListenFail
			t.Errorf("Client's sender ack needed count was incorrect after incoming packet (%d).\n", ackLen)
			return
		}
	}

	ch <- clientSuccess
}

func TestReliablePackets(t *testing.T) {
	// communicate over a simple channel to coordinate the test
	serverChan := make(chan int)
	clientChan := make(chan int)

	// launch the server
	go reliableServer(t, serverChan)

	// wait until it's ready for connections
	signal := <-serverChan
	if signal == serverReady {
		t.Logf("Server is ready for connections.\n")
	} else {
		t.Errorf("Couldn't set up server correctly (%d).", signal)
		t.FailNow()
	}

	// launch the client
	go reliableClient(t, clientChan)

	// check for success
	if result := <-clientChan; result != clientSuccess {
		t.Error("Client failed to connect.")
		t.FailNow()
	}

	t.Logf("Client connection was successful.")

}
