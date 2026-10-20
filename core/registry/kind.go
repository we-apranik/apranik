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
	KindStreamDNSServer
	KindDatagramDNSServer
	KindAPI
)

var kindDescriptions = [...]struct {
	name    string
	carrier carrier
}{
	KindBaseStream:              {"base stream transport", carrierStream},
	KindOverlayStream:           {"overlay stream transport", carrierStream},
	KindBaseDatagram:            {"base datagram transport", carrierDatagram},
	KindOverlayDatagram:         {"overlay datagram transport", carrierDatagram},
	KindStreamProxy:             {"stream proxy", carrierStream},
	KindDatagramOverStreamProxy: {"datagram over stream proxy", carrierStream},
	KindDatagramProxy:           {"datagram proxy", carrierDatagram},
	KindSniffer:                 {"sniffer", carrierStream},
	KindStreamDNSServer:         {"stream DNS server", carrierStream},
	KindDatagramDNSServer:       {"datagram DNS server", carrierDatagram},
	KindAPI:                     {"API", carrierStream},
}

func (k Kind) isKnown() bool { return k >= 0 && int(k) < len(kindDescriptions) }

func (k Kind) String() string {
	if !k.isKnown() {
		return fmt.Sprintf("unknown kind %d", int(k))
	}
	return kindDescriptions[k].name
}

type carrier int

const (
	carrierStream carrier = iota
	carrierDatagram
)

func (k Kind) carrier() carrier {
	if !k.isKnown() {
		return carrierStream
	}
	return kindDescriptions[k].carrier
}
