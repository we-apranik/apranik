package stats

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

func closableConversation(registry *Registry, info ConversationInfo) (*Conversation, *atomic.Int32) {
	var closed atomic.Int32
	conversation := registry.TrackConversation(info, func() { closed.Add(1) })
	return conversation, &closed
}

func idOf(conversation *Conversation) string { return strconv.FormatUint(conversation.id, 10) }

func TestCloseLiveConversationCallsTheCloserOnce(t *testing.T) {
	registry := trackingRegistry()
	conversation, closed := closableConversation(registry, ConversationInfo{InboundTag: "socks-in"})
	other, otherClosed := closableConversation(registry, ConversationInfo{})

	if err := registry.CloseLiveConversation(idOf(conversation)); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 || otherClosed.Load() != 0 {
		t.Errorf("closer calls: closed one %d, other %d; want 1 and 0", closed.Load(), otherClosed.Load())
	}
	other.End()
}

func TestCloseLiveConversationReportsWhyItCouldNot(t *testing.T) {
	registry := trackingRegistry()
	conversation, _ := closableConversation(registry, ConversationInfo{})
	uncloseable := registry.TrackConversation(ConversationInfo{}, nil)
	ended, _ := closableConversation(registry, ConversationInfo{})
	ended.End()

	for name, tc := range map[string]struct {
		registry *Registry
		id       string
		want     error
	}{
		"unknown id":        {registry, "999999", ErrLiveConversationNotFound},
		"not a number":      {registry, "abc", ErrLiveConversationNotFound},
		"empty id":          {registry, "", ErrLiveConversationNotFound},
		"already ended":     {registry, idOf(ended), ErrLiveConversationNotFound},
		"no way to close":   {registry, idOf(uncloseable), ErrLiveConversationNotCloseable},
		"tracking switched": {newTestRegistry(Config{}), idOf(conversation), ErrLiveConversationsNotTracked},
		"nil registry":      {nil, "1", ErrLiveConversationsNotTracked},
	} {
		t.Run(name, func(t *testing.T) {
			if err := tc.registry.CloseLiveConversation(tc.id); !errors.Is(err, tc.want) {
				t.Errorf("CloseLiveConversation(%q) = %v, want %v", tc.id, err, tc.want)
			}
		})
	}
}

func TestClosingEveryLiveConversationReachesEveryShard(t *testing.T) {
	registry := trackingRegistry()
	var closed atomic.Int32
	const conversations = 5 * liveShardCount
	for range conversations {
		registry.TrackConversation(ConversationInfo{}, func() { closed.Add(1) })
	}

	if err := closeEverything(registry); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != conversations {
		t.Errorf("closers called %d times, want %d", closed.Load(), conversations)
	}
	if err := closeEverything(newTestRegistry(Config{})); !errors.Is(err, ErrLiveConversationsNotTracked) {
		t.Errorf("tracking off: %v", err)
	}
}

func TestCloseLiveMatchingConversationsSelectsByInfo(t *testing.T) {
	registry := trackingRegistry()
	_, throughA := closableConversation(registry, ConversationInfo{OutboundTags: []string{"proxy", "server-a"}})
	_, throughB := closableConversation(registry, ConversationInfo{OutboundTags: []string{"proxy", "server-b"}})
	_, direct := closableConversation(registry, ConversationInfo{OutboundTags: []string{"server-a"}})

	err := registry.CloseLiveMatchingConversations(func(info ConversationInfo) bool {
		return len(info.OutboundTags) == 2 && info.OutboundTags[1] == "server-a"
	})
	if err != nil {
		t.Fatal(err)
	}
	if throughA.Load() != 1 || throughB.Load() != 0 || direct.Load() != 0 {
		t.Errorf("closed: through a %d, through b %d, direct %d; want 1, 0, 0", throughA.Load(), throughB.Load(), direct.Load())
	}
}

func TestACloserThatEndsTheConversationDoesNotDeadlock(t *testing.T) {
	registry := trackingRegistry()
	var conversation *Conversation
	conversation = registry.TrackConversation(ConversationInfo{}, func() { conversation.End() })

	done := make(chan error, 1)
	go func() { done <- registry.CloseLiveConversation(idOf(conversation)) }()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := registry.CloseLiveConversation(idOf(conversation)); !errors.Is(err, ErrLiveConversationNotFound) {
		t.Errorf("closing a conversation that ended itself: %v", err)
	}

	var again *Conversation
	again = registry.TrackConversation(ConversationInfo{}, func() { again.End() })
	if err := closeEverything(registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.LiveConversations()) != 0 {
		t.Error("a conversation that ended itself is still listed")
	}
}

func TestClosingWhileConversationsComeAndGo(t *testing.T) {
	registry := trackingRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var group sync.WaitGroup
	for range 4 {
		group.Add(1)
		go func() {
			defer group.Done()
			for ctx.Err() == nil {
				conversation := registry.TrackConversation(ConversationInfo{}, func() {})
				conversation.End()
			}
		}()
	}
	for range 200 {
		_ = closeEverything(registry)
		_ = registry.LiveConversations()
		_ = registry.CloseLiveConversation("1")
	}
	cancel()
	group.Wait()
}

func closeEverything(registry *Registry) error {
	return registry.CloseLiveMatchingConversations(func(ConversationInfo) bool { return true })
}
