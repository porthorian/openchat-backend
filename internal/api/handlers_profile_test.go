package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/openchat/openchat-backend/internal/app"
)

func TestProfileLifecycleEndpoints(t *testing.T) {
	cfg := app.Config{
		HTTPAddr:      ":0",
		PublicBaseURL: "",
		SignalingPath: "/v1/rtc/signaling",
		TicketTTL:     60 * time.Second,
		TicketSecret:  "test-secret",
		Environment:   "test",
	}
	server := NewServer(cfg, slog.Default())
	ts := httptest.NewServer(server.Router())
	defer ts.Close()

	httpClient := &http.Client{}
	userUID := "uid_profile_test"

	getReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/profile/me", nil)
	if err != nil {
		t.Fatalf("build get profile request: %v", err)
	}
	getReq.Header.Set("X-OpenChat-User-UID", userUID)

	getResp, err := httpClient.Do(getReq)
	if err != nil {
		t.Fatalf("get profile failed: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getResp.Body)
		t.Fatalf("unexpected profile status: %d body=%s", getResp.StatusCode, string(body))
	}

	var currentProfile struct {
		UserUID        string `json:"user_uid"`
		ProfileVersion int    `json:"profile_version"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&currentProfile); err != nil {
		t.Fatalf("decode profile response: %v", err)
	}
	if currentProfile.UserUID != userUID {
		t.Fatalf("expected profile uid %s, got %s", userUID, currentProfile.UserUID)
	}
	if currentProfile.ProfileVersion <= 0 {
		t.Fatalf("expected positive profile version, got %d", currentProfile.ProfileVersion)
	}

	avatarPNG := testPNGBytes(t)
	var avatarBody bytes.Buffer
	avatarWriter := multipart.NewWriter(&avatarBody)
	avatarPart, err := avatarWriter.CreateFormFile("file", "avatar.png")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := avatarPart.Write(avatarPNG); err != nil {
		t.Fatalf("write avatar payload: %v", err)
	}
	if err := avatarWriter.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	uploadReq, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/profile/avatar", &avatarBody)
	if err != nil {
		t.Fatalf("build upload request: %v", err)
	}
	uploadReq.Header.Set("X-OpenChat-User-UID", userUID)
	uploadReq.Header.Set("Content-Type", avatarWriter.FormDataContentType())

	uploadResp, err := httpClient.Do(uploadReq)
	if err != nil {
		t.Fatalf("upload avatar failed: %v", err)
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(uploadResp.Body)
		t.Fatalf("unexpected upload status: %d body=%s", uploadResp.StatusCode, string(body))
	}

	var uploaded struct {
		AvatarAssetID string `json:"avatar_asset_id"`
		AvatarURL     string `json:"avatar_url"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.AvatarAssetID == "" {
		t.Fatalf("expected avatar_asset_id")
	}
	if uploaded.AvatarURL == "" {
		t.Fatalf("expected avatar_url")
	}

	updatePayload := map[string]any{
		"display_name":    "Vinnie",
		"avatar_mode":     "uploaded",
		"avatar_asset_id": uploaded.AvatarAssetID,
	}
	updateBytes, _ := json.Marshal(updatePayload)
	updateReq, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/profile/me", bytes.NewReader(updateBytes))
	if err != nil {
		t.Fatalf("build update profile request: %v", err)
	}
	updateReq.Header.Set("X-OpenChat-User-UID", userUID)
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("If-Match", strconv.Itoa(currentProfile.ProfileVersion))

	updateResp, err := httpClient.Do(updateReq)
	if err != nil {
		t.Fatalf("update profile failed: %v", err)
	}
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(updateResp.Body)
		t.Fatalf("unexpected update status: %d body=%s", updateResp.StatusCode, string(body))
	}

	var updated struct {
		DisplayName    string  `json:"display_name"`
		AvatarMode     string  `json:"avatar_mode"`
		AvatarAssetID  *string `json:"avatar_asset_id"`
		ProfileVersion int     `json:"profile_version"`
	}
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.DisplayName != "Vinnie" {
		t.Fatalf("expected display name Vinnie, got %s", updated.DisplayName)
	}
	if updated.AvatarMode != "uploaded" {
		t.Fatalf("expected avatar_mode uploaded, got %s", updated.AvatarMode)
	}
	if updated.AvatarAssetID == nil || *updated.AvatarAssetID != uploaded.AvatarAssetID {
		t.Fatalf("expected avatar asset id %s", uploaded.AvatarAssetID)
	}
	if updated.ProfileVersion <= currentProfile.ProfileVersion {
		t.Fatalf("expected profile version to increment")
	}

	avatarReq, err := http.NewRequest(http.MethodGet, ts.URL+uploaded.AvatarURL, nil)
	if err != nil {
		t.Fatalf("build avatar get request: %v", err)
	}
	avatarResp, err := httpClient.Do(avatarReq)
	if err != nil {
		t.Fatalf("get avatar failed: %v", err)
	}
	defer avatarResp.Body.Close()
	if avatarResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(avatarResp.Body)
		t.Fatalf("unexpected avatar get status: %d body=%s", avatarResp.StatusCode, string(body))
	}
	if contentType := avatarResp.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("expected image/png avatar content type, got %s", contentType)
	}

	batchReq, err := http.NewRequest(http.MethodGet, ts.URL+"/v1/profiles:batch?user_uid="+userUID+"&user_uid=uid_other", nil)
	if err != nil {
		t.Fatalf("build batch request: %v", err)
	}
	batchReq.Header.Set("X-OpenChat-User-UID", userUID)

	batchResp, err := httpClient.Do(batchReq)
	if err != nil {
		t.Fatalf("batch profiles failed: %v", err)
	}
	defer batchResp.Body.Close()
	if batchResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(batchResp.Body)
		t.Fatalf("unexpected batch status: %d body=%s", batchResp.StatusCode, string(body))
	}
	var batchPayload struct {
		Profiles []struct {
			UserUID string `json:"user_uid"`
		} `json:"profiles"`
	}
	if err := json.NewDecoder(batchResp.Body).Decode(&batchPayload); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	if len(batchPayload.Profiles) != 2 {
		t.Fatalf("expected 2 profiles from batch response, got %d", len(batchPayload.Profiles))
	}

	conflictReq, err := http.NewRequest(http.MethodPut, ts.URL+"/v1/profile/me", bytes.NewReader(updateBytes))
	if err != nil {
		t.Fatalf("build conflict request: %v", err)
	}
	conflictReq.Header.Set("X-OpenChat-User-UID", userUID)
	conflictReq.Header.Set("Content-Type", "application/json")
	conflictReq.Header.Set("If-Match", strconv.Itoa(currentProfile.ProfileVersion))

	conflictResp, err := httpClient.Do(conflictReq)
	if err != nil {
		t.Fatalf("conflict update failed: %v", err)
	}
	defer conflictResp.Body.Close()
	if conflictResp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(conflictResp.Body)
		t.Fatalf("expected conflict status, got %d body=%s", conflictResp.StatusCode, string(body))
	}
}

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 120, B: 200, A: 255})
		}
	}

	buf := bytes.NewBuffer(nil)
	if err := png.Encode(buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
