package stats

import "errors"

var (
	ErrLiveConversationNotFound     = errors.New("stats: no live conversation has this id")
	ErrLiveConversationNotCloseable = errors.New("stats: this live conversation was tracked without a way to close it")
	ErrLiveConversationsNotTracked  = errors.New("stats: live conversations are not tracked")
)
