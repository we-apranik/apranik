package stats

import (
	"cmp"
	"net/netip"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/The-Apranik/Apranik/spec"
)

// ConnectionInfo is what is known about a live connection: a TCP connection
// once it is dialed, or one UDP destination of a session once it has carried a
// datagram. It is filled by core, never by a module.
type ConnectionInfo struct {
	Network     spec.Network
	InboundTag  string
	User        string // empty when the protocol does not authenticate
	Source      spec.Endpoint
	Destination spec.Destination

	// The outbound routing named, then each outbound beneath it that carried
	// the connection when it is a group.
	OutboundTags []string

	// Which rule decided: a Decision's Rule, such as "routing.rules[3]".
	Rule string
}

// ConnectionBytes is one live connection's own payload bytes: only the two
// numbers a single connection can have, so a live entry stays small. A nil
// *ConnectionBytes counts nothing.
type ConnectionBytes struct {
	uplinkBytes   atomic.Uint64
	downlinkBytes atomic.Uint64
}

func (b *ConnectionBytes) AddUplinkBytes(byteCount uint64) {
	if b != nil {
		b.uplinkBytes.Add(byteCount)
	}
}

func (b *ConnectionBytes) AddDownlinkBytes(byteCount uint64) {
	if b != nil {
		b.downlinkBytes.Add(byteCount)
	}
}

// Connection is one registered live connection. It owns its own bytes; the
// handler gives them to the relay beside the shared counters. A nil
// *Connection is a connection nobody tracks, and every method does nothing on it.
type Connection struct {
	id        uint64
	info      ConnectionInfo
	startedAt time.Time
	bytes     ConnectionBytes
	registry  *Registry
}

// This connection's own bytes. Nil for a nil *Connection, which the relay skips.
func (c *Connection) Bytes() *ConnectionBytes {
	if c == nil {
		return nil
	}
	return &c.bytes
}

// The connection has ended; it is no longer listed.
func (c *Connection) End() {
	if c == nil {
		return
	}
	shard := c.registry.liveShardOf(c.id)
	shard.mu.Lock()
	delete(shard.connections, c.id)
	shard.mu.Unlock()
}

// TrackConnection registers a live connection, or returns nil when live
// connections are not tracked. Only connections that are past their handshake
// and dialed are tracked, so a connection still being set up is not listed.
// Each gets the next number in a sequence, so a bigger one began later.
func (r *Registry) TrackConnection(info ConnectionInfo) *Connection {
	if r == nil || !r.trackLiveConnections {
		return nil
	}
	connection := &Connection{info: info, startedAt: time.Now(), registry: r}
	connection.id = r.lastConnectionID.Add(1)
	shard := r.liveShardOf(connection.id)
	shard.mu.Lock()
	if shard.connections == nil {
		shard.connections = make(map[uint64]*Connection)
	}
	shard.connections[connection.id] = connection
	shard.mu.Unlock()
	return connection
}

func (r *Registry) liveShardOf(id uint64) *liveShard { return &r.liveShards[id%liveShardCount] }

// Whether TrackConnection registers anything, so a reader can say that live
// connections are switched off instead of answering with an empty list.
func (r *Registry) TracksLiveConnections() bool {
	return r != nil && r.trackLiveConnections
}

// ConnectionSnapshot is a copy of one live connection at one moment.
//
// ID is the connection's number in the sequence, as decimal text: it is not a
// secret, and a larger one began later.
type ConnectionSnapshot struct {
	ID            string
	Info          ConnectionInfo
	StartedAt     time.Time
	UplinkBytes   uint64
	DownlinkBytes uint64
}

// Every live connection, oldest first, as a copy that shares nothing with the
// live ones. Empty when live connections are not tracked; TracksLiveConnections
// tells that apart from no traffic. Its size follows the number of live
// connections, which is why it is a read of its own.
func (r *Registry) LiveConnections() []ConnectionSnapshot {
	if r == nil || !r.trackLiveConnections {
		return nil
	}
	var connections []*Connection
	for index := range r.liveShards {
		shard := &r.liveShards[index]
		shard.mu.Lock()
		for _, connection := range shard.connections {
			connections = append(connections, connection)
		}
		shard.mu.Unlock()
	}

	slices.SortFunc(connections, func(a, b *Connection) int { return cmp.Compare(a.id, b.id) })
	snapshots := make([]ConnectionSnapshot, 0, len(connections))
	for _, connection := range connections {
		info := connection.info
		info.OutboundTags = slices.Clone(info.OutboundTags)
		snapshots = append(snapshots, ConnectionSnapshot{
			ID:            strconv.FormatUint(connection.id, 10),
			Info:          info,
			StartedAt:     connection.startedAt,
			UplinkBytes:   connection.bytes.uplinkBytes.Load(),
			DownlinkBytes: connection.bytes.downlinkBytes.Load(),
		})
	}
	return snapshots
}

// OnlineUser is a user with at least one live connection.
type OnlineUser struct {
	Name        string
	Connections int
	IPs         []OnlineIP
}

// OnlineIP is a source address of a user's live connections. LastSeen is when
// the newest live connection from it began, as Xray's last seen is. When the
// last one ends the address is gone: nothing is kept.
type OnlineIP struct {
	Address  netip.Addr
	LastSeen time.Time
}

// The users who have a live connection now, by name, each with the distinct
// source addresses of those connections. Built when asked from the live
// connections, so it costs nothing while nobody asks. A protocol that does not
// authenticate has no users to list. Empty when live connections are not
// tracked.
func (r *Registry) OnlineUsers() []OnlineUser {
	live := r.LiveConnections()
	if len(live) == 0 {
		return nil
	}

	byName := make(map[string]*OnlineUser)
	lastSeenByAddress := make(map[string]map[netip.Addr]time.Time)
	for _, connection := range live {
		name := connection.Info.User
		if name == "" {
			continue
		}
		user, found := byName[name]
		if !found {
			user = &OnlineUser{Name: name}
			byName[name] = user
			lastSeenByAddress[name] = make(map[netip.Addr]time.Time)
		}
		user.Connections++
		address := connection.Info.Source.Addr()
		if address.IsValid() && connection.StartedAt.After(lastSeenByAddress[name][address]) {
			lastSeenByAddress[name][address] = connection.StartedAt
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
