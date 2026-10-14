package apicoreprovider_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/we-apranik/apranik/core/apicoreprovider"
	"github.com/we-apranik/apranik/core/blueprint"
	"github.com/we-apranik/apranik/core/component"
	"github.com/we-apranik/apranik/core/logging"
	"github.com/we-apranik/apranik/core/stats"
	"github.com/we-apranik/apranik/spec"
)

type namedUsers struct {
	mu    sync.Mutex
	names []string
}

func (u *namedUsers) AddUser(user json.RawMessage) (string, error) {
	var parsed struct{ Name string }
	if err := json.Unmarshal(user, &parsed); err != nil {
		return "", fmt.Errorf("%w: %w", spec.ErrInvalidUser, err)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, existing := range u.names {
		if existing == parsed.Name {
			return "", fmt.Errorf("a user called %q: %w", parsed.Name, spec.ErrUserExists)
		}
	}
	u.names = append(u.names, parsed.Name)
	return parsed.Name, nil
}

func (u *namedUsers) RemoveUser(name string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	for index, existing := range u.names {
		if existing == name {
			u.names = append(u.names[:index:index], u.names[index+1:]...)
			return nil
		}
	}
	return spec.ErrNoSuchUser
}

func (u *namedUsers) ListUsers() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.names...)
}

type inboundBase struct{ tag string }

func (i inboundBase) Tag() string                                            { return i.tag }
func (inboundBase) Activate(context.Context, component.TrafficHandler) error { return nil }
func (inboundBase) Close() error                                             { return nil }

type managingInbound struct {
	inboundBase
	users spec.UserManager
}

func (i managingInbound) UserManager() spec.UserManager { return i.users }

type usersSetup struct {
	provider      *apicoreprovider.Provider
	registry      *stats.Registry
	first, second *namedUsers
	logs          *logging.Subscription
}

func newUsersSetup(t *testing.T, trackConnections bool) *usersSetup {
	t.Helper()
	first, second := &namedUsers{}, &namedUsers{}
	system, err := logging.New(logging.Config{Level: logging.LevelInfo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { system.Close() })
	logs, err := system.SubscribeToLog(logging.LevelInfo, 64)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logs.Cancel)

	registry := stats.NewRegistry(stats.Config{InboundTags: []string{"first", "second"}, TrackLiveConversations: trackConnections})
	provider, err := apicoreprovider.New(apicoreprovider.RunningCore{
		Blueprint: &blueprint.Blueprint{Inbounds: []blueprint.Inbound{{Tag: "first"}, {Tag: "second"}, {Tag: "plain"}, {Tag: "empty"}}},
		Inbounds: []component.Inbound{
			managingInbound{inboundBase{"first"}, first},
			managingInbound{inboundBase{"second"}, second},
			inboundBase{"plain"},
			managingInbound{inboundBase{"empty"}, nil},
		},
		Stats:  registry,
		Logger: system.Logger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &usersSetup{provider: provider, registry: registry, first: first, second: second, logs: logs}
}

func userObject(name, password string) json.RawMessage {
	raw, err := json.Marshal(map[string]string{"name": name, "password": password})
	if err != nil {
		panic(err)
	}
	return raw
}

func TestUsersAreAddedListedAndRemovedOnTheInboundNamed(t *testing.T) {
	s := newUsersSetup(t, true)

	for _, name := range []string{"alice", "bob"} {
		got, err := s.provider.AddInboundUser("first", userObject(name, "secret"))
		if err != nil || got != name {
			t.Fatalf("AddInboundUser(%s) = %q, %v", name, got, err)
		}
	}
	if _, err := s.provider.AddInboundUser("second", userObject("carol", "secret")); err != nil {
		t.Fatal(err)
	}

	if got, err := s.provider.ListInboundUsers("first"); err != nil || strings.Join(got, ",") != "alice,bob" {
		t.Errorf("ListInboundUsers(first) = %v, %v", got, err)
	}
	if got, _ := s.provider.ListInboundUsers("second"); strings.Join(got, ",") != "carol" {
		t.Errorf("ListInboundUsers(second) = %v: a user leaked between inbounds", got)
	}

	if err := s.provider.RemoveInboundUser("first", "alice"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.provider.ListInboundUsers("first"); strings.Join(got, ",") != "bob" {
		t.Errorf("ListInboundUsers(first) = %v after removing alice", got)
	}
}

func TestAnInboundThatDoesNotManageUsersIsNoSuchInboundWithAReason(t *testing.T) {
	s := newUsersSetup(t, true)

	for tag, wantReason := range map[string]string{
		"nowhere": `there is no inbound "nowhere"`,
		"plain":   `inbound "plain" does not manage users`,
		"empty":   `inbound "empty" does not manage users`,
	} {
		_, listErr := s.provider.ListInboundUsers(tag)
		_, addErr := s.provider.AddInboundUser(tag, userObject("alice", "secret"))
		removeErr := s.provider.RemoveInboundUser(tag, "alice")
		for operation, err := range map[string]error{"list": listErr, "add": addErr, "remove": removeErr} {
			if !errors.Is(err, spec.ErrAPINoSuchInbound) || !strings.Contains(err.Error(), wantReason) {
				t.Errorf("%s on %q = %v, want ErrAPINoSuchInbound saying %q", operation, tag, err, wantReason)
			}
		}
	}
}

func TestTheProtocolsOwnErrorsComeThroughUnchanged(t *testing.T) {
	s := newUsersSetup(t, true)
	if _, err := s.provider.AddInboundUser("first", userObject("alice", "secret")); err != nil {
		t.Fatal(err)
	}

	if _, err := s.provider.AddInboundUser("first", userObject("alice", "secret")); !errors.Is(err, spec.ErrUserExists) {
		t.Errorf("a taken name = %v, want ErrUserExists", err)
	}
	if _, err := s.provider.AddInboundUser("first", json.RawMessage(`not json`)); !errors.Is(err, spec.ErrInvalidUser) {
		t.Errorf("a bad object = %v, want ErrInvalidUser", err)
	}
	if err := s.provider.RemoveInboundUser("first", "nobody"); !errors.Is(err, spec.ErrNoSuchUser) {
		t.Errorf("an unknown user = %v, want ErrNoSuchUser", err)
	}
}

func TestRemovingAUserEndsTheirConversationsOnThatInboundOnly(t *testing.T) {
	s := newUsersSetup(t, true)
	for _, tag := range []string{"first", "second"} {
		if _, err := s.provider.AddInboundUser(tag, userObject("alice", "secret")); err != nil {
			t.Fatal(err)
		}
	}
	closed := map[string]*atomic.Int32{}
	track := func(label, inboundTag, user string) {
		var count atomic.Int32
		closed[label] = &count
		s.registry.TrackConversation(stats.ConversationInfo{InboundTag: inboundTag, User: user}, func() { count.Add(1) })
	}
	track("alice on first", "first", "alice")
	track("alice on first, again", "first", "alice")
	track("bob on first", "first", "bob")
	track("alice on second", "second", "alice")
	track("nobody on first", "first", "")

	if err := s.provider.RemoveInboundUser("first", "alice"); err != nil {
		t.Fatal(err)
	}

	for label, count := range closed {
		want := label == "alice on first" || label == "alice on first, again"
		if (count.Load() > 0) != want {
			t.Errorf("%q: closed %d times, want closed: %v", label, count.Load(), want)
		}
	}
}

func TestRemovingAUserWithoutLiveTrackingStillRemovesThemAndSaysTheConnectionsStay(t *testing.T) {
	s := newUsersSetup(t, false)
	if _, err := s.provider.AddInboundUser("first", userObject("alice", "secret")); err != nil {
		t.Fatal(err)
	}
	drain(s.logs)

	if err := s.provider.RemoveInboundUser("first", "alice"); err != nil {
		t.Fatalf("RemoveInboundUser: %v", err)
	}

	if got, _ := s.provider.ListInboundUsers("first"); len(got) != 0 {
		t.Errorf("the user is still listed: %v", got)
	}
	messages := messagesOf(drain(s.logs))
	if !strings.Contains(strings.Join(messages, "|"), "open connections stay") {
		t.Errorf("no log said the connections stay: %v", messages)
	}
}

func TestEveryChangeIsLoggedWithTheInboundAndTheNameAndNeverThePassword(t *testing.T) {
	s := newUsersSetup(t, true)
	const password = "very-secret-password-value"

	if _, err := s.provider.AddInboundUser("first", userObject("alice", password)); err != nil {
		t.Fatal(err)
	}
	if err := s.provider.RemoveInboundUser("first", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.provider.AddInboundUser("first", json.RawMessage(`{"name":`+`"alice","password":"`+password+`" bad`)); err == nil {
		t.Fatal("a broken object was accepted")
	}

	records := drain(s.logs)
	var added, removed bool
	for _, record := range records {
		line := record.Message
		record.Attrs(func(attribute slog.Attr) bool {
			line += " " + attribute.Key + "=" + attribute.Value.String()
			return true
		})
		if strings.Contains(line, password) {
			t.Errorf("a log line carries the password: %s", line)
		}
		added = added || (record.Message == "User added" && strings.Contains(line, "inbound=first") && strings.Contains(line, "user=alice"))
		removed = removed || (record.Message == "User removed" && strings.Contains(line, "inbound=first") && strings.Contains(line, "user=alice"))
	}
	if !added || !removed {
		t.Errorf("added logged: %v, removed logged: %v; records: %v", added, removed, messagesOf(records))
	}
}

func TestTheNumberOfBuiltInboundsMustMatchTheBlueprint(t *testing.T) {
	_, err := apicoreprovider.New(apicoreprovider.RunningCore{Blueprint: &blueprint.Blueprint{Inbounds: []blueprint.Inbound{{Tag: "first"}}}})
	if err == nil {
		t.Error("a blueprint with an inbound and no built inbounds was accepted")
	}
}

func drain(subscription *logging.Subscription) []slog.Record {
	var records []slog.Record
	for {
		select {
		case record := <-subscription.Records():
			records = append(records, record)
		default:
			return records
		}
	}
}

func messagesOf(records []slog.Record) []string {
	messages := make([]string, len(records))
	for index, record := range records {
		messages[index] = record.Message
	}
	return messages
}
