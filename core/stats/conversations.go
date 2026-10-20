package stats

import (
	"cmp"
	"context"
	"net/netip"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/we-apranik/apranik/spec"
)

// It is filled by core, never by a module.
type ConversationInfo struct {
	Network    spec.Network
	InboundTag string

	InboundProtocol string
	User            string // empty when the protocol does not authenticate
	Source          spec.Endpoint
	Destination     spec.Destination

	// Empty otherwise, and for UDP, which sniffs nothing. The destination is never replaced by it.
	SniffedDomain string

	OutboundTags []string

	// Which rule decided: a Decision's Rule, such as "routing.rules[3]".
	Rule string
}

// ConversationBytes is one live conversation's own payload bytes: only the two numbers a single
// conversation can have, so a live entry stays small.
type ConversationBytes struct {
	uplinkBytes   atomic.Uint64
	downlinkBytes atomic.Uint64
}

func (b *ConversationBytes) AddUplinkBytes(byteCount uint64) {
	if b != nil {
		b.uplinkBytes.Add(byteCount)
	}
}

func (b *ConversationBytes) AddDownlinkBytes(byteCount uint64) {
	if b != nil {
		b.downlinkBytes.Add(byteCount)
	}
}

type Conversation struct {
	id        uint64
	info      ConversationInfo
	startedAt time.Time
	bytes     ConversationBytes
	registry  *Registry

	closeRequested context.CancelFunc
}

func (c *Conversation) Bytes() *ConversationBytes {
	if c == nil {
		return nil
	}
	return &c.bytes
}

func (c *Conversation) End() {
	if c == nil {
		return
	}
	shard := c.registry.liveShardOf(c.id)
	shard.mu.Lock()
	delete(shard.conversations, c.id)
	shard.mu.Unlock()
}

// Only conversations that are past their handshake and dialed are tracked, so a conversation still
// being set up is not listed. Each gets the next number in a sequence, so a bigger one began later.
func (r *Registry) TrackConversation(info ConversationInfo, closeRequested context.CancelFunc) *Conversation {
	if r == nil || !r.trackLiveConversations {
		return nil
	}
	conversation := &Conversation{info: info, startedAt: time.Now(), registry: r, closeRequested: closeRequested}
	conversation.id = r.lastConversationID.Add(1)
	shard := r.liveShardOf(conversation.id)
	shard.mu.Lock()
	if shard.conversations == nil {
		shard.conversations = make(map[uint64]*Conversation)
	}
	shard.conversations[conversation.id] = conversation
	shard.mu.Unlock()
	return conversation
}

func (r *Registry) liveShardOf(id uint64) *liveShard { return &r.liveShards[id%liveShardCount] }

// Whether TrackConversation registers anything, so a reader can say that live
// conversations are switched off instead of answering with an empty list.
func (r *Registry) IsLiveTrackingEnabled() bool {
	return r != nil && r.trackLiveConversations
}

type ConversationSnapshot struct {
	ID            string
	Info          ConversationInfo
	StartedAt     time.Time
	UplinkBytes   uint64
	DownlinkBytes uint64
}

func (r *Registry) LiveConversations() []ConversationSnapshot {
	if r == nil || !r.trackLiveConversations {
		return nil
	}
	var conversations []*Conversation
	for index := range r.liveShards {
		shard := &r.liveShards[index]
		shard.mu.Lock()
		for _, conversation := range shard.conversations {
			conversations = append(conversations, conversation)
		}
		shard.mu.Unlock()
	}

	slices.SortFunc(conversations, func(a, b *Conversation) int { return cmp.Compare(a.id, b.id) })
	snapshots := make([]ConversationSnapshot, 0, len(conversations))
	for _, conversation := range conversations {
		info := conversation.info
		info.OutboundTags = slices.Clone(info.OutboundTags)
		snapshots = append(snapshots, ConversationSnapshot{
			ID:            strconv.FormatUint(conversation.id, 10),
			Info:          info,
			StartedAt:     conversation.startedAt,
			UplinkBytes:   conversation.bytes.uplinkBytes.Load(),
			DownlinkBytes: conversation.bytes.downlinkBytes.Load(),
		})
	}
	return snapshots
}

type OnlineUser struct {
	Name          string
	Conversations int
	IPs           []OnlineIP
}

// LastSeen is when the newest live conversation from it began, as Xray's last seen is.
type OnlineIP struct {
	Address  netip.Addr
	LastSeen time.Time
}

func (r *Registry) OnlineUsers() []OnlineUser {
	live := r.LiveConversations()
	if len(live) == 0 {
		return nil
	}

	byName := make(map[string]*OnlineUser)
	lastSeenByAddress := make(map[string]map[netip.Addr]time.Time)
	for _, conversation := range live {
		name := conversation.Info.User
		if name == "" {
			continue
		}
		user, found := byName[name]
		if !found {
			user = &OnlineUser{Name: name}
			byName[name] = user
			lastSeenByAddress[name] = make(map[netip.Addr]time.Time)
		}
		user.Conversations++
		address := conversation.Info.Source.Addr()
		if address.IsValid() && conversation.StartedAt.After(lastSeenByAddress[name][address]) {
			lastSeenByAddress[name][address] = conversation.StartedAt
		}
	}

	users := make([]OnlineUser, 0, len(byName))
	for name, user := range byName {
		for address, lastSeen := range lastSeenByAddress[name] {
			user.IPs = append(user.IPs, OnlineIP{Address: address, LastSeen: lastSeen})
		}
		slices.SortFunc(user.IPs, func(a, b OnlineIP) int { return a.Address.Compare(b.Address) })
		users = append(users, *user)
	}
	slices.SortFunc(users, func(a, b OnlineUser) int { return cmp.Compare(a.Name, b.Name) })
	return users
}

func (r *Registry) CloseLiveConversation(id string) error {
	if !r.IsLiveTrackingEnabled() {
		return ErrLiveConversationsNotTracked
	}
	number, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return ErrLiveConversationNotFound
	}

	shard := r.liveShardOf(number)
	shard.mu.Lock()
	conversation, found := shard.conversations[number]
	var closeRequested context.CancelFunc
	if found {
		closeRequested = conversation.closeRequested
	}
	shard.mu.Unlock()

	switch {
	case !found:
		return ErrLiveConversationNotFound
	case closeRequested == nil:
		return ErrLiveConversationNotCloseable
	}
	closeRequested()
	return nil
}

// match runs under a shard lock: it must be cheap and must not call the registry.
func (r *Registry) CloseLiveMatchingConversations(match func(ConversationInfo) bool) error {
	if !r.IsLiveTrackingEnabled() {
		return ErrLiveConversationsNotTracked
	}
	var closers []context.CancelFunc
	for index := range r.liveShards {
		shard := &r.liveShards[index]
		shard.mu.Lock()
		for _, conversation := range shard.conversations {
			if conversation.closeRequested != nil && match(conversation.info) {
				closers = append(closers, conversation.closeRequested)
			}
		}
		shard.mu.Unlock()
	}
	for _, closeRequested := range closers {
		closeRequested()
	}
	return nil
}
