package rtc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidTicket = errors.New("invalid join ticket")
	ErrExpiredTicket = errors.New("join ticket expired")
	ErrReplayTicket  = errors.New("join ticket replayed")
)

type IssueTicketInput struct {
	ServerID    string
	ChannelID   string
	UserUID     string
	DeviceID    string
	Permissions Permissions
}

type TokenService struct {
	secret    []byte
	ttl       time.Duration
	usedJTIs  map[string]int64
	usedMutex sync.Mutex
}

func NewTokenService(secret string, ttl time.Duration) *TokenService {
	return &TokenService{
		secret:   []byte(secret),
		ttl:      ttl,
		usedJTIs: make(map[string]int64),
	}
}

func (s *TokenService) Issue(input IssueTicketInput) (string, TicketClaims, error) {
	if strings.TrimSpace(input.ServerID) == "" || strings.TrimSpace(input.ChannelID) == "" {
		return "", TicketClaims{}, fmt.Errorf("server and channel ids are required")
	}
	now := time.Now().UTC()
	claims := TicketClaims{
		ServerID:    input.ServerID,
		ChannelID:   input.ChannelID,
		UserUID:     input.UserUID,
		DeviceID:    input.DeviceID,
		Permissions: input.Permissions,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(s.ttl).Unix(),
		JTI:         uuid.NewString(),
	}

	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", TicketClaims{}, fmt.Errorf("marshal claims: %w", err)
	}
	payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signatureEncoded := base64.RawURLEncoding.EncodeToString(s.sign(payloadEncoded))

	return payloadEncoded + "." + signatureEncoded, claims, nil
}

func (s *TokenService) ParseAndConsume(ticket string) (TicketClaims, error) {
	parts := strings.Split(ticket, ".")
	if len(parts) != 2 {
		return TicketClaims{}, ErrInvalidTicket
	}
	payloadEncoded := parts[0]
	signatureEncoded := parts[1]

	signature, err := base64.RawURLEncoding.DecodeString(signatureEncoded)
	if err != nil {
		return TicketClaims{}, ErrInvalidTicket
	}

	expected := s.sign(payloadEncoded)
	if !hmac.Equal(signature, expected) {
		return TicketClaims{}, ErrInvalidTicket
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadEncoded)
	if err != nil {
		return TicketClaims{}, ErrInvalidTicket
	}

	var claims TicketClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return TicketClaims{}, ErrInvalidTicket
	}

	now := time.Now().UTC().Unix()
	if claims.ExpiresAt <= now {
		return TicketClaims{}, ErrExpiredTicket
	}

	s.usedMutex.Lock()
	defer s.usedMutex.Unlock()
	s.gcUsedJTIs(now)
	if _, exists := s.usedJTIs[claims.JTI]; exists {
		return TicketClaims{}, ErrReplayTicket
	}
	s.usedJTIs[claims.JTI] = claims.ExpiresAt

	return claims, nil
}

func (s *TokenService) sign(payloadEncoded string) []byte {
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(payloadEncoded))
	return mac.Sum(nil)
}

func (s *TokenService) gcUsedJTIs(nowUnix int64) {
	if len(s.usedJTIs) < 5000 {
		return
	}
	for jti, exp := range s.usedJTIs {
		if exp <= nowUnix {
			delete(s.usedJTIs, jti)
		}
	}
}
