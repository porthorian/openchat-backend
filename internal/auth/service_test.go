package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/openchat/openchat-backend/internal/store/postgres"
)

func TestUIDForKeyIsServerScoped(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	first, err := UIDForKey("server-a", pub)
	if err != nil {
		t.Fatal(err)
	}
	second, err := UIDForKey("server-b", pub)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("same key correlated across servers")
	}
	if _, err := UIDForKey("server-a", pub[:4]); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("short key accepted: %v", err)
	}
}

func TestChallengeRejectsImpersonationAndReplay(t *testing.T) {
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
	serverID := "srv_auth_integration"
	if _, err := pool.Exec(ctx, `INSERT INTO servers(server_id,display_name) VALUES($1,'Auth integration') ON CONFLICT DO NOTHING`, serverID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM servers WHERE server_id=$1`, serverID)
	service := New(pool)
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BeginChallenge(ctx, serverID, "uid_someone_else", "device-1", pub); !errors.Is(err, ErrBindingRequired) {
		t.Fatalf("accepted unverified UID: %v", err)
	}
	challenge, err := service.BeginChallenge(ctx, serverID, "", "device-1", pub)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteChallenge(ctx, serverID, challenge.ID, make([]byte, 64)); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("accepted invalid signature: %v", err)
	}
	session, err := service.CompleteChallenge(ctx, serverID, challenge.ID, ed25519.Sign(private, []byte(challenge.Payload)))
	if err != nil {
		t.Fatal(err)
	}
	if session.UserUID != challenge.UserUID || session.Token == "" {
		t.Fatalf("invalid session: %+v", session)
	}
	if _, err := service.VerifyToken(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteChallenge(ctx, serverID, challenge.ID, ed25519.Sign(private, []byte(challenge.Payload))); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("replayed challenge: %v", err)
	}
	if err := service.RevokeUserSessions(ctx, serverID, session.UserUID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyToken(ctx, session.Token); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked session accepted: %v", err)
	}
}

func TestLegacyBindingApprovalRespectsRoleRank(t *testing.T) {
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
	serverID := fmt.Sprintf("srv_binding_integration_%d", time.Now().UnixNano())
	owner, mod, admin, member := "uid_binding_owner", "uid_binding_mod", "uid_binding_admin", "uid_binding_member"
	for _, uid := range []string{owner, mod, admin, member} {
		if _, err := pool.Exec(ctx, `INSERT INTO users(user_uid) VALUES($1) ON CONFLICT DO NOTHING`, uid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO servers(server_id,display_name) VALUES($1,'Binding integration') ON CONFLICT DO NOTHING`, serverID); err != nil {
		t.Fatal(err)
	}
	// The binding audit is immutable, so this disposable test database retains the fixture.
	for _, item := range []struct{ uid, role string }{{owner, "owner"}, {mod, "moderator"}, {admin, "admin"}, {member, "member"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO server_memberships(server_id,user_uid,role) VALUES($1,$2,$3) ON CONFLICT(server_id,user_uid) DO UPDATE SET role=$3,membership_state='active'`, serverID, item.uid, item.role); err != nil {
			t.Fatal(err)
		}
	}
	svc := New(pool)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ApproveLegacyBinding(ctx, serverID, mod, admin, pub, "out-of-band-ticket-1"); !errors.Is(err, ErrBindingForbidden) {
		t.Fatalf("moderator bound admin: %v", err)
	}
	if err := svc.ApproveLegacyBinding(ctx, serverID, mod, owner, pub, "out-of-band-ticket-1"); !errors.Is(err, ErrBindingForbidden) {
		t.Fatalf("moderator bound owner: %v", err)
	}
	if err := svc.ApproveLegacyBinding(ctx, serverID, mod, member, pub, "out-of-band-ticket-1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ApproveLegacyBinding(ctx, serverID, owner, member, pub, "out-of-band-ticket-1"); !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("verified binding changed: %v", err)
	}
	challenge, err := svc.BeginChallenge(ctx, serverID, member, "device-1", pub)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CompleteChallenge(ctx, serverID, challenge.ID, ed25519.Sign(priv, []byte(challenge.Payload)))
	if err != nil || session.UserUID != member {
		t.Fatalf("legacy UID lost: %v %+v", err, session)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM moderation_audit WHERE server_id=$1 AND event_type='identity.binding.approved'`, serverID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 {
		t.Fatalf("expected one audit record, got %d", auditCount)
	}
}
