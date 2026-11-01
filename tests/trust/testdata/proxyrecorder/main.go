// Command proxyrecorder is a test helper for the CTO-13 §3 test 4 harness
// (CTO-15 PR3). It is a minimal HTTPS proxy that records the CONNECT target of
// each client and never opens a tunnel. It lives under testdata so `go list
// ./...` (and the no-network static trust test) ignores it; the trust test
// builds it by its explicit path, so the test package itself imports no
// network package.
//
// It prints "ADDR <host:port>" once it is listening, then "CONNECT <target>"
// for every CONNECT line it reads, each flushed immediately.
package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxyrecorder:", err)
		os.Exit(1)
	}
	fmt.Println("ADDR", ln.Addr().String())
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			line, _ := bufio.NewReader(c).ReadString('\n')
			if f := strings.Fields(line); len(f) >= 2 && f[0] == "CONNECT" {
				fmt.Println("CONNECT", f[1])
			}
			_, _ = c.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n")) // no tunnel
		}(conn)
	}
}
