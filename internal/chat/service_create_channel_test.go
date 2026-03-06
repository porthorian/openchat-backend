package chat

import (
	"errors"
	"strings"
	"testing"
)

type channelCreateBroadcasterSpy struct {
	events []ChannelCreatedEvent
}

func (s *channelCreateBroadcasterSpy) BroadcastMessage(_ Message) {}

func (s *channelCreateBroadcasterSpy) BroadcastReadAck(_ ChannelReadAckUpdate) {}

func (s *channelCreateBroadcasterSpy) BroadcastChannelCreated(event ChannelCreatedEvent) {
	s.events = append(s.events, event)
}

func (s *channelCreateBroadcasterSpy) BroadcastCategoryCreated(_ CategoryCreatedEvent)           {}
func (s *channelCreateBroadcasterSpy) BroadcastCategoryUpdated(_ CategoryUpdatedEvent)           {}
func (s *channelCreateBroadcasterSpy) BroadcastChannelLayoutUpdated(_ ChannelLayoutUpdatedEvent) {}

func (s *channelCreateBroadcasterSpy) BroadcastServerUpdated(_ ServerUpdatedEvent) {}

func TestCreateChannel_FirstCreatorBecomesOwner(t *testing.T) {
	svc := NewService("http://localhost:8080")
	spy := &channelCreateBroadcasterSpy{}
	svc.SetBroadcaster(spy)

	created, err := svc.CreateChannel(SeedServerIDHarbor, "uid_owner_alpha", "grp_general", "engineering", ChannelTypeText)
	if err != nil {
		t.Fatalf("create channel failed: %v", err)
	}
	if created.ServerID != SeedServerIDHarbor {
		t.Fatalf("unexpected server id: %s", created.ServerID)
	}
	if created.GroupID != "grp_general" {
		t.Fatalf("unexpected group id: %s", created.GroupID)
	}
	if created.Channel.ID == "" {
		t.Fatalf("expected created channel id")
	}
	if !strings.HasPrefix(created.Channel.ID, "ch_") {
		t.Fatalf("expected text channel id prefix ch_, got %s", created.Channel.ID)
	}
	if created.Channel.Type != ChannelTypeText {
		t.Fatalf("expected channel type text, got %s", created.Channel.Type)
	}

	if len(spy.events) != 1 {
		t.Fatalf("expected one broadcast event, got %d", len(spy.events))
	}
	if spy.events[0].Channel.ID != created.Channel.ID {
		t.Fatalf("expected broadcasted channel id %s, got %s", created.Channel.ID, spy.events[0].Channel.ID)
	}

	groups, err := svc.ListChannelGroups(SeedServerIDHarbor)
	if err != nil {
		t.Fatalf("list groups failed: %v", err)
	}
	if !groupContainsChannel(groups, "grp_general", created.Channel.ID) {
		t.Fatalf("expected new channel in grp_general")
	}

	if _, err := svc.ListMessages(created.Channel.ID, 10); err != nil {
		t.Fatalf("expected created channel message timeline, got error: %v", err)
	}

	second, err := svc.CreateChannel(SeedServerIDHarbor, "uid_owner_alpha", "grp_voice", "daily standup", ChannelTypeVoice)
	if err != nil {
		t.Fatalf("owner should create second channel: %v", err)
	}
	if second.Channel.Type != ChannelTypeVoice {
		t.Fatalf("expected voice channel, got %s", second.Channel.Type)
	}
	if !strings.HasPrefix(second.Channel.ID, "vc_") {
		t.Fatalf("expected voice channel id prefix vc_, got %s", second.Channel.ID)
	}

	_, err = svc.CreateChannel(SeedServerIDHarbor, "uid_not_owner", "grp_general", "forbidden", ChannelTypeText)
	if !errors.Is(err, ErrChannelCreateForbidden) {
		t.Fatalf("expected ErrChannelCreateForbidden, got %v", err)
	}
}

func TestCreateChannel_ValidatesInputAndGroupType(t *testing.T) {
	svc := NewService("http://localhost:8080")

	_, err := svc.CreateChannel("srv_missing", "uid_owner", "grp_general", "x", ChannelTypeText)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unknown server id") {
		t.Fatalf("expected unknown server error, got %v", err)
	}

	_, err = svc.CreateChannel(SeedServerIDHarbor, "uid_owner", "", "x", ChannelTypeText)
	if !errors.Is(err, ErrChannelGroupNotFound) {
		t.Fatalf("expected ErrChannelGroupNotFound for empty group, got %v", err)
	}

	_, err = svc.CreateChannel(SeedServerIDHarbor, "uid_owner", "grp_general", "", ChannelTypeText)
	if !errors.Is(err, ErrChannelNameInvalid) {
		t.Fatalf("expected ErrChannelNameInvalid, got %v", err)
	}

	_, err = svc.CreateChannel(SeedServerIDHarbor, "uid_owner", "grp_general", "x", ChannelType("forum"))
	if !errors.Is(err, ErrChannelTypeUnsupported) {
		t.Fatalf("expected ErrChannelTypeUnsupported, got %v", err)
	}

	_, err = svc.CreateChannel(SeedServerIDHarbor, "uid_owner", "grp_missing", "x", ChannelTypeText)
	if !errors.Is(err, ErrChannelGroupNotFound) {
		t.Fatalf("expected ErrChannelGroupNotFound for unknown group, got %v", err)
	}

	_, err = svc.CreateChannel(SeedServerIDHarbor, "uid_owner", "grp_general", "voice in text group", ChannelTypeVoice)
	if err != nil {
		t.Fatalf("expected mixed category support for voice in text group, got %v", err)
	}
}

func groupContainsChannel(groups []ChannelGroup, groupID string, channelID string) bool {
	for _, group := range groups {
		if group.ID != groupID {
			continue
		}
		for _, channel := range group.Channels {
			if channel.ID == channelID {
				return true
			}
		}
	}
	return false
}
