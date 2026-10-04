package registry_test

import (
	"errors"
	"testing"

	"github.com/The-Apranik/Apranik/core/registry"
	"github.com/The-Apranik/Apranik/spec"
)

type fakeDNSServer struct{ name string }

func (f fakeDNSServer) Info() spec.ModuleInfo                                      { return spec.ModuleInfo{Name: f.name} }
func (fakeDNSServer) Validate() error                                              { return nil }
func (fakeDNSServer) Warnings() []string                                           { return nil }
func (fakeDNSServer) NeedsTLS() bool                                               { return false }
func (fakeDNSServer) NewStreamDNSServer(spec.StreamDialer) (spec.DNSServer, error) { return nil, nil }
func (fakeDNSServer) NewDatagramDNSServer(spec.DatagramDialer) (spec.DNSServer, error) {
	return nil, nil
}

func streamDNS(name string) func() spec.StreamDNSServerModule {
	return func() spec.StreamDNSServerModule { return fakeDNSServer{name} }
}

func datagramDNS(name string) func() spec.DatagramDNSServerModule {
	return func() spec.DatagramDNSServerModule { return fakeDNSServer{name} }
}

func TestDNSServerNames(t *testing.T) {
	dnsServers := registry.New().DNSServers()

	var reserved *registry.ReservedNameError
	if err := dnsServers.RegisterStream(streamDNS("system")); !errors.As(err, &reserved) {
		t.Errorf("system: %v, want a ReservedNameError", err)
	}

	if err := dnsServers.RegisterStream(streamDNS("plain")); err != nil {
		t.Fatal(err)
	}
	var duplicate *registry.DuplicateModuleError
	if err := dnsServers.RegisterDatagram(datagramDNS("plain")); !errors.As(err, &duplicate) {
		t.Errorf("one name under both kinds: %v, want a DuplicateModuleError", err)
	}

	if _, err := dnsServers.LookupStream("plain"); err != nil {
		t.Errorf("LookupStream: %v", err)
	}
	if _, err := dnsServers.LookupDatagram("plain"); err == nil {
		t.Error("LookupDatagram found a stream server")
	}
	if kinds := dnsServers.Kinds("plain"); len(kinds) != 1 || kinds[0] != registry.KindStreamDNSServer {
		t.Errorf("Kinds = %v", kinds)
	}
}

func TestDefaultRegistersTheDNSServers(t *testing.T) {
	modules, err := registry.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tcp", "dot", "doh"} {
		if _, err := modules.DNSServers().LookupStream(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := modules.DNSServers().LookupDatagram("udp"); err != nil {
		t.Errorf("udp: %v", err)
	}
}
