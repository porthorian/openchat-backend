package serveractions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrForbidden = errors.New("action forbidden")
var ErrConflict = errors.New("action conflicts with current state")
var ErrExpired = errors.New("invite expired, revoked, or exhausted")
var ErrNotFound = errors.New("resource not found")

type Service struct{ DB *pgxpool.Pool }

func New(db *pgxpool.Pool) *Service { return &Service{DB: db} }

type Invite struct {
	ID           string     `json:"invite_id"`
	ServerID     string     `json:"server_id"`
	Code         string     `json:"code,omitempty"`
	CreatedByUID string     `json:"created_by_uid"`
	ExpiresAt    time.Time  `json:"expires_at"`
	MaxUses      int        `json:"max_uses"`
	UseCount     int        `json:"use_count"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *Service) CreateInvite(ctx context.Context, serverID, userUID string, expiresAt time.Time, maxUses int) (Invite, error) {
	if s == nil || s.DB == nil {
		return Invite{}, errors.New("invite storage unavailable")
	}
	if maxUses < 1 || maxUses > 1000 || !expiresAt.After(time.Now().Add(time.Minute)) || expiresAt.After(time.Now().Add(30*24*time.Hour)) {
		return Invite{}, ErrConflict
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Invite{}, err
	}
	defer tx.Rollback(ctx)
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM server_memberships WHERE server_id=$1 AND user_uid=$2 AND membership_state='active'`, serverID, userUID).Scan(&role)
	if err != nil || (role != "owner" && role != "admin") {
		return Invite{}, ErrForbidden
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return Invite{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(code))
	result := Invite{ServerID: serverID, Code: code, CreatedByUID: userUID, ExpiresAt: expiresAt, MaxUses: maxUses}
	err = tx.QueryRow(ctx, `INSERT INTO server_invites(server_id,code_hash,created_by_uid,expires_at,max_uses) VALUES($1,$2,$3,$4,$5) RETURNING invite_id,use_count,created_at`, serverID, digest[:], userUID, expiresAt, maxUses).Scan(&result.ID, &result.UseCount, &result.CreatedAt)
	if err != nil {
		return Invite{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invite{}, err
	}
	return result, nil
}

func (s *Service) ListInvites(ctx context.Context, serverID, userUID string, limit int, before *time.Time) ([]Invite, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("invite storage unavailable")
	}
	var role string
	err := s.DB.QueryRow(ctx, `SELECT role FROM server_memberships WHERE server_id=$1 AND user_uid=$2 AND membership_state='active'`, serverID, userUID).Scan(&role)
	if err != nil || (role != "owner" && role != "admin") {
		return nil, ErrForbidden
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if before == nil {
		now := time.Now().Add(time.Hour)
		before = &now
	}
	rows, err := s.DB.Query(ctx, `SELECT invite_id,created_by_uid,expires_at,max_uses,use_count,revoked_at,created_at FROM server_invites WHERE server_id=$1 AND created_at<$2 ORDER BY created_at DESC,invite_id DESC LIMIT $3`, serverID, *before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Invite, 0, limit)
	for rows.Next() {
		item := Invite{ServerID: serverID}
		if err := rows.Scan(&item.ID, &item.CreatedByUID, &item.ExpiresAt, &item.MaxUses, &item.UseCount, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) RevokeInvite(ctx context.Context, serverID, userUID, inviteID string) error {
	if s == nil || s.DB == nil {
		return errors.New("invite storage unavailable")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var role string
	err = tx.QueryRow(ctx, `SELECT role FROM server_memberships WHERE server_id=$1 AND user_uid=$2 AND membership_state='active'`, serverID, userUID).Scan(&role)
	if err != nil || (role != "owner" && role != "admin") {
		return ErrForbidden
	}
	tag, err := tx.Exec(ctx, `UPDATE server_invites SET revoked_at=COALESCE(revoked_at,now()) WHERE server_id=$1 AND invite_id=$2`, serverID, inviteID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// Redeem locks the invite and membership in one transaction. A repeated
// redemption by an already-active member does not consume another use.
func (s *Service) Redeem(ctx context.Context, code, userUID, sessionServerID string) (string, error) {
	if s == nil || s.DB == nil {
		return "", errors.New("invite storage unavailable")
	}
	digest := sha256.Sum256([]byte(code))
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var inviteID, serverID string
	var expires time.Time
	var revoked *time.Time
	var maxUses, useCount int
	err = tx.QueryRow(ctx, `SELECT invite_id,server_id,expires_at,revoked_at,max_uses,use_count FROM server_invites WHERE code_hash=$1 FOR UPDATE`, digest[:]).Scan(&inviteID, &serverID, &expires, &revoked, &maxUses, &useCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if serverID != sessionServerID {
		return "", ErrForbidden
	}
	if revoked != nil || !expires.After(time.Now()) || useCount >= maxUses {
		return "", ErrExpired
	}
	var state string
	err = tx.QueryRow(ctx, `SELECT membership_state FROM server_memberships WHERE server_id=$1 AND user_uid=$2 FOR UPDATE`, serverID, userUID).Scan(&state)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if state == "banned" {
		return "", ErrForbidden
	}
	if state == "active" {
		return serverID, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO server_memberships(server_id,user_uid,role,membership_state) VALUES($1,$2,'member','active') ON CONFLICT(server_id,user_uid) DO UPDATE SET membership_state='active',role='member',joined_at=now(),left_at=NULL,removed_at=NULL`, serverID, userUID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE server_invites SET use_count=use_count+1 WHERE invite_id=$1`, inviteID); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return serverID, nil
}
