package fragment

import (
	"errors"
	"time"
)

// Used when tcp_packets is on and piece_delays is not set. Not a default of
// the field itself: then a config turning tcp_packets off would have to clear
// a delay it never set.
var defaultPieceDelays = []DurationRange{{Min: 10 * time.Millisecond, Max: 20 * time.Millisecond}}

// PROVISIONAL defaults, to tune from field tests.
func defaultSettings() Settings {
	return Settings{
		TLSRecords: true,
		TCPPackets: true,
		PieceSizes: []Range{{Min: 3, Max: 5}, {Min: 10, Max: 20}},
		MaxPieces:  Range{Min: 4, Max: 8},
		SplitSNI:   true,
	}
}

type Settings struct {
	// Cut the ClientHello into several TLS records, each with its own
	// header. Beats DPI that joins packets but parses only the first record.
	TLSRecords bool `json:"tls_records"`

	// Send the pieces as separate TCP packets, waiting piece_delays after
	// each. Beats DPI that inspects each packet alone.
	TCPPackets bool `json:"tcp_packets"`

	PieceSizes []Range `json:"piece_sizes"`

	PieceDelays []DurationRange `json:"piece_delays"`

	MaxPieces Range `json:"max_pieces"`

	SplitSNI bool `json:"split_sni"`
}

func (s *Settings) Validate() error {
	var problems []error
	if !s.TLSRecords && !s.TCPPackets {
		problems = append(problems, errors.New("fragment: turn on tls_records, tcp_packets or both; with neither nothing is split"))
	}
	if len(s.PieceSizes) == 0 {
		problems = append(problems, errors.New("fragment: piece_sizes needs at least one entry"))
	}
	for _, size := range s.PieceSizes {
		if size.Min == 0 {
			problems = append(problems, errors.New("fragment: piece_sizes entries must be at least 1 byte; an empty TLS record breaks the RFC and Go servers refuse it"))
			break
		}
	}
	if s.PieceDelays != nil && !s.TCPPackets {
		problems = append(problems, errors.New("fragment: piece_delays needs tcp_packets: without it every piece leaves in one write, so there is nothing to wait between"))
	}
	if s.PieceDelays != nil && len(s.PieceDelays) == 0 {
		problems = append(problems, errors.New("fragment: piece_delays needs at least one entry; leave it out for the default"))
	}
	if s.MaxPieces.Min < 2 {
		problems = append(problems, errors.New("fragment: max_pieces must allow at least 2 pieces; one piece splits nothing, and zero is not a way to ask for no limit"))
	}
	return errors.Join(problems...)
}

func (s *Settings) Warnings() []string { return nil }

func (s *Settings) pieceDelays() []DurationRange {
	if s.PieceDelays == nil {
		return defaultPieceDelays
	}
	return s.PieceDelays
}
