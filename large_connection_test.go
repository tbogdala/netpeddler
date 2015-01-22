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
  "testing"
  "runtime"
  "math/rand"
  "time"
)

var (
  largeTestListenAddress = "127.0.0.1:42001"
)

const (
  largeTestServerBufferSize = 512 * 1024
)



func largeTestServer(t *testing.T, ch chan int) {
  npConn, err := CreateListener(largeTestListenAddress, largeTestServerBufferSize)
  if err != nil {
    ch <- serverListenFail
    t.Errorf("Failed to resolve the address to listen on for the server.\n%v", err)
    return
  }
  defer npConn.Close()
  t.Logf("Server buffer is %d bytes.\n", largeTestServerBufferSize)

  // let the test know we're ready
  ch <- serverReady

  for {
    runtime.Gosched()

    // attempt to read in a packet, block until it happens
    p, _, err := npConn.Read()
    if err != nil {
      ch <- serverFailedRead
      t.Errorf("Failed to read data from UDP.\n%v", err)
    } else {
      t.Logf("Packet payload size: %d\n", p.PayloadSize)

      // self destruct
      npConn.Close()
      break
    }
  }
}

func largeTestClient(t *testing.T, ch chan int) {
  npConn, err := CreateSender(largeTestListenAddress)
  if err != nil {
    t.Errorf("Client failed to resolve the address to send to.\n%v", err)
    return
  }
  defer npConn.Close()

  // create a packet to send
  const amount = 32*32*32
  rand.Seed(time.Now().UTC().UnixNano())
  testPayload := make([]byte, amount)
  for i:=0; i<amount; i++ {
    testPayload[i] = byte(rand.Intn(255))
  }

  packet, err := NewPacket(42,1,7,uint32(amount), testPayload)
  if err != nil {
    ch <- clientSendFail
    t.Errorf("Failed to create client packet.\n%v", err)
    return
  }

  // send the packet
  err = npConn.Send(packet)
  if err != nil {
    ch <- clientSendFail
    t.Errorf("Client failed to send data.\n%v", err)
    return
  }

  ch <- clientSuccess
}

func TestLargeConnection(t *testing.T) {
  // communicate over a simple channel to coordinate the test
  serverChan := make(chan int)
  clientChan := make(chan int)

  // launch the server
  go largeTestServer(t, serverChan)

  // wait until it's ready for connections then spawn the client routine
  for signal := range serverChan {
    if signal == serverReady {
      t.Logf("Server is ready for connections.\n")
      break
    } else if signal == serverListenFail {
      t.Error("Couldn't set up server correctly.")
      t.FailNow()
    } else {
      t.Logf("Unknown message id on test channel.\n")
      t.FailNow()
    }
  }

  // launch the client
  go largeTestClient(t, clientChan)

  // check for success
  if result := <-clientChan; result != clientSuccess {
    t.Error("Client failed to connect.")
    t.FailNow()
  }

  t.Logf("Client connection was successful.")
}
