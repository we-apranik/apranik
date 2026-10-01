package fragment

import (
	"math/rand/v2"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/shared/tlsclienthello"
	"github.com/The-Apranik/Apranik/spec"
)

// fragmentingStream holds back the first bytes written until they form a
// whole TLS record, sends that record cut into pieces, and from then on
// passes every write straight through. A first write that is not a TLS
// handshake record passes through at once, uncut.
//
// Holding back matters under freedom: the relay forwards the user's
// ClientHello as it reads it, and a ClientHello larger than one TCP segment
// (Chrome's, with its post-quantum key share) arrives in two reads.
type fragmentingStream struct {
	spec.Stream
	settings *Settings

	writeMutex      sync.Mutex
	heldBack        []byte
	passingThrough  atomic.Bool
	writeDeadline   atomic.Int64 // Unix nanoseconds; 0 means none
	closed          chan struct{}
	closeClosedOnce sync.Once
}

func newFragmentingStream(underlyingStream spec.Stream, settings *Settings) *fragmentingStream {
	return &fragmentingStream{Stream: underlyingStream, settings: settings, closed: make(chan struct{})}
}

func (s *fragmentingStream) Write(payload []byte) (int, error) {
	if s.passingThrough.Load() {
		return s.Stream.Write(payload)
	}
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()
	if s.passingThrough.Load() {
		return s.Stream.Write(payload)
	}
	if len(payload) == 0 {
		return 0, nil
	}

	s.heldBack = append(s.heldBack, payload...)
	if s.heldBack[0] != tlsclienthello.RecordTypeHandshake {
		return len(payload), s.releaseHeldBackUncut()
	}
	if len(s.heldBack) < tlsclienthello.RecordHeaderSize {
		return len(payload), nil
	}
	if !tlsclienthello.IsHandshakeRecordHeader(s.heldBack) {
		return len(payload), s.releaseHeldBackUncut()
	}
	recordEnd := tlsclienthello.RecordHeaderSize + tlsclienthello.RecordBodySize(s.heldBack)
	if len(s.heldBack) < recordEnd {
		return len(payload), nil
	}

	record, afterRecord := s.heldBack[:recordEnd], s.heldBack[recordEnd:]
	s.heldBack = nil
	s.passingThrough.Store(true)
	if err := s.sendCut(record); err != nil {
		return 0, err
	}
	if len(afterRecord) > 0 {
		if _, err := s.Stream.Write(afterRecord); err != nil {
			return 0, err
		}
	}
	return len(payload), nil
}

func (s *fragmentingStream) releaseHeldBackUncut() error {
	heldBack := s.heldBack
	s.heldBack = nil
	s.passingThrough.Store(true)
	_, err := s.Stream.Write(heldBack)
	return err
}

// A half-close ends the chance of a whole record, so whatever was held back
// goes out as it is.
func (s *fragmentingStream) CloseWrite() error {
	s.writeMutex.Lock()
	if !s.passingThrough.Load() && len(s.heldBack) > 0 {
		if err := s.releaseHeldBackUncut(); err != nil {
			s.writeMutex.Unlock()
			return err
		}
	}
	s.passingThrough.Store(true)
	s.writeMutex.Unlock()
	return s.Stream.CloseWrite()
}

func (s *fragmentingStream) Close() error {
	s.closeClosedOnce.Do(func() { close(s.closed) })
	return s.Stream.Close()
}

func (s *fragmentingStream) SetDeadline(deadline time.Time) error {
	s.rememberWriteDeadline(deadline)
	return s.Stream.SetDeadline(deadline)
}

func (s *fragmentingStream) SetWriteDeadline(deadline time.Time) error {
	s.rememberWriteDeadline(deadline)
	return s.Stream.SetWriteDeadline(deadline)
}

func (s *fragmentingStream) rememberWriteDeadline(deadline time.Time) {
	if deadline.IsZero() {
		s.writeDeadline.Store(0)
		return
	}
	s.writeDeadline.Store(deadline.UnixNano())
}

func (s *fragmentingStream) sendCut(record []byte) error {
	header, body := record[:tlsclienthello.RecordHeaderSize], record[tlsclienthello.RecordHeaderSize:]

	// With records, the pieces are the body's, each given a header of its
	// own; without, they are slices of the record as it is.
	region, regionStartInRecord := record, 0
	if s.settings.TLSRecords {
		region, regionStartInRecord = body, tlsclienthello.RecordHeaderSize
	}

	serverNameStart, serverNameLength := -1, 0
	if s.settings.SplitSNI {
		if start, length, found := serverNameInRecord(record); found {
			serverNameStart, serverNameLength = start-regionStartInRecord, length
		}
	}

	cuts := cutPositions(len(region), s.settings, serverNameStart, serverNameLength)
	pieces := make([][]byte, 0, len(cuts)+1)
	previousCut := 0
	for _, cut := range append(cuts, len(region)) {
		piece := region[previousCut:cut]
		if s.settings.TLSRecords {
			piece = append([]byte{header[0], header[1], header[2], byte(len(piece) >> 8), byte(len(piece))}, piece...)
		}
		pieces = append(pieces, piece)
		previousCut = cut
	}

	if !s.settings.TCPPackets {
		_, err := s.Stream.Write(slices.Concat(pieces...))
		return err
	}
	delays := s.settings.pieceDelays()
	for index, piece := range pieces {
		if _, err := s.Stream.Write(piece); err != nil {
			return err
		}
		if index < len(pieces)-1 {
			if err := s.wait(entryFor(delays, index).pick()); err != nil {
				return err
			}
		}
	}
	return nil
}

// Returns the server name's offset and length within record, when record
// holds a whole ClientHello that names one. A ClientHello spanning several
// records is not searched: only the first record is cut.
func serverNameInRecord(record []byte) (start, length int, found bool) {
	clientHello, err := tlsclienthello.Reassemble(record)
	if err != nil {
		return 0, 0, false
	}
	serverName, err := tlsclienthello.FindServerName(clientHello)
	if err != nil || len(serverName.Name) < 2 {
		return 0, 0, false
	}
	return tlsclienthello.RecordHeaderSize + tlsclienthello.HandshakeHeaderSize + serverName.Offset, len(serverName.Name), true
}

// cutPositions returns where to cut a region of regionSize bytes, ascending:
// sizes from piece_sizes, at most max_pieces pieces, and one cut strictly
// inside the server name when serverNameStart is not -1.
func cutPositions(regionSize int, settings *Settings, serverNameStart, serverNameLength int) []int {
	mustSplitServerName := serverNameStart >= 0 && serverNameLength >= 2 &&
		serverNameStart+serverNameLength <= regionSize
	maxPieces := settings.MaxPieces.pick()

	var cuts []int
	position := 0
	for pieceIndex := 0; ; pieceIndex++ {
		piecesSoFar := len(cuts) + 1
		if mustSplitServerName {
			piecesSoFar++
		}
		if maxPieces > 0 && piecesSoFar >= maxPieces {
			break
		}
		position += entryFor(settings.PieceSizes, pieceIndex).pick()
		if position >= regionSize {
			break
		}
		cuts = append(cuts, position)
	}

	if mustSplitServerName {
		serverNameEnd := serverNameStart + serverNameLength
		alreadySplit := slices.ContainsFunc(cuts, func(cut int) bool {
			return cut > serverNameStart && cut < serverNameEnd
		})
		if !alreadySplit {
			cutInsideName := serverNameStart + 1 + rand.IntN(serverNameLength-1)
			cuts = append(cuts, cutInsideName)
			slices.Sort(cuts)
		}
	}
	return cuts
}

func (s *fragmentingStream) wait(delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	deadlineExceeded := false
	if deadline := s.writeDeadline.Load(); deadline != 0 {
		if untilDeadline := time.Until(time.Unix(0, deadline)); untilDeadline < delay {
			delay, deadlineExceeded = max(untilDeadline, 0), true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		if deadlineExceeded {
			return os.ErrDeadlineExceeded
		}
		return nil
	case <-s.closed:
		return net.ErrClosed
	}
}
