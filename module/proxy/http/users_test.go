package http_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/config/types"
	proxyhttp "github.com/we-apranik/apranik/module/proxy/http"
	"github.com/we-apranik/apranik/spec"
)

type serverWithUsers interface {
	spec.StreamProxyServer
	spec.UserManager
}

func newManagedServer(t *testing.T, users ...proxyhttp.User) serverWithUsers {
	t.Helper()
	server, err := (&proxyhttp.Proxy{Settings: proxyhttp.Settings{Users: users}}).NewStreamServer()
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

func login(t *testing.T, server spec.StreamProxyServer, username string, password types.Secret) (spec.Metadata, error) {
	t.Helper()
	clientSide, serverSide := pipe(t)

	results := make(chan accepted, 1)
	go func() {
		payload, metadata, err := server.AcceptStream(context.Background(), serverSide)
		results <- accepted{payload, metadata, err}
	}()

	client, err := (&proxyhttp.Proxy{Settings: proxyhttp.Settings{Username: username, Password: password}}).NewStreamClient()
	if err != nil {
		t.Fatal(err)
	}
	go client.RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkTCP, Destination: domainDst("example.com", 443)})

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
	_, err := login(t, server, username, password)
	if !errors.Is(err, spec.ErrAuthenticationFailed) {
		t.Errorf("%q: error = %v, want an authentication failure", username, err)
	}
}

var twoUsers = []proxyhttp.User{{Name: "alice", Password: "alice-password"}, {Name: "bob", Password: "bob-password"}}

func TestServerWithoutUsersAsksForNoLogin(t *testing.T) {
	requireLogin(t, newManagedServer(t), "", "", "")
}

func TestEveryUserLogsInWithTheirOwnPassword(t *testing.T) {
	server := newManagedServer(t, twoUsers...)

	requireLogin(t, server, "alice", "alice-password", "alice")
	requireLogin(t, server, "bob", "bob-password", "bob")
	requireRefused(t, server, "alice", "bob-password")
	requireRefused(t, server, "carol", "alice-password")
	requireRefused(t, server, "", "")
}

func TestAPasswordMayContainAColon(t *testing.T) {
	server := newManagedServer(t, proxyhttp.User{Name: "alice", Password: "se:cret"})

	requireLogin(t, server, "alice", "se:cret", "alice")
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

func TestTheFirstUserMakesAnOpenServerAskForALogin(t *testing.T) {
	server := newManagedServer(t)
	requireLogin(t, server, "", "", "")

	if _, err := server.AddUser(userJSON("alice", "alice-password")); err != nil {
		t.Fatal(err)
	}

	requireRefused(t, server, "", "")
	requireLogin(t, server, "alice", "alice-password", "alice")
}

func TestRemovingTheLastUserLeavesTheServerOpen(t *testing.T) {
	server := newManagedServer(t, proxyhttp.User{Name: "alice", Password: "alice-password"})
	requireRefused(t, server, "", "")

	if err := server.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}

	requireLogin(t, server, "", "", "")
}

func TestARefusedClientIsToldToAuthenticate(t *testing.T) {
	clientSide, serverSide := pipe(t)
	results := serverOn(t, &proxyhttp.Settings{Users: twoUsers}, serverSide)

	go clientSide.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))

	reader := bufio.NewReader(clientSide)
	status, _ := reader.ReadString('\n')
	if !strings.Contains(status, "407") {
		t.Errorf("status = %q, want 407", status)
	}
	<-results
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
		"a colon in the name":       {userJSON("ca:rol", password), spec.ErrInvalidUser},
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
		settings proxyhttp.Settings
		build    func(*proxyhttp.Proxy) error
		want     string
	}{
		"an inbound with a username": {
			proxyhttp.Settings{Username: "u", Password: "p"},
			func(p *proxyhttp.Proxy) error { _, err := p.NewStreamServer(); return err },
			"takes users, not username and password",
		},
		"an outbound with users": {
			proxyhttp.Settings{Users: twoUsers},
			func(p *proxyhttp.Proxy) error { _, err := p.NewStreamClient(); return err },
			"takes username and password, not users",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.build(&proxyhttp.Proxy{Settings: tc.settings})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to say %q", err, tc.want)
			}
		})
	}
}

func TestValidateRefusesWhatAUserCannotHave(t *testing.T) {
	const secret = "distinct-secret-value"
	for name, users := range map[string][]proxyhttp.User{
		"a colon in the name":       {{Name: "ali:ce", Password: secret}},
		"a newline in the name":     {{Name: "alice\n", Password: secret}},
		"an escape in the password": {{Name: "alice", Password: secret + "\x1b"}},
		"no name":                   {{Name: "", Password: secret}},
		"no password":               {{Name: "alice"}},
		"a repeated name":           {{Name: "alice", Password: secret}, {Name: "alice", Password: secret + "2"}},
	} {
		t.Run(name, func(t *testing.T) {
			settings := proxyhttp.Settings{Users: users}
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
	module := proxyhttp.NewProxyModule()
	decoder := json.NewDecoder(strings.NewReader(`{"users":[{"name":"alice","password":"hunter2"}]}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(module); err != nil {
		t.Fatal(err)
	}

	users := module.(*proxyhttp.Proxy).Users
	if len(users) != 1 || users[0].Name != "alice" || users[0].Password.Reveal() != "hunter2" {
		t.Errorf("decoded %+v", users)
	}
}

func TestUsersAreWarnedAboutLikeACredential(t *testing.T) {
	if got := (&proxyhttp.Settings{Users: twoUsers}).Warnings(); len(got) == 0 {
		t.Error("Basic authentication with users gave no warning about the password crossing the wire recoverably")
	}
}
