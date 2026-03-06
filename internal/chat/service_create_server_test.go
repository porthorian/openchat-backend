package chat

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreateServerAndOwnershipClaimLifecycle(t *testing.T) {
	svc := NewService("http://localhost:8080")

	created, err := svc.CreateServer("uid_creator_backend", "Penny Lab", "Test server", "ocean")
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}
	if _, parseErr := uuid.Parse(created.Server.ServerID); parseErr != nil {
		t.Fatalf("expected uuid server id, got %s", created.Server.ServerID)
	}
	if created.Server.DisplayName != "Penny Lab" {
		t.Fatalf("unexpected display name: %s", created.Server.DisplayName)
	}
	if created.OwnershipClaim.Token == "" || created.OwnershipClaim.ExpiresAt == "" {
		t.Fatalf("expected ownership claim token and expiry")
	}

	groups, err := svc.ListChannelGroups(created.Server.ServerID)
	if err != nil {
		t.Fatalf("list channel groups failed: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected one default category, got %d", len(groups))
	}
	if len(groups[0].Channels) != 2 {
		t.Fatalf("expected two starter channels, got %d", len(groups[0].Channels))
	}
	if groups[0].Channels[0].Type != ChannelTypeText || groups[0].Channels[1].Type != ChannelTypeVoice {
		t.Fatalf("expected starter text+voice channels, got %+v", groups[0].Channels)
	}

	_, err = svc.CreateCategory(created.Server.ServerID, "uid_creator_backend", "ops", "text")
	if !errors.Is(err, ErrOwnershipClaimRequired) {
		t.Fatalf("expected ownership claim required before mutation, got %v", err)
	}

	_, err = svc.ClaimServerOwnership(created.Server.ServerID, "uid_creator_server", "claim_invalid")
	if !errors.Is(err, ErrOwnershipClaimInvalid) {
		t.Fatalf("expected invalid claim token error, got %v", err)
	}

	claimed, err := svc.ClaimServerOwnership(created.Server.ServerID, "uid_creator_server", created.OwnershipClaim.Token)
	if err != nil {
		t.Fatalf("claim ownership failed: %v", err)
	}
	if claimed.OwnerUserUID != "uid_creator_server" {
		t.Fatalf("unexpected owner uid: %s", claimed.OwnerUserUID)
	}

	_, err = svc.ClaimServerOwnership(created.Server.ServerID, "uid_other", created.OwnershipClaim.Token)
	if !errors.Is(err, ErrOwnershipClaimForbidden) {
		t.Fatalf("expected claim forbidden after owner set, got %v", err)
	}

	_, err = svc.CreateChannel(created.Server.ServerID, "uid_creator_server", groups[0].ID, "ops-text", ChannelTypeText)
	if err != nil {
		t.Fatalf("owner should create channel after claim: %v", err)
	}

	_, err = svc.UpdateServerSettings(created.Server.ServerID, "uid_other", "Blocked", "", "ocean")
	if !errors.Is(err, ErrServerSettingsForbidden) {
		t.Fatalf("expected owner-only settings update after claim, got %v", err)
	}
}

func TestClaimServerOwnershipExpiredToken(t *testing.T) {
	svc := NewService("http://localhost:8080")
	created, err := svc.CreateServer("uid_creator_backend", "Expired Token Server", "", "ocean")
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	svc.mu.Lock()
	state := svc.ownershipClaimsByToken[created.OwnershipClaim.Token]
	state.ExpiresAt = time.Now().UTC().Add(-1 * time.Second)
	svc.ownershipClaimsByToken[created.OwnershipClaim.Token] = state
	svc.mu.Unlock()

	_, err = svc.ClaimServerOwnership(created.Server.ServerID, "uid_creator_server", created.OwnershipClaim.Token)
	if !errors.Is(err, ErrOwnershipClaimExpired) {
		t.Fatalf("expected expired claim token error, got %v", err)
	}
}

func TestCreateServerValidation(t *testing.T) {
	svc := NewService("http://localhost:8080")

	_, err := svc.CreateServer("uid_creator_backend", "", "", "ocean")
	if !errors.Is(err, ErrServerDisplayNameInvalid) {
		t.Fatalf("expected display name validation error, got %v", err)
	}

	_, err = svc.CreateServer("uid_creator_backend", "Valid", strings.Repeat("x", 281), "ocean")
	if !errors.Is(err, ErrServerDescriptionInvalid) {
		t.Fatalf("expected description validation error, got %v", err)
	}

	_, err = svc.CreateServer("uid_creator_backend", "Valid", "", "invalid")
	if !errors.Is(err, ErrServerBannerPresetInvalid) {
		t.Fatalf("expected banner preset validation error, got %v", err)
	}
}
