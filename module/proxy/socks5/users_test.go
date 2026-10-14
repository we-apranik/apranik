package socks5_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/config/types"
	"github.com/we-apranik/apranik/module/proxy/socks5"
	"github.com/we-apranik/apranik/spec"
)

type serverWithUsers interface {
	spec.StreamProxyServer
	spec.UserManager
}

func newManagedServer(t *testing.T, users ...socks5.User) serverWithUsers {
	t.Helper()
	proxy := &socks5.Proxy{Settings: socks5.Settings{UDPRelay: true, Users: users}}
	server, err := proxy.NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	managed, ok := server.(serverWithUsers)
	if !ok {
		t.Fatal("the server does not implement spec.UserManager")
	}
	return managed
}

func userJSON(name, password string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"name": name, "password": password})
	if err != nil {
		panic(err)
	}
	return raw
}

// login runs a CONNECT handshake against the server as a client with the
// credentials (none when the username is empty) and returns what the server
// made of it.
func login(t *testing.T, server spec.StreamProxyServer, username string, password types.Secret) (spec.Metadata, error) {
	t.Helper()
	clientSide, serverSide := pipe(t)

	results := make(chan accepted, 1)
	go func() {
		payload, metadata, err := server.AcceptStream(context.Background(), serverSide)
		results <- accepted{payload, metadata, err}
	}()

	client, err := (&socks5.Proxy{Settings: socks5.Settings{Username: username, Password: password}}).NewStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	client.RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkTCP, Destination: domainDst("example.com", 443)})

	got := <-results
	return got.meta, got.err
}

func requireLogin(t *testing.T, server spec.StreamProxyServer, username string, password types.Secret, wantUser string) {
	t.Helper()
	metadata, err := login(t, server, username, password)
	if err != nil {
		t.Fatalf("%q was refused: %v", username, err)
	}
	if metadata.User != wantUser {
		t.Errorf("User = %q, want %q", metadata.User, wantUser)
	}
}

func requireRefused(t *testing.T, server spec.StreamProxyServer, username string, password types.Secret) {
	t.Helper()
	if _, err := login(t, server, username, password); err == nil {
		t.Errorf("%q was accepted", username)
	}
}

var twoUsers = []socks5.User{{Name: "alice", Password: "alice-password"}, {Name: "bob", Password: "bob-password"}}

func TestServerWithoutUsersAsksForNoLogin(t *testing.T) {
	server := newManagedServer(t)

	requireLogin(t, server, "", "", "")
}

func TestEveryUserLogsInWithTheirOwnPassword(t *testing.T) {
	server := newManagedServer(t, twoUsers...)

	requireLogin(t, server, "alice", "alice-password", "alice")
	requireLogin(t, server, "bob", "bob-password", "bob")
	requireRefused(t, server, "alice", "bob-password")
	requireRefused(t, server, "carol", "alice-password")
}

func TestTwoUsersMayShareAPassword(t *testing.T) {
	server := newManagedServer(t, socks5.User{Name: "alice", Password: "shared"}, socks5.User{Name: "bob", Password: "shared"})

	requireLogin(t, server, "alice", "shared", "alice")
	requireLogin(t, server, "bob", "shared", "bob")
}

func TestAUserAddedWhileRunningCanLogIn(t *testing.T) {
	server := newManagedServer(t, twoUsers...)
	requireRefused(t, server, "carol", "carol-password")

	name, err := server.AddUser(userJSON("carol", "carol-password"))
	if err != nil || name != "carol" {
		t.Fatalf("AddUser = %q, %v", name, err)
	}

	requireLogin(t, server, "carol", "carol-password", "carol")
	requireLogin(t, server, "alice", "alice-password", "alice")
}

func TestARemovedUserIsRefusedAndTheOthersStay(t *testing.T) {
	server := newManagedServer(t, twoUsers...)

	if err := server.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}

	requireRefused(t, server, "alice", "alice-password")
	requireLogin(t, server, "bob", "bob-password", "bob")
}

// With no users a server asks for no login; the first user changes that.
func TestTheFirstUserMakesAnOpenServerAskForALogin(t *testing.T) {
	server := newManagedServer(t)
	requireLogin(t, server, "", "", "")

	if _, err := server.AddUser(userJSON("alice", "alice-password")); err != nil {
		t.Fatal(err)
	}

	requireRefused(t, server, "", "")
	requireLogin(t, server, "alice", "alice-password", "alice")
}

// The other direction is the documented cost of "no users, no login".
func TestRemovingTheLastUserLeavesTheServerOpen(t *testing.T) {
	server := newManagedServer(t, socks5.User{Name: "alice", Password: "alice-password"})
	requireRefused(t, server, "", "")

	if err := server.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}

	requireLogin(t, server, "", "", "")
}

func TestAddUserRefusals(t *testing.T) {
	const password = "very-secret-password"
	for name, tc := range map[string]struct {
		user json.RawMessage
		want error
	}{
		"a taken name":              {userJSON("alice", password), spec.ErrUserExists},
		"no name":                   {userJSON("", password), spec.ErrInvalidUser},
		"no password":               {userJSON("carol", ""), spec.ErrInvalidUser},
		"a name over 255 bytes":     {userJSON(strings.Repeat("a", 256), password), spec.ErrInvalidUser},
		"a password over 255 bytes": {userJSON("carol", strings.Repeat("p", 256)), spec.ErrInvalidUser},
		"a newline in the name":     {userJSON("carol\n", password), spec.ErrInvalidUser},
		"a control in the password": {userJSON("carol", "pass\x1bword"), spec.ErrInvalidUser},
		"an unknown key":            {json.RawMessage(`{"name": "carol", "pasword": "` + password + `"}`), spec.ErrInvalidUser},
		"a number for the password": {json.RawMessage(`{"name": "carol", "password": 12345}`), spec.ErrInvalidUser},
		"two objects":               {json.RawMessage(`{"name": "carol", "password": "` + password + `"} {}`), spec.ErrInvalidUser},
		"a list":                    {json.RawMessage(`[]`), spec.ErrInvalidUser},
		"nothing":                   {nil, spec.ErrInvalidUser},
	} {
		t.Run(name, func(t *testing.T) {
			server := newManagedServer(t, twoUsers...)

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
	server := newManagedServer(t)

	if _, err := server.AddUser(userJSON("سارا", "گذرواژه")); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	requireLogin(t, server, "سارا", "گذرواژه", "سارا")
}

func TestRemoveUserOfAnUnknownName(t *testing.T) {
	server := newManagedServer(t, twoUsers...)

	if err := server.RemoveUser("nobody"); !errors.Is(err, spec.ErrNoSuchUser) {
		t.Errorf("error = %v, want ErrNoSuchUser", err)
	}
}

func TestListUsersKeepsTheOrderTheyWereAddedAndIsACopy(t *testing.T) {
	server := newManagedServer(t, twoUsers...)
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
	server.ListUsers()[0] = "changed"
	if got := server.ListUsers()[0]; got != "alice" {
		t.Errorf("a caller changed the server's list: %q", got)
	}
}

func TestUsersChangeWhileClientsLogIn(t *testing.T) {
	server := newManagedServer(t, twoUsers...)

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
				if metadata, err := login(t, server, "alice", "alice-password"); err != nil || metadata.User != "alice" {
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
		settings socks5.Settings
		build    func(*socks5.Proxy) error
		want     string
	}{
		"an inbound with a username": {
			socks5.Settings{Username: "u", Password: "p"},
			func(p *socks5.Proxy) error { _, err := p.NewStreamServer(); return err },
			"takes users, not username and password",
		},
		"an outbound with users": {
			socks5.Settings{Users: twoUsers},
			func(p *socks5.Proxy) error { _, err := p.NewStreamClient(); return err },
			"takes username and password, not users",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.build(&socks5.Proxy{Settings: tc.settings})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

func TestValidateRefusesWhatAUserCannotHave(t *testing.T) {
	const secret = "distinct-secret-value"
	for name, users := range map[string][]socks5.User{
		"a newline in the name":     {{Name: "alice\n", Password: secret}},
		"an escape in the password": {{Name: "alice", Password: secret + "\x1b"}},
		"no name":                   {{Name: "", Password: secret}},
		"no password":               {{Name: "alice"}},
		"a long name":               {{Name: strings.Repeat("a", 256), Password: secret}},
		"a repeated name":           {{Name: "alice", Password: secret}, {Name: "alice", Password: secret + "2"}},
	} {
		t.Run(name, func(t *testing.T) {
			settings := socks5.Settings{Users: users}
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

func TestDecodeFillsUsers(t *testing.T) {
	module := socks5.NewProxyModule()
	decoder := json.NewDecoder(strings.NewReader(`{"users":[{"name":"alice","password":"hunter2"}]}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatal(err)
	}

	users := module.(*socks5.Proxy).Users
	if len(users) != 1 || users[0].Name != "alice" || users[0].Password.Reveal() != "hunter2" {
		t.Errorf("decoded %+v", users)
	}
}
