netpeddler: a Go library for fast networking using UDP
======================================================

Netpeddler is a networking library that simplifies the sending of packets
over UDP.

Instead of always having the ordering and reliability of TCP, it is possible
to selectively have reliable packets via UDP using this library. It works by having
each packet have an ACK mask built into it that can potentially acknowledge the
reception of a packet. This idea of an ACK bitmask is described in this 
[Gaffer On Games](http://gafferongames.com/networking-for-game-programmers/reliability-and-flow-control/)
article.

**NOTE: This library is in early development and API stability cannot be guaranteed.**


Installation
------------

You can get the latest copy of the library using this command:

```bash
go get github.com/tbogdala/netpeddler
```

You can then use it in your code by importing it:

```go
import "github.com/tbogdala/netpeddler"
```


Usage
-----

For now, users can browse the test files for examples on how to use netpeddler.

* basic_connection_test.go
* large_connection_test.go
* reliable_test.go
* retry_test.go


License
-------

Netpeddler is released under the BSD license. See the `LICENSE` file for more details.


TODO
----

* A banlist to check connections against.
* Better documentation
