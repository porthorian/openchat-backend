package chat

import (
	"errors"
	"testing"
)

func TestCreateCategory_FirstCreatorBecomesOwner(t *testing.T) {
	svc := NewService("http://localhost:8080")

	created, err := svc.CreateCategory(SeedServerIDHarbor, "uid_owner", "Project Space", "text")
	if err != nil {
		t.Fatalf("create category failed: %v", err)
	}
	if created.ServerID != SeedServerIDHarbor {
		t.Fatalf("unexpected server id: %s", created.ServerID)
	}
	if created.Group.ID == "" || created.Group.Label != "Project Space" {
		t.Fatalf("unexpected created group payload: %+v", created.Group)
	}

	if _, err := svc.CreateCategory(SeedServerIDHarbor, "uid_other", "Forbidden", "voice"); !errors.Is(err, ErrCategoryCreateForbidden) {
		t.Fatalf("expected ErrCategoryCreateForbidden, got %v", err)
	}

	if _, err := svc.CreateChannel(SeedServerIDHarbor, "uid_owner", created.Group.ID, "voice-in-new-group", ChannelTypeVoice); err != nil {
		t.Fatalf("owner should create channel in created group: %v", err)
	}
}

func TestUpdateServerSettings_ValidatesInputAndOwnerGate(t *testing.T) {
	svc := NewService("http://localhost:8080")

	updated, err := svc.UpdateServerSettings(
		SeedServerIDHarbor,
		"uid_owner",
		"Harbor Prime",
		"Updated harbor description.",
		"sunset",
	)
	if err != nil {
		t.Fatalf("update server settings failed: %v", err)
	}
	if updated.DisplayName != "Harbor Prime" || updated.Description != "Updated harbor description." || updated.BannerPreset != "sunset" {
		t.Fatalf("unexpected updated payload: %+v", updated)
	}

	settings, err := svc.GetServerSettings(SeedServerIDHarbor)
	if err != nil {
		t.Fatalf("get server settings failed: %v", err)
	}
	if settings.DisplayName != "Harbor Prime" || settings.Description != "Updated harbor description." || settings.BannerPreset != "sunset" {
		t.Fatalf("unexpected persisted settings: %+v", settings)
	}

	if _, err := svc.UpdateServerSettings(SeedServerIDHarbor, "uid_other", "X", "", "ocean"); !errors.Is(err, ErrServerSettingsForbidden) {
		t.Fatalf("expected ErrServerSettingsForbidden, got %v", err)
	}

	if _, err := svc.UpdateServerSettings(SeedServerIDHarbor, "uid_owner", "Harbor Prime", "", "invalid"); !errors.Is(err, ErrServerBannerPresetInvalid) {
		t.Fatalf("expected ErrServerBannerPresetInvalid, got %v", err)
	}
}
