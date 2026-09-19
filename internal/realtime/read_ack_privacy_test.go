package realtime

import (
	"io"
	"log/slog"
	"testing"

	"github.com/openchat/openchat-backend/internal/chat"
)

func TestReadAckOnlyReachesOwningUser(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	owner := &client{userUID: "uid_owner", send: make(chan Envelope, 1)}
	other := &client{userUID: "uid_other", send: make(chan Envelope, 1)}
	hub.subscribersByRoom["ch_test"] = map[string]*client{"owner": owner, "other": other}
	hub.BroadcastReadAck(chat.ChannelReadAckUpdate{ChannelID: "ch_test", UserUID: "uid_owner", LastReadMessageID: "msg_1"})
	if len(owner.send) != 1 {
		t.Fatal("owner did not receive read ack")
	}
	if len(other.send) != 0 {
		t.Fatal("another member received private read ack")
	}
}
