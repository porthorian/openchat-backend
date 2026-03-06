package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openchat/openchat-backend/internal/app"
	"github.com/openchat/openchat-backend/internal/chat"
)

func TestUpdateCategoryAndChannelLayoutEndpoints(t *testing.T) {
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

	renameRaw, err := json.Marshal(map[string]any{"name": "General Hub"})
	if err != nil {
		t.Fatalf("marshal rename payload: %v", err)
	}
	renameReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/grp_general",
		bytes.NewReader(renameRaw),
	)
	if err != nil {
		t.Fatalf("build rename request: %v", err)
	}
	renameReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	renameReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	renameReq.Header.Set("Content-Type", "application/json")
	renameResp, err := http.DefaultClient.Do(renameReq)
	if err != nil {
		t.Fatalf("rename category request failed: %v", err)
	}
	defer renameResp.Body.Close()
	if renameResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(renameResp.Body)
		t.Fatalf("unexpected rename status: %d body=%s", renameResp.StatusCode, string(body))
	}

	var renamePayload struct {
		ServerID string `json:"server_id"`
		Group    struct {
			ID    string `json:"id"`
			Label string `json:"label"`
		} `json:"group"`
	}
	if err := json.NewDecoder(renameResp.Body).Decode(&renamePayload); err != nil {
		t.Fatalf("decode rename payload: %v", err)
	}
	if renamePayload.ServerID != chat.SeedServerIDHarbor {
		t.Fatalf("expected harbor server id, got %s", renamePayload.ServerID)
	}
	if renamePayload.Group.ID != "grp_general" || renamePayload.Group.Label != "General Hub" {
		t.Fatalf("unexpected updated category payload: %+v", renamePayload.Group)
	}

	layoutRaw, err := json.Marshal(map[string]any{
		"groups": []map[string]any{
			{"id": "grp_voice", "channel_ids": []string{"vc_party"}},
			{"id": "grp_general", "channel_ids": []string{"ch_release", "ch_general", "vc_general", "ch_design"}},
			{"id": "grp_ops", "channel_ids": []string{"ch_outage"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal layout payload: %v", err)
	}
	layoutReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/channel-layout",
		bytes.NewReader(layoutRaw),
	)
	if err != nil {
		t.Fatalf("build layout request: %v", err)
	}
	layoutReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	layoutReq.Header.Set("X-OpenChat-Device-ID", "desktop_test")
	layoutReq.Header.Set("Content-Type", "application/json")
	layoutResp, err := http.DefaultClient.Do(layoutReq)
	if err != nil {
		t.Fatalf("update layout request failed: %v", err)
	}
	defer layoutResp.Body.Close()
	if layoutResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(layoutResp.Body)
		t.Fatalf("unexpected layout status: %d body=%s", layoutResp.StatusCode, string(body))
	}

	var layoutPayload struct {
		ServerID string `json:"server_id"`
		Groups   []struct {
			ID       string `json:"id"`
			Channels []struct {
				ID string `json:"id"`
			} `json:"channels"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(layoutResp.Body).Decode(&layoutPayload); err != nil {
		t.Fatalf("decode layout payload: %v", err)
	}
	if len(layoutPayload.Groups) != 3 {
		t.Fatalf("expected 3 groups in layout response, got %d", len(layoutPayload.Groups))
	}
	if layoutPayload.Groups[0].ID != "grp_voice" {
		t.Fatalf("expected grp_voice first, got %s", layoutPayload.Groups[0].ID)
	}
	if len(layoutPayload.Groups[1].Channels) != 4 || layoutPayload.Groups[1].Channels[2].ID != "vc_general" {
		t.Fatalf("expected vc_general moved into grp_general, got %+v", layoutPayload.Groups[1].Channels)
	}

	listedResp, err := http.Get(ts.URL + "/v1/servers/" + chat.SeedServerIDHarbor + "/channels")
	if err != nil {
		t.Fatalf("list channels request failed: %v", err)
	}
	defer listedResp.Body.Close()
	if listedResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(listedResp.Body)
		t.Fatalf("unexpected list status: %d body=%s", listedResp.StatusCode, string(body))
	}
	var listed struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(listedResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list payload: %v", err)
	}
	if len(listed.Groups) != 3 || listed.Groups[0].ID != "grp_voice" {
		t.Fatalf("expected persisted group reorder, got %+v", listed.Groups)
	}
}

func TestUpdateCategoryAndChannelLayoutOwnerGateAndValidation(t *testing.T) {
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

	// Establish owner on seeded server.
	establishReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/grp_general",
		bytes.NewReader([]byte(`{"name":"general"}`)),
	)
	if err != nil {
		t.Fatalf("build establish request: %v", err)
	}
	establishReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	establishReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	establishReq.Header.Set("Content-Type", "application/json")
	establishResp, err := http.DefaultClient.Do(establishReq)
	if err != nil {
		t.Fatalf("establish owner request failed: %v", err)
	}
	establishResp.Body.Close()
	if establishResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(establishResp.Body)
		t.Fatalf("expected establish owner success, got %d body=%s", establishResp.StatusCode, string(body))
	}

	nonOwnerCategoryReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/grp_general",
		bytes.NewReader([]byte(`{"name":"forbidden"}`)),
	)
	if err != nil {
		t.Fatalf("build non-owner category request: %v", err)
	}
	nonOwnerCategoryReq.Header.Set("X-OpenChat-User-UID", "uid_other")
	nonOwnerCategoryReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")
	nonOwnerCategoryReq.Header.Set("Content-Type", "application/json")
	nonOwnerCategoryResp, err := http.DefaultClient.Do(nonOwnerCategoryReq)
	if err != nil {
		t.Fatalf("non-owner category request failed: %v", err)
	}
	defer nonOwnerCategoryResp.Body.Close()
	if nonOwnerCategoryResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(nonOwnerCategoryResp.Body)
		t.Fatalf("expected category update forbidden, got %d body=%s", nonOwnerCategoryResp.StatusCode, string(body))
	}
	var forbiddenCategoryPayload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(nonOwnerCategoryResp.Body).Decode(&forbiddenCategoryPayload); err != nil {
		t.Fatalf("decode category forbidden payload: %v", err)
	}
	if forbiddenCategoryPayload.Code != "category_update_forbidden" {
		t.Fatalf("expected category_update_forbidden code, got %s", forbiddenCategoryPayload.Code)
	}

	nonOwnerLayoutReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/channel-layout",
		bytes.NewReader([]byte(`{"groups":[{"id":"grp_general","channel_ids":["ch_general","ch_design","ch_release"]},{"id":"grp_ops","channel_ids":["ch_outage"]},{"id":"grp_voice","channel_ids":["vc_general","vc_party"]}]}`)),
	)
	if err != nil {
		t.Fatalf("build non-owner layout request: %v", err)
	}
	nonOwnerLayoutReq.Header.Set("X-OpenChat-User-UID", "uid_other")
	nonOwnerLayoutReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")
	nonOwnerLayoutReq.Header.Set("Content-Type", "application/json")
	nonOwnerLayoutResp, err := http.DefaultClient.Do(nonOwnerLayoutReq)
	if err != nil {
		t.Fatalf("non-owner layout request failed: %v", err)
	}
	defer nonOwnerLayoutResp.Body.Close()
	if nonOwnerLayoutResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(nonOwnerLayoutResp.Body)
		t.Fatalf("expected channel layout forbidden, got %d body=%s", nonOwnerLayoutResp.StatusCode, string(body))
	}
	var forbiddenLayoutPayload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(nonOwnerLayoutResp.Body).Decode(&forbiddenLayoutPayload); err != nil {
		t.Fatalf("decode layout forbidden payload: %v", err)
	}
	if forbiddenLayoutPayload.Code != "channel_layout_forbidden" {
		t.Fatalf("expected channel_layout_forbidden code, got %s", forbiddenLayoutPayload.Code)
	}

	invalidLayoutReq, err := http.NewRequest(
		http.MethodPut,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/channel-layout",
		bytes.NewReader([]byte(`{"groups":[{"id":"grp_general","channel_ids":["ch_general","ch_design"]},{"id":"grp_ops","channel_ids":["ch_outage"]},{"id":"grp_voice","channel_ids":["vc_general","vc_party"]}]}`)),
	)
	if err != nil {
		t.Fatalf("build invalid layout request: %v", err)
	}
	invalidLayoutReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	invalidLayoutReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	invalidLayoutReq.Header.Set("Content-Type", "application/json")
	invalidLayoutResp, err := http.DefaultClient.Do(invalidLayoutReq)
	if err != nil {
		t.Fatalf("invalid layout request failed: %v", err)
	}
	defer invalidLayoutResp.Body.Close()
	if invalidLayoutResp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(invalidLayoutResp.Body)
		t.Fatalf("expected invalid channel layout bad request, got %d body=%s", invalidLayoutResp.StatusCode, string(body))
	}
	var invalidLayoutPayload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(invalidLayoutResp.Body).Decode(&invalidLayoutPayload); err != nil {
		t.Fatalf("decode invalid layout payload: %v", err)
	}
	if invalidLayoutPayload.Code != "invalid_channel_layout" {
		t.Fatalf("expected invalid_channel_layout code, got %s", invalidLayoutPayload.Code)
	}
}

func TestDeleteCategoryEndpointValidationAndOwnerGate(t *testing.T) {
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

	createPayloadRaw, err := json.Marshal(map[string]any{
		"name": "Delete Me",
		"kind": "text",
	})
	if err != nil {
		t.Fatalf("marshal create payload: %v", err)
	}
	createReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories",
		bytes.NewReader(createPayloadRaw),
	)
	if err != nil {
		t.Fatalf("build create request: %v", err)
	}
	createReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	createReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatalf("create category request failed: %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("expected create category success, got %d body=%s", createResp.StatusCode, string(body))
	}
	var created struct {
		Group struct {
			ID string `json:"id"`
		} `json:"group"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode created payload: %v", err)
	}
	if created.Group.ID == "" {
		t.Fatalf("expected created group id")
	}

	deleteReq, err := http.NewRequest(
		http.MethodDelete,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/"+created.Group.ID,
		nil,
	)
	if err != nil {
		t.Fatalf("build delete request: %v", err)
	}
	deleteReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	deleteReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	deleteResp, err := http.DefaultClient.Do(deleteReq)
	if err != nil {
		t.Fatalf("delete empty category request failed: %v", err)
	}
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(deleteResp.Body)
		t.Fatalf("expected empty delete success, got %d body=%s", deleteResp.StatusCode, string(body))
	}
	var deletedPayload struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(deleteResp.Body).Decode(&deletedPayload); err != nil {
		t.Fatalf("decode delete payload: %v", err)
	}
	for _, group := range deletedPayload.Groups {
		if group.ID == created.Group.ID {
			t.Fatalf("expected deleted group to be absent from response groups")
		}
	}

	nonEmptyReq, err := http.NewRequest(
		http.MethodDelete,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/grp_general",
		nil,
	)
	if err != nil {
		t.Fatalf("build non-empty delete request: %v", err)
	}
	nonEmptyReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	nonEmptyReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	nonEmptyResp, err := http.DefaultClient.Do(nonEmptyReq)
	if err != nil {
		t.Fatalf("non-empty delete request failed: %v", err)
	}
	defer nonEmptyResp.Body.Close()
	if nonEmptyResp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(nonEmptyResp.Body)
		t.Fatalf("expected category_not_empty bad request, got %d body=%s", nonEmptyResp.StatusCode, string(body))
	}
	var nonEmptyPayload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(nonEmptyResp.Body).Decode(&nonEmptyPayload); err != nil {
		t.Fatalf("decode non-empty delete payload: %v", err)
	}
	if nonEmptyPayload.Code != "category_not_empty" {
		t.Fatalf("expected category_not_empty code, got %s", nonEmptyPayload.Code)
	}

	blockedCreateReq, err := http.NewRequest(
		http.MethodPost,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories",
		bytes.NewReader(createPayloadRaw),
	)
	if err != nil {
		t.Fatalf("build blocked create request: %v", err)
	}
	blockedCreateReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	blockedCreateReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	blockedCreateReq.Header.Set("Content-Type", "application/json")
	blockedCreateResp, err := http.DefaultClient.Do(blockedCreateReq)
	if err != nil {
		t.Fatalf("blocked create request failed: %v", err)
	}
	defer blockedCreateResp.Body.Close()
	if blockedCreateResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(blockedCreateResp.Body)
		t.Fatalf("expected second create category success, got %d body=%s", blockedCreateResp.StatusCode, string(body))
	}
	var blockedCreated struct {
		Group struct {
			ID string `json:"id"`
		} `json:"group"`
	}
	if err := json.NewDecoder(blockedCreateResp.Body).Decode(&blockedCreated); err != nil {
		t.Fatalf("decode second created payload: %v", err)
	}
	if blockedCreated.Group.ID == "" {
		t.Fatalf("expected second created group id")
	}

	forbiddenReq, err := http.NewRequest(
		http.MethodDelete,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/"+blockedCreated.Group.ID,
		nil,
	)
	if err != nil {
		t.Fatalf("build forbidden delete request: %v", err)
	}
	forbiddenReq.Header.Set("X-OpenChat-User-UID", "uid_other")
	forbiddenReq.Header.Set("X-OpenChat-Device-ID", "desktop_other")
	forbiddenResp, err := http.DefaultClient.Do(forbiddenReq)
	if err != nil {
		t.Fatalf("forbidden delete request failed: %v", err)
	}
	defer forbiddenResp.Body.Close()
	if forbiddenResp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(forbiddenResp.Body)
		t.Fatalf("expected category_delete_forbidden, got %d body=%s", forbiddenResp.StatusCode, string(body))
	}
	var forbiddenPayload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(forbiddenResp.Body).Decode(&forbiddenPayload); err != nil {
		t.Fatalf("decode forbidden payload: %v", err)
	}
	if forbiddenPayload.Code != "category_delete_forbidden" {
		t.Fatalf("expected category_delete_forbidden code, got %s", forbiddenPayload.Code)
	}

	missingReq, err := http.NewRequest(
		http.MethodDelete,
		ts.URL+"/v1/servers/"+chat.SeedServerIDHarbor+"/categories/grp_missing",
		nil,
	)
	if err != nil {
		t.Fatalf("build missing delete request: %v", err)
	}
	missingReq.Header.Set("X-OpenChat-User-UID", "uid_owner")
	missingReq.Header.Set("X-OpenChat-Device-ID", "desktop_owner")
	missingResp, err := http.DefaultClient.Do(missingReq)
	if err != nil {
		t.Fatalf("missing delete request failed: %v", err)
	}
	defer missingResp.Body.Close()
	if missingResp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(missingResp.Body)
		t.Fatalf("expected category_not_found, got %d body=%s", missingResp.StatusCode, string(body))
	}
	var missingPayload struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(missingResp.Body).Decode(&missingPayload); err != nil {
		t.Fatalf("decode missing payload: %v", err)
	}
	if missingPayload.Code != "category_not_found" {
		t.Fatalf("expected category_not_found code, got %s", missingPayload.Code)
	}
}
