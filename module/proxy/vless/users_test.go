package vless_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/we-apranik/apranik/module/proxy/vless"
	"github.com/we-apranik/apranik/spec"
)

func userManager(t *testing.T) (spec.UserManager, spec.StreamProxyServer) {
	t.Helper()
	streamServer, err := (&vless.Proxy{Settings: serverSettings}).NewStreamServer()
	if err != nil {
		t.Fatal(err)
	}
	manager, ok := streamServer.(spec.UserManager)
	if !ok {
		t.Fatal("the server does not implement spec.UserManager")
	}
	return manager, streamServer
}

const carolUUID = "7e2e5f6a-96f8-4f0b-bf5e-6c2a5f7e1d33"

func TestAUserAddedWhileRunningCanConnectAtOnce(t *testing.T) {
	manager, server := userManager(t)
	name, err := manager.AddUser(json.RawMessage(`{"name": "carol", "uuid": "` + carolUUID + `"}`))
	if err != nil || name != "carol" {
		t.Fatalf("AddUser = %q, %v", name, err)
	}

	clientSide, serverSide := pipe(t)
	go func() {
		upstream, _ := newClient(t, carolUUID).RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkTCP, Destination: domainDestination("example.com", 443)})
		upstream.Write([]byte("hi"))
	}()
	_, metadata, err := server.AcceptStream(context.Background(), serverSide)
	if err != nil || metadata.User != "carol" {
		t.Errorf("accept = %+v, %v, want carol", metadata, err)
	}
}

func TestARemovedUserIsRefusedAtOnceAndTheOthersStay(t *testing.T) {
	manager, server := userManager(t)
	if err := manager.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}
	if got := manager.ListUsers(); len(got) != 1 || got[0] != "bob" {
		t.Errorf("ListUsers = %v, want [bob]", got)
	}

	clientSide, serverSide := pipe(t)
	go func() {
		upstream, _ := newClient(t, aliceUUID).RequestStream(context.Background(), clientSide, spec.Metadata{Network: spec.NetworkTCP, Destination: domainDestination("example.com", 443)})
		upstream.Write([]byte("hi"))
	}()
	if _, _, err := server.AcceptStream(context.Background(), serverSide); !errors.Is(err, spec.ErrAuthenticationFailed) {
		t.Errorf("a removed user's request: error = %v, want spec.ErrAuthenticationFailed", err)
	}
}

func TestRemovingTheLastUserLeavesTheInboundRefusingEveryone(t *testing.T) {
	manager, _ := userManager(t)
	manager.RemoveUser("alice")
	if err := manager.RemoveUser("bob"); err != nil {
		t.Fatal(err)
	}
	if got := manager.ListUsers(); len(got) != 0 {
		t.Errorf("ListUsers = %v, want none", got)
	}
}

func TestAddUserRefusesWhatIsNotAUser(t *testing.T) {
	manager, _ := userManager(t)
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"existing name", `{"name": "alice", "uuid": "` + carolUUID + `"}`, spec.ErrUserExists},
		{"existing uuid", `{"name": "carol", "uuid": "` + aliceUUID + `"}`, spec.ErrInvalidUser},
		{"existing uuid spelled differently", `{"name": "carol", "uuid": "` + strings.ToUpper(aliceUUID) + `"}`, spec.ErrInvalidUser},
		{"no name", `{"uuid": "` + carolUUID + `"}`, spec.ErrInvalidUser},
		{"no uuid", `{"name": "carol"}`, spec.ErrInvalidUser},
		{"not a uuid", `{"name": "carol", "uuid": "` + strings.Repeat("x", 31) + `"}`, spec.ErrInvalidUser},
		{"control character in the name", `{"name": "car\nol", "uuid": "` + carolUUID + `"}`, spec.ErrInvalidUser},
		{"unknown key", `{"name": "carol", "uuid": "` + carolUUID + `", "flow": "xtls-rprx-vision"}`, spec.ErrInvalidUser},
		{"two values", `{"name": "carol", "uuid": "` + carolUUID + `"} {}`, spec.ErrInvalidUser},
		{"not an object", `"carol"`, spec.ErrInvalidUser},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := manager.AddUser(json.RawMessage(test.raw)); !errors.Is(err, test.want) {
				t.Errorf("error = %v, want one wrapping %v", err, test.want)
			}
		})
	}
	if got := manager.ListUsers(); len(got) != 2 {
		t.Errorf("ListUsers = %v, want the refused users not added", got)
	}
}

func TestAnErrorFromAddUserNeverHoldsTheUUID(t *testing.T) {
	manager, _ := userManager(t)
	for _, raw := range []string{
		`{"name": "alice", "uuid": "` + aliceUUID + `"}`,
		`{"name": "carol", "uuid": "` + aliceUUID + `"}`,
		`{"name": "carol", "uuid": "` + strings.Repeat("x", 31) + `"}`,
		`{"name": "carol", "uuid": "` + carolUUID + `", "password": "` + carolUUID + `"}`,
	} {
		_, err := manager.AddUser(json.RawMessage(raw))
		if err == nil {
			t.Fatalf("%s was added", raw)
		}
		for _, secret := range []string{aliceUUID, carolUUID, strings.Repeat("x", 31)} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q holds a UUID", err)
			}
		}
	}
}

func TestRemoveUserRefusesAnUnknownName(t *testing.T) {
	manager, _ := userManager(t)
	if err := manager.RemoveUser("nobody"); !errors.Is(err, spec.ErrNoSuchUser) {
		t.Errorf("error = %v, want spec.ErrNoSuchUser", err)
	}
}

func TestUsersChangeWhileHandshakesRun(t *testing.T) {
	manager, _ := userManager(t)
	var group sync.WaitGroup
	for index := range 20 {
		group.Add(2)
		go func() {
			defer group.Done()
			name := "user" + string(rune('a'+index))
			manager.AddUser(json.RawMessage(`{"name": "` + name + `", "uuid": "` + name + `"}`))
			manager.RemoveUser(name)
		}()
		go func() {
			defer group.Done()
			manager.ListUsers()
		}()
	}
	group.Wait()
}
