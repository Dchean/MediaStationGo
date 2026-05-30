package service

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	OpenSourceUserLimit = 20
	LicensedUserLimit   = 100

	LicenseSettingActivation = "license.activation"
)

type LicenseActivationState struct {
	Valid          bool   `json:"valid"`
	LicenseType    string `json:"license_type,omitempty"`
	ExpiryDate     string `json:"expiry_date,omitempty"`
	MaxDevices     int    `json:"max_devices,omitempty"`
	MaxUsers       *int   `json:"max_users,omitempty"`
	UnlimitedUsers bool   `json:"unlimited_users,omitempty"`
	DaysRemaining  *int   `json:"days_remaining,omitempty"`
	NextHeartbeat  string `json:"next_heartbeat,omitempty"`
	DeviceID       string `json:"device_id,omitempty"`
	DeviceName     string `json:"device_name,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
}

func LicensedMaxUsers(ctx context.Context, repos *repository.Container) int64 {
	state, ok := loadLicenseActivationState(ctx, repos)
	if ok && state.Valid && !licenseExpired(state.ExpiryDate) {
		if state.UnlimitedUsers {
			return math.MaxInt64
		}
		if state.MaxUsers != nil && *state.MaxUsers > 0 {
			return int64(*state.MaxUsers)
		}
		return LicensedUserLimit
	}
	return OpenSourceUserLimit
}

func LicenseActive(ctx context.Context, repos *repository.Container) bool {
	if repos == nil || repos.Setting == nil {
		return false
	}
	state, ok := loadLicenseActivationState(ctx, repos)
	return ok && state.Valid && !licenseExpired(state.ExpiryDate)
}

func loadLicenseActivationState(ctx context.Context, repos *repository.Container) (LicenseActivationState, bool) {
	if repos == nil || repos.Setting == nil {
		return LicenseActivationState{}, false
	}
	raw, err := repos.Setting.Get(ctx, LicenseSettingActivation)
	if err != nil || raw == "" {
		return LicenseActivationState{}, false
	}
	var state LicenseActivationState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return LicenseActivationState{}, false
	}
	return state, true
}

func licenseExpired(expiry string) bool {
	if expiry == "" {
		return false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, expiry); err == nil {
			return time.Now().After(t)
		}
	}
	return false
}
