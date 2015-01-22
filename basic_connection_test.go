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
)

var (
  testListenAddress = "127.0.0.1:42000"
)

const (
  testServerBufferSize = 1500
)

// test state messages that are sent on channels
const (
  serverReady = 0
  serverListenFail
  serverGotConnectionMsg
  serverFailedRead
  clientSuccess
  clientDialFail
  clientSendFail
)

func server(t *testing.T, ch chan int) {
  npConn, err := CreateListener(testListenAddress, testServerBufferSize)
  if err != nil {
    ch <- serverListenFail
    t.Errorf("Failed to resolve the address to listen on for the server.\n%v", err)
    return
  }
  defer npConn.Close()

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
      t.Logf("Packet: %v\n", string(p.Payload[:p.PayloadSize]))

      // self destruct
      npConn.Close()
      break
    }
  }
}

func client(t *testing.T, ch chan int) {
  npConn, err := CreateSender(testListenAddress)
  if err != nil {
    t.Errorf("Client failed to resolve the address to send to.\n%v", err)
    return
  }
  defer npConn.Close()

  // create a packet to send
  testPayload := []byte("Connection seems to work!")
  packet, err := NewPacket(42,1,7,uint32(len(testPayload)), testPayload)
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

func TestBasicConnection(t *testing.T) {
    // communicate over a simple channel to coordinate the test
  serverChan := make(chan int)
  clientChan := make(chan int)

  // launch the server
  go server(t, serverChan)

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
  go client(t, clientChan)

  // check for success
  if result := <-clientChan; result != clientSuccess {
    t.Error("Client failed to connect.")
    t.FailNow()
  }

  t.Logf("Client connection was successful.")

}
