package mixed_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/The-Apranik/Apranik/core/component"
	"github.com/The-Apranik/Apranik/core/inbound/mixed"
	"github.com/The-Apranik/Apranik/spec"
)

// ---------- fan-out, with servers that record what they are asked ----------

type managedFake struct {
	mu        sync.Mutex
	names     []string
	addErr    error
	removeErr error
	calls     []string
}

func (*managedFake) AcceptStream(context.Context, spec.Stream) (spec.Stream, spec.Metadata, error) {
	return nil, spec.Metadata{}, errors.New("not used")
}

func (f *managedFake) AddUser(user json.RawMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var parsed struct{ Name string }
	if err := json.Unmarshal(user, &parsed); err != nil {
		return "", fmt.Errorf("%w: %w", spec.ErrInvalidUser, err)
	}
	f.calls = append(f.calls, "add "+parsed.Name)
	if f.addErr != nil {
		return "", f.addErr
	}
	f.names = append(f.names, parsed.Name)
	return parsed.Name, nil
}

func (f *managedFake) RemoveUser(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "remove "+name)
	if f.removeErr != nil {
		return f.removeErr
	}
	for index, existing := range f.names {
		if existing == name {
			f.names = append(f.names[:index:index], f.names[index+1:]...)
			return nil
		}
	}
	return spec.ErrNoSuchUser
}

func (f *managedFake) ListUsers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.names...)
}

func (f *managedFake) history() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, ",")
}

// unmanagedFake can serve a protocol but cannot change its users.
type unmanagedFake struct{}

func (unmanagedFake) AcceptStream(context.Context, spec.Stream) (spec.Stream, spec.Metadata, error) {
	return nil, spec.Metadata{}, errors.New("not used")
}

func newManager(t *testing.T, socks5Server, httpServer spec.StreamProxyServer) spec.UserManager {
	t.Helper()
	manager, ok := mixed.NewStreamServer(socks5Server, httpServer, 0).(spec.UserManager)
	if !ok {
		t.Fatal("the mixed server does not implement spec.UserManager")
	}
	return manager
}

func TestMixedCannotChangeUsersUnlessBothProtocolsCan(t *testing.T) {
	for name, servers := range map[string][2]spec.StreamProxyServer{
		"neither":     {unmanagedFake{}, unmanagedFake{}},
		"only socks5": {&managedFake{}, unmanagedFake{}},
		"only http":   {unmanagedFake{}, &managedFake{}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := mixed.NewStreamServer(servers[0], servers[1], 0).(spec.UserManager); ok {
				t.Error("the mixed server claims to manage users, but a half cannot")
			}
		})
	}
}

func TestMixedAddsAUserToBothProtocolsInOrder(t *testing.T) {
	socks5Server, httpServer := &managedFake{}, &managedFake{}
	manager := newManager(t, socks5Server, httpServer)

	name, err := manager.AddUser(json.RawMessage(`{"name": "alice"}`))
	if err != nil || name != "alice" {
		t.Fatalf("AddUser = %q, %v", name, err)
	}

	if socks5Server.history() != "add alice" || httpServer.history() != "add alice" {
		t.Errorf("socks5 saw %q and http saw %q", socks5Server.history(), httpServer.history())
	}
	if got := manager.ListUsers(); len(got) != 1 || got[0] != "alice" {
		t.Errorf("ListUsers = %v", got)
	}
}

func TestMixedUndoesTheFirstAddWhenTheSecondProtocolRefusesTheUser(t *testing.T) {
	socks5Server := &managedFake{}
	httpServer := &managedFake{addErr: fmt.Errorf("%w: http cannot carry this name", spec.ErrInvalidUser)}
	manager := newManager(t, socks5Server, httpServer)

	_, err := manager.AddUser(json.RawMessage(`{"name": "a:b"}`))

	if !errors.Is(err, spec.ErrInvalidUser) {
		t.Fatalf("error = %v, want ErrInvalidUser", err)
	}
	if !strings.Contains(err.Error(), "http") {
		t.Errorf("the error does not name the protocol that refused: %v", err)
	}
	if got := socks5Server.ListUsers(); len(got) != 0 {
		t.Errorf("socks5 kept a user http refused: %v", got)
	}
	if socks5Server.history() != "add a:b,remove a:b" {
		t.Errorf("socks5 history = %q, want the add undone", socks5Server.history())
	}
}

func TestMixedStopsAtTheFirstProtocolThatRefusesAUser(t *testing.T) {
	socks5Server := &managedFake{addErr: spec.ErrUserExists}
	httpServer := &managedFake{}
	manager := newManager(t, socks5Server, httpServer)

	_, err := manager.AddUser(json.RawMessage(`{"name": "alice"}`))

	if !errors.Is(err, spec.ErrUserExists) {
		t.Fatalf("error = %v, want ErrUserExists", err)
	}
	if httpServer.history() != "" {
		t.Errorf("http was asked after socks5 refused: %q", httpServer.history())
	}
}

func TestMixedRemovesAUserFromBothProtocols(t *testing.T) {
	socks5Server, httpServer := &managedFake{names: []string{"alice"}}, &managedFake{names: []string{"alice"}}
	manager := newManager(t, socks5Server, httpServer)

	if err := manager.RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}

	if len(socks5Server.ListUsers()) != 0 || len(httpServer.ListUsers()) != 0 {
		t.Errorf("socks5 has %v and http has %v", socks5Server.ListUsers(), httpServer.ListUsers())
	}
}

func TestMixedRemovalOfAnUnknownUserChangesNothing(t *testing.T) {
	socks5Server, httpServer := &managedFake{names: []string{"alice"}}, &managedFake{names: []string{"alice"}}
	manager := newManager(t, socks5Server, httpServer)

	if err := manager.RemoveUser("nobody"); !errors.Is(err, spec.ErrNoSuchUser) {
		t.Errorf("error = %v, want ErrNoSuchUser", err)
	}
	if httpServer.history() != "" {
		t.Errorf("http was asked to remove a user socks5 does not have: %q", httpServer.history())
	}
}

func TestMixedRemovalHealsAUserOnlySocks5HadAlready(t *testing.T) {
	socks5Server, httpServer := &managedFake{names: []string{"alice"}}, &managedFake{}
	manager := newManager(t, socks5Server, httpServer)

	if err := manager.RemoveUser("alice"); err != nil {
		t.Errorf("error = %v, want the removal to succeed: http already lacked the user", err)
	}
}

func TestMixedChangesOneUserAtATime(t *testing.T) {
	socks5Server, httpServer := &managedFake{}, &managedFake{}
	manager := newManager(t, socks5Server, httpServer)

	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 25 {
				user := json.RawMessage(fmt.Sprintf(`{"name": "worker-%d-%d"}`, worker, round))
				name, err := manager.AddUser(user)
				if err != nil {
					t.Errorf("AddUser: %v", err)
					return
				}
				manager.ListUsers()
				if err := manager.RemoveUser(name); err != nil {
					t.Errorf("RemoveUser: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if len(socks5Server.ListUsers()) != 0 || len(httpServer.ListUsers()) != 0 {
		t.Errorf("the lists differ or kept users: socks5 %v, http %v", socks5Server.ListUsers(), httpServer.ListUsers())
	}
}

// ---------- the real protocols behind a real inbound ----------

func userManagerOf(t *testing.T, in component.Inbound) spec.UserManager {
	t.Helper()
	managing, ok := in.(component.UserManagingInbound)
	if !ok {
		t.Fatal("the inbound does not implement component.UserManagingInbound")
	}
	manager := managing.UserManager()
	if manager == nil {
		t.Fatal("the inbound has no user manager")
	}
	return manager
}

// socks5Login logs in over SOCKS5 and, when accepted, asks to CONNECT.
func socks5Login(t *testing.T, address net.Addr, username, password string) bool {
	t.Helper()
	connection := dialMixed(t, address)
	connection.Write([]byte{0x05, 0x01, 0x02})
	if method := readExactly(t, connection, 2); method[1] != 0x02 {
		return false
	}
	credentials := append([]byte{0x01, byte(len(username))}, username...)
	credentials = append(append(credentials, byte(len(password))), password...)
	connection.Write(credentials)
	if status := readExactly(t, connection, 2); status[1] != 0x00 {
		return false
	}
	connection.Write(append([]byte{0x05, 0x01, 0x00, 0x03, byte(len("example.com"))}, append([]byte("example.com"), 0x01, 0xbb)...))
	return true
}

// httpLogin sends a CONNECT with the credentials and reports the status code.
func httpLogin(t *testing.T, address net.Addr, username, password string) bool {
	t.Helper()
	connection := dialMixed(t, address)
	authorization := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	connection.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"))
	status, err := bufio.NewReader(connection).ReadString('\n')
	return err == nil && strings.Contains(status, " 200 ")
}

func TestAUserLogsInOverBothProtocolsAndReachesTheHandlerByName(t *testing.T) {
	address, handler, _ := startMixedInboundReturningIt(t, mixedInboundOptions{users: []json.RawMessage{json.RawMessage(`{"name": "alice", "password": "secret"}`)}})

	if !socks5Login(t, address, "alice", "secret") {
		t.Fatal("alice was refused over socks5")
	}
	if got := awaitDispatch(t, handler); got.metadata.User != "alice" {
		t.Errorf("socks5: User = %q, want alice", got.metadata.User)
	}

	if !httpLogin(t, address, "alice", "secret") {
		t.Fatal("alice was refused over http")
	}
	if got := awaitDispatch(t, handler); got.metadata.User != "alice" {
		t.Errorf("http: User = %q, want alice", got.metadata.User)
	}

	if socks5Login(t, address, "alice", "wrong") || httpLogin(t, address, "alice", "wrong") {
		t.Error("a wrong password was accepted")
	}
}

func TestAUserAddedToTheRunningInboundLogsInOverBothProtocols(t *testing.T) {
	address, _, in := startMixedInboundReturningIt(t, mixedInboundOptions{users: []json.RawMessage{json.RawMessage(`{"name": "alice", "password": "secret"}`)}})
	manager := userManagerOf(t, in)
	if socks5Login(t, address, "bob", "bob-password") || httpLogin(t, address, "bob", "bob-password") {
		t.Fatal("bob logged in before he was added")
	}

	if _, err := manager.AddUser(json.RawMessage(`{"name": "bob", "password": "bob-password"}`)); err != nil {
		t.Fatal(err)
	}

	if !socks5Login(t, address, "bob", "bob-password") || !httpLogin(t, address, "bob", "bob-password") {
		t.Error("bob was refused over a protocol after he was added")
	}
	if got := strings.Join(manager.ListUsers(), ","); got != "alice,bob" {
		t.Errorf("ListUsers = %s", got)
	}
}

func TestARemovedUserIsRefusedOverBothProtocols(t *testing.T) {
	address, _, in := startMixedInboundReturningIt(t, mixedInboundOptions{users: []json.RawMessage{
		json.RawMessage(`{"name": "alice", "password": "secret"}`),
		json.RawMessage(`{"name": "bob", "password": "bob-password"}`),
	}})

	if err := userManagerOf(t, in).RemoveUser("alice"); err != nil {
		t.Fatal(err)
	}

	if socks5Login(t, address, "alice", "secret") || httpLogin(t, address, "alice", "secret") {
		t.Error("a removed user logged in")
	}
	if !socks5Login(t, address, "bob", "bob-password") {
		t.Error("another user was refused after a removal")
	}
}

// A colon is legal for SOCKS5 and not for Basic authentication, and a user is
// valid only if both protocols take it.
func TestAUserHTTPCannotCarryIsRefusedAndLeftInNeitherProtocol(t *testing.T) {
	address, _, in := startMixedInboundReturningIt(t, mixedInboundOptions{users: []json.RawMessage{json.RawMessage(`{"name": "alice", "password": "secret"}`)}})
	manager := userManagerOf(t, in)

	_, err := manager.AddUser(json.RawMessage(`{"name": "a:b", "password": "secret"}`))

	if !errors.Is(err, spec.ErrInvalidUser) || !strings.Contains(err.Error(), "http") {
		t.Fatalf("error = %v, want ErrInvalidUser naming http", err)
	}
	if got := manager.ListUsers(); len(got) != 1 {
		t.Errorf("ListUsers = %v", got)
	}
	if socks5Login(t, address, "a:b", "secret") {
		t.Error("socks5 kept a user http refused")
	}
}
