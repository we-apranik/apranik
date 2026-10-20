package registry_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/we-apranik/apranik/core/registry"
	"github.com/we-apranik/apranik/spec"
)

type settings struct{ name string }

func (settings) Validate() error    { return nil }
func (settings) Warnings() []string { return nil }
func (s settings) Info() spec.ModuleInfo {
	return spec.ModuleInfo{Name: s.name, Description: "a fake"}
}

type baseStream struct{ settings }

func (baseStream) NewStreamDialer(spec.Resolver) (spec.StreamDialer, error) { return nil, nil }
func (baseStream) NewStreamListener(spec.Endpoint) (spec.StreamListener, error) {
	return nil, nil
}

type overlayStream struct{ settings }

func (overlayStream) NewStreamOverlayClient() (spec.StreamOverlayClient, error) { return nil, nil }
func (overlayStream) NewStreamOverlayServer() (spec.StreamOverlayServer, error) { return nil, nil }

type baseDatagram struct{ settings }

func (baseDatagram) NewDatagramDialer(spec.Resolver) (spec.DatagramDialer, error) { return nil, nil }
func (baseDatagram) NewDatagramListener(spec.Endpoint, spec.Resolver) (spec.DatagramListener, error) {
	return nil, nil
}

type overlayDatagram struct{ settings }

func (overlayDatagram) WrapDatagramDialer(spec.DatagramDialer) (spec.DatagramDialer, error) {
	return nil, nil
}
func (overlayDatagram) WrapDatagramListener(spec.DatagramListener) (spec.DatagramListener, error) {
	return nil, nil
}

type streamProxy struct{ settings }

func (streamProxy) NewStreamServer() (spec.StreamProxyServer, error) { return nil, nil }
func (streamProxy) NewStreamClient() (spec.StreamProxyClient, error) { return nil, nil }

type datagramProxy struct{ settings }

func (datagramProxy) NewDatagramServer() (spec.DatagramProxyServer, error) { return nil, nil }
func (datagramProxy) NewDatagramClient() (spec.DatagramProxyClient, error) { return nil, nil }

type streamAndDatagramOverStreamProxy struct{ settings }

func (streamAndDatagramOverStreamProxy) NewStreamServer() (spec.StreamProxyServer, error) {
	return nil, nil
}
func (streamAndDatagramOverStreamProxy) NewStreamClient() (spec.StreamProxyClient, error) {
	return nil, nil
}
func (streamAndDatagramOverStreamProxy) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return nil, nil
}
func (streamAndDatagramOverStreamProxy) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return nil, nil
}

type datagramOverStreamProxy struct{ settings }

func (datagramOverStreamProxy) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return nil, nil
}
func (datagramOverStreamProxy) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return nil, nil
}

type proxyWithEveryKind struct{ settings }

func (*proxyWithEveryKind) NewStreamServer() (spec.StreamProxyServer, error) { return nil, nil }
func (*proxyWithEveryKind) NewStreamClient() (spec.StreamProxyClient, error) { return nil, nil }
func (*proxyWithEveryKind) NewDatagramOverStreamServer() (spec.DatagramOverStreamProxyServer, error) {
	return nil, nil
}
func (*proxyWithEveryKind) NewDatagramOverStreamClient() (spec.DatagramOverStreamProxyClient, error) {
	return nil, nil
}
func (*proxyWithEveryKind) NewDatagramServer() (spec.DatagramProxyServer, error) { return nil, nil }
func (*proxyWithEveryKind) NewDatagramClient() (spec.DatagramProxyClient, error) { return nil, nil }

func newBaseStream(name string) func() spec.BaseStreamTransportModule {
	return func() spec.BaseStreamTransportModule { return baseStream{settings{name}} }
}

func newOverlayStream(name string) func() spec.OverlayStreamTransportModule {
	return func() spec.OverlayStreamTransportModule { return overlayStream{settings{name}} }
}

func newBaseDatagram(name string) func() spec.BaseDatagramTransportModule {
	return func() spec.BaseDatagramTransportModule { return baseDatagram{settings{name}} }
}

func newOverlayDatagram(name string) func() spec.OverlayDatagramTransportModule {
	return func() spec.OverlayDatagramTransportModule { return overlayDatagram{settings{name}} }
}

func newStreamProxy(name string) func() spec.StreamProxyModule {
	return func() spec.StreamProxyModule { return streamProxy{settings{name}} }
}

func TestNilConstructorIsRefused(t *testing.T) {
	transports := &registry.TransportRegistry{}

	if err := transports.RegisterBaseStream(nil); err == nil {
		t.Error("a nil constructor was accepted")
	}
}

func TestNameMustBeWellFormed(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"uppercase":     "TCP",
		"leading digit": "2tcp",
		"space":         "my tcp",
		"slash":         "transport/tcp",
	}

	for label, name := range cases {
		t.Run(label, func(t *testing.T) {
			transports := &registry.TransportRegistry{}
			if err := transports.RegisterBaseStream(newBaseStream(name)); err == nil {
				t.Errorf("name %q was accepted", name)
			}
		})
	}

	transports := &registry.TransportRegistry{}
	if err := transports.RegisterBaseStream(newBaseStream("ss-2022_v1")); err != nil {
		t.Errorf("a well-formed name was refused: %v", err)
	}
}

func TestInfoMustBeConstant(t *testing.T) {
	var calls int
	varying := func() spec.BaseStreamTransportModule {
		calls++
		return baseStream{settings{name: "tcp" + strings.Repeat("x", calls)}}
	}

	transports := &registry.TransportRegistry{}
	err := transports.RegisterBaseStream(varying)
	if err == nil {
		t.Fatal("a module with a varying Info was accepted")
	}

	var inconsistent *registry.InconsistentInfoError
	if !errors.As(err, &inconsistent) {
		t.Errorf("error is %T, want *registry.InconsistentInfoError", err)
	}
}

func TestDuplicateSameKindIsRefused(t *testing.T) {
	transports := &registry.TransportRegistry{}

	if err := transports.RegisterBaseStream(newBaseStream("tcp")); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	err := transports.RegisterBaseStream(newBaseStream("tcp"))
	var duplicate *registry.DuplicateModuleError
	if !errors.As(err, &duplicate) {
		t.Fatalf("error is %v (%T), want *registry.DuplicateModuleError", err, err)
	}
}

func TestOneNameMayHoldTwoKindsOfDifferentCarrierShape(t *testing.T) {
	transports := &registry.TransportRegistry{}

	if err := transports.RegisterBaseStream(newBaseStream("quic")); err != nil {
		t.Fatalf("base stream: %v", err)
	}
	if err := transports.RegisterBaseDatagram(newBaseDatagram("quic")); err != nil {
		t.Errorf("base datagram under the same name was refused: %v", err)
	}

	overlays := &registry.TransportRegistry{}
	if err := overlays.RegisterOverlayStream(newOverlayStream("obfs")); err != nil {
		t.Fatalf("overlay stream: %v", err)
	}
	if err := overlays.RegisterOverlayDatagram(newOverlayDatagram("obfs")); err != nil {
		t.Errorf("overlay datagram under the same name was refused: %v", err)
	}
}

func TestOneNameMayNotBeBothBaseAndOverlayOfTheSameShape(t *testing.T) {
	t.Run("stream", func(t *testing.T) {
		transports := &registry.TransportRegistry{}
		if err := transports.RegisterBaseStream(newBaseStream("tls")); err != nil {
			t.Fatalf("base stream: %v", err)
		}

		err := transports.RegisterOverlayStream(newOverlayStream("tls"))
		var conflict *registry.KindConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("error is %v (%T), want *registry.KindConflictError", err, err)
		}
	})

	t.Run("datagram", func(t *testing.T) {
		transports := &registry.TransportRegistry{}
		if err := transports.RegisterBaseDatagram(newBaseDatagram("dtls")); err != nil {
			t.Fatalf("base datagram: %v", err)
		}

		err := transports.RegisterOverlayDatagram(newOverlayDatagram("dtls"))
		var conflict *registry.KindConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("error is %v (%T), want *registry.KindConflictError", err, err)
		}
	})
}

func TestAProxyFillingEveryKindIsRegisteredOnce(t *testing.T) {
	protocols := &registry.ProxyProtocolRegistry{}
	newProxy := func() spec.StreamDatagramOverStreamAndDatagramProxyModule {
		return &proxyWithEveryKind{settings{"ss2022"}}
	}

	if err := protocols.RegisterStreamDatagramOverStreamAndDatagram(newProxy); err != nil {
		t.Fatalf("register: %v", err)
	}

	for _, kind := range []registry.Kind{registry.KindStreamProxy, registry.KindDatagramOverStreamProxy, registry.KindDatagramProxy} {
		newModules, err := protocols.Lookup("ss2022", kind)
		if err != nil {
			t.Fatalf("lookup as %s: %v", kind, err)
		}
		modules := newModules()
		if modules.Stream == nil || modules.DatagramOverStream == nil || modules.Datagram == nil {
			t.Fatalf("a kind is missing: %+v", modules)
		}
		if any(modules.Config) != any(modules.Stream) || any(modules.Config) != any(modules.DatagramOverStream) || any(modules.Config) != any(modules.Datagram) {
			t.Errorf("the kinds are not one value: %+v", modules)
		}
	}

	if got := len(protocols.Modules()); got != 3 {
		t.Errorf("listed %d rows for ss2022, want one per kind", got)
	}
}

func TestAProxyNameCannotBeRegisteredAgainUnderAnyKind(t *testing.T) {
	protocols := &registry.ProxyProtocolRegistry{}
	if err := protocols.RegisterStreamAndDatagramOverStream(func() spec.StreamAndDatagramOverStreamProxyModule {
		return &streamAndDatagramOverStreamProxy{settings{"trojan"}}
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	again := map[string]error{
		"stream":               protocols.RegisterStream(newStreamProxy("trojan")),
		"datagram":             protocols.RegisterDatagram(func() spec.DatagramProxyModule { return datagramProxy{settings{"trojan"}} }),
		"datagram over stream": protocols.RegisterDatagramOverStream(func() spec.DatagramOverStreamProxyModule { return datagramOverStreamProxy{settings{"trojan"}} }),
	}
	for kind, err := range again {
		var duplicate *registry.DuplicateModuleError
		if !errors.As(err, &duplicate) {
			t.Errorf("registering trojan again as %s: got %v, want DuplicateModuleError", kind, err)
		}
	}
}

func TestLookupOfAKindTheProxyDoesNotFillNamesTheKindsItFills(t *testing.T) {
	protocols := &registry.ProxyProtocolRegistry{}
	if err := protocols.RegisterStream(newStreamProxy("plain")); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := protocols.Lookup("plain", registry.KindDatagramProxy)
	var unknown *registry.UnknownModuleError
	if !errors.As(err, &unknown) || len(unknown.Registered) != 1 || unknown.Registered[0] != registry.KindStreamProxy {
		t.Errorf("got %v, want UnknownModuleError naming the stream proxy", err)
	}
	if _, err := protocols.Lookup("absent", registry.KindStreamProxy); !errors.As(err, &unknown) || len(unknown.Registered) != 0 {
		t.Errorf("an unregistered name gave %v", err)
	}
}

func TestTransportsAndProxiesHaveSeparateNamespaces(t *testing.T) {
	modules := registry.New()

	if err := modules.Transports().RegisterBaseStream(newBaseStream("shared")); err != nil {
		t.Fatalf("transport: %v", err)
	}
	if err := modules.ProxyProtocols().RegisterStream(newStreamProxy("shared")); err != nil {
		t.Errorf("a proxy protocol collided with a transport of the same name: %v", err)
	}
}

func TestLookupRoundTrip(t *testing.T) {
	modules := registry.New()
	if err := modules.Transports().RegisterBaseStream(newBaseStream("tcp")); err != nil {
		t.Fatalf("register: %v", err)
	}

	newModule, err := modules.Transports().LookupBaseStream("tcp")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got := newModule().Info().Name; got != "tcp" {
		t.Errorf("constructed module reports %q, want tcp", got)
	}
}

// The error has to tell "no such module" apart from "right module, wrong
// position", because the second is the mistake a user actually makes.
func TestUnknownModuleErrorNamesTheKindsTheNameReallyHolds(t *testing.T) {
	modules := registry.New()
	if err := modules.Transports().RegisterOverlayStream(newOverlayStream("tls")); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := modules.Transports().LookupBaseStream("tls")
	var unknown *registry.UnknownModuleError
	if !errors.As(err, &unknown) {
		t.Fatalf("error is %T, want *registry.UnknownModuleError", err)
	}
	if len(unknown.Registered) != 1 || unknown.Registered[0] != registry.KindOverlayStream {
		t.Errorf("Registered = %v, want [overlay stream transport]", unknown.Registered)
	}
	if !strings.Contains(err.Error(), "overlay stream transport") {
		t.Errorf("message %q does not say what tls actually is", err)
	}

	_, err = modules.Transports().LookupBaseStream("nonesuch")
	if !errors.As(err, &unknown) {
		t.Fatalf("error is %T, want *registry.UnknownModuleError", err)
	}
	if len(unknown.Registered) != 0 {
		t.Errorf("Registered = %v, want empty for a name nothing holds", unknown.Registered)
	}
}

func TestDefaultRegisters(t *testing.T) {
	modules, err := registry.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	listings := append(modules.Transports().Modules(), modules.ProxyProtocols().Modules()...)
	found := make(map[string]bool, len(listings))
	for _, listing := range listings {
		found[listing.Name] = true
		if listing.Description == "" {
			t.Errorf("%s has no description", listing.Name)
		}
	}

	for _, name := range []string{"tcp", "udp", "socks5", "http"} {
		if !found[name] {
			t.Errorf("%q is not registered by Default", name)
		}
	}
}

func TestTheDefaultRegistryBuildsTrojanOnceForBothKinds(t *testing.T) {
	modules, err := registry.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	newModules, err := modules.ProxyProtocols().Lookup("trojan", registry.KindStreamProxy)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	trojan := newModules()
	if trojan.Stream == nil || trojan.DatagramOverStream == nil || trojan.Datagram != nil {
		t.Fatalf("trojan fills the wrong kinds: %+v", trojan)
	}
	if any(trojan.Stream) != any(trojan.DatagramOverStream) {
		t.Error("trojan's two kinds are two values")
	}
}

func TestTheDefaultRegistryBuildsVLESSOnceForBothKinds(t *testing.T) {
	modules, err := registry.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	newModules, err := modules.ProxyProtocols().Lookup("vless", registry.KindStreamProxy)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	vless := newModules()
	if vless.Stream == nil || vless.DatagramOverStream == nil || vless.Datagram != nil {
		t.Fatalf("vless fills the wrong kinds: %+v", vless)
	}
	if any(vless.Stream) != any(vless.DatagramOverStream) {
		t.Error("vless's two kinds are two values")
	}
}

func TestListingsAreSorted(t *testing.T) {
	modules, err := registry.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	listings := modules.Transports().Modules()
	for i := 1; i < len(listings); i++ {
		if listings[i-1].Name > listings[i].Name {
			t.Errorf("listings are not sorted: %q before %q", listings[i-1].Name, listings[i].Name)
		}
	}
}

func TestNoRegisteredModuleDefaultsAMapField(t *testing.T) {
	modules, err := registry.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	for _, listing := range modules.Transports().Modules() {
		checkNoDefaultedMap(t, listing, constructTransport(t, modules, listing))
	}
	for _, listing := range modules.ProxyProtocols().Modules() {
		checkNoDefaultedMap(t, listing, constructProxy(t, modules, listing))
	}
}

func checkNoDefaultedMap(t *testing.T, listing registry.Listing, module any) {
	t.Helper()

	value := reflect.ValueOf(module)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return
	}
	walkForMaps(t, listing, value, value.Type().Name())
}

func walkForMaps(t *testing.T, listing registry.Listing, value reflect.Value, path string) {
	t.Helper()

	switch value.Kind() {
	case reflect.Map:
		if value.Len() > 0 {
			t.Errorf("%s (%s): field %s is a map with %d default entries; encoding/json merges into it, so a user cannot remove them",
				listing.Name, listing.Kind, path, value.Len())
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			walkForMaps(t, listing, value.Field(i), path+"."+field.Name)
		}
	case reflect.Pointer:
		if !value.IsNil() {
			walkForMaps(t, listing, value.Elem(), path)
		}
	}
}

func constructTransport(t *testing.T, modules *registry.Registry, listing registry.Listing) any {
	t.Helper()

	switch listing.Kind {
	case registry.KindBaseStream:
		newModule, err := modules.Transports().LookupBaseStream(listing.Name)
		if err != nil {
			t.Fatalf("lookup %s: %v", listing.Name, err)
		}
		return newModule()
	case registry.KindOverlayStream:
		newModule, err := modules.Transports().LookupOverlayStream(listing.Name)
		if err != nil {
			t.Fatalf("lookup %s: %v", listing.Name, err)
		}
		return newModule()
	case registry.KindBaseDatagram:
		newModule, err := modules.Transports().LookupBaseDatagram(listing.Name)
		if err != nil {
			t.Fatalf("lookup %s: %v", listing.Name, err)
		}
		return newModule()
	default:
		newModule, err := modules.Transports().LookupOverlayDatagram(listing.Name)
		if err != nil {
			t.Fatalf("lookup %s: %v", listing.Name, err)
		}
		return newModule()
	}
}

func constructProxy(t *testing.T, modules *registry.Registry, listing registry.Listing) any {
	t.Helper()

	newModules, err := modules.ProxyProtocols().Lookup(listing.Name, listing.Kind)
	if err != nil {
		t.Fatalf("lookup %s: %v", listing.Name, err)
	}
	return newModules().Config
}

type snifferModule struct{ settings }

func (snifferModule) NewSniffer() (spec.Sniffer, error) { return nil, nil }

func TestASnifferIsRegisteredOnceAndLookedUpByName(t *testing.T) {
	modules := registry.New()
	newSniffer := func() spec.SnifferModule { return snifferModule{settings{name: "fake"}} }

	if err := modules.Sniffers().Register(newSniffer); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var duplicate *registry.DuplicateModuleError
	if err := modules.Sniffers().Register(newSniffer); !errors.As(err, &duplicate) {
		t.Errorf("second Register returned %v, want DuplicateModuleError", err)
	}
	if _, err := modules.Sniffers().Lookup("fake"); err != nil {
		t.Errorf("Lookup: %v", err)
	}
	if _, err := modules.Sniffers().Lookup("missing"); err == nil {
		t.Error("Lookup of an unregistered sniffer succeeded")
	}
}

func TestProxyProtocolRegistryRefusesReservedNames(t *testing.T) {
	for _, name := range []string{registry.FreedomProtocolName, registry.BlockProtocolName, registry.MixedProtocolName, registry.URLTestProtocolName, registry.SelectorProtocolName, registry.FallbackProtocolName} {
		err := registry.New().ProxyProtocols().RegisterStream(newStreamProxy(name))
		var reserved *registry.ReservedNameError
		if !errors.As(err, &reserved) {
			t.Fatalf("registering %q: got %v, want ReservedNameError", name, err)
		}
	}
}

type apiModule struct{ settings }

func (apiModule) NewAPIServer() (spec.APIServer, error) { return nil, nil }

func TestAnAPIIsRegisteredOnceListedAndLookedUpByName(t *testing.T) {
	modules := registry.New()
	newAPI := func() spec.APIModule { return apiModule{settings{name: "fake"}} }

	if err := modules.APIs().Register(newAPI); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var duplicate *registry.DuplicateModuleError
	if err := modules.APIs().Register(newAPI); !errors.As(err, &duplicate) {
		t.Errorf("second Register returned %v, want DuplicateModuleError", err)
	}
	if _, err := modules.APIs().Lookup("fake"); err != nil {
		t.Errorf("Lookup: %v", err)
	}
	if _, err := modules.APIs().Lookup("missing"); err == nil {
		t.Error("Lookup of an unregistered API succeeded")
	}
	listings := modules.APIs().Modules()
	if len(listings) != 1 || listings[0].Name != "fake" || listings[0].Kind != registry.KindAPI {
		t.Errorf("Modules = %+v, want the one fake API", listings)
	}
	if registry.KindAPI.String() != "API" {
		t.Errorf("KindAPI is %q", registry.KindAPI.String())
	}
}
