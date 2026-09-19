package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidKey       = errors.New("invalid signing key")
	ErrBindingRequired  = errors.New("legacy UID requires a verified key binding")
	ErrChallengeExpired = errors.New("challenge expired or already used")
	ErrInvalidSignature = errors.New("invalid challenge signature")
	ErrSessionInvalid   = errors.New("session expired or revoked")
)

const challengeTTL = 5 * time.Minute
const sessionTTL = 24 * time.Hour

type Service struct{ DB *pgxpool.Pool }

type Challenge struct {
	ID        string    `json:"challenge_id"`
	ServerID  string    `json:"server_id"`
	UserUID   string    `json:"user_uid"`
	Payload   string    `json:"payload"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Session struct {
	ID        string    `json:"session_id"`
	ServerID  string    `json:"server_id"`
	UserUID   string    `json:"user_uid"`
	DeviceID  string    `json:"device_id"`
	Token     string    `json:"token,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// UIDForKey derives a server-scoped UID. The server ID is included to avoid
// correlating one key across independent OpenChat backends.
func UIDForKey(serverID string, publicKey ed25519.PublicKey) (string, error) {
	if strings.TrimSpace(serverID) == "" || len(publicKey) != ed25519.PublicKeySize {
		return "", ErrInvalidKey
	}
	digest := sha256.Sum256(append(append([]byte("openchat-uid-v1\x00"), []byte(serverID)...), append([]byte{0}, publicKey...)...))
	return "uid_" + hex.EncodeToString(digest[:16]), nil
}

func New(db *pgxpool.Pool) *Service { return &Service{DB: db} }

func (s *Service) BeginChallenge(ctx context.Context, serverID, requestedUID, deviceID string, publicKey ed25519.PublicKey) (Challenge, error) {
	if s == nil || s.DB == nil {
		return Challenge{}, errors.New("verified sessions unavailable")
	}
	if len(publicKey) != ed25519.PublicKeySize || len(deviceID) == 0 || len(deviceID) > 128 {
		return Challenge{}, ErrInvalidKey
	}
	derivedUID, err := UIDForKey(serverID, publicKey)
	if err != nil {
		return Challenge{}, err
	}
	uid := strings.TrimSpace(requestedUID)
	if uid == "" {
		uid = derivedUID
	}
	if len(uid) > 128 {
		return Challenge{}, ErrBindingRequired
	}
	if uid != derivedUID {
		var approved bool
		err := s.DB.QueryRow(ctx, `SELECT verified_at IS NOT NULL FROM legacy_identity_bindings WHERE server_id=$1 AND user_uid=$2 AND public_key=$3`, serverID, uid, []byte(publicKey)).Scan(&approved)
		if err != nil || !approved {
			return Challenge{}, ErrBindingRequired
		}
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return Challenge{}, err
	}
	expires := time.Now().UTC().Add(challengeTTL)
	payload := fmt.Sprintf("openchat-session-v1\n%s\n%s\n%s\n%s\n%s", serverID, uid, deviceID, base64.RawURLEncoding.EncodeToString(nonce), expires.Format(time.RFC3339Nano))
	var result Challenge
	result.ServerID, result.UserUID, result.Payload, result.ExpiresAt = serverID, uid, payload, expires
	err = s.DB.QueryRow(ctx, `INSERT INTO identity_challenges (server_id,user_uid,device_id,nonce,public_key,signed_payload,expires_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING challenge_id`, serverID, uid, deviceID, nonce, []byte(publicKey), payload, expires).Scan(&result.ID)
	return result, err
}

func (s *Service) CompleteChallenge(ctx context.Context, expectedServerID, challengeID string, signature []byte) (Session, error) {
	if s == nil || s.DB == nil {
		return Session{}, errors.New("verified sessions unavailable")
	}
	if len(signature) != ed25519.SignatureSize {
		return Session{}, ErrInvalidSignature
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback(ctx)
	var serverID, uid, deviceID, payload string
	var pub []byte
	var expires time.Time
	var consumedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT server_id,user_uid,device_id,public_key,signed_payload,expires_at,consumed_at FROM identity_challenges WHERE challenge_id=$1 FOR UPDATE`, challengeID).Scan(&serverID, &uid, &deviceID, &pub, &payload, &expires, &consumedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrChallengeExpired
	}
	if err != nil {
		return Session{}, err
	}
	if serverID != expectedServerID {
		return Session{}, ErrChallengeExpired
	}
	if consumedAt != nil || !expires.After(time.Now()) {
		return Session{}, ErrChallengeExpired
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(payload), signature) {
		return Session{}, ErrInvalidSignature
	}
	derivedUID, err := UIDForKey(serverID, ed25519.PublicKey(pub))
	if err != nil {
		return Session{}, err
	}
	if uid != derivedUID {
		var approved bool
		err := tx.QueryRow(ctx, `SELECT verified_at IS NOT NULL FROM legacy_identity_bindings WHERE server_id=$1 AND user_uid=$2 AND public_key=$3 FOR UPDATE`, serverID, uid, pub).Scan(&approved)
		if err != nil || !approved {
			return Session{}, ErrBindingRequired
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE identity_challenges SET consumed_at=now() WHERE challenge_id=$1`, challengeID); err != nil {
		return Session{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO users(user_uid) VALUES($1) ON CONFLICT DO NOTHING`, uid); err != nil {
		return Session{}, err
	}
	subject := serverID + ":" + base64.RawURLEncoding.EncodeToString(pub)
	bindingTag, err := tx.Exec(ctx, `INSERT INTO auth_identity_bindings(user_uid,provider,provider_subject,is_primary,verified_at) VALUES($1,'ed25519',$2,false,now()) ON CONFLICT(provider,provider_subject) DO UPDATE SET verified_at=COALESCE(auth_identity_bindings.verified_at,now()) WHERE auth_identity_bindings.user_uid=EXCLUDED.user_uid`, uid, subject)
	if err != nil {
		return Session{}, err
	}
	if bindingTag.RowsAffected() != 1 {
		return Session{}, ErrBindingRequired
	}
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return Session{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	tokenHash := sha256.Sum256([]byte(token))
	result := Session{ServerID: serverID, UserUID: uid, DeviceID: deviceID, Token: token, ExpiresAt: time.Now().UTC().Add(sessionTTL)}
	err = tx.QueryRow(ctx, `INSERT INTO auth_sessions(server_id,user_uid,provider,token_hash,device_id,expires_at) VALUES($1,$2,'ed25519',$3,$4,$5) RETURNING session_id`, serverID, uid, tokenHash[:], deviceID, result.ExpiresAt).Scan(&result.ID)
	if err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, err
	}
	return result, nil
}

func (s *Service) VerifyToken(ctx context.Context, token string) (Session, error) {
	if s == nil || s.DB == nil || len(token) < 40 || len(token) > 128 {
		return Session{}, ErrSessionInvalid
	}
	digest := sha256.Sum256([]byte(token))
	var result Session
	err := s.DB.QueryRow(ctx, `SELECT session_id,server_id,user_uid,COALESCE(device_id,''),expires_at FROM auth_sessions WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now() AND provider='ed25519'`, digest[:]).Scan(&result.ID, &result.ServerID, &result.UserUID, &result.DeviceID, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrSessionInvalid
	}
	if err != nil {
		return Session{}, err
	}
	return result, nil
}

func (s *Service) RevokeUserSessions(ctx context.Context, serverID, userUID string) error {
	if s == nil || s.DB == nil {
		return errors.New("verified sessions unavailable")
	}
	_, err := s.DB.Exec(ctx, `UPDATE auth_sessions SET revoked_at=now() WHERE server_id=$1 AND user_uid=$2 AND revoked_at IS NULL`, serverID, userUID)
	return err
}

func (s *Service) CanAccessServer(ctx context.Context, serverID, userUID string) (bool, error) {
	if s == nil || s.DB == nil {
		return false, ErrSessionInvalid
	}
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM server_memberships WHERE server_id=$1 AND user_uid=$2 AND membership_state='active')`, serverID, userUID).Scan(&allowed)
	return allowed, err
}

var ErrBindingForbidden = errors.New("identity binding approval forbidden")
var ErrBindingConflict = errors.New("identity binding already verified")

// ApproveLegacyBinding requires an independently verified claimant and writes
// the approval and audit metadata atomically. Owner key recovery is deliberately
// reserved for the operator procedure.
func (s *Service) ApproveLegacyBinding(ctx context.Context, serverID, approverUID, targetUID string, publicKey ed25519.PublicKey, verificationReference string) error {
	if s == nil || s.DB == nil {
		return ErrSessionInvalid
	}
	if len(publicKey) != ed25519.PublicKeySize || len(strings.TrimSpace(verificationReference)) < 8 || len(verificationReference) > 256 {
		return ErrInvalidKey
	}
	if approverUID == targetUID {
		return ErrBindingForbidden
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var approverRole, targetRole string
	err = tx.QueryRow(ctx, `SELECT role FROM server_memberships WHERE server_id=$1 AND user_uid=$2 AND membership_state='active' FOR UPDATE`, serverID, approverUID).Scan(&approverRole)
	if err != nil {
		return ErrBindingForbidden
	}
	err = tx.QueryRow(ctx, `SELECT role FROM server_memberships WHERE server_id=$1 AND user_uid=$2 FOR UPDATE`, serverID, targetUID).Scan(&targetRole)
	if err != nil {
		return ErrBindingForbidden
	}
	if targetRole == "owner" || !(approverRole == "owner" || (approverRole == "moderator" && targetRole == "member")) {
		return ErrBindingForbidden
	}
	var existingKey []byte
	var verifiedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT public_key,verified_at FROM legacy_identity_bindings WHERE server_id=$1 AND user_uid=$2 FOR UPDATE`, serverID, targetUID).Scan(&existingKey, &verifiedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if verifiedAt != nil {
		return ErrBindingConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO legacy_identity_bindings(server_id,user_uid,public_key,approved_by_uid,verified_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(server_id,user_uid) DO UPDATE SET public_key=EXCLUDED.public_key,approved_by_uid=EXCLUDED.approved_by_uid,verified_at=now() WHERE legacy_identity_bindings.verified_at IS NULL`, serverID, targetUID, []byte(publicKey), approverUID)
	if err != nil {
		return err
	}
	fingerprint := sha256.Sum256(publicKey)
	_, err = tx.Exec(ctx, `INSERT INTO moderation_audit(server_id,actor_uid,event_type,payload) VALUES($1,$2,'identity.binding.approved',jsonb_build_object('target_uid',$3::text,'key_fingerprint',$4::text,'verification_reference',$5::text))`, serverID, approverUID, targetUID, hex.EncodeToString(fingerprint[:]), strings.TrimSpace(verificationReference))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
