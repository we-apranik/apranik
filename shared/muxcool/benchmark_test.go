package muxcool_test

import (
	"io"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/shared/muxcool"
)

func BenchmarkRawTCPThroughput(b *testing.B) {
	for _, size := range []int{1 << 10, 8 << 10, 32 << 10} {
		b.Run(byteSize(size), func(b *testing.B) {
			writer, reader := connected(b)
			defer writer.Close()
			defer reader.Close()
			go io.Copy(io.Discard, reader)
			payload := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := writer.Write(payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStreamThroughput(b *testing.B) {
	for _, size := range []int{1 << 10, 8 << 10, 32 << 10} {
		b.Run(byteSize(size), func(b *testing.B) {
			client, server := pair(b, muxcool.ServerOptions{})
			opened, accepted := openAndAccept(b, client, server, domainDestinationNoT("example.com", 80))
			go io.Copy(io.Discard, accepted)
			payload := make([]byte, size)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := opened.Write(payload); err != nil {
					b.Fatal(err)
				}
			}
			opened.Close()
		})
	}
}

func BenchmarkStreamsInParallelThroughput(b *testing.B) {
	for _, streams := range []int{1, 4, 16} {
		b.Run(byteSize(streams), func(b *testing.B) {
			client, server := pair(b, muxcool.ServerOptions{})
			opened := make([]interface{ Write([]byte) (int, error) }, streams)
			for index := range streams {
				stream, accepted := openAndAccept(b, client, server, domainDestinationNoT("example.com", 80))
				opened[index] = stream
				go io.Copy(io.Discard, accepted)
			}
			payload := make([]byte, 8<<10)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()

			var group sync.WaitGroup
			perStream := b.N/streams + 1
			for _, stream := range opened {
				group.Add(1)
				go func() {
					defer group.Done()
					for range perStream {
						if _, err := stream.Write(payload); err != nil {
							return
						}
					}
				}()
			}
			group.Wait()
		})
	}
}

func BenchmarkDatagramRoundTrip(b *testing.B) {
	client, server := pair(b, muxcool.ServerOptions{})
	destination := ipDestinationNoT("8.8.8.8", 53)
	opened, accepted := openDatagramAndAccept(b, client, server, destination, []byte("first"))
	readBuffer := make([]byte, 100)
	accepted.ReadFrom(readBuffer)
	go func() {
		buffer := make([]byte, 2000)
		for {
			n, from, err := accepted.ReadFrom(buffer)
			if err != nil {
				return
			}
			accepted.WriteTo(buffer[:n], from)
		}
	}()
	payload := make([]byte, 512)
	reply := make([]byte, 2000)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := opened.WriteTo(payload, destination); err != nil {
			b.Fatal(err)
		}
		if _, _, err := opened.ReadFrom(reply); err != nil {
			b.Fatal(err)
		}
	}
}

func byteSize(n int) string {
	switch {
	case n >= 1<<10 && n%(1<<10) == 0:
		return itoa(n>>10) + "KiB"
	default:
		return itoa(n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
