package chat

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type ChannelType string

const (
	ChannelTypeText  ChannelType = "text"
	ChannelTypeVoice ChannelType = "voice"
)

type Channel struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        ChannelType `json:"type"`
	UnreadCount int         `json:"unread_count,omitempty"`
}

type ChannelGroup struct {
	ID       string    `json:"id"`
	Label    string    `json:"label"`
	Kind     string    `json:"kind"`
	Channels []Channel `json:"channels"`
}

type Member struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Message struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	AuthorUID string `json:"author_uid"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

type ServerDirectoryEntry struct {
	ServerID                  string `json:"server_id"`
	DisplayName               string `json:"display_name"`
	IconText                  string `json:"icon_text"`
	TrustState                string `json:"trust_state"`
	IdentityHandshakeStrategy string `json:"identity_handshake_strategy"`
	UserIdentifierPolicy      string `json:"user_identifier_policy"`
}

type MessageBroadcaster interface {
	BroadcastMessage(message Message)
}

type Service struct {
	mu sync.RWMutex

	servers               []ServerDirectoryEntry
	channelGroupsByServer map[string][]ChannelGroup
	membersByServer       map[string][]Member
	messagesByChannel     map[string][]Message
	channelServerByID     map[string]string
	channelTypeByID       map[string]ChannelType

	broadcaster MessageBroadcaster
}

func NewService() *Service {
	svc := &Service{
		servers:               seedServerDirectory(),
		channelGroupsByServer: seedChannelGroups(),
		membersByServer:       seedMembers(),
		messagesByChannel:     seedMessages(),
		channelServerByID:     make(map[string]string),
		channelTypeByID:       make(map[string]ChannelType),
	}
	svc.indexChannels()
	return svc
}

func (s *Service) ListServers() []ServerDirectoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	servers := make([]ServerDirectoryEntry, len(s.servers))
	copy(servers, s.servers)
	return servers
}

func (s *Service) SetBroadcaster(b MessageBroadcaster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcaster = b
}

func (s *Service) ListChannelGroups(serverID string) ([]ChannelGroup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	groups, ok := s.channelGroupsByServer[serverID]
	if !ok {
		return nil, fmt.Errorf("unknown server id: %s", serverID)
	}
	return cloneGroups(groups), nil
}

func (s *Service) ListMembers(serverID string) ([]Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	members, ok := s.membersByServer[serverID]
	if !ok {
		return nil, fmt.Errorf("unknown server id: %s", serverID)
	}
	cloned := make([]Member, len(members))
	copy(cloned, members)
	return cloned, nil
}

func (s *Service) ListMessages(channelID string, limit int) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.channelTypeByID[channelID]; !ok {
		return nil, fmt.Errorf("unknown channel id: %s", channelID)
	}
	messages := s.messagesByChannel[channelID]
	if limit <= 0 || limit > len(messages) {
		limit = len(messages)
	}
	start := len(messages) - limit
	if start < 0 {
		start = 0
	}
	cloned := make([]Message, len(messages[start:]))
	copy(cloned, messages[start:])
	return cloned, nil
}

func (s *Service) CreateMessage(channelID string, authorUID string, body string) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Message{}, errors.New("message body is required")
	}

	s.mu.Lock()
	channelType, ok := s.channelTypeByID[channelID]
	if !ok {
		s.mu.Unlock()
		return Message{}, fmt.Errorf("unknown channel id: %s", channelID)
	}
	if channelType != ChannelTypeText {
		s.mu.Unlock()
		return Message{}, errors.New("messages can only be sent to text channels")
	}

	message := Message{
		ID:        "msg_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		ChannelID: channelID,
		AuthorUID: authorUID,
		Body:      body,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	s.messagesByChannel[channelID] = append(s.messagesByChannel[channelID], message)
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastMessage(message)
	}
	return message, nil
}

func (s *Service) ServerExists(serverID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.channelGroupsByServer[serverID]
	return ok
}

func (s *Service) ChannelExists(channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.channelTypeByID[channelID]
	return ok
}

func (s *Service) IsVoiceChannel(channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channelTypeByID[channelID] == ChannelTypeVoice
}

func (s *Service) indexChannels() {
	for serverID, groups := range s.channelGroupsByServer {
		for _, group := range groups {
			for _, channel := range group.Channels {
				s.channelServerByID[channel.ID] = serverID
				s.channelTypeByID[channel.ID] = channel.Type
			}
		}
	}
}

func cloneGroups(groups []ChannelGroup) []ChannelGroup {
	out := make([]ChannelGroup, len(groups))
	for idx, group := range groups {
		channels := make([]Channel, len(group.Channels))
		copy(channels, group.Channels)
		out[idx] = ChannelGroup{
			ID:       group.ID,
			Label:    group.Label,
			Kind:     group.Kind,
			Channels: channels,
		}
	}
	return out
}

func seedServerDirectory() []ServerDirectoryEntry {
	return []ServerDirectoryEntry{
		{
			ServerID:                  "srv_harbor",
			DisplayName:               "Harbor Guild",
			IconText:                  "HG",
			TrustState:                "verified",
			IdentityHandshakeStrategy: "challenge_signature",
			UserIdentifierPolicy:      "server_scoped",
		},
		{
			ServerID:                  "srv_testlab",
			DisplayName:               "TestLab Server",
			IconText:                  "TL",
			TrustState:                "verified",
			IdentityHandshakeStrategy: "challenge_signature",
			UserIdentifierPolicy:      "server_scoped",
		},
	}
}

func seedChannelGroups() map[string][]ChannelGroup {
	return map[string][]ChannelGroup{
		"srv_harbor": {
			{
				ID:    "grp_general",
				Label: "general",
				Kind:  "text",
				Channels: []Channel{
					{ID: "ch_general", Name: "general", Type: ChannelTypeText},
					{ID: "ch_design", Name: "design", Type: ChannelTypeText},
					{ID: "ch_release", Name: "release-notes", Type: ChannelTypeText},
				},
			},
			{
				ID:    "grp_ops",
				Label: "ops",
				Kind:  "text",
				Channels: []Channel{
					{ID: "ch_outage", Name: "outage-watch", Type: ChannelTypeText},
				},
			},
			{
				ID:    "grp_voice",
				Label: "Voice Channels",
				Kind:  "voice",
				Channels: []Channel{
					{ID: "vc_general", Name: "General Voice", Type: ChannelTypeVoice},
					{ID: "vc_party", Name: "Party Chat", Type: ChannelTypeVoice},
				},
			},
		},
		"srv_testlab": {
			{
				ID:    "grp_test_text",
				Label: "test boards",
				Kind:  "text",
				Channels: []Channel{
					{ID: "tl_ch_general", Name: "test-general", Type: ChannelTypeText},
					{ID: "tl_ch_qa", Name: "qa-playground", Type: ChannelTypeText},
				},
			},
			{
				ID:    "grp_test_voice",
				Label: "Voice Channels",
				Kind:  "voice",
				Channels: []Channel{
					{ID: "tl_vc_huddle", Name: "Huddle Room", Type: ChannelTypeVoice},
					{ID: "tl_vc_pairing", Name: "Pairing Booth", Type: ChannelTypeVoice},
				},
			},
		},
	}
}

func seedMembers() map[string][]Member {
	return map[string][]Member{
		"srv_harbor": {
			{ID: "mem_1", Name: "Lyra", Status: "online"},
			{ID: "mem_2", Name: "Orin", Status: "idle"},
			{ID: "mem_3", Name: "Mira", Status: "online"},
			{ID: "mem_4", Name: "Calix", Status: "dnd"},
		},
		"srv_testlab": {
			{ID: "mem_t1", Name: "Devon", Status: "online"},
			{ID: "mem_t2", Name: "Rhea", Status: "idle"},
			{ID: "mem_t3", Name: "Pax", Status: "online"},
		},
	}
}

func seedMessages() map[string][]Message {
	now := time.Now().UTC()
	return map[string][]Message{
		"ch_general": {
			{ID: "msg_seed_01", ChannelID: "ch_general", AuthorUID: "uid_seed_1", Body: "Welcome to OpenChat Harbor.", CreatedAt: now.Add(-30 * time.Minute).Format(time.RFC3339)},
			{ID: "msg_seed_02", ChannelID: "ch_general", AuthorUID: "uid_seed_2", Body: "Realtime messaging is enabled.", CreatedAt: now.Add(-24 * time.Minute).Format(time.RFC3339)},
		},
		"ch_design": {
			{ID: "msg_seed_11", ChannelID: "ch_design", AuthorUID: "uid_seed_3", Body: "Design channel ready for discussion.", CreatedAt: now.Add(-18 * time.Minute).Format(time.RFC3339)},
		},
		"ch_release": {},
		"ch_outage":  {},
		"vc_general": {},
		"vc_party":   {},
		"tl_ch_general": {
			{ID: "msg_tl_01", ChannelID: "tl_ch_general", AuthorUID: "uid_tl_1", Body: "TestLab server online.", CreatedAt: now.Add(-22 * time.Minute).Format(time.RFC3339)},
			{ID: "msg_tl_02", ChannelID: "tl_ch_general", AuthorUID: "uid_tl_2", Body: "Use this channel for integration testing.", CreatedAt: now.Add(-15 * time.Minute).Format(time.RFC3339)},
		},
		"tl_ch_qa": {
			{ID: "msg_tl_11", ChannelID: "tl_ch_qa", AuthorUID: "uid_tl_3", Body: "QA board ready for smoke checks.", CreatedAt: now.Add(-9 * time.Minute).Format(time.RFC3339)},
		},
		"tl_vc_huddle":  {},
		"tl_vc_pairing": {},
	}
}
