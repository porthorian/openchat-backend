package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/openchat/openchat-backend/internal/chat"
)

const multipartBodySlackBytes = 16 * 1024

var (
	errInvalidMessagePayload   = errors.New("invalid message payload")
	errInvalidMultipartPayload = errors.New("invalid multipart message payload")
	errAttachmentReadFailed    = errors.New("unable to read attachment upload")
	errAttachmentTooLarge      = errors.New("attachment exceeds max upload size")
	errAttachmentCountExceeded = errors.New("too many attachments in one message")
)

func (s *Server) listChannelGroups(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	groups, err := s.chat.ListChannelGroups(serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": serverID,
		"groups":    groups,
	})
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	serverID := strings.TrimSpace(chi.URLParam(r, "serverID"))
	members, err := s.chat.ListMembers(serverID)
	if err != nil {
		writeError(w, http.StatusNotFound, "server_not_found", err.Error(), false)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": serverID,
		"members":   members,
	})
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	limit := 100
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err == nil && parsed > 0 {
			limit = parsed
		}
	}

	messages, err := s.chat.ListMessages(channelID, limit)
	if err != nil {
		writeError(w, http.StatusNotFound, "channel_not_found", err.Error(), false)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channel_id": channelID,
		"messages":   messages,
	})
}

func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	if channelID == "" {
		writeError(w, http.StatusBadRequest, "invalid_channel", "channel id is required", false)
		return
	}

	body, replyToMessageID, uploads, payloadErr := parseCreateMessagePayload(w, r, s.chat)
	if payloadErr != nil {
		switch {
		case errors.Is(payloadErr, errAttachmentTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "attachment_too_large", "attachment exceeds max upload size", false)
		case errors.Is(payloadErr, errAttachmentCountExceeded):
			writeError(w, http.StatusBadRequest, "attachment_count_exceeded", "too many attachments in one message", false)
		case errors.Is(payloadErr, errAttachmentReadFailed):
			writeError(w, http.StatusBadRequest, "invalid_payload", "unable to read attachment upload", false)
		case errors.Is(payloadErr, errInvalidMultipartPayload):
			writeError(w, http.StatusBadRequest, "invalid_payload", "invalid multipart message payload", false)
		default:
			writeError(w, http.StatusBadRequest, "invalid_payload", "invalid message payload", false)
		}
		return
	}

	requester := requesterFromContext(r.Context())
	message, err := s.chat.CreateMessage(channelID, requester.UserUID, body, uploads, replyToMessageID)
	if err != nil {
		switch {
		case errors.Is(err, chat.ErrMessageEmpty):
			writeError(w, http.StatusBadRequest, "message_empty", "message body or attachment is required", false)
		case errors.Is(err, chat.ErrReplyTargetNotFound):
			writeError(w, http.StatusBadRequest, "reply_target_not_found", "reply target message not found", false)
		case errors.Is(err, chat.ErrTooManyAttachments):
			writeError(w, http.StatusBadRequest, "attachment_count_exceeded", "too many attachments in one message", false)
		case errors.Is(err, chat.ErrAttachmentTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "attachment_too_large", "attachment exceeds max upload size", false)
		case errors.Is(err, chat.ErrAttachmentTypeUnsupported):
			writeError(w, http.StatusUnsupportedMediaType, "attachment_type_unsupported", "attachment mime type is unsupported", false)
		case errors.Is(err, chat.ErrAttachmentImageInvalid):
			writeError(w, http.StatusBadRequest, "attachment_invalid_image", "attachment image payload is invalid", false)
		default:
			writeError(w, http.StatusBadRequest, "message_create_failed", err.Error(), false)
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"message": message,
	})
}

func (s *Server) getMessageAttachment(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(chi.URLParam(r, "channelID"))
	attachmentID := strings.TrimSpace(chi.URLParam(r, "attachmentID"))
	attachment, content, err := s.chat.AttachmentContent(channelID, attachmentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "attachment_not_found", "attachment not found", false)
		return
	}

	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func parseCreateMessagePayload(
	w http.ResponseWriter,
	r *http.Request,
	chatService *chat.Service,
) (string, string, []chat.AttachmentUploadInput, error) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		maxBytes, maxFiles, _ := chatService.AttachmentUploadRules()
		maxBodyBytes := int64(maxBytes*maxFiles + multipartBodySlackBytes)
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		if err := r.ParseMultipartForm(maxBodyBytes); err != nil {
			return "", "", nil, errInvalidMultipartPayload
		}
		if r.MultipartForm == nil {
			return "", "", nil, errInvalidMultipartPayload
		}

		files := r.MultipartForm.File["files"]
		if len(files) > maxFiles {
			return "", "", nil, errAttachmentCountExceeded
		}

		uploads := make([]chat.AttachmentUploadInput, 0, len(files))
		for _, header := range files {
			file, openErr := header.Open()
			if openErr != nil {
				return "", "", nil, errAttachmentReadFailed
			}

			content, readErr := io.ReadAll(io.LimitReader(file, int64(maxBytes+1)))
			closeErr := file.Close()
			if readErr != nil || closeErr != nil {
				return "", "", nil, errAttachmentReadFailed
			}
			if len(content) > maxBytes {
				return "", "", nil, errAttachmentTooLarge
			}

			uploads = append(uploads, chat.AttachmentUploadInput{
				FileName:    header.Filename,
				ContentType: strings.TrimSpace(header.Header.Get("Content-Type")),
				Data:        content,
			})
		}

		return r.FormValue("body"), strings.TrimSpace(r.FormValue("reply_to_message_id")), uploads, nil
	}

	var body struct {
		Body             string `json:"body"`
		ReplyToMessageID string `json:"reply_to_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", "", nil, errInvalidMessagePayload
	}
	return body.Body, strings.TrimSpace(body.ReplyToMessageID), nil, nil
}

func (s *Server) realtimeWS(w http.ResponseWriter, r *http.Request) {
	s.realtime.ServeWS(w, r)
}
