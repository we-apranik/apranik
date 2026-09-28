package websocket

import (
	"testing"
)

// Throughput of the stream in each direction: the client masks every byte,
// the server unmasks it. Run with -benchmem; allocations per write should be 0.
func benchmarkDirection(b *testing.B, clientWrites bool, writeSize int) {
	client, server := connect(b, newModule(b, nil), newModule(b, nil))
	writer, reader := client, server
	if !clientWrites {
		writer, reader = server, client
	}
	payload := make([]byte, writeSize)
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 32<<10)
		for total := 0; total < b.N*writeSize; {
			n, err := reader.Read(buffer)
			if err != nil {
				return
			}
			total += n
		}
	}()
	b.SetBytes(int64(writeSize))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := writer.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
	<-done
}

func BenchmarkClientToServer1KiB(b *testing.B)  { benchmarkDirection(b, true, 1<<10) }
func BenchmarkClientToServer16KiB(b *testing.B) { benchmarkDirection(b, true, 16<<10) }
func BenchmarkClientToServer64KiB(b *testing.B) { benchmarkDirection(b, true, 64<<10) }
func BenchmarkServerToClient16KiB(b *testing.B) { benchmarkDirection(b, false, 16<<10) }

func BenchmarkMask16KiB(b *testing.B) {
	payload := make([]byte, 16<<10)
	b.SetBytes(int64(len(payload)))
	for b.Loop() {
		maskBytes([4]byte{1, 2, 3, 4}, 1, payload)
	}
}
