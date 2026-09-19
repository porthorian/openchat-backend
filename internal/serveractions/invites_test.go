package serveractions

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/openchat/openchat-backend/internal/store/postgres"
)

func TestInviteLimitAndRoleGate(t *testing.T) {
	dsn := os.Getenv("OPENCHAT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("OPENCHAT_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := postgres.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	serverID := "srv_invite_integration"
	owner := "uid_invite_owner"
	member := "uid_invite_member"
	first := "uid_invite_first"
	second := "uid_invite_second"
	for _, uid := range []string{owner, member, first, second} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(user_uid) VALUES($1) ON CONFLICT DO NOTHING`, uid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO servers(server_id,display_name) VALUES($1,'Invite integration') ON CONFLICT DO NOTHING`, serverID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM servers WHERE server_id=$1`, serverID)
	for _, item := range []struct{ uid, role string }{{owner, "owner"}, {member, "member"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO server_memberships(server_id,user_uid,role) VALUES($1,$2,$3) ON CONFLICT(server_id,user_uid) DO UPDATE SET role=$3,membership_state='active'`, serverID, item.uid, item.role); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(pool)
	expiry := time.Now().Add(time.Hour)
	if _, err := svc.CreateInvite(ctx, serverID, member, expiry, 1); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member created invite: %v", err)
	}
	invite, err := svc.CreateInvite(ctx, serverID, owner, expiry, 1)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, uid := range []string{first, second} {
		wg.Add(1)
		go func(uid string) {
			defer wg.Done()
			_, err := svc.Redeem(ctx, invite.Code, uid, serverID)
			results <- err
		}(uid)
	}
	wg.Wait()
	close(results)
	successes, expired := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrExpired) {
			expired++
		} else {
			t.Fatalf("unexpected redeem result: %v", err)
		}
	}
	if successes != 1 || expired != 1 {
		t.Fatalf("expected one success and one exhausted invite; got %d and %d", successes, expired)
	}
	var useCount int
	if err := pool.QueryRow(ctx, `SELECT use_count FROM server_invites WHERE invite_id=$1`, invite.ID).Scan(&useCount); err != nil {
		t.Fatal(err)
	}
	if useCount != 1 {
		t.Fatalf("use_count=%d", useCount)
	}
}
