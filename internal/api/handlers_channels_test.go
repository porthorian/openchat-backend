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
	if err := writer.WriteField("body", "pasted image"); err != nil {
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
	if created.Message.Body != "pasted image" {
		t.Fatalf("expected body to round-trip, got %q", created.Message.Body)
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
