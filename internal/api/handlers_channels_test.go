package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/chat"
)

var onePixelPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92,
	0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e,
	0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestCreateMessageWithImageAttachment(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("body", "pasted image @here @uid_attachment_test"); err != nil {
		t.Fatalf("write body field: %v", err)
	}
	if err := writer.WriteField("reply_to_message_id", "msg_seed_01"); err != nil {
		t.Fatalf("write reply_to_message_id field: %v", err)
	}
	fileWriter, err := writer.CreateFormFile("files", "image.png")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := fileWriter.Write(onePixelPNG); err != nil {
		t.Fatalf("write png payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/channels/ch_general/messages", &body)
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	req.Header.Set("X-OpenChat-User-UID", "uid_attachment_test")
	req.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send create request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected create status: %d body=%s", resp.StatusCode, string(payload))
	}

	var created struct {
		Message struct {
			Body    string `json:"body"`
			ReplyTo *struct {
				MessageID   string `json:"message_id"`
				AuthorUID   string `json:"author_uid"`
				PreviewText string `json:"preview_text"`
			} `json:"reply_to"`
			Mentions []struct {
				Type     string `json:"type"`
				Token    string `json:"token"`
				TargetID string `json:"target_id"`
			} `json:"mentions"`
			Attachments []struct {
				AttachmentID string `json:"attachment_id"`
				URL          string `json:"url"`
				ContentType  string `json:"content_type"`
			} `json:"attachments"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Message.Body != "pasted image @here @uid_attachment_test" {
		t.Fatalf("expected body to round-trip, got %q", created.Message.Body)
	}
	if len(created.Message.Mentions) == 0 {
		t.Fatalf("expected mentions payload in created message")
	}
	var hasHereMention bool
	var hasUserMention bool
	for _, mention := range created.Message.Mentions {
		if mention.Type == "channel" && mention.Token == "@here" {
			hasHereMention = true
		}
		if mention.Type == "user" && mention.TargetID == "uid_attachment_test" {
			hasUserMention = true
		}
	}
	if !hasHereMention {
		t.Fatalf("expected @here mention metadata")
	}
	if !hasUserMention {
		t.Fatalf("expected user mention metadata")
	}
	if created.Message.ReplyTo == nil {
		t.Fatalf("expected reply_to payload")
	}
	if created.Message.ReplyTo.MessageID != "msg_seed_01" {
		t.Fatalf("expected reply message id msg_seed_01, got %q", created.Message.ReplyTo.MessageID)
	}
	if created.Message.ReplyTo.AuthorUID == "" {
		t.Fatalf("expected reply author uid in payload")
	}
	if created.Message.ReplyTo.PreviewText == "" {
		t.Fatalf("expected reply preview text in payload")
	}
	if len(created.Message.Attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(created.Message.Attachments))
	}
	attachment := created.Message.Attachments[0]
	if attachment.AttachmentID == "" {
		t.Fatalf("expected attachment_id in response")
	}
	if attachment.URL == "" {
		t.Fatalf("expected attachment url in response")
	}
	if attachment.ContentType != "image/png" {
		t.Fatalf("expected image/png content type, got %s", attachment.ContentType)
	}

	assetResp, err := http.Get(ts.URL + "/v1/channels/ch_general/attachments/" + attachment.AttachmentID)
	if err != nil {
		t.Fatalf("fetch attachment: %v", err)
	}
	defer assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(assetResp.Body)
		t.Fatalf("unexpected attachment status: %d body=%s", assetResp.StatusCode, string(payload))
	}
	if assetResp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("unexpected attachment content type: %s", assetResp.Header.Get("Content-Type"))
	}
	assetBody, err := io.ReadAll(assetResp.Body)
	if err != nil {
		t.Fatalf("read attachment body: %v", err)
	}
	if !bytes.Equal(assetBody, onePixelPNG) {
		t.Fatalf("attachment bytes mismatch")
	}
}

func TestCreateMessageRejectsEmptyTextAndAttachments(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	payload := map[string]string{"body": "   "}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/channels/ch_general/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	req.Header.Set("X-OpenChat-User-UID", "uid_attachment_test")
	req.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send create request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(payload))
	}

	var apiErr struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if apiErr.Code != "message_empty" {
		t.Fatalf("expected message_empty code, got %s", apiErr.Code)
	}
}

func TestCreateMessageRejectsUnknownReplyTarget(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	payload := map[string]string{
		"body":                "reply target should fail",
		"reply_to_message_id": "msg_missing_404",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/channels/ch_general/messages", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	req.Header.Set("X-OpenChat-User-UID", "uid_attachment_test")
	req.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("send create request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status: %d body=%s", resp.StatusCode, string(payload))
	}

	var apiErr struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if apiErr.Code != "reply_target_not_found" {
		t.Fatalf("expected reply_target_not_found code, got %s", apiErr.Code)
	}
}

func TestCreateChannelSuccessAndServerListingIncludesCreatedChannel(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	createPayload := map[string]string{
		"name":     "team-standup",
		"type":     "voice",
		"group_id": "grp_voice",
	}
	rawPayload, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("marshal create payload: %v", err)
	}

	createReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/channels", bytes.NewReader(rawPayload))
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	createReq.Header.Set("X-OpenChat-User-UID", "uid_channel_owner")
	createReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create channel request failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("unexpected create channel status: %d body=%s", createResp.StatusCode, string(body))
	}

	var created struct {
		ServerID     string `json:"server_id"`
		GroupID      string `json:"group_id"`
		CreatedByUID string `json:"created_by_uid"`
		CreatedAt    string `json:"created_at"`
		Channel      struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"channel"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ServerID != chat.SeedServerIDHarbor {
		t.Fatalf("expected harbor server id, got %s", created.ServerID)
	}
	if created.GroupID != "grp_voice" {
		t.Fatalf("expected group_id grp_voice, got %s", created.GroupID)
	}
	if created.CreatedByUID != "uid_channel_owner" {
		t.Fatalf("expected created_by_uid uid_channel_owner, got %s", created.CreatedByUID)
	}
	if created.Channel.ID == "" {
		t.Fatalf("expected created channel id")
	}
	if created.Channel.Type != "voice" {
		t.Fatalf("expected channel type voice, got %s", created.Channel.Type)
	}

	listResp, err := http.Get(ts.URL + "/v1/servers/" + chat.SeedServerIDHarbor + "/channels")
	if err != nil {
		t.Fatalf("list channel groups request failed: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listResp.Body)
		t.Fatalf("unexpected list status: %d body=%s", listResp.StatusCode, string(body))
	}

	var listed struct {
		Groups []struct {
			ID       string `json:"id"`
			Channels []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"channels"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	var found bool
	for _, group := range listed.Groups {
		if group.ID != "grp_voice" {
			continue
		}
		for _, channel := range group.Channels {
			if channel.ID == created.Channel.ID {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected created channel %s in voice group listing", created.Channel.ID)
	}
}

func TestCreateChannelAllowsMixedCategoryAndRejectsNonOwner(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "http://localhost:8080",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	create := func(userUID string, payload map[string]string) *http.Response {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/channels", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("X-OpenChat-User-UID", userUID)
		req.Header.Set("X-OpenChat-Device-ID", "desktop_test")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("send request: %v", err)
		}
		return resp
	}

	ownerResp := create("uid_owner", map[string]string{
		"name":     "owner-text",
		"type":     "text",
		"group_id": "grp_general",
	})
	defer ownerResp.Body.Close()
	if ownerResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(ownerResp.Body)
		t.Fatalf("expected owner create success, got %d body=%s", ownerResp.StatusCode, string(body))
	}

	mixedCategoryResp := create("uid_owner", map[string]string{
		"name":     "voice-in-text",
		"type":     "voice",
		"group_id": "grp_general",
	})
	defer mixedCategoryResp.Body.Close()
	if mixedCategoryResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(mixedCategoryResp.Body)
		t.Fatalf("expected mixed-category create success, got %d body=%s", mixedCategoryResp.StatusCode, string(body))
	}

	forbiddenResp := create("uid_other", map[string]string{
		"name":     "not-allowed",
		"type":     "text",
		"group_id": "grp_general",
	})
	defer forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(forbiddenResp.Body)
		t.Fatalf("expected 403 for non-owner create, got %d body=%s", forbiddenResp.StatusCode, string(body))
	}
	var forbiddenErr struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(forbiddenResp.Body).Decode(&forbiddenErr); err != nil {
		t.Fatalf("decode forbidden response: %v", err)
	}
	if forbiddenErr.Code != "channel_create_forbidden" {
		t.Fatalf("expected channel_create_forbidden code, got %s", forbiddenErr.Code)
	}
}
