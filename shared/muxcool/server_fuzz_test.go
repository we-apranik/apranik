package muxcool_test

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/we-apranik/apranik/shared/muxcool"
)

// Whatever a peer sends, a server whose sub-connections are all being read must
// neither panic nor hang: it ends the main connection or reads on, and once the
// peer closes it is done.
func FuzzServerReadsArbitraryBytes(f *testing.F) {
	f.Add(bytesOf(0x00, 0x14, 0x00, 0x01, 0x01, 0x01, 0x01, 0x00, 0x50, 0x02, 0x0B, "example.com", 0x00, 0x03, "abc"))
	f.Add(bytesOf(0x00, 0x0C, 0x00, 0x03, 0x01, 0x01, 0x02, 0x00, 0x35, 0x01, 8, 8, 8, 8, 0x00, 0x01, "q", 0x00, 0x04, 0x00, 0x03, 0x03, 0x00))
	f.Add(bytesOf(0x00, 0x04, 0x00, 0x01, 0x02, 0x01, 0x00, 0x05, "hello"))
	f.Add(bytesOf(0x00, 0x04, 0x00, 0x01, 0x09, 0x00))
	f.Add(bytesOf(0x00, 0x04, 0xBE, 0xEF, 0x04, 0x01, 0x00, 0x02, "no"))

	f.Fuzz(func(t *testing.T, wire []byte) {
		peerSide, serverSide := net.Pipe()
		server := muxcool.NewServer(serverSide, muxcool.ServerOptions{IdleTimeout: -1})
		defer server.Close()
		defer peerSide.Close()

		go func() {
			for {
				accepted, err := server.AcceptSubConnection()
				if err != nil {
					return
				}
				if accepted.Stream != nil {
					go io.Copy(io.Discard, accepted.Stream)
				} else {
					go func() {
						buffer := make([]byte, 100)
						for {
							if _, _, err := accepted.Datagram.ReadFrom(buffer); err != nil && !errors.Is(err, muxcool.ErrDatagramTooLarge) {
								return
							}
						}
					}()
				}
			}
		}()

		peerSide.SetWriteDeadline(time.Now().Add(2 * time.Second))
		go io.Copy(io.Discard, peerSide) // the server's answers
		peerSide.Write(wire)
		peerSide.Close()

		select {
		case <-server.Done():
		case <-time.After(5 * time.Second):
			t.Fatalf("the server did not finish after the peer closed, after % x", wire)
		}
	})
}
