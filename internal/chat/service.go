package chat

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"sort"
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
	ID          string              `json:"id"`
	ChannelID   string              `json:"channel_id"`
	AuthorUID   string              `json:"author_uid"`
	Body        string              `json:"body"`
	CreatedAt   string              `json:"created_at"`
	Attachments []MessageAttachment `json:"attachments,omitempty"`
}

type MessageAttachment struct {
	AttachmentID string `json:"attachment_id"`
	FileName     string `json:"file_name"`
	URL          string `json:"url"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	ContentType  string `json:"content_type"`
	Bytes        int    `json:"bytes"`
}

type AttachmentUploadInput struct {
	FileName    string
	ContentType string
	Data        []byte
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

	publicBaseURL string

	servers               []ServerDirectoryEntry
	channelGroupsByServer map[string][]ChannelGroup
	membersByServer       map[string][]Member
	messagesByChannel     map[string][]Message
	attachmentsByID       map[string]attachmentBlob
	channelServerByID     map[string]string
	channelTypeByID       map[string]ChannelType
	leftServersByUser     map[string]map[string]time.Time

	maxAttachmentBytes       int
	maxAttachmentsPerMessage int
	allowedAttachmentTypes   map[string]struct{}

	broadcaster MessageBroadcaster
}

type attachmentBlob struct {
	metadata  MessageAttachment
	channelID string
	content   []byte
}

var (
	ErrMessageEmpty              = errors.New("message body or attachment is required")
	ErrAttachmentTooLarge        = errors.New("attachment exceeds max upload size")
	ErrAttachmentTypeUnsupported = errors.New("attachment mime type is unsupported")
	ErrAttachmentImageInvalid    = errors.New("attachment image payload is invalid")
	ErrTooManyAttachments        = errors.New("too many attachments")
	ErrAttachmentNotFound        = errors.New("attachment not found")
)

func NewService(publicBaseURL string) *Service {
	svc := &Service{
		publicBaseURL:            strings.TrimSuffix(strings.TrimSpace(publicBaseURL), "/"),
		servers:                  seedServerDirectory(),
		channelGroupsByServer:    seedChannelGroups(),
		membersByServer:          seedMembers(),
		messagesByChannel:        seedMessages(),
		attachmentsByID:          make(map[string]attachmentBlob),
		channelServerByID:        make(map[string]string),
		channelTypeByID:          make(map[string]ChannelType),
		leftServersByUser:        make(map[string]map[string]time.Time),
		maxAttachmentBytes:       50 * 1024 * 1024,
		maxAttachmentsPerMessage: 4,
		allowedAttachmentTypes: map[string]struct{}{
			"image/png":  {},
			"image/jpeg": {},
			"image/gif":  {},
		},
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

func (s *Service) ListServersForUser(userUID string) []ServerDirectoryEntry {
	userUID = strings.TrimSpace(userUID)
	s.mu.RLock()
	defer s.mu.RUnlock()

	servers := make([]ServerDirectoryEntry, 0, len(s.servers))
	leftByServerID := s.leftServersByUser[userUID]
	for _, server := range s.servers {
		if leftByServerID != nil {
			if _, left := leftByServerID[server.ServerID]; left {
				continue
			}
		}
		servers = append(servers, server)
	}
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
	return cloneMessages(messages[start:]), nil
}

func (s *Service) AttachmentUploadRules() (maxBytes int, maxFiles int, mimeTypes []string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mimeTypes = make([]string, 0, len(s.allowedAttachmentTypes))
	for mime := range s.allowedAttachmentTypes {
		mimeTypes = append(mimeTypes, mime)
	}
	sort.Strings(mimeTypes)
	return s.maxAttachmentBytes, s.maxAttachmentsPerMessage, mimeTypes
}

func (s *Service) CreateMessage(channelID string, authorUID string, body string, uploads []AttachmentUploadInput) (Message, error) {
	body = strings.TrimSpace(body)

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
	if len(uploads) > s.maxAttachmentsPerMessage {
		s.mu.Unlock()
		return Message{}, ErrTooManyAttachments
	}

	attachments := make([]MessageAttachment, 0, len(uploads))
	for _, upload := range uploads {
		attachment, content, err := s.buildAttachment(channelID, upload)
		if err != nil {
			s.mu.Unlock()
			return Message{}, err
		}
		s.attachmentsByID[attachment.AttachmentID] = attachmentBlob{
			metadata:  attachment,
			channelID: channelID,
			content:   content,
		}
		attachments = append(attachments, attachment)
	}

	if body == "" && len(attachments) == 0 {
		s.mu.Unlock()
		return Message{}, ErrMessageEmpty
	}

	message := Message{
		ID:          "msg_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		ChannelID:   channelID,
		AuthorUID:   authorUID,
		Body:        body,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Attachments: attachments,
	}
	s.messagesByChannel[channelID] = append(s.messagesByChannel[channelID], cloneMessage(message))
	broadcaster := s.broadcaster
	broadcastMessage := cloneMessage(message)
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastMessage(broadcastMessage)
	}
	return cloneMessage(message), nil
}

func (s *Service) AttachmentContent(channelID string, attachmentID string) (MessageAttachment, []byte, error) {
	channelID = strings.TrimSpace(channelID)
	attachmentID = strings.TrimSpace(attachmentID)
	if channelID == "" || attachmentID == "" {
		return MessageAttachment{}, nil, ErrAttachmentNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	blob, ok := s.attachmentsByID[attachmentID]
	if !ok || blob.channelID != channelID {
		return MessageAttachment{}, nil, ErrAttachmentNotFound
	}
	return cloneMessageAttachment(blob.metadata), append([]byte(nil), blob.content...), nil
}

func (s *Service) buildAttachment(channelID string, upload AttachmentUploadInput) (MessageAttachment, []byte, error) {
	content := upload.Data
	if len(content) == 0 {
		return MessageAttachment{}, nil, ErrAttachmentImageInvalid
	}
	if len(content) > s.maxAttachmentBytes {
		return MessageAttachment{}, nil, ErrAttachmentTooLarge
	}

	contentType := normalizeAttachmentContentType(upload.ContentType, content)
	if _, ok := s.allowedAttachmentTypes[contentType]; !ok {
		return MessageAttachment{}, nil, ErrAttachmentTypeUnsupported
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return MessageAttachment{}, nil, ErrAttachmentImageInvalid
	}

	attachmentID := "att_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	attachment := MessageAttachment{
		AttachmentID: attachmentID,
		FileName:     normalizeAttachmentFileName(upload.FileName, contentType),
		URL:          s.attachmentURL(channelID, attachmentID),
		Width:        cfg.Width,
		Height:       cfg.Height,
		ContentType:  contentType,
		Bytes:        len(content),
	}

	return attachment, append([]byte(nil), content...), nil
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

func (s *Service) LeaveServer(serverID string, userUID string) error {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)
	if serverID == "" {
		return errors.New("server id is required")
	}
	if userUID == "" {
		return errors.New("user uid is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.channelGroupsByServer[serverID]; !ok {
		return fmt.Errorf("unknown server id: %s", serverID)
	}

	leftByServerID := s.leftServersByUser[userUID]
	if leftByServerID == nil {
		leftByServerID = make(map[string]time.Time)
		s.leftServersByUser[userUID] = leftByServerID
	}
	leftByServerID[serverID] = time.Now().UTC()
	return nil
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

func cloneMessages(messages []Message) []Message {
	out := make([]Message, len(messages))
	for idx, message := range messages {
		out[idx] = cloneMessage(message)
	}
	return out
}

func cloneMessage(message Message) Message {
	out := message
	if len(message.Attachments) > 0 {
		out.Attachments = make([]MessageAttachment, len(message.Attachments))
		for idx, attachment := range message.Attachments {
			out.Attachments[idx] = cloneMessageAttachment(attachment)
		}
	}
	return out
}

func cloneMessageAttachment(attachment MessageAttachment) MessageAttachment {
	return attachment
}

func (s *Service) attachmentURL(channelID string, attachmentID string) string {
	path := fmt.Sprintf("/v1/channels/%s/attachments/%s", channelID, attachmentID)
	if s.publicBaseURL == "" {
		return path
	}
	return s.publicBaseURL + path
}

func normalizeAttachmentContentType(contentType string, body []byte) string {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if contentType != "" {
		if idx := strings.Index(contentType, ";"); idx >= 0 {
			contentType = strings.TrimSpace(contentType[:idx])
		}
	}
	if len(body) > 0 {
		detected := strings.ToLower(http.DetectContentType(body))
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = detected
		}
	}
	return contentType
}

func normalizeAttachmentFileName(fileName string, contentType string) string {
	trimmed := strings.TrimSpace(fileName)
	if trimmed != "" {
		return filepath.Base(trimmed)
	}

	switch contentType {
	case "image/jpeg":
		return "image.jpg"
	case "image/gif":
		return "image.gif"
	default:
		return "image.png"
	}
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
