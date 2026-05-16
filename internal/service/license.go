// Package service — license key management.
//
// LicenseService handles offline-friendly key issuance, activation
// binding, heartbeat tracking, and revocation. Keys are 24 random
// uppercase chars in groups of four (e.g. ABCD-1234-EFGH-5678-IJKL-90MN)
// — the same shape the Vue admin UI expects.
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// LicenseService manages license keys + activations.
type LicenseService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewLicenseService is the constructor.
func NewLicenseService(log *zap.Logger, repo *repository.Container) *LicenseService {
	return &LicenseService{log: log, repo: repo}
}

// Generate creates a new license key. ExpiresAt nil means "perpetual".
func (s *LicenseService) Generate(
	ctx context.Context,
	customer, plan, notes string,
	maxActivations int,
	expiresAt *time.Time,
) (*model.LicenseKey, error) {
	if maxActivations <= 0 {
		maxActivations = 1
	}
	k := &model.LicenseKey{
		Key:            randomLicenseKey(),
		Customer:       strings.TrimSpace(customer),
		Plan:           strings.TrimSpace(plan),
		MaxActivations: maxActivations,
		Notes:          strings.TrimSpace(notes),
		IssuedAt:       time.Now(),
		ExpiresAt:      expiresAt,
	}
	if err := s.repo.License.Create(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

// List returns every key (admin view).
func (s *LicenseService) List(ctx context.Context) ([]model.LicenseKey, error) {
	return s.repo.License.List(ctx)
}

// Activate binds a key to a device. Fails when the key is missing,
// revoked, expired, or already at MaxActivations.
func (s *LicenseService) Activate(
	ctx context.Context,
	key, deviceID, deviceName, ip string,
) (*model.LicenseActivation, error) {
	k, err := s.repo.License.FindByKey(ctx, key)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, errors.New("invalid key")
	}
	if k.Revoked {
		return nil, errors.New("key revoked")
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("key expired")
	}
	count, err := s.repo.License.CountActiveActivations(ctx, k.ID)
	if err != nil {
		return nil, err
	}
	if int(count) >= k.MaxActivations {
		return nil, errors.New("activation limit reached")
	}
	a := &model.LicenseActivation{
		KeyID:      k.ID,
		DeviceID:   strings.TrimSpace(deviceID),
		DeviceName: strings.TrimSpace(deviceName),
		IP:         ip,
	}
	if err := s.repo.License.AddActivation(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// ListActivations returns activations for a single key.
func (s *LicenseService) ListActivations(ctx context.Context, keyID string) ([]model.LicenseActivation, error) {
	return s.repo.License.ListActivations(ctx, keyID)
}

// Unbind marks one activation as released.
func (s *LicenseService) Unbind(ctx context.Context, activationID string) error {
	return s.repo.License.UnbindActivation(ctx, activationID)
}

// Revoke marks the entire key as revoked.
func (s *LicenseService) Revoke(ctx context.Context, keyID string) error {
	return s.repo.License.Update(ctx, keyID, map[string]any{"revoked": true})
}

// Heartbeat records the last time an activation phoned home.
func (s *LicenseService) Heartbeat(ctx context.Context, activationID string) error {
	return s.repo.License.TouchHeartbeat(ctx, activationID)
}

// Status returns a summary suitable for the Vue / React status panel.
func (s *LicenseService) Status(ctx context.Context, keyID string) (map[string]any, error) {
	k, err := s.repo.License.FindByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	if k == nil {
		return nil, errors.New("key not found")
	}
	count, _ := s.repo.License.CountActiveActivations(ctx, keyID)
	valid := !k.Revoked
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		valid = false
	}
	return map[string]any{
		"key":                k,
		"active_activations": count,
		"valid":              valid,
	}, nil
}

// randomLicenseKey produces a 24-char hyphenated key of A-Z and 0-9.
func randomLicenseKey() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // omit confusables
	out := make([]byte, 24)
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	// Group every 4 chars with a hyphen.
	var sb strings.Builder
	for i, c := range out {
		if i > 0 && i%4 == 0 {
			sb.WriteByte('-')
		}
		sb.WriteByte(byte(c))
	}
	return sb.String()
}
