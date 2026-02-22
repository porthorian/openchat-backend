package chat

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type MessageMentionType string

const (
	MessageMentionTypeUser    MessageMentionType = "user"
	MessageMentionTypeChannel MessageMentionType = "channel"
)

type MessageMentionRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type MessageMention struct {
	Type        MessageMentionType   `json:"type"`
	Token       string               `json:"token,omitempty"`
	TargetID    string               `json:"target_id,omitempty"`
	DisplayText string               `json:"display_text,omitempty"`
	Range       *MessageMentionRange `json:"range,omitempty"`
}

type MentionCandidate struct {
	Type        MessageMentionType `json:"type"`
	Token       string             `json:"token,omitempty"`
	TargetID    string             `json:"target_id,omitempty"`
	DisplayText string             `json:"display_text"`
}

type ChannelReadAck struct {
	ChannelID         string `json:"channel_id"`
	UserUID           string `json:"user_uid"`
	LastReadMessageID string `json:"last_read_message_id,omitempty"`
	AckedAt           string `json:"acked_at,omitempty"`
	CursorIndex       *int   `json:"cursor_index,omitempty"`
}

type ChannelReadAckUpdate struct {
	ChannelID         string `json:"channel_id"`
	UserUID           string `json:"user_uid"`
	LastReadMessageID string `json:"last_read_message_id,omitempty"`
	AckedAt           string `json:"acked_at,omitempty"`
	CursorIndex       *int   `json:"cursor_index,omitempty"`
}

var ErrReadAckMessageNotFound = errors.New("read ack message not found")

func (s *Service) ResolveMentionCandidates(channelID string, requesterUID string, query string, limit int) ([]MentionCandidate, error) {
	channelID = strings.TrimSpace(channelID)
	requesterUID = strings.TrimSpace(requesterUID)
	query = normalizeMentionQuery(query)
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.channelTypeByID[channelID]; !ok {
		return nil, fmt.Errorf("unknown channel id: %s", channelID)
	}

	candidates := make([]MentionCandidate, 0, limit)

	channelTokens := []string{"@here", "@channel"}
	for _, token := range channelTokens {
		if !matchesMentionQuery(token, query) {
			continue
		}
		candidates = append(candidates, MentionCandidate{
			Type:        MessageMentionTypeChannel,
			Token:       token,
			TargetID:    strings.TrimPrefix(token, "@"),
			DisplayText: strings.TrimPrefix(token, "@"),
		})
		if len(candidates) >= limit {
			return candidates[:limit], nil
		}
	}

	serverID := s.channelServerByID[channelID]
	knownUserUIDs := make(map[string]struct{})
	if requesterUID != "" {
		knownUserUIDs[requesterUID] = struct{}{}
	}
	if serverID != "" {
		for _, group := range s.channelGroupsByServer[serverID] {
			for _, channel := range group.Channels {
				if channel.Type != ChannelTypeText {
					continue
				}
				for _, message := range s.messagesByChannel[channel.ID] {
					uid := strings.TrimSpace(message.AuthorUID)
					if uid == "" {
						continue
					}
					knownUserUIDs[uid] = struct{}{}
				}
			}
		}
	}

	userUIDs := make([]string, 0, len(knownUserUIDs))
	for uid := range knownUserUIDs {
		if !matchesMentionQuery(uid, query) {
			continue
		}
		userUIDs = append(userUIDs, uid)
	}
	sort.Strings(userUIDs)

	for _, uid := range userUIDs {
		candidates = append(candidates, MentionCandidate{
			Type:        MessageMentionTypeUser,
			Token:       "@" + uid,
			TargetID:    uid,
			DisplayText: uid,
		})
		if len(candidates) >= limit {
			return candidates[:limit], nil
		}
	}

	return candidates, nil
}

func (s *Service) GetReadAck(channelID string, userUID string) (ChannelReadAck, error) {
	channelID = strings.TrimSpace(channelID)
	userUID = strings.TrimSpace(userUID)

	s.mu.RLock()
	defer s.mu.RUnlock()

	if _, ok := s.channelTypeByID[channelID]; !ok {
		return ChannelReadAck{}, fmt.Errorf("unknown channel id: %s", channelID)
	}
	if userUID == "" {
		return ChannelReadAck{}, errors.New("user uid is required")
	}

	readAck, ok := s.readAcksByChannelUser[readAckKey(channelID, userUID)]
	if !ok {
		return ChannelReadAck{
			ChannelID: channelID,
			UserUID:   userUID,
		}, nil
	}
	return cloneReadAck(readAck), nil
}

func (s *Service) UpdateReadAck(channelID string, userUID string, lastReadMessageID string) (ChannelReadAck, bool, error) {
	channelID = strings.TrimSpace(channelID)
	userUID = strings.TrimSpace(userUID)
	lastReadMessageID = strings.TrimSpace(lastReadMessageID)

	s.mu.Lock()
	if _, ok := s.channelTypeByID[channelID]; !ok {
		s.mu.Unlock()
		return ChannelReadAck{}, false, fmt.Errorf("unknown channel id: %s", channelID)
	}
	if userUID == "" {
		s.mu.Unlock()
		return ChannelReadAck{}, false, errors.New("user uid is required")
	}

	newCursorIndex := -1
	if lastReadMessageID == "" {
		if messages := s.messagesByChannel[channelID]; len(messages) > 0 {
			last := messages[len(messages)-1]
			lastReadMessageID = last.ID
			newCursorIndex = len(messages) - 1
		}
	} else {
		index, found := s.findMessageIndexByIDLocked(channelID, lastReadMessageID)
		if !found {
			s.mu.Unlock()
			return ChannelReadAck{}, false, ErrReadAckMessageNotFound
		}
		newCursorIndex = index
	}

	ackKey := readAckKey(channelID, userUID)
	existing, hasExisting := s.readAcksByChannelUser[ackKey]
	existingCursorIndex := -1
	if hasExisting && existing.CursorIndex != nil {
		existingCursorIndex = *existing.CursorIndex
	}

	if hasExisting {
		if newCursorIndex < existingCursorIndex {
			s.mu.Unlock()
			return cloneReadAck(existing), false, nil
		}
		if newCursorIndex == existingCursorIndex && strings.EqualFold(existing.LastReadMessageID, lastReadMessageID) {
			s.mu.Unlock()
			return cloneReadAck(existing), false, nil
		}
	}

	updated := ChannelReadAck{
		ChannelID:         channelID,
		UserUID:           userUID,
		LastReadMessageID: lastReadMessageID,
		AckedAt:           time.Now().UTC().Format(time.RFC3339),
	}
	if newCursorIndex >= 0 {
		cursor := newCursorIndex
		updated.CursorIndex = &cursor
	}

	s.readAcksByChannelUser[ackKey] = cloneReadAck(updated)
	broadcaster := s.broadcaster
	updatePayload := ChannelReadAckUpdate{
		ChannelID:         updated.ChannelID,
		UserUID:           updated.UserUID,
		LastReadMessageID: updated.LastReadMessageID,
		AckedAt:           updated.AckedAt,
		CursorIndex:       cloneOptionalInt(updated.CursorIndex),
	}
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastReadAck(updatePayload)
	}

	return cloneReadAck(updated), true, nil
}

func (s *Service) findMessageIndexByIDLocked(channelID string, messageID string) (int, bool) {
	for index, message := range s.messagesByChannel[channelID] {
		if message.ID == messageID {
			return index, true
		}
	}
	return -1, false
}

func readAckKey(channelID string, userUID string) string {
	return channelID + "|" + userUID
}

func cloneReadAck(readAck ChannelReadAck) ChannelReadAck {
	out := readAck
	out.CursorIndex = cloneOptionalInt(readAck.CursorIndex)
	return out
}

func cloneOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneMessageMentions(mentions []MessageMention) []MessageMention {
	if len(mentions) == 0 {
		return nil
	}
	cloned := make([]MessageMention, len(mentions))
	for index, mention := range mentions {
		cloned[index] = MessageMention{
			Type:        mention.Type,
			Token:       mention.Token,
			TargetID:    mention.TargetID,
			DisplayText: mention.DisplayText,
			Range:       cloneMessageMentionRange(mention.Range),
		}
	}
	return cloned
}

func cloneMessageMentionRange(r *MessageMentionRange) *MessageMentionRange {
	if r == nil {
		return nil
	}
	cloned := *r
	return &cloned
}

func extractMentions(body string) []MessageMention {
	if body == "" {
		return nil
	}

	mentions := make([]MessageMention, 0, 4)
	for index := 0; index < len(body); {
		r, size := utf8.DecodeRuneInString(body[index:])
		if r == utf8.RuneError && size == 1 {
			index += size
			continue
		}

		if r != '@' {
			index += size
			continue
		}

		if index > 0 {
			previousRune, _ := utf8.DecodeLastRuneInString(body[:index])
			if isMentionTokenRune(previousRune) {
				index += size
				continue
			}
		}

		tokenStart := index + size
		cursor := tokenStart
		for cursor < len(body) {
			nextRune, nextSize := utf8.DecodeRuneInString(body[cursor:])
			if nextRune == utf8.RuneError && nextSize == 1 {
				break
			}
			if !isMentionTokenRune(nextRune) {
				break
			}
			cursor += nextSize
		}

		if cursor == tokenStart {
			index += size
			continue
		}

		rawToken := body[tokenStart:cursor]
		normalizedToken := strings.ToLower(rawToken)
		mention := MessageMention{
			Type:        MessageMentionTypeUser,
			Token:       "@" + rawToken,
			TargetID:    rawToken,
			DisplayText: rawToken,
			Range: &MessageMentionRange{
				Start: index,
				End:   cursor,
			},
		}

		switch normalizedToken {
		case "here", "channel":
			mention.Type = MessageMentionTypeChannel
			mention.Token = "@" + normalizedToken
			mention.TargetID = normalizedToken
			mention.DisplayText = normalizedToken
		}

		mentions = append(mentions, mention)
		index = cursor
	}

	if len(mentions) == 0 {
		return nil
	}
	return mentions
}

func normalizeMentionQuery(query string) string {
	trimmed := strings.TrimSpace(strings.ToLower(query))
	trimmed = strings.TrimPrefix(trimmed, "@")
	return trimmed
}

func matchesMentionQuery(candidate string, query string) bool {
	if query == "" {
		return true
	}
	normalizedCandidate := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(candidate), "@"))
	return strings.HasPrefix(normalizedCandidate, query) || strings.Contains(normalizedCandidate, query)
}

func isMentionTokenRune(value rune) bool {
	if value == '_' || value == '-' || value == '.' {
		return true
	}
	return unicode.IsLetter(value) || unicode.IsDigit(value)
}

func (s *Service) hydrateSeedMentionMetadata() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for channelID, messages := range s.messagesByChannel {
		if len(messages) == 0 {
			continue
		}
		next := make([]Message, len(messages))
		for index, message := range messages {
			cloned := cloneMessage(message)
			if len(cloned.Mentions) == 0 {
				cloned.Mentions = extractMentions(cloned.Body)
			}
			next[index] = cloned
		}
		s.messagesByChannel[channelID] = next
	}
}
