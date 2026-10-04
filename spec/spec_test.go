package spec_test

import (
	"net"
	"net/netip"
	"testing"

	"github.com/The-Apranik/Apranik/spec"
)

// fakeAddr is a net.Addr of a kind the fast path does not know, so it takes
// the string path.
type fakeAddr string

func (fakeAddr) Network() string  { return "fake" }
func (a fakeAddr) String() string { return string(a) }

func TestNewEndpointFromNetAddr(t *testing.T) {
	for _, tc := range []struct {
		name    string
		address net.Addr
		want    string
	}{
		{"tcp4", &net.TCPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 80}, "1.2.3.4:80"},
		{"udp6", &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 53}, "[2001:db8::1]:53"},
		{"ipv4 mapped into ipv6", &net.TCPAddr{IP: net.ParseIP("::ffff:1.2.3.4"), Port: 443}, "1.2.3.4:443"},
		{"zoned", &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 5353, Zone: "eth0"}, "[fe80::1%eth0]:5353"},
		{"another kind of address", fakeAddr("5.6.7.8:9"), "5.6.7.8:9"},
		{"unix socket", &net.UnixAddr{Name: "/tmp/socket", Net: "unix"}, "<none>"},
		{"nil interface", nil, "<none>"},
		{"nil TCP address", (*net.TCPAddr)(nil), "<none>"},
		{"TCP address with no IP", &net.TCPAddr{Port: 80}, "<none>"},
	} {
		if got := spec.NewEndpointFromNetAddr(tc.address).String(); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func BenchmarkNewEndpointFromTCPAddr(b *testing.B) {
	address := net.TCPAddrFromAddrPort(netip.MustParseAddrPort("1.2.3.4:443"))
	b.ReportAllocs()
	for b.Loop() {
		spec.NewEndpointFromNetAddr(address)
	}
}

type fakeConfig struct{}

func (fakeConfig) Validate() error    { return nil }
func (fakeConfig) Warnings() []string { return nil }

type fakeStreamDNSServerModule struct{ fakeConfig }

func (fakeStreamDNSServerModule) Info() spec.ModuleInfo { return spec.ModuleInfo{Name: "fake"} }
func (fakeStreamDNSServerModule) NeedsTLS() bool        { return true }
func (fakeStreamDNSServerModule) NewStreamDNSServer(spec.StreamDialer) (spec.DNSServer, error) {
	return nil, nil
}

type fakeDatagramDNSServerModule struct{ fakeConfig }

func (fakeDatagramDNSServerModule) Info() spec.ModuleInfo { return spec.ModuleInfo{Name: "fake"} }
func (fakeDatagramDNSServerModule) NewDatagramDNSServer(spec.DatagramDialer) (spec.DNSServer, error) {
	return nil, nil
}

func TestDNSServerKindsAreModules(t *testing.T) {
	var stream spec.StreamDNSServerModule = fakeStreamDNSServerModule{}
	var datagram spec.DatagramDNSServerModule = fakeDatagramDNSServerModule{}

	for _, module := range []spec.Config{stream, datagram} {
		if err := module.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	}
	if !stream.NeedsTLS() {
		t.Fatal("NeedsTLS: want the fake's true")
	}
}
