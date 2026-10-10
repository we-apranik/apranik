package builder_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/The-Apranik/Apranik/core/blueprint"
	"github.com/The-Apranik/Apranik/core/builder"
	"github.com/The-Apranik/Apranik/core/component"
	proxyhttp "github.com/The-Apranik/Apranik/module/proxy/http"
	"github.com/The-Apranik/Apranik/module/proxy/socks5"
	"github.com/The-Apranik/Apranik/module/proxy/trojan"
	"github.com/The-Apranik/Apranik/module/transport/base/tcp"
	"github.com/The-Apranik/Apranik/spec"
)

func mixedWithUsers(users ...string) blueprint.Inbound {
	raw := make([]json.RawMessage, len(users))
	for index, user := range users {
		raw[index] = json.RawMessage(user)
	}
	return blueprint.Inbound{
		Tag:             "mixed-in",
		Bind:            endpoint("127.0.0.1", 0),
		Network:         spec.NetworkTCP,
		StreamTransport: blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()},
		Mixed:           &blueprint.Mixed{SOCKS5: socks5.NewProxyModule(), HTTP: proxyhttp.NewProxyModule(), Users: raw},
	}
}

func newBuilder(t *testing.T) *builder.Builder {
	t.Helper()
	built, err := builder.New(blueprint.Log{})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}
	t.Cleanup(func() { built.Close() })
	return built
}

func userManagerOf(t *testing.T, in component.Inbound) spec.UserManager {
	t.Helper()
	managing, ok := in.(component.UserManagingInbound)
	if !ok {
		t.Fatalf("%T does not implement component.UserManagingInbound", in)
	}
	return managing.UserManager()
}

func TestAMixedInboundIsBuiltWithItsUsers(t *testing.T) {
	in, err := newBuilder(t).BuildInbound(mixedWithUsers(
		`{"name": "alice", "password": "a"}`,
		`{"name": "bob", "password": "b"}`,
	))
	if err != nil {
		t.Fatalf("BuildInbound: %v", err)
	}
	defer in.Close()

	manager := userManagerOf(t, in)
	if manager == nil {
		t.Fatal("a mixed inbound has no user manager")
	}
	if got := strings.Join(manager.ListUsers(), ","); got != "alice,bob" {
		t.Errorf("ListUsers = %s, want alice,bob in the order they were written", got)
	}
}

func TestAMixedUserOneProtocolRefusesNamesTheEntryAndTheProtocol(t *testing.T) {
	_, err := newBuilder(t).BuildInbound(mixedWithUsers(
		`{"name": "alice", "password": "a"}`,
		`{"name": "a:b", "password": "b"}`,
	))

	if err == nil {
		t.Fatal("a user HTTP cannot carry was accepted")
	}
	for _, want := range []string{"mixed", "users[1]", "http"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	if !errors.Is(err, spec.ErrInvalidUser) {
		t.Errorf("error = %v, want it to wrap ErrInvalidUser", err)
	}
}

func TestACheckOfAMixedInboundSeesTheSameRefusal(t *testing.T) {
	err := newBuilder(t).CheckInbound(mixedWithUsers(`{"name": "a:b", "password": "b"}`))

	if err == nil || !errors.Is(err, spec.ErrInvalidUser) {
		t.Errorf("CheckInbound = %v, want a refusal wrapping ErrInvalidUser", err)
	}
}

func TestAMixedInboundRefusesTheSameNameTwice(t *testing.T) {
	_, err := newBuilder(t).BuildInbound(mixedWithUsers(
		`{"name": "alice", "password": "a"}`,
		`{"name": "alice", "password": "b"}`,
	))

	if !errors.Is(err, spec.ErrUserExists) {
		t.Errorf("error = %v, want ErrUserExists", err)
	}
}

func TestUsersWrittenUnderAHalfOfMixedAreRefused(t *testing.T) {
	for name, halves := range map[string]*blueprint.Mixed{
		"socks5": {
			SOCKS5: &socks5.Proxy{Settings: socks5.Settings{UDPRelay: true, Users: []socks5.User{{Name: "alice", Password: "a"}}}},
			HTTP:   proxyhttp.NewProxyModule(),
		},
		"http": {
			SOCKS5: socks5.NewProxyModule(),
			HTTP:   &proxyhttp.Proxy{Settings: proxyhttp.Settings{Users: []proxyhttp.User{{Name: "alice", Password: "a"}}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			inbound := mixedWithUsers()
			inbound.Mixed = halves

			_, err := newBuilder(t).BuildInbound(inbound)

			if err == nil || !strings.Contains(err.Error(), "belong to mixed, not under "+name) {
				t.Errorf("error = %v, want it to say the users belong to mixed", err)
			}
		})
	}
}

func TestAMixedInboundWithoutUsersAsksForNoLogin(t *testing.T) {
	in, err := newBuilder(t).BuildInbound(mixedWithUsers())
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	if got := userManagerOf(t, in).ListUsers(); len(got) != 0 {
		t.Errorf("ListUsers = %v", got)
	}
}

func TestATrojanInboundChangesItsUsersForTheStreamAndTheUDPPathAtOnce(t *testing.T) {
	proxy := &trojan.Proxy{Settings: trojan.Settings{Users: []trojan.User{{Name: "alice", Password: "a"}}}}
	in, err := newBuilder(t).BuildInbound(blueprint.Inbound{
		Tag:                     "trojan-in",
		Bind:                    endpoint("127.0.0.1", 0),
		Network:                 spec.NetworkTCP,
		StreamTransport:         blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()},
		StreamProxy:             proxy,
		DatagramOverStreamProxy: proxy,
	})
	if err != nil {
		t.Fatalf("BuildInbound: %v", err)
	}
	defer in.Close()

	manager := userManagerOf(t, in)
	if manager == nil {
		t.Fatal("a trojan inbound has no user manager")
	}
	if _, err := manager.AddUser(json.RawMessage(`{"name": "bob", "password": "b"}`)); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(manager.ListUsers(), ","); got != "alice,bob" {
		t.Errorf("ListUsers = %s", got)
	}
}

func TestAnInboundWhoseProtocolCannotChangeUsersHasNoManager(t *testing.T) {
	in, err := newBuilder(t).BuildInbound(blueprint.Inbound{
		Tag:             "plain-in",
		Bind:            endpoint("127.0.0.1", 0),
		Network:         spec.NetworkTCP,
		StreamTransport: blueprint.StreamTransportStack{Base: tcp.NewBaseStreamTransportModule()},
		StreamProxy:     passthroughProxy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()

	if manager := userManagerOf(t, in); manager != nil {
		t.Errorf("UserManager = %T, want nil", manager)
	}
}
