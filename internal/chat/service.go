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

const (
	SeedServerIDHarbor  = "0d5f6a8b-1f43-4f4f-9d8e-9ab6f913f4f8"
	SeedServerIDTestLab = "6b6d8f24-a39f-4f8a-9f9d-0f70f2a2e9f1"
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
	ID          string                 `json:"id"`
	ChannelID   string                 `json:"channel_id"`
	AuthorUID   string                 `json:"author_uid"`
	Body        string                 `json:"body"`
	CreatedAt   string                 `json:"created_at"`
	ReplyTo     *MessageReplyReference `json:"reply_to,omitempty"`
	Mentions    []MessageMention       `json:"mentions,omitempty"`
	Attachments []MessageAttachment    `json:"attachments,omitempty"`
}

type MessageReplyReference struct {
	MessageID         string `json:"message_id"`
	AuthorUID         string `json:"author_uid,omitempty"`
	AuthorDisplayName string `json:"author_display_name,omitempty"`
	PreviewText       string `json:"preview_text,omitempty"`
	IsUnavailable     bool   `json:"is_unavailable"`
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
	Description               string `json:"description"`
	BannerPreset              string `json:"banner_preset,omitempty"`
	IconText                  string `json:"icon_text"`
	TrustState                string `json:"trust_state"`
	IdentityHandshakeStrategy string `json:"identity_handshake_strategy"`
	UserIdentifierPolicy      string `json:"user_identifier_policy"`
}

type ServerSettings struct {
	ServerID     string `json:"server_id"`
	DisplayName  string `json:"display_name"`
	Description  string `json:"description"`
	BannerPreset string `json:"banner_preset"`
}

type ServerOwnershipClaim struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type ServerCreated struct {
	Server         ServerDirectoryEntry `json:"server"`
	CreatedByUID   string               `json:"created_by_uid"`
	CreatedAt      string               `json:"created_at"`
	OwnershipClaim ServerOwnershipClaim `json:"ownership_claim"`
}

type ServerOwnershipClaimed struct {
	ServerID     string `json:"server_id"`
	OwnerUserUID string `json:"owner_user_uid"`
	ClaimedAt    string `json:"claimed_at"`
}

type MessageBroadcaster interface {
	BroadcastMessage(message Message)
	BroadcastReadAck(update ChannelReadAckUpdate)
	BroadcastChannelCreated(event ChannelCreatedEvent)
	BroadcastCategoryCreated(event CategoryCreatedEvent)
	BroadcastCategoryUpdated(event CategoryUpdatedEvent)
	BroadcastChannelLayoutUpdated(event ChannelLayoutUpdatedEvent)
	BroadcastServerUpdated(event ServerUpdatedEvent)
}

type ChannelCreatedEvent struct {
	ServerID     string  `json:"server_id"`
	GroupID      string  `json:"group_id"`
	Channel      Channel `json:"channel"`
	CreatedByUID string  `json:"created_by_uid"`
	CreatedAt    string  `json:"created_at"`
}

type CategoryCreatedEvent struct {
	ServerID     string       `json:"server_id"`
	Group        ChannelGroup `json:"group"`
	CreatedByUID string       `json:"created_by_uid"`
	CreatedAt    string       `json:"created_at"`
}

type CategoryUpdatedEvent struct {
	ServerID     string       `json:"server_id"`
	Group        ChannelGroup `json:"group"`
	UpdatedByUID string       `json:"updated_by_uid"`
	UpdatedAt    string       `json:"updated_at"`
}

type ChannelLayoutGroup struct {
	ID         string   `json:"id"`
	ChannelIDs []string `json:"channel_ids"`
}

type ChannelLayoutUpdatedEvent struct {
	ServerID     string         `json:"server_id"`
	Groups       []ChannelGroup `json:"groups"`
	UpdatedByUID string         `json:"updated_by_uid"`
	UpdatedAt    string         `json:"updated_at"`
}

type ServerUpdatedEvent struct {
	ServerID     string `json:"server_id"`
	DisplayName  string `json:"display_name"`
	Description  string `json:"description"`
	BannerPreset string `json:"banner_preset"`
	UpdatedByUID string `json:"updated_by_uid"`
	UpdatedAt    string `json:"updated_at"`
}

type Service struct {
	mu sync.RWMutex

	publicBaseURL string

	servers                        []ServerDirectoryEntry
	channelGroupsByServer          map[string][]ChannelGroup
	membersByServer                map[string][]Member
	messagesByChannel              map[string][]Message
	readAcksByChannelUser          map[string]ChannelReadAck
	attachmentsByID                map[string]attachmentBlob
	channelServerByID              map[string]string
	channelTypeByID                map[string]ChannelType
	ownerUserUIDByServer           map[string]string
	requiresOwnershipClaimByServer map[string]bool
	ownershipClaimsByToken         map[string]ownershipClaimState
	leftServersByUser              map[string]map[string]time.Time

	maxAttachmentBytes       int
	maxAttachmentsPerMessage int
	allowedAttachmentTypes   map[string]struct{}

	broadcaster MessageBroadcaster
}

type ownershipClaimState struct {
	ServerID  string
	ExpiresAt time.Time
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
	ErrReplyTargetNotFound       = errors.New("reply target message not found")
	ErrChannelNameInvalid        = errors.New("channel name is invalid")
	ErrChannelTypeUnsupported    = errors.New("channel type unsupported")
	ErrChannelGroupNotFound      = errors.New("channel group not found")
	ErrChannelCreateForbidden    = errors.New("channel create is forbidden")
	ErrCategoryNameInvalid       = errors.New("category name is invalid")
	ErrCategoryNotFound          = errors.New("category not found")
	ErrCategoryKindUnsupported   = errors.New("category kind unsupported")
	ErrCategoryCreateForbidden   = errors.New("category create is forbidden")
	ErrCategoryUpdateForbidden   = errors.New("category update is forbidden")
	ErrChannelLayoutInvalid      = errors.New("channel layout is invalid")
	ErrChannelLayoutForbidden    = errors.New("channel layout update is forbidden")
	ErrServerSettingsForbidden   = errors.New("server settings update is forbidden")
	ErrServerDisplayNameInvalid  = errors.New("server display name is invalid")
	ErrServerDescriptionInvalid  = errors.New("server description is invalid")
	ErrServerBannerPresetInvalid = errors.New("server banner preset is invalid")
	ErrOwnershipClaimRequired    = errors.New("ownership claim required")
	ErrOwnershipClaimInvalid     = errors.New("ownership claim invalid")
	ErrOwnershipClaimExpired     = errors.New("ownership claim expired")
	ErrOwnershipClaimForbidden   = errors.New("ownership claim forbidden")
)

const ownershipClaimTTL = 5 * time.Minute

func NewService(publicBaseURL string) *Service {
	svc := &Service{
		publicBaseURL:                  strings.TrimSuffix(strings.TrimSpace(publicBaseURL), "/"),
		servers:                        seedServerDirectory(),
		channelGroupsByServer:          seedChannelGroups(),
		membersByServer:                seedMembers(),
		messagesByChannel:              seedMessages(),
		readAcksByChannelUser:          make(map[string]ChannelReadAck),
		attachmentsByID:                make(map[string]attachmentBlob),
		channelServerByID:              make(map[string]string),
		channelTypeByID:                make(map[string]ChannelType),
		ownerUserUIDByServer:           make(map[string]string),
		requiresOwnershipClaimByServer: make(map[string]bool),
		ownershipClaimsByToken:         make(map[string]ownershipClaimState),
		leftServersByUser:              make(map[string]map[string]time.Time),
		maxAttachmentBytes:             50 * 1024 * 1024,
		maxAttachmentsPerMessage:       4,
		allowedAttachmentTypes: map[string]struct{}{
			"image/png":  {},
			"image/jpeg": {},
			"image/gif":  {},
		},
	}
	svc.indexChannels()
	svc.hydrateSeedMentionMetadata()
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

func (s *Service) CreateMessage(
	channelID string,
	authorUID string,
	body string,
	uploads []AttachmentUploadInput,
	replyToMessageID string,
) (Message, error) {
	body = strings.TrimSpace(body)
	replyToMessageID = strings.TrimSpace(replyToMessageID)

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

	var replyTo *MessageReplyReference
	if replyToMessageID != "" {
		replyMessage, found := s.findMessageByIDLocked(channelID, replyToMessageID)
		if !found {
			s.mu.Unlock()
			return Message{}, ErrReplyTargetNotFound
		}
		replyTo = &MessageReplyReference{
			MessageID:         replyMessage.ID,
			AuthorUID:         replyMessage.AuthorUID,
			AuthorDisplayName: replyMessage.AuthorUID,
			PreviewText:       buildReplyPreviewText(replyMessage.Body),
			IsUnavailable:     false,
		}
	}

	message := Message{
		ID:          "msg_" + strings.ReplaceAll(uuid.NewString()[:8], "-", ""),
		ChannelID:   channelID,
		AuthorUID:   authorUID,
		Body:        body,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		ReplyTo:     cloneMessageReplyReference(replyTo),
		Mentions:    extractMentions(body),
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

func (s *Service) findMessageByIDLocked(channelID string, messageID string) (Message, bool) {
	for _, message := range s.messagesByChannel[channelID] {
		if message.ID == messageID {
			return cloneMessage(message), true
		}
	}
	return Message{}, false
}

func buildReplyPreviewText(body string) string {
	const maxPreviewRunes = 220
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(body), "\r", ""), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts = append(parts, trimmed)
	}
	preview := strings.Join(parts, " ")
	if preview == "" {
		return ""
	}
	runes := []rune(preview)
	if len(runes) <= maxPreviewRunes {
		return preview
	}
	return string(runes[:maxPreviewRunes-1]) + "…"
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

func (s *Service) ServerIDForChannel(channelID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	serverID, ok := s.channelServerByID[channelID]
	return serverID, ok
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

func (s *Service) CreateServer(userUID string, displayName string, description string, bannerPreset string) (ServerCreated, error) {
	userUID = strings.TrimSpace(userUID)
	displayName = strings.TrimSpace(displayName)
	description = strings.TrimSpace(description)
	bannerPreset = strings.TrimSpace(strings.ToLower(bannerPreset))
	if bannerPreset == "" {
		bannerPreset = "ocean"
	}

	if userUID == "" {
		return ServerCreated{}, errors.New("user uid is required")
	}
	if displayName == "" || len([]rune(displayName)) > 100 {
		return ServerCreated{}, ErrServerDisplayNameInvalid
	}
	if len([]rune(description)) > 280 {
		return ServerCreated{}, ErrServerDescriptionInvalid
	}
	if !isValidBannerPreset(bannerPreset) {
		return ServerCreated{}, ErrServerBannerPresetInvalid
	}

	s.mu.Lock()
	now := time.Now().UTC()
	serverID := ""
	for {
		candidate := uuid.NewString()
		if s.findServerIndexLocked(candidate) >= 0 {
			continue
		}
		serverID = candidate
		break
	}

	groupID := s.nextUniqueGroupIDLocked()
	textChannelID := s.nextUniqueChannelIDLocked("ch_")
	voiceChannelID := s.nextUniqueChannelIDLocked("vc_")
	serverEntry := ServerDirectoryEntry{
		ServerID:                  serverID,
		DisplayName:               displayName,
		Description:               description,
		BannerPreset:              bannerPreset,
		IconText:                  deriveIconText(displayName, ""),
		TrustState:                "unverified",
		IdentityHandshakeStrategy: "challenge_signature",
		UserIdentifierPolicy:      "server_scoped",
	}
	s.servers = append(s.servers, serverEntry)
	s.channelGroupsByServer[serverID] = []ChannelGroup{
		{
			ID:    groupID,
			Label: "general",
			Kind:  "text",
			Channels: []Channel{
				{ID: textChannelID, Name: "general", Type: ChannelTypeText},
				{ID: voiceChannelID, Name: "General Voice", Type: ChannelTypeVoice},
			},
		},
	}
	s.membersByServer[serverID] = []Member{
		{ID: userUID, Name: userUID, Status: "online"},
	}
	s.channelServerByID[textChannelID] = serverID
	s.channelServerByID[voiceChannelID] = serverID
	s.channelTypeByID[textChannelID] = ChannelTypeText
	s.channelTypeByID[voiceChannelID] = ChannelTypeVoice
	s.messagesByChannel[textChannelID] = []Message{}
	s.messagesByChannel[voiceChannelID] = []Message{}
	s.requiresOwnershipClaimByServer[serverID] = true
	claimToken := "claim_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	claimExpiresAt := now.Add(ownershipClaimTTL)
	s.ownershipClaimsByToken[claimToken] = ownershipClaimState{
		ServerID:  serverID,
		ExpiresAt: claimExpiresAt,
	}
	s.mu.Unlock()

	return ServerCreated{
		Server:       serverEntry,
		CreatedByUID: userUID,
		CreatedAt:    now.Format(time.RFC3339),
		OwnershipClaim: ServerOwnershipClaim{
			Token:     claimToken,
			ExpiresAt: claimExpiresAt.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) ClaimServerOwnership(serverID string, userUID string, claimToken string) (ServerOwnershipClaimed, error) {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)
	claimToken = strings.TrimSpace(claimToken)
	if serverID == "" {
		return ServerOwnershipClaimed{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	if userUID == "" {
		return ServerOwnershipClaimed{}, errors.New("user uid is required")
	}
	if claimToken == "" {
		return ServerOwnershipClaimed{}, ErrOwnershipClaimInvalid
	}

	s.mu.Lock()
	if s.findServerIndexLocked(serverID) < 0 {
		s.mu.Unlock()
		return ServerOwnershipClaimed{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	ownerUID := strings.TrimSpace(s.ownerUserUIDByServer[serverID])
	if !s.requiresOwnershipClaimByServer[serverID] {
		if ownerUID == userUID {
			claimed := ServerOwnershipClaimed{
				ServerID:     serverID,
				OwnerUserUID: userUID,
				ClaimedAt:    time.Now().UTC().Format(time.RFC3339),
			}
			s.mu.Unlock()
			return claimed, nil
		}
		s.mu.Unlock()
		return ServerOwnershipClaimed{}, ErrOwnershipClaimForbidden
	}
	if ownerUID != "" && ownerUID != userUID {
		s.mu.Unlock()
		return ServerOwnershipClaimed{}, ErrOwnershipClaimForbidden
	}

	claimState, ok := s.ownershipClaimsByToken[claimToken]
	if !ok || claimState.ServerID != serverID {
		s.mu.Unlock()
		return ServerOwnershipClaimed{}, ErrOwnershipClaimInvalid
	}
	if time.Now().UTC().After(claimState.ExpiresAt) {
		delete(s.ownershipClaimsByToken, claimToken)
		s.mu.Unlock()
		return ServerOwnershipClaimed{}, ErrOwnershipClaimExpired
	}

	s.ownerUserUIDByServer[serverID] = userUID
	s.requiresOwnershipClaimByServer[serverID] = false
	for token, state := range s.ownershipClaimsByToken {
		if state.ServerID == serverID {
			delete(s.ownershipClaimsByToken, token)
		}
	}

	claimed := ServerOwnershipClaimed{
		ServerID:     serverID,
		OwnerUserUID: userUID,
		ClaimedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	s.mu.Unlock()
	return claimed, nil
}

func (s *Service) CreateChannel(serverID string, userUID string, groupID string, name string, channelType ChannelType) (ChannelCreatedEvent, error) {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)
	groupID = strings.TrimSpace(groupID)
	name = strings.TrimSpace(name)

	if serverID == "" {
		return ChannelCreatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	if userUID == "" {
		return ChannelCreatedEvent{}, errors.New("user uid is required")
	}
	if groupID == "" {
		return ChannelCreatedEvent{}, ErrChannelGroupNotFound
	}
	if name == "" || len([]rune(name)) > 100 {
		return ChannelCreatedEvent{}, ErrChannelNameInvalid
	}
	if channelType != ChannelTypeText && channelType != ChannelTypeVoice {
		return ChannelCreatedEvent{}, ErrChannelTypeUnsupported
	}

	s.mu.Lock()
	groups, ok := s.channelGroupsByServer[serverID]
	if !ok {
		s.mu.Unlock()
		return ChannelCreatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	ownerUID, ownerResolveErr := s.resolveOwnerForMutationLocked(serverID, userUID)
	if ownerResolveErr != nil {
		s.mu.Unlock()
		return ChannelCreatedEvent{}, ownerResolveErr
	}
	if ownerUID != userUID {
		s.mu.Unlock()
		return ChannelCreatedEvent{}, ErrChannelCreateForbidden
	}

	targetGroupIndex := -1
	for index, group := range groups {
		if strings.TrimSpace(group.ID) == groupID {
			targetGroupIndex = index
			break
		}
	}
	if targetGroupIndex < 0 {
		s.mu.Unlock()
		return ChannelCreatedEvent{}, ErrChannelGroupNotFound
	}

	channelPrefix := "ch_"
	if channelType == ChannelTypeVoice {
		channelPrefix = "vc_"
	}
	channelID := ""
	for {
		candidate := channelPrefix + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		if _, exists := s.channelTypeByID[candidate]; exists {
			continue
		}
		channelID = candidate
		break
	}

	channel := Channel{
		ID:   channelID,
		Name: name,
		Type: channelType,
	}
	groups[targetGroupIndex].Channels = append(groups[targetGroupIndex].Channels, channel)
	s.channelGroupsByServer[serverID] = groups
	s.channelServerByID[channelID] = serverID
	s.channelTypeByID[channelID] = channelType
	s.messagesByChannel[channelID] = []Message{}

	createdEvent := ChannelCreatedEvent{
		ServerID:     serverID,
		GroupID:      groupID,
		Channel:      channel,
		CreatedByUID: userUID,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastChannelCreated(cloneChannelCreatedEvent(createdEvent))
	}

	return cloneChannelCreatedEvent(createdEvent), nil
}

func (s *Service) CreateCategory(serverID string, userUID string, name string, kind string) (CategoryCreatedEvent, error) {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)
	name = strings.TrimSpace(name)
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = string(ChannelTypeText)
	}

	if serverID == "" {
		return CategoryCreatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	if userUID == "" {
		return CategoryCreatedEvent{}, errors.New("user uid is required")
	}
	if name == "" || len([]rune(name)) > 100 {
		return CategoryCreatedEvent{}, ErrCategoryNameInvalid
	}
	if kind != string(ChannelTypeText) && kind != string(ChannelTypeVoice) {
		return CategoryCreatedEvent{}, ErrCategoryKindUnsupported
	}

	s.mu.Lock()
	groups, ok := s.channelGroupsByServer[serverID]
	if !ok {
		s.mu.Unlock()
		return CategoryCreatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	ownerUID, ownerResolveErr := s.resolveOwnerForMutationLocked(serverID, userUID)
	if ownerResolveErr != nil {
		s.mu.Unlock()
		return CategoryCreatedEvent{}, ownerResolveErr
	}
	if ownerUID != userUID {
		s.mu.Unlock()
		return CategoryCreatedEvent{}, ErrCategoryCreateForbidden
	}

	groupID := "grp_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	group := ChannelGroup{
		ID:       groupID,
		Label:    name,
		Kind:     kind,
		Channels: []Channel{},
	}
	s.channelGroupsByServer[serverID] = append(groups, group)

	created := CategoryCreatedEvent{
		ServerID:     serverID,
		Group:        group,
		CreatedByUID: userUID,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastCategoryCreated(cloneCategoryCreatedEvent(created))
	}
	return cloneCategoryCreatedEvent(created), nil
}

func (s *Service) UpdateCategory(serverID string, userUID string, groupID string, name string) (CategoryUpdatedEvent, error) {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)
	groupID = strings.TrimSpace(groupID)
	name = strings.TrimSpace(name)

	if serverID == "" {
		return CategoryUpdatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	if userUID == "" {
		return CategoryUpdatedEvent{}, errors.New("user uid is required")
	}
	if groupID == "" {
		return CategoryUpdatedEvent{}, ErrCategoryNotFound
	}
	if name == "" || len([]rune(name)) > 100 {
		return CategoryUpdatedEvent{}, ErrCategoryNameInvalid
	}

	s.mu.Lock()
	groups, ok := s.channelGroupsByServer[serverID]
	if !ok {
		s.mu.Unlock()
		return CategoryUpdatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	ownerUID, ownerResolveErr := s.resolveOwnerForMutationLocked(serverID, userUID)
	if ownerResolveErr != nil {
		s.mu.Unlock()
		return CategoryUpdatedEvent{}, ownerResolveErr
	}
	if ownerUID != userUID {
		s.mu.Unlock()
		return CategoryUpdatedEvent{}, ErrCategoryUpdateForbidden
	}

	targetGroupIndex := -1
	for index, group := range groups {
		if strings.TrimSpace(group.ID) != groupID {
			continue
		}
		targetGroupIndex = index
		break
	}
	if targetGroupIndex < 0 {
		s.mu.Unlock()
		return CategoryUpdatedEvent{}, ErrCategoryNotFound
	}

	groups[targetGroupIndex].Label = name
	s.channelGroupsByServer[serverID] = groups

	updated := CategoryUpdatedEvent{
		ServerID:     serverID,
		Group:        cloneGroups([]ChannelGroup{groups[targetGroupIndex]})[0],
		UpdatedByUID: userUID,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastCategoryUpdated(cloneCategoryUpdatedEvent(updated))
	}
	return cloneCategoryUpdatedEvent(updated), nil
}

func (s *Service) UpdateChannelLayout(serverID string, userUID string, layout []ChannelLayoutGroup) (ChannelLayoutUpdatedEvent, error) {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)

	if serverID == "" {
		return ChannelLayoutUpdatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	if userUID == "" {
		return ChannelLayoutUpdatedEvent{}, errors.New("user uid is required")
	}

	s.mu.Lock()
	groups, ok := s.channelGroupsByServer[serverID]
	if !ok {
		s.mu.Unlock()
		return ChannelLayoutUpdatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	ownerUID, ownerResolveErr := s.resolveOwnerForMutationLocked(serverID, userUID)
	if ownerResolveErr != nil {
		s.mu.Unlock()
		return ChannelLayoutUpdatedEvent{}, ownerResolveErr
	}
	if ownerUID != userUID {
		s.mu.Unlock()
		return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutForbidden
	}

	existingGroupsByID := make(map[string]ChannelGroup, len(groups))
	existingChannelsByID := make(map[string]Channel)
	for _, group := range groups {
		existingGroupsByID[group.ID] = group
		for _, channel := range group.Channels {
			existingChannelsByID[channel.ID] = channel
		}
	}

	if len(layout) != len(groups) {
		s.mu.Unlock()
		return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
	}

	seenGroupIDs := make(map[string]struct{}, len(layout))
	seenChannelIDs := make(map[string]struct{}, len(existingChannelsByID))
	nextGroups := make([]ChannelGroup, 0, len(layout))
	for _, requestedGroup := range layout {
		groupID := strings.TrimSpace(requestedGroup.ID)
		if groupID == "" {
			s.mu.Unlock()
			return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
		}
		sourceGroup, groupExists := existingGroupsByID[groupID]
		if !groupExists {
			s.mu.Unlock()
			return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
		}
		if _, duplicateGroup := seenGroupIDs[groupID]; duplicateGroup {
			s.mu.Unlock()
			return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
		}
		seenGroupIDs[groupID] = struct{}{}

		nextChannels := make([]Channel, 0, len(requestedGroup.ChannelIDs))
		for _, rawChannelID := range requestedGroup.ChannelIDs {
			channelID := strings.TrimSpace(rawChannelID)
			if channelID == "" {
				s.mu.Unlock()
				return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
			}
			channel, channelExists := existingChannelsByID[channelID]
			if !channelExists {
				s.mu.Unlock()
				return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
			}
			if _, duplicateChannel := seenChannelIDs[channelID]; duplicateChannel {
				s.mu.Unlock()
				return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
			}
			seenChannelIDs[channelID] = struct{}{}
			nextChannels = append(nextChannels, channel)
		}

		nextGroups = append(nextGroups, ChannelGroup{
			ID:       sourceGroup.ID,
			Label:    sourceGroup.Label,
			Kind:     sourceGroup.Kind,
			Channels: nextChannels,
		})
	}

	if len(seenGroupIDs) != len(groups) || len(seenChannelIDs) != len(existingChannelsByID) {
		s.mu.Unlock()
		return ChannelLayoutUpdatedEvent{}, ErrChannelLayoutInvalid
	}

	s.channelGroupsByServer[serverID] = nextGroups

	updated := ChannelLayoutUpdatedEvent{
		ServerID:     serverID,
		Groups:       cloneGroups(nextGroups),
		UpdatedByUID: userUID,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastChannelLayoutUpdated(cloneChannelLayoutUpdatedEvent(updated))
	}
	return cloneChannelLayoutUpdatedEvent(updated), nil
}

func (s *Service) GetServerSettings(serverID string) (ServerSettings, error) {
	serverID = strings.TrimSpace(serverID)
	if serverID == "" {
		return ServerSettings{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.findServerEntryLocked(serverID)
	if !ok {
		return ServerSettings{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	return ServerSettings{
		ServerID:     entry.ServerID,
		DisplayName:  entry.DisplayName,
		Description:  entry.Description,
		BannerPreset: entry.BannerPreset,
	}, nil
}

func (s *Service) UpdateServerSettings(
	serverID string,
	userUID string,
	displayName string,
	description string,
	bannerPreset string,
) (ServerUpdatedEvent, error) {
	serverID = strings.TrimSpace(serverID)
	userUID = strings.TrimSpace(userUID)
	displayName = strings.TrimSpace(displayName)
	description = strings.TrimSpace(description)
	bannerPreset = strings.TrimSpace(strings.ToLower(bannerPreset))

	if serverID == "" {
		return ServerUpdatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}
	if userUID == "" {
		return ServerUpdatedEvent{}, errors.New("user uid is required")
	}
	if displayName == "" || len([]rune(displayName)) > 100 {
		return ServerUpdatedEvent{}, ErrServerDisplayNameInvalid
	}
	if len([]rune(description)) > 280 {
		return ServerUpdatedEvent{}, ErrServerDescriptionInvalid
	}
	if !isValidBannerPreset(bannerPreset) {
		return ServerUpdatedEvent{}, ErrServerBannerPresetInvalid
	}

	s.mu.Lock()
	serverIndex := s.findServerIndexLocked(serverID)
	if serverIndex < 0 {
		s.mu.Unlock()
		return ServerUpdatedEvent{}, fmt.Errorf("unknown server id: %s", serverID)
	}

	ownerUID, ownerResolveErr := s.resolveOwnerForMutationLocked(serverID, userUID)
	if ownerResolveErr != nil {
		s.mu.Unlock()
		return ServerUpdatedEvent{}, ownerResolveErr
	}
	if ownerUID != userUID {
		s.mu.Unlock()
		return ServerUpdatedEvent{}, ErrServerSettingsForbidden
	}

	entry := s.servers[serverIndex]
	entry.DisplayName = displayName
	entry.Description = description
	entry.BannerPreset = bannerPreset
	entry.IconText = deriveIconText(displayName, entry.IconText)
	s.servers[serverIndex] = entry

	updated := ServerUpdatedEvent{
		ServerID:     serverID,
		DisplayName:  entry.DisplayName,
		Description:  entry.Description,
		BannerPreset: entry.BannerPreset,
		UpdatedByUID: userUID,
		UpdatedAt:    time.Now().UTC().Format(time.RFC3339),
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastServerUpdated(cloneServerUpdatedEvent(updated))
	}
	return cloneServerUpdatedEvent(updated), nil
}

func (s *Service) ensureServerOwnerLocked(serverID string, userUID string) string {
	ownerUID := strings.TrimSpace(s.ownerUserUIDByServer[serverID])
	if ownerUID == "" {
		ownerUID = userUID
		s.ownerUserUIDByServer[serverID] = ownerUID
	}
	return ownerUID
}

func (s *Service) resolveOwnerForMutationLocked(serverID string, userUID string) (string, error) {
	if s.requiresOwnershipClaimByServer[serverID] {
		ownerUID := strings.TrimSpace(s.ownerUserUIDByServer[serverID])
		if ownerUID == "" {
			return "", ErrOwnershipClaimRequired
		}
		return ownerUID, nil
	}
	return s.ensureServerOwnerLocked(serverID, userUID), nil
}

func (s *Service) nextUniqueGroupIDLocked() string {
	for {
		candidate := "grp_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		collides := false
		for _, groups := range s.channelGroupsByServer {
			for _, group := range groups {
				if group.ID != candidate {
					continue
				}
				collides = true
				break
			}
			if collides {
				break
			}
		}
		if collides {
			continue
		}
		return candidate
	}
}

func (s *Service) nextUniqueChannelIDLocked(prefix string) string {
	for {
		candidate := prefix + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		if _, exists := s.channelTypeByID[candidate]; exists {
			continue
		}
		return candidate
	}
}

func (s *Service) findServerIndexLocked(serverID string) int {
	for index, server := range s.servers {
		if strings.TrimSpace(server.ServerID) == serverID {
			return index
		}
	}
	return -1
}

func (s *Service) findServerEntryLocked(serverID string) (ServerDirectoryEntry, bool) {
	index := s.findServerIndexLocked(serverID)
	if index < 0 {
		return ServerDirectoryEntry{}, false
	}
	return s.servers[index], true
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
	out.ReplyTo = cloneMessageReplyReference(message.ReplyTo)
	out.Mentions = cloneMessageMentions(message.Mentions)
	if len(message.Attachments) > 0 {
		out.Attachments = make([]MessageAttachment, len(message.Attachments))
		for idx, attachment := range message.Attachments {
			out.Attachments[idx] = cloneMessageAttachment(attachment)
		}
	}
	return out
}

func cloneMessageReplyReference(reply *MessageReplyReference) *MessageReplyReference {
	if reply == nil {
		return nil
	}
	cloned := *reply
	return &cloned
}

func cloneMessageAttachment(attachment MessageAttachment) MessageAttachment {
	return attachment
}

func cloneChannelCreatedEvent(event ChannelCreatedEvent) ChannelCreatedEvent {
	return ChannelCreatedEvent{
		ServerID:     event.ServerID,
		GroupID:      event.GroupID,
		Channel:      event.Channel,
		CreatedByUID: event.CreatedByUID,
		CreatedAt:    event.CreatedAt,
	}
}

func cloneCategoryCreatedEvent(event CategoryCreatedEvent) CategoryCreatedEvent {
	return CategoryCreatedEvent{
		ServerID:     event.ServerID,
		Group:        cloneGroups([]ChannelGroup{event.Group})[0],
		CreatedByUID: event.CreatedByUID,
		CreatedAt:    event.CreatedAt,
	}
}

func cloneCategoryUpdatedEvent(event CategoryUpdatedEvent) CategoryUpdatedEvent {
	return CategoryUpdatedEvent{
		ServerID:     event.ServerID,
		Group:        cloneGroups([]ChannelGroup{event.Group})[0],
		UpdatedByUID: event.UpdatedByUID,
		UpdatedAt:    event.UpdatedAt,
	}
}

func cloneChannelLayoutUpdatedEvent(event ChannelLayoutUpdatedEvent) ChannelLayoutUpdatedEvent {
	return ChannelLayoutUpdatedEvent{
		ServerID:     event.ServerID,
		Groups:       cloneGroups(event.Groups),
		UpdatedByUID: event.UpdatedByUID,
		UpdatedAt:    event.UpdatedAt,
	}
}

func cloneServerUpdatedEvent(event ServerUpdatedEvent) ServerUpdatedEvent {
	return ServerUpdatedEvent{
		ServerID:     event.ServerID,
		DisplayName:  event.DisplayName,
		Description:  event.Description,
		BannerPreset: event.BannerPreset,
		UpdatedByUID: event.UpdatedByUID,
		UpdatedAt:    event.UpdatedAt,
	}
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

func deriveIconText(displayName string, fallback string) string {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		fallback = strings.TrimSpace(strings.ToUpper(fallback))
		if fallback == "" {
			return "SV"
		}
		if len(fallback) > 2 {
			return fallback[:2]
		}
		return fallback
	}
	parts := strings.Fields(displayName)
	if len(parts) >= 2 {
		return strings.ToUpper(string([]rune(parts[0])[0]) + string([]rune(parts[1])[0]))
	}
	runes := []rune(strings.ToUpper(displayName))
	if len(runes) == 1 {
		return string(runes[0])
	}
	if len(runes) >= 2 {
		return string(runes[:2])
	}
	return "SV"
}

func isValidBannerPreset(value string) bool {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "ocean", "sunset", "ember", "forest", "midnight", "orchid", "teal", "gold", "slate":
		return true
	default:
		return false
	}
}

func seedServerDirectory() []ServerDirectoryEntry {
	return []ServerDirectoryEntry{
		{
			ServerID:                  SeedServerIDHarbor,
			DisplayName:               "Harbor Guild",
			Description:               "Core OpenChat workspace for day-to-day collaboration.",
			BannerPreset:              "ocean",
			IconText:                  "HG",
			TrustState:                "verified",
			IdentityHandshakeStrategy: "challenge_signature",
			UserIdentifierPolicy:      "server_scoped",
		},
		{
			ServerID:                  SeedServerIDTestLab,
			DisplayName:               "TestLab Server",
			Description:               "Experimental server used for integration and QA checks.",
			BannerPreset:              "slate",
			IconText:                  "TL",
			TrustState:                "verified",
			IdentityHandshakeStrategy: "challenge_signature",
			UserIdentifierPolicy:      "server_scoped",
		},
	}
}

func seedChannelGroups() map[string][]ChannelGroup {
	return map[string][]ChannelGroup{
		SeedServerIDHarbor: {
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
		SeedServerIDTestLab: {
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
		SeedServerIDHarbor: {
			{ID: "mem_1", Name: "Lyra", Status: "online"},
			{ID: "mem_2", Name: "Orin", Status: "idle"},
			{ID: "mem_3", Name: "Mira", Status: "online"},
			{ID: "mem_4", Name: "Calix", Status: "dnd"},
		},
		SeedServerIDTestLab: {
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
