package peekable_test

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/The-Apranik/Apranik/core/net/peekable"
	"github.com/The-Apranik/Apranik/spec"
)

// readerStream is a spec.Stream whose reads come from a fixed reader.
type readerStream struct {
	net.Conn
	reader io.Reader
}

func (s readerStream) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (readerStream) CloseWrite() error            { return spec.ErrNoHalfClose }

// One byte per Read, so every test exercises fields split across reads.
func streamOf(data []byte) readerStream {
	return readerStream{reader: iotest.OneByteReader(bytes.NewReader(data))}
}

func TestPeekDoesNotConsume(t *testing.T) {
	stream := peekable.NewStream(streamOf([]byte("hello world")))

	peeked, err := stream.Peek(5)
	if err != nil || string(peeked) != "hello" {
		t.Fatalf("Peek(5) = %q, %v", peeked, err)
	}
	all, err := io.ReadAll(stream)
	if err != nil || string(all) != "hello world" {
		t.Errorf("ReadAll after Peek = %q, %v", all, err)
	}
}

func TestPeekExactlyTakesNoMoreThanItWasAskedFor(t *testing.T) {
	for _, n := range []int{1, 2, 7, 100} {
		stream := peekable.NewStream(readerStream{reader: bytes.NewReader(make([]byte, 4096))})
		peeked, err := stream.PeekExactly(n)
		if err != nil || len(peeked) != n {
			t.Fatalf("PeekExactly(%d) = %d bytes, %v", n, len(peeked), err)
		}
		if buffered := stream.Buffered(); buffered != n {
			t.Errorf("PeekExactly(%d) took %d bytes", n, buffered)
		}
	}
}

func TestPeekExactlyAfterReadingTakesNoMore(t *testing.T) {
	for _, stream := range []*peekable.Stream{
		peekable.NewStream(readerStream{reader: strings.NewReader("abcdefgh")}),
		peekable.NewRecordingStream(readerStream{reader: strings.NewReader("abcdefgh")}),
	} {
		if _, err := stream.PeekExactly(2); err != nil {
			t.Fatal(err)
		}
		io.ReadFull(stream, make([]byte, 2))
		peeked, err := stream.PeekExactly(3)
		if err != nil || string(peeked) != "cde" || stream.Buffered() != 3 {
			t.Errorf("PeekExactly(3) after reading 2 = %q, %v, %d buffered", peeked, err, stream.Buffered())
		}
		rest, _ := io.ReadAll(stream)
		if string(rest) != "cdefgh" {
			t.Errorf("rest = %q", rest)
		}
	}
}

func TestPeekExactlyShortStreamReturnsWhatThereIsWithTheError(t *testing.T) {
	stream := peekable.NewStream(streamOf([]byte("abc")))
	peeked, err := stream.PeekExactly(10)
	if string(peeked) != "abc" || !errors.Is(err, io.EOF) {
		t.Errorf("PeekExactly(10) = %q, %v; want \"abc\", io.EOF", peeked, err)
	}
}

func TestPeekShortStreamReturnsWhatThereIsWithTheError(t *testing.T) {
	stream := peekable.NewStream(streamOf([]byte("abc")))
	peeked, err := stream.Peek(10)
	if string(peeked) != "abc" || !errors.Is(err, io.EOF) {
		t.Errorf("Peek(10) = %q, %v; want \"abc\", io.EOF", peeked, err)
	}
}

func TestPeekBeyondTheBufferIsRefused(t *testing.T) {
	stream := peekable.NewStream(streamOf(nil))
	if _, err := stream.Peek(peekable.MaxBufferedBytes + 1); !errors.Is(err, peekable.ErrBufferFull) {
		t.Errorf("err = %v, want ErrBufferFull", err)
	}
}

func TestRewindReplaysEverythingTheHandshakeRead(t *testing.T) {
	stream := peekable.NewRecordingStream(streamOf([]byte("HEADER|payload")))

	header := make([]byte, len("HEADER|"))
	if _, err := io.ReadFull(stream, header); err != nil {
		t.Fatal(err)
	}
	if err := stream.Rewind(); err != nil {
		t.Fatal(err)
	}
	all, _ := io.ReadAll(stream)
	if string(all) != "HEADER|payload" {
		t.Errorf("after Rewind = %q, want everything from the first byte", all)
	}
}

func TestStopRecordingKeepsPeekedBytesButCannotRewind(t *testing.T) {
	stream := peekable.NewRecordingStream(streamOf([]byte("HEADER|payload")))

	if _, err := io.ReadFull(stream, make([]byte, len("HEADER|"))); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Peek(3); err != nil {
		t.Fatal(err)
	}
	stream.StopRecording()

	if err := stream.Rewind(); !errors.Is(err, peekable.ErrNotRewindable) {
		t.Errorf("Rewind after StopRecording = %v, want ErrNotRewindable", err)
	}
	rest, _ := io.ReadAll(stream)
	if string(rest) != "payload" {
		t.Errorf("after StopRecording = %q, want \"payload\"", rest)
	}
}

func TestRecordingPastTheBufferCannotRewindButStillReads(t *testing.T) {
	data := bytes.Repeat([]byte{'x'}, peekable.MaxBufferedBytes+10)
	stream := peekable.NewRecordingStream(readerStream{reader: bytes.NewReader(data)})

	all, err := io.ReadAll(stream)
	if err != nil || len(all) != len(data) {
		t.Fatalf("read %d bytes, %v; want %d", len(all), err, len(data))
	}
	if err := stream.Rewind(); !errors.Is(err, peekable.ErrNotRewindable) {
		t.Errorf("Rewind = %v, want ErrNotRewindable", err)
	}
}
