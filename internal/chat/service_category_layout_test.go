package chat

import (
	"errors"
	"testing"
)

func TestUpdateCategory_ValidatesAndEnforcesOwner(t *testing.T) {
	svc := NewService("http://localhost:8080")

	if _, err := svc.UpdateCategory(SeedServerIDHarbor, "uid_owner", "grp_general", "General Hub"); err != nil {
		t.Fatalf("expected owner to update category name, got %v", err)
	}

	groups, err := svc.ListChannelGroups(SeedServerIDHarbor)
	if err != nil {
		t.Fatalf("list channel groups failed: %v", err)
	}
	if groups[0].ID != "grp_general" || groups[0].Label != "General Hub" {
		t.Fatalf("expected grp_general to be renamed, got %+v", groups[0])
	}

	if _, err := svc.UpdateCategory(SeedServerIDHarbor, "uid_other", "grp_general", "Forbidden"); !errors.Is(err, ErrCategoryUpdateForbidden) {
		t.Fatalf("expected ErrCategoryUpdateForbidden, got %v", err)
	}
	if _, err := svc.UpdateCategory(SeedServerIDHarbor, "uid_owner", "grp_missing", "Missing"); !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
	if _, err := svc.UpdateCategory(SeedServerIDHarbor, "uid_owner", "grp_general", ""); !errors.Is(err, ErrCategoryNameInvalid) {
		t.Fatalf("expected ErrCategoryNameInvalid, got %v", err)
	}
}

func TestUpdateCategory_RequiresOwnershipClaimWhenPending(t *testing.T) {
	svc := NewService("http://localhost:8080")
	created, err := svc.CreateServer("uid_creator", "Created Server", "", "ocean")
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	groups, err := svc.ListChannelGroups(created.Server.ServerID)
	if err != nil {
		t.Fatalf("list channel groups failed: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("expected seeded category")
	}

	if _, err := svc.UpdateCategory(created.Server.ServerID, "uid_creator", groups[0].ID, "renamed"); !errors.Is(err, ErrOwnershipClaimRequired) {
		t.Fatalf("expected ErrOwnershipClaimRequired before claim, got %v", err)
	}
}

func TestUpdateChannelLayout_ReordersAndMovesChannels(t *testing.T) {
	svc := NewService("http://localhost:8080")

	layout := []ChannelLayoutGroup{
		{ID: "grp_voice", ChannelIDs: []string{"vc_party"}},
		{ID: "grp_general", ChannelIDs: []string{"ch_release", "ch_general", "vc_general", "ch_design"}},
		{ID: "grp_ops", ChannelIDs: []string{"ch_outage"}},
	}

	updated, err := svc.UpdateChannelLayout(SeedServerIDHarbor, "uid_owner", layout)
	if err != nil {
		t.Fatalf("expected layout update success, got %v", err)
	}
	if len(updated.Groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(updated.Groups))
	}
	if updated.Groups[0].ID != "grp_voice" {
		t.Fatalf("expected grp_voice first after reorder, got %s", updated.Groups[0].ID)
	}
	if len(updated.Groups[1].Channels) != 4 || updated.Groups[1].Channels[2].ID != "vc_general" {
		t.Fatalf("expected vc_general moved into grp_general, got %+v", updated.Groups[1].Channels)
	}

	groups, err := svc.ListChannelGroups(SeedServerIDHarbor)
	if err != nil {
		t.Fatalf("list channel groups failed: %v", err)
	}
	if groups[0].ID != "grp_voice" || groups[1].ID != "grp_general" || groups[2].ID != "grp_ops" {
		t.Fatalf("unexpected persisted group order: %+v", groups)
	}
}

func TestUpdateChannelLayout_ValidatesStrictCoverageAndOwner(t *testing.T) {
	svc := NewService("http://localhost:8080")

	if _, err := svc.UpdateChannelLayout(SeedServerIDHarbor, "uid_owner", []ChannelLayoutGroup{
		{ID: "grp_general", ChannelIDs: []string{"ch_general", "ch_design"}},
		{ID: "grp_ops", ChannelIDs: []string{"ch_outage"}},
		{ID: "grp_voice", ChannelIDs: []string{"vc_general", "vc_party"}},
	}); !errors.Is(err, ErrChannelLayoutInvalid) {
		t.Fatalf("expected ErrChannelLayoutInvalid for missing channel coverage, got %v", err)
	}

	if _, err := svc.UpdateChannelLayout(SeedServerIDHarbor, "uid_owner", []ChannelLayoutGroup{
		{ID: "grp_general", ChannelIDs: []string{"ch_general", "ch_design", "ch_release"}},
		{ID: "grp_general", ChannelIDs: []string{"ch_outage"}},
		{ID: "grp_voice", ChannelIDs: []string{"vc_general", "vc_party"}},
	}); !errors.Is(err, ErrChannelLayoutInvalid) {
		t.Fatalf("expected ErrChannelLayoutInvalid for duplicate group id, got %v", err)
	}

	if _, err := svc.UpdateChannelLayout(SeedServerIDHarbor, "uid_other", []ChannelLayoutGroup{
		{ID: "grp_general", ChannelIDs: []string{"ch_general", "ch_design", "ch_release"}},
		{ID: "grp_ops", ChannelIDs: []string{"ch_outage"}},
		{ID: "grp_voice", ChannelIDs: []string{"vc_general", "vc_party"}},
	}); !errors.Is(err, ErrChannelLayoutForbidden) {
		t.Fatalf("expected ErrChannelLayoutForbidden for non-owner, got %v", err)
	}
}

func TestUpdateChannelLayout_RequiresOwnershipClaimWhenPending(t *testing.T) {
	svc := NewService("http://localhost:8080")
	created, err := svc.CreateServer("uid_creator", "Created Server", "", "ocean")
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	groups, err := svc.ListChannelGroups(created.Server.ServerID)
	if err != nil {
		t.Fatalf("list channel groups failed: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("expected seeded category")
	}

	layout := []ChannelLayoutGroup{{
		ID:         groups[0].ID,
		ChannelIDs: []string{groups[0].Channels[0].ID, groups[0].Channels[1].ID},
	}}
	if _, err := svc.UpdateChannelLayout(created.Server.ServerID, "uid_creator", layout); !errors.Is(err, ErrOwnershipClaimRequired) {
		t.Fatalf("expected ErrOwnershipClaimRequired before claim, got %v", err)
	}
}

func TestDeleteCategory_RequiresEmptyCategoryAndOwner(t *testing.T) {
	svc := NewService("http://localhost:8080")

	created, err := svc.CreateCategory(SeedServerIDHarbor, "uid_owner", "Temporary Empty", "text")
	if err != nil {
		t.Fatalf("create empty category failed: %v", err)
	}

	updated, err := svc.DeleteCategory(SeedServerIDHarbor, "uid_owner", created.Group.ID)
	if err != nil {
		t.Fatalf("expected owner to delete empty category, got %v", err)
	}
	for _, group := range updated.Groups {
		if group.ID == created.Group.ID {
			t.Fatalf("expected deleted category to be absent from updated layout")
		}
	}

	if _, err := svc.DeleteCategory(SeedServerIDHarbor, "uid_owner", "grp_general"); !errors.Is(err, ErrCategoryNotEmpty) {
		t.Fatalf("expected ErrCategoryNotEmpty, got %v", err)
	}

	createdBlocked, err := svc.CreateCategory(SeedServerIDHarbor, "uid_owner", "Blocked Delete", "text")
	if err != nil {
		t.Fatalf("create second empty category failed: %v", err)
	}
	if _, err := svc.DeleteCategory(SeedServerIDHarbor, "uid_other", createdBlocked.Group.ID); !errors.Is(err, ErrCategoryDeleteForbidden) {
		t.Fatalf("expected ErrCategoryDeleteForbidden, got %v", err)
	}

	if _, err := svc.DeleteCategory(SeedServerIDHarbor, "uid_owner", "grp_missing"); !errors.Is(err, ErrCategoryNotFound) {
		t.Fatalf("expected ErrCategoryNotFound, got %v", err)
	}
}

func TestDeleteCategory_RequiresOwnershipClaimWhenPending(t *testing.T) {
	svc := NewService("http://localhost:8080")
	created, err := svc.CreateServer("uid_creator", "Created Server", "", "ocean")
	if err != nil {
		t.Fatalf("create server failed: %v", err)
	}

	groups, err := svc.ListChannelGroups(created.Server.ServerID)
	if err != nil {
		t.Fatalf("list channel groups failed: %v", err)
	}
	if len(groups) == 0 {
		t.Fatalf("expected seeded category")
	}

	if _, err := svc.DeleteCategory(created.Server.ServerID, "uid_creator", groups[0].ID); !errors.Is(err, ErrOwnershipClaimRequired) {
		t.Fatalf("expected ErrOwnershipClaimRequired before claim, got %v", err)
	}
}
