package trojan_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/module/proxy/trojan"
	"github.com/we-apranik/apranik/spec"
)

type serverWithUsers interface {
	spec.StreamProxyServer
	spec.DatagramOverStreamProxyServer
	spec.UserManager
}

func newManagedServer(t *testing.T, settings trojan.Settings) serverWithUsers {
	t.Helper()
	proxy := &trojan.Proxy{Settings: settings}
	streamServer, err := proxy.NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	server, ok := streamServer.(serverWithUsers)
	if !ok {
		t.Fatal("the server does not implement spec.UserManager")
	}
	return server
}

func userJSON(name, password string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"name": name, "password": password})
	if err != nil {
		panic(err)
	}
	return raw
}

// connectAs runs a CONNECT handshake as a client with the password and returns
// what the server made of it.
func connectAs(t *testing.T, server spec.StreamProxyServer, password types.Secret) (spec.Metadata, error) {
	t.Helper()
	clientSide, serverSide := pipe(t)
	upstream, err := newClient(t, password).RequestStream(context.Background(), clientSide, spec.Metadata{
		Network: spec.NetworkTCP, Destination: domainDestination("example.com", 443),
	})
	if err != nil {
		t.Fatal(err)
	}
	go upstream.Write([]byte("payload"))
	_, metadata, err := server.AcceptStream(context.Background(), newBufferedStream(serverSide))
	return metadata, err
}

func requireConnects(t *testing.T, server spec.StreamProxyServer, password types.Secret, wantUser string) {
	t.Helper()
	metadata, err := connectAs(t, server, password)
	if err != nil {
		t.Fatalf("%s was refused: %v", wantUser, err)
	}
	if metadata.User != wantUser {
		t.Errorf("User = %q, want %q", metadata.User, wantUser)
	}
}

func requireRefused(t *testing.T, server spec.StreamProxyServer, password types.Secret) {
	t.Helper()
	_, err := connectAs(t, server, password)
	if !errors.Is(err, spec.ErrAuthenticationFailed) || !errors.Is(err, spec.ErrFallbackRequested) {
		t.Errorf("error = %v, want an authentication failure that asks for the fallback", err)
	}
}

func TestAUserAddedWhileRunningCanConnect(t *testing.T) {
	server := newManagedServer(t, serverSettings)
	requireRefused(t, server, "carol-password")

	name, err := server.AddUser(userJSON("carol", "carol-password"))
	if err != nil || name != "carol" {
		t.Fatalf("AddUser = %q, %v", name, err)
	}

	requireConnects(t, server, "carol-password", "carol")
	requireConnects(t, server, "alice-password", "alice")
}

func TestARemovedUserIsRefusedAndTheOthersStay(t *testing.T) {
	server := newManagedServer(t, serverSettings)

	if err := server.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}

	requireRefused(t, server, "alice-password")
	requireConnects(t, server, "bob-password", "bob")
}

func TestOneServerSeesAChangeOnTheStreamAndTheUDPPath(t *testing.T) {
	proxy := &trojan.Proxy{Settings: serverSettings}
	streamServer, err := proxy.NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	datagramServer, err := proxy.NewDatagramOverStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := streamServer.(spec.UserManager).AddUser(userJSON("carol", "carol-password")); err != nil {
		t.Fatal(err)
	}

	clientSide, serverSide := pipe(t)
	datagram, err := newClient(t, "carol-password").RequestDatagramOverStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkUDP})
	if err != nil {
		t.Fatal(err)
	}
	go datagram.WriteTo([]byte("query"), domainDestination("dns.example", 53))

	peekable := newBufferedStream(serverSide)
	isDatagram, err := datagramServer.IsDatagramOverStream(context.Background(), peekable)
	if err != nil || !isDatagram {
		t.Fatalf("IsDatagramOverStream = %v, %v", isDatagram, err)
	}
	_, metadata, err := datagramServer.AcceptDatagramOverStream(context.Background(), peekable)
	if err != nil || metadata.User != "carol" {
		t.Errorf("a user added through the stream server was not accepted on the UDP path: %+v, %v", metadata, err)
	}
}

func TestAddUserRefusals(t *testing.T) {
	const password = "very-secret-password"
	for name, tc := range map[string]struct {
		user json.RawMessage
		want error
	}{
		"a taken name":                {userJSON("alice", password), spec.ErrUserExists},
		"a password another user has": {userJSON("carol", "alice-password"), spec.ErrInvalidUser},
		"no name":                     {userJSON("", password), spec.ErrInvalidUser},
		"no password":                 {userJSON("carol", ""), spec.ErrInvalidUser},
		"a newline in the name":       {userJSON("carol\n", password), spec.ErrInvalidUser},
		"a nul in the password":       {userJSON("carol", "pass\x00word"), spec.ErrInvalidUser},
		"an unknown key":              {json.RawMessage(`{"name": "carol", "pasword": "` + password + `"}`), spec.ErrInvalidUser},
		"a number for the password":   {json.RawMessage(`{"name": "carol", "password": 12345}`), spec.ErrInvalidUser},
		"two objects":                 {json.RawMessage(`{"name": "carol", "password": "` + password + `"} {"name": "dave"}`), spec.ErrInvalidUser},
		"a list":                      {json.RawMessage(`[{"name": "carol", "password": "` + password + `"}]`), spec.ErrInvalidUser},
		"not JSON":                    {json.RawMessage(`carol`), spec.ErrInvalidUser},
		"nothing":                     {nil, spec.ErrInvalidUser},
	} {
		t.Run(name, func(t *testing.T) {
			server := newManagedServer(t, serverSettings)

			_, err := server.AddUser(tc.user)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), password) {
				t.Errorf("the error repeats the password: %v", err)
			}
			if got := server.ListUsers(); len(got) != 2 {
				t.Errorf("a refused user changed the list: %v", got)
			}
		})
	}
}

func TestANonEnglishNameAndPasswordAreAccepted(t *testing.T) {
	server := newManagedServer(t, serverSettings)

	if _, err := server.AddUser(userJSON("سارا", "گذرواژه")); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	requireConnects(t, server, "گذرواژه", "سارا")
}

func TestRemoveUserOfAnUnknownName(t *testing.T) {
	server := newManagedServer(t, serverSettings)

	if err := server.RemoveUser("nobody"); !errors.Is(err, spec.ErrNoSuchUser) {
		t.Errorf("error = %v, want ErrNoSuchUser", err)
	}
	if got := server.ListUsers(); len(got) != 2 {
		t.Errorf("the list changed: %v", got)
	}
}

func TestListUsersKeepsTheOrderTheyWereAdded(t *testing.T) {
	server := newManagedServer(t, serverSettings)
	for _, name := range []string{"carol", "dave"} {
		if _, err := server.AddUser(userJSON(name, name+"-password")); err != nil {
			t.Fatal(err)
		}
	}
	if err := server.RemoveUser("bob"); err != nil {
		t.Fatal(err)
	}

	if got, want := strings.Join(server.ListUsers(), ","), "alice,carol,dave"; got != want {
		t.Errorf("ListUsers = %s, want %s", got, want)
	}
}

func TestTheListIsACopy(t *testing.T) {
	server := newManagedServer(t, serverSettings)

	server.ListUsers()[0] = "changed"

	if got := server.ListUsers()[0]; got != "alice" {
		t.Errorf("a caller changed the server's list: %q", got)
	}
}

func TestRemovingTheLastUserRefusesEveryoneUntilOneIsAdded(t *testing.T) {
	server := newManagedServer(t, serverSettings)
	for _, name := range []string{"alice", "bob"} {
		if err := server.RemoveUser(name); err != nil {
			t.Fatal(err)
		}
	}

	requireRefused(t, server, "alice-password")
	if got := server.ListUsers(); len(got) != 0 {
		t.Errorf("ListUsers = %v", got)
	}

	if _, err := server.AddUser(userJSON("alice", "new-password")); err != nil {
		t.Fatal(err)
	}
	requireConnects(t, server, "new-password", "alice")
	requireRefused(t, server, "alice-password")
}

func TestUsersChangeWhileClientsConnect(t *testing.T) {
	server := newManagedServer(t, serverSettings)

	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 25 {
				name := fmt.Sprintf("worker-%d-%d", worker, round)
				if _, err := server.AddUser(userJSON(name, name+"-password")); err != nil {
					t.Errorf("AddUser(%s): %v", name, err)
					return
				}
				server.ListUsers()
				if err := server.RemoveUser(name); err != nil {
					t.Errorf("RemoveUser(%s): %v", name, err)
					return
				}
			}
		}()
	}
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				if metadata, err := connectAs(t, server, "alice-password"); err != nil || metadata.User != "alice" {
					t.Errorf("alice: %+v, %v", metadata, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if got := server.ListUsers(); len(got) != 2 {
		t.Errorf("ListUsers = %v, want the two users the server began with", got)
	}
}

func TestEachRoleRefusesTheOtherRolesSetting(t *testing.T) {
	for name, tc := range map[string]struct {
		settings trojan.Settings
		build    func(*trojan.Proxy) error
		want     string
	}{
		"an inbound with a password": {
			trojan.Settings{Password: "p", Users: serverSettings.Users},
			func(p *trojan.Proxy) error { _, err := p.NewStreamServer(); return err },
			"takes users, not password",
		},
		"an inbound with no users": {
			trojan.Settings{},
			func(p *trojan.Proxy) error { _, err := p.NewStreamServer(); return err },
			"needs users",
		},
		"an outbound with users": {
			trojan.Settings{Password: "p", Users: serverSettings.Users},
			func(p *trojan.Proxy) error { _, err := p.NewStreamClient(); return err },
			"takes a password, not users",
		},
		"an outbound with no password": {
			trojan.Settings{},
			func(p *trojan.Proxy) error { _, err := p.NewStreamClient(); return err },
			"needs a password",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.build(&trojan.Proxy{Settings: tc.settings})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

func TestValidateRefusesWhatAUserCannotHave(t *testing.T) {
	const secret = "distinct-secret-value"
	for name, user := range map[string]trojan.User{
		"a newline in the name":     {Name: "alice\n", Password: secret},
		"an escape in the password": {Name: "alice", Password: secret + "\x1b[31m"},
		"no name":                   {Name: "", Password: secret},
		"no password":               {Name: "alice"},
	} {
		t.Run(name, func(t *testing.T) {
			settings := trojan.Settings{Users: []trojan.User{user}}
			err := settings.Validate()
			if err == nil {
				t.Fatal("Validate accepted it")
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("the error repeats the password: %q", err)
			}
		})
	}
}
