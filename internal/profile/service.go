package profile

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type AvatarMode string

const (
	AvatarModeGenerated AvatarMode = "generated"
	AvatarModeUploaded  AvatarMode = "uploaded"
)

var (
	ErrDisplayNameInvalid    = errors.New("display name is invalid")
	ErrAvatarModeUnsupported = errors.New("avatar mode unsupported")
	ErrAvatarPresetInvalid   = errors.New("avatar preset invalid")
	ErrAvatarAssetNotFound   = errors.New("avatar asset not found")
	ErrAvatarTypeUnsupported = errors.New("avatar type unsupported")
	ErrAvatarTooLarge        = errors.New("avatar too large")
	ErrAvatarDimensions      = errors.New("avatar dimensions exceeded")
	ErrProfileConflict       = errors.New("profile conflict")
)

var displayNamePattern = regexp.MustCompile(`^[\p{L}\p{N} ._\-]+$`)

type CanonicalProfile struct {
	UserUID        string     `json:"user_uid"`
	DisplayName    string     `json:"display_name"`
	AvatarMode     AvatarMode `json:"avatar_mode"`
	AvatarPresetID *string    `json:"avatar_preset_id"`
	AvatarAssetID  *string    `json:"avatar_asset_id"`
	AvatarURL      *string    `json:"avatar_url"`
	ProfileVersion int        `json:"profile_version"`
	UpdatedAt      string     `json:"updated_at"`
}

type AvatarAsset struct {
	AvatarAssetID string `json:"avatar_asset_id"`
	AvatarURL     string `json:"avatar_url"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	ContentType   string `json:"content_type"`
	Bytes         int    `json:"bytes"`
}

type UpdateInput struct {
	DisplayName   string
	AvatarMode    AvatarMode
	AvatarPreset  string
	AvatarAssetID string
}

type Broadcaster interface {
	BroadcastProfileUpdated(profile CanonicalProfile)
}

type Service struct {
	mu sync.RWMutex

	publicBaseURL string
	serverID      string

	displayNameMin int
	displayNameMax int
	maxUploadBytes int
	maxImageWidth  int
	maxImageHeight int

	allowedAvatarPresets map[string]struct{}
	allowedMimeTypes     map[string]struct{}

	profilesByUID map[string]CanonicalProfile
	avatarsByID   map[string]avatarBlob

	broadcaster Broadcaster
}

type avatarBlob struct {
	metadata AvatarAsset
	content  []byte
}

func NewService(publicBaseURL string, serverID string) *Service {
	presets := map[string]struct{}{}
	for _, preset := range []string{"preset_01", "preset_02", "preset_03", "preset_04", "preset_05", "preset_06"} {
		presets[preset] = struct{}{}
	}

	return &Service{
		publicBaseURL:        strings.TrimSuffix(strings.TrimSpace(publicBaseURL), "/"),
		serverID:             strings.TrimSpace(serverID),
		displayNameMin:       2,
		displayNameMax:       32,
		maxUploadBytes:       2 * 1024 * 1024,
		maxImageWidth:        1024,
		maxImageHeight:       1024,
		allowedAvatarPresets: presets,
		allowedMimeTypes:     map[string]struct{}{"image/png": {}, "image/jpeg": {}},
		profilesByUID:        make(map[string]CanonicalProfile),
		avatarsByID:          make(map[string]avatarBlob),
		broadcaster:          nil,
	}
}

func (s *Service) SetBroadcaster(b Broadcaster) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcaster = b
}

func (s *Service) ServerID() string {
	return s.serverID
}

func (s *Service) DisplayNameRules() (minLen int, maxLen int) {
	return s.displayNameMin, s.displayNameMax
}

func (s *Service) AvatarUploadRules() (maxBytes int, maxWidth int, maxHeight int, mimeTypes []string) {
	mimeTypes = make([]string, 0, len(s.allowedMimeTypes))
	for mime := range s.allowedMimeTypes {
		mimeTypes = append(mimeTypes, mime)
	}
	sort.Strings(mimeTypes)
	return s.maxUploadBytes, s.maxImageWidth, s.maxImageHeight, mimeTypes
}

func (s *Service) AllowedAvatarModes() []string {
	return []string{string(AvatarModeGenerated), string(AvatarModeUploaded)}
}

func (s *Service) GetOrCreate(userUID string) CanonicalProfile {
	userUID = normalizeUID(userUID)
	s.mu.Lock()
	defer s.mu.Unlock()
	profile := s.getOrCreateLocked(userUID)
	return cloneProfile(profile)
}

func (s *Service) BatchGet(userUIDs []string) []CanonicalProfile {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]struct{}, len(userUIDs))
	out := make([]CanonicalProfile, 0, len(userUIDs))
	for _, rawUID := range userUIDs {
		userUID := normalizeUID(rawUID)
		if userUID == "" {
			continue
		}
		if _, exists := seen[userUID]; exists {
			continue
		}
		seen[userUID] = struct{}{}
		out = append(out, cloneProfile(s.getOrCreateLocked(userUID)))
	}
	return out
}

func (s *Service) UploadAvatar(contentType string, data []byte) (AvatarAsset, error) {
	contentType = normalizeContentType(contentType, data)
	if _, ok := s.allowedMimeTypes[contentType]; !ok {
		return AvatarAsset{}, ErrAvatarTypeUnsupported
	}
	if len(data) == 0 || len(data) > s.maxUploadBytes {
		return AvatarAsset{}, ErrAvatarTooLarge
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return AvatarAsset{}, ErrAvatarTypeUnsupported
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > s.maxImageWidth || cfg.Height > s.maxImageHeight {
		return AvatarAsset{}, ErrAvatarDimensions
	}

	assetID := "asset_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	assetURL := s.avatarAssetURL(assetID)
	asset := AvatarAsset{
		AvatarAssetID: assetID,
		AvatarURL:     assetURL,
		Width:         cfg.Width,
		Height:        cfg.Height,
		ContentType:   contentType,
		Bytes:         len(data),
	}

	s.mu.Lock()
	s.avatarsByID[assetID] = avatarBlob{
		metadata: asset,
		content:  append([]byte(nil), data...),
	}
	s.mu.Unlock()
	return asset, nil
}

func (s *Service) AvatarContent(assetID string) (AvatarAsset, []byte, error) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return AvatarAsset{}, nil, ErrAvatarAssetNotFound
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	blob, ok := s.avatarsByID[assetID]
	if !ok {
		return AvatarAsset{}, nil, ErrAvatarAssetNotFound
	}
	return blob.metadata, append([]byte(nil), blob.content...), nil
}

func (s *Service) Update(userUID string, input UpdateInput, expectedVersion *int) (CanonicalProfile, error) {
	userUID = normalizeUID(userUID)
	if userUID == "" {
		return CanonicalProfile{}, ErrDisplayNameInvalid
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if err := s.validateDisplayName(displayName); err != nil {
		return CanonicalProfile{}, err
	}

	s.mu.Lock()
	profile := s.getOrCreateLocked(userUID)

	if expectedVersion != nil && profile.ProfileVersion != *expectedVersion {
		s.mu.Unlock()
		return CanonicalProfile{}, ErrProfileConflict
	}

	profile.DisplayName = displayName
	profile.AvatarMode = input.AvatarMode
	switch input.AvatarMode {
	case AvatarModeGenerated:
		preset := strings.TrimSpace(input.AvatarPreset)
		if _, ok := s.allowedAvatarPresets[preset]; !ok {
			s.mu.Unlock()
			return CanonicalProfile{}, ErrAvatarPresetInvalid
		}
		profile.AvatarPresetID = strPtr(preset)
		profile.AvatarAssetID = nil
		profile.AvatarURL = nil
	case AvatarModeUploaded:
		assetID := strings.TrimSpace(input.AvatarAssetID)
		blob, ok := s.avatarsByID[assetID]
		if !ok {
			s.mu.Unlock()
			return CanonicalProfile{}, ErrAvatarAssetNotFound
		}
		profile.AvatarPresetID = nil
		profile.AvatarAssetID = strPtr(assetID)
		profile.AvatarURL = strPtr(blob.metadata.AvatarURL)
	default:
		s.mu.Unlock()
		return CanonicalProfile{}, ErrAvatarModeUnsupported
	}

	profile.ProfileVersion++
	profile.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	s.profilesByUID[userUID] = profile
	broadcaster := s.broadcaster
	updated := cloneProfile(profile)
	s.mu.Unlock()

	if broadcaster != nil {
		broadcaster.BroadcastProfileUpdated(updated)
	}
	return updated, nil
}

func (s *Service) getOrCreateLocked(userUID string) CanonicalProfile {
	profile, exists := s.profilesByUID[userUID]
	if exists {
		return profile
	}

	presetID := defaultPresetForUID(userUID)
	now := time.Now().UTC().Format(time.RFC3339)
	profile = CanonicalProfile{
		UserUID:        userUID,
		DisplayName:    defaultDisplayName(userUID),
		AvatarMode:     AvatarModeGenerated,
		AvatarPresetID: strPtr(presetID),
		AvatarAssetID:  nil,
		AvatarURL:      nil,
		ProfileVersion: 1,
		UpdatedAt:      now,
	}
	s.profilesByUID[userUID] = profile
	return profile
}

func (s *Service) validateDisplayName(displayName string) error {
	runeCount := len([]rune(displayName))
	if runeCount < s.displayNameMin || runeCount > s.displayNameMax {
		return ErrDisplayNameInvalid
	}
	if !displayNamePattern.MatchString(displayName) {
		return ErrDisplayNameInvalid
	}
	return nil
}

func (s *Service) avatarAssetURL(assetID string) string {
	if s.publicBaseURL == "" {
		return fmt.Sprintf("/v1/profile/avatar/%s", assetID)
	}
	return fmt.Sprintf("%s/v1/profile/avatar/%s", s.publicBaseURL, assetID)
}

func normalizeUID(userUID string) string {
	userUID = strings.TrimSpace(userUID)
	if userUID == "" {
		return ""
	}
	return userUID
}

func normalizeContentType(contentType string, body []byte) string {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if contentType != "" {
		if idx := strings.Index(contentType, ";"); idx >= 0 {
			contentType = strings.TrimSpace(contentType[:idx])
		}
	}
	if len(body) > 0 {
		detected := strings.ToLower(http.DetectContentType(body))
		if contentType == "" || contentType == "application/octet-stream" {
			contentType = detected
		}
	}
	return contentType
}

func cloneProfile(profile CanonicalProfile) CanonicalProfile {
	out := profile
	if profile.AvatarPresetID != nil {
		out.AvatarPresetID = strPtr(*profile.AvatarPresetID)
	}
	if profile.AvatarAssetID != nil {
		out.AvatarAssetID = strPtr(*profile.AvatarAssetID)
	}
	if profile.AvatarURL != nil {
		out.AvatarURL = strPtr(*profile.AvatarURL)
	}
	return out
}

func strPtr(value string) *string {
	value = strings.TrimSpace(value)
	return &value
}

func defaultPresetForUID(userUID string) string {
	choices := []string{"preset_01", "preset_02", "preset_03", "preset_04", "preset_05", "preset_06"}
	sum := 0
	for _, r := range userUID {
		sum += int(r)
	}
	return choices[sum%len(choices)]
}

func defaultDisplayName(userUID string) string {
	if strings.HasPrefix(userUID, "uid_") && len(userUID) > 4 {
		trimmed := userUID[4:]
		if len(trimmed) > 10 {
			trimmed = trimmed[:10]
		}
		return "User " + trimmed
	}
	if len(userUID) > 14 {
		return "User " + userUID[:14]
	}
	return "User " + userUID
}
