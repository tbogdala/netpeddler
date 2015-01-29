/* Copyright 2015, Timothy Bogdala <tdb@animal-machine.com>
   See the LICENSE file for more details. */

/*

This test creates a basic server and tries to connect
to it as a client.

Make sure to run with the `-cpu` flag for multi-core testing!

  go test -cpu 4

*/

package netpeddler

import (
	"fmt"
	"testing"
)

var (
	ackTestPort = 42002
)

const (
	ackTestServerBufferSize = 1500
)

func ackTestServer(t *testing.T, ch chan int, clientCh chan int) {
	npConn, err := NewConnection(ackTestServerBufferSize, fmt.Sprintf("127.0.0.1:%d", ackTestPort), "")
	if err != nil {
		ch <- serverListenFail
		t.Errorf("Failed to resolve the address to listen on for the server.\n%v\n", err)
		return
	}

	defer npConn.Close() // safeguard

	// let the test know we're ready
	ch <- serverReady

	// attempt to read in a packet, block until it happens
	var i uint32
	secArray := []uint32{1, 2, 5, 3, 4, 5, 2, 6, 7, 8, 50}
	lssArray := []uint32{1, 2, 5, 5, 5, 5, 5, 6, 7, 8, 50}
	maskArray := []uint32{0x01, 0x03, 0x19, 0x1D, 0x1F, 0x01F, 0x1F, 0x3F, 0x7F, 0xFF, 0x01}
	t.Logf("Server is starting seq packet request loop.\n")
	for i = 0; i < uint32(len(secArray)); i++ {
		// tell the client what sequence to use
		clientCh <- int(secArray[i])

		p, _, err := npConn.Read()
		if err != nil {
			ch <- serverFailedRead
			t.Errorf("Failed to read data from UDP.\n%v\n", err)
		}
		if npConn.lastSeenSeq == lssArray[i] && npConn.lastAckMask == maskArray[i] {
			t.Logf("Server got correct seq and ack masks: %v\n", string(p.Payload[:p.PayloadSize]))
		} else {
			t.Errorf("Server wanted seq,ackmask of %d,%x and got %d,%x\n",
				lssArray[i], maskArray[i], npConn.lastSeenSeq, npConn.lastAckMask)
		}
	}

	ch <- clientSuccess
}

func ackTestClient(t *testing.T, ch chan int) {
	npConn, err := NewConnection(ackTestServerBufferSize, "127.0.0.1:0", fmt.Sprintf("127.0.0.1:%d", ackTestPort))
	if err != nil {
		t.Errorf("Client failed to resolve the address to send to.\n%v\n", err)
		return
	}
	defer npConn.Close()

	for {
		select {
		case packetSeq := <-ch:
			// create a packet to send
			testPayload := []byte(fmt.Sprintf("Ack Test %d", packetSeq))
			packet := NewPacket(42, uint32(packetSeq), 0, 0, 0, uint32(len(testPayload)), testPayload)

			// send the packet
			// NOTE: generating new acks disabled since tests rely on preset values
			err = npConn.Send(packet, false, nil)
			if err != nil {
				t.Errorf("Client failed to send data.\n%v\n", err)
				return
			}

		default:
			// nothing
		}
	}
	ch <- clientSuccess
}

func TestAckMessages(t *testing.T) {
	// communicate over a simple channel to coordinate the test
	serverChan := make(chan int)
	clientChan := make(chan int)

	// launch the server
	go ackTestServer(t, serverChan, clientChan)

	// wait until it's ready for connections then spawn the client routine
	signal := <-serverChan
	if signal == serverReady {
		t.Logf("Server is ready for connections.\n")
	} else {
		t.Error("Couldn't set up server correctly.\n")
		t.FailNow()
	}

	// launch the client
	go ackTestClient(t, clientChan)

	// check for success
	for {
		select {
		case result := <-serverChan:
			if result != clientSuccess {
				t.Error("Client failed to connect.\n")
				t.FailNow()
			} else {
				return
			}
		default:
			//
		}
	}

	t.Logf("Client connection was successful.\n")
}

func doAckTest(t *testing.T, lss, curmask, cur, expmask, expseq uint32) {
	mask, seq := calcNewAckMask(lss, cur, curmask)
	if mask == expmask && seq == expseq {
		t.Logf("calcNewAckMask(%x,%x,%x) got expected %x,%x\n",
			lss, cur, curmask, expmask, expseq)
	} else {
		t.Errorf("calcNewAckMask(%x,%x,%x) expected %x,%x but got %x,%x\n",
			lss, cur, curmask, expmask, expseq, mask, seq)
	}
}

func TestAckCalculations(t *testing.T) {
	//     lss | curmask | cur | expmask |e xpseq
	// basic seq progression
	// mask goes from 0000 0000  ->  0000 0001.
	doAckTest(t, 0, 0x0000, 1, 0x0001, 1)
	// mask goes from 0000 0001  ->  0000 00011.
	doAckTest(t, 1, 0x0001, 2, 0x0003, 2)

	// the jump between seq 2 -> 5 moves the mask over
	// mask goes from 0000 0011  ->  0001 1001.
	doAckTest(t, 2, 0x0003, 5, 0x0019, 5)

	// an old seq comes in
	// mask goes from 0001 1001  ->  0001 1101.
	doAckTest(t, 5, 0x0019, 3, 0x001D, 5)
	// mask goes from 0001 1001  ->  0001 1111.
	doAckTest(t, 5, 0x001D, 4, 0x001F, 5)

	// a repeat seq? shouldn't happen, but lets test
	doAckTest(t, 5, 0x001F, 5, 0x001F, 5)
	doAckTest(t, 5, 0x001F, 2, 0x001F, 5)

	// a normal progression
	// mask goes from 0001 1111  ->  0011 1111.
	doAckTest(t, 5, 0x001F, 6, 0x003F, 6)
	// mask goes from 0011 1111  ->  01111 1111.
	doAckTest(t, 6, 0x003F, 7, 0x007F, 7)
	// mask goes from 0111 1111  ->  11111 1111.
	doAckTest(t, 7, 0x007F, 8, 0x00FF, 8)

	// test a big jump wiping out the mask
	doAckTest(t, 8, 0x00FF, 50, 0x0001, 50)
}
