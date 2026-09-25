package registry

import "fmt"

// Kind exists so an error can say what a name actually is -- "tls is an
// overlay stream transport" -- not only that the lookup missed. It never
// selects behaviour: registration and lookup are typed methods.
type Kind int

const (
	KindBaseStream Kind = iota
	KindOverlayStream
	KindBaseDatagram
	KindOverlayDatagram
	KindStreamProxy
	KindDatagramOverStreamProxy
	KindDatagramProxy
	KindSniffer
)

func (k Kind) String() string {
	switch k {
	case KindBaseStream:
		return "base stream transport"
	case KindOverlayStream:
		return "overlay stream transport"
	case KindBaseDatagram:
		return "base datagram transport"
	case KindOverlayDatagram:
		return "overlay datagram transport"
	case KindStreamProxy:
		return "stream proxy"
	case KindDatagramOverStreamProxy:
		return "datagram-over-stream proxy"
	case KindDatagramProxy:
		return "datagram proxy"
	case KindSniffer:
		return "sniffer"
	default:
		return fmt.Sprintf("unknown kind %d", int(k))
	}
}

// Two transport kinds may share a name only when their carriers differ. Both
// of the same shape would make position in a stack ambiguous; one base-stream
// and one base-datagram is how QUIC expresses carrying both.
type carrier int

const (
	carrierStream carrier = iota
	carrierDatagram
)

func (k Kind) carrier() carrier {
	switch k {
	case KindBaseDatagram, KindOverlayDatagram, KindDatagramProxy:
		return carrierDatagram
	default:
		return carrierStream
	}
}
