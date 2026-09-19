package chat

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type BulkReadAckInput struct {
	ChannelID         string `json:"channel_id"`
	LastReadMessageID string `json:"last_read_message_id"`
}

var ErrBulkReadAckInvalid = errors.New("invalid bulk read ack")

// UpdateReadAcksBulk validates every channel and cursor before applying any
// update. Returned cursors are authoritative and never move backward.
func (s *Service) UpdateReadAcksBulk(serverID, userUID string, input []BulkReadAckInput) ([]ChannelReadAck, error) {
	if strings.TrimSpace(serverID) == "" || strings.TrimSpace(userUID) == "" || len(input) > 500 {
		return nil, ErrBulkReadAckInvalid
	}
	s.mu.Lock()
	type candidate struct {
		channelID, messageID string
		index                int
	}
	candidates := make([]candidate, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, item := range input {
		channelID := strings.TrimSpace(item.ChannelID)
		if channelID == "" || s.channelServerByID[channelID] != serverID || s.channelTypeByID[channelID] != ChannelTypeText {
			s.mu.Unlock()
			return nil, ErrBulkReadAckInvalid
		}
		if _, exists := seen[channelID]; exists {
			s.mu.Unlock()
			return nil, ErrBulkReadAckInvalid
		}
		seen[channelID] = struct{}{}
		messageID := strings.TrimSpace(item.LastReadMessageID)
		index := -1
		if messageID == "" {
			messages := s.messagesByChannel[channelID]
			if len(messages) > 0 {
				index = len(messages) - 1
				messageID = messages[index].ID
			}
		} else {
			var found bool
			index, found = s.findMessageIndexByIDLocked(channelID, messageID)
			if !found {
				s.mu.Unlock()
				return nil, fmt.Errorf("%w: %s", ErrReadAckMessageNotFound, messageID)
			}
		}
		candidates = append(candidates, candidate{channelID, messageID, index})
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result := make([]ChannelReadAck, 0, len(candidates))
	updates := make([]ChannelReadAckUpdate, 0, len(candidates))
	for _, item := range candidates {
		key := readAckKey(item.channelID, userUID)
		existing, exists := s.readAcksByChannelUser[key]
		oldIndex := -1
		if exists && existing.CursorIndex != nil {
			oldIndex = *existing.CursorIndex
		}
		if exists && item.index <= oldIndex {
			result = append(result, cloneReadAck(existing))
			continue
		}
		next := ChannelReadAck{ChannelID: item.channelID, UserUID: userUID, LastReadMessageID: item.messageID, AckedAt: now}
		if item.index >= 0 {
			cursor := item.index
			next.CursorIndex = &cursor
		}
		s.readAcksByChannelUser[key] = cloneReadAck(next)
		result = append(result, cloneReadAck(next))
		updates = append(updates, ChannelReadAckUpdate{ChannelID: next.ChannelID, UserUID: next.UserUID, LastReadMessageID: next.LastReadMessageID, AckedAt: next.AckedAt, CursorIndex: cloneOptionalInt(next.CursorIndex)})
	}
	broadcaster := s.broadcaster
	s.mu.Unlock()
	if broadcaster != nil {
		for _, update := range updates {
			broadcaster.BroadcastReadAck(update)
		}
	}
	return result, nil
}
