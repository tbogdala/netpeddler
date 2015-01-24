/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

package netpeddler

import (
	"testing"
	"time"
)

var (
	retryTestListenAddress = "127.0.0.1:42004"
	retryTestCount         = 0
	retrySpeed             = time.Millisecond * 100
)

func retryServer(t *testing.T, ch chan int) {
	listener, err := CreateListener(retryTestListenAddress, testServerBufferSize)
	if err != nil {
		ch <- serverListenFail
		t.Errorf("Failed to resolve the address to listen on for the server.\n%v", err)
		return
	}
	defer listener.Close()

	// let the test know we're ready
	ch <- serverReady

	for {
		// attempt to read in a packet, block until it happens
		p, _, err := listener.Read()
		if err != nil {
			ch <- serverFailedRead
			t.Errorf("Failed to read data from UDP.\n%v", err)
			return
		}

		// We got the packet
		t.Logf("Server got packet: %v\n", string(p.Payload[:p.PayloadSize]))
		t.Logf("Listener's last seq: %d ; ack mask: %x", listener.lastSeenSeq, listener.lastAckMask)

		retryTestCount++
	}
}

func retryClient(t *testing.T, ch chan int) {
	sender, err := CreateSender(retryTestListenAddress)
	if err != nil {
		t.Errorf("Client failed to resolve the address to send to.\n%v", err)
		return
	}
	defer sender.Close()

	testPayload := []byte("PING")
	packet, err := NewPacket(42, 0, 0, 0, 0, uint32(len(testPayload)), testPayload)
	if err != nil {
		ch <- clientSendFail
		t.Errorf("Failed to create client packet.\n%v", err)
		return
	}

	t.Logf("Client sending packet: %+v\n", string(packet.Payload[:packet.PayloadSize]))

	// send the PING
	const retryCount = 5
	err = sender.SendReliable(packet, true, retrySpeed, retryCount)
	if err != nil {
		ch <- clientSendFail
		t.Errorf("Client failed to send data.\n%v", err)
		return
	}

	endTime := time.Now().Add(retrySpeed * (retryCount + 1))
	for {
		sender.Tick()
		if time.Now().After(endTime) {
			// we should have our retry count now
			if retryCount != retryTestCount-1 { //-1 for the first packet sent
				ch <- clientSendFail
				t.Errorf("Client failed to retry packets enough (%d).\n", retryTestCount)
				return
			} else {
				break
			}
		}
	}

	ch <- clientSuccess
}

// TestRetryPackets tests the ability to resend packets that weren't ack'd
func TestRetryPackets(t *testing.T) {
	// communicate over a simple channel to coordinate the test
	serverChan := make(chan int)
	clientChan := make(chan int)

	// launch the server
	go retryServer(t, serverChan)

	// wait until it's ready for connections
	signal := <-serverChan
	if signal == serverReady {
		t.Logf("Server is ready for connections.\n")
	} else {
		t.Errorf("Couldn't set up server correctly (%d).", signal)
		t.FailNow()
	}

	// launch the client
	go retryClient(t, clientChan)

	// check for success
	if result := <-clientChan; result != clientSuccess {
		t.Error("Client failed to get the correct amount of retries.")
		t.FailNow()
	}

	t.Logf("Client connection was successful.")

}