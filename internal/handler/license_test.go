package handler

import (
	"testing"
	"time"

	"github.com/ShukeBta/MediaStationGo/internal/service"
)

func TestLicenseStatusMaxUsersUsesLicensedLimit(t *testing.T) {
	maxUsers := 25
	state := service.LicenseActivationState{Valid: true, MaxUsers: &maxUsers}

	if got := licenseStatusMaxUsers(state); got != maxUsers {
		t.Fatalf("expected licensed max users %d, got %#v", maxUsers, got)
	}
}

func TestLicenseStatusMaxUsersAllowsUnlimited(t *testing.T) {
	state := service.LicenseActivationState{Valid: true, UnlimitedUsers: true}

	if got := licenseStatusMaxUsers(state); got != nil {
		t.Fatalf("expected unlimited max users to be nil, got %#v", got)
	}
}

func TestLicenseStatusMaxUsersFallsBackToOpenSourceLimit(t *testing.T) {
	state := service.LicenseActivationState{}

	if got := licenseStatusMaxUsers(state); got != service.OpenSourceUserLimit {
		t.Fatalf("expected open-source max users %d, got %#v", service.OpenSourceUserLimit, got)
	}
}

func TestApplyLicenseStatusReflectsEditedLimitAndClearsExpiry(t *testing.T) {
	maxUsers := 60
	licenseType := "subscription"
	state := service.LicenseActivationState{
		Valid:          true,
		LicenseType:    "enterprise",
		ExpiryDate:     "2026-01-01",
		MaxDevices:     2,
		UnlimitedUsers: true,
	}

	applyLicenseStatus(&state, licenseServerStatusResp{
		Valid:          true,
		LicenseType:    &licenseType,
		ExpiryDate:     nil,
		MaxDevices:     5,
		MaxUsers:       &maxUsers,
		UnlimitedUsers: false,
		DeviceName:     "Edited Device",
	}, "device-1")

	if !state.Valid || state.LicenseType != "subscription" || state.ExpiryDate != "" || state.MaxDevices != 5 {
		t.Fatalf("status fields were not fully refreshed: %+v", state)
	}
	if state.MaxUsers == nil || *state.MaxUsers != 60 || state.UnlimitedUsers {
		t.Fatalf("user limit was not refreshed from status: %+v", state)
	}
	if state.DeviceID != "device-1" || state.DeviceName != "Edited Device" {
		t.Fatalf("device fields were not refreshed: %+v", state)
	}
}

func TestApplyLicenseStatusReflectsUnlimitedUsers(t *testing.T) {
	maxUsers := 30
	state := service.LicenseActivationState{Valid: true, MaxUsers: &maxUsers}

	applyLicenseStatus(&state, licenseServerStatusResp{
		Valid:          true,
		MaxUsers:       nil,
		UnlimitedUsers: true,
	}, "device-1")

	if state.MaxUsers != nil || !state.UnlimitedUsers {
		t.Fatalf("unlimited status should clear previous finite user limit: %+v", state)
	}
}

func TestLicenseHeartbeatPayloadIncludesStoredLicenseKey(t *testing.T) {
	payload := licenseHeartbeatPayload(service.LicenseActivationState{
		LicenseKey: "MS-ABCD-EFGH-JKLM-NPQR",
	}, "device-1", "NAS")

	if payload["fingerprint"] != "device-1" || payload["instance_id"] != "device-1" || payload["device_name"] != "NAS" {
		t.Fatalf("heartbeat identity payload is wrong: %#v", payload)
	}
	if payload["key"] != "MS-ABCD-EFGH-JKLM-NPQR" {
		t.Fatalf("heartbeat should include stored license key for server-side backfill: %#v", payload)
	}
}

func TestLicenseHeartbeatDueUsesTwelveHourWindow(t *testing.T) {
	state := service.LicenseActivationState{
		Valid:     true,
		UpdatedAt: time.Now().Add(-11 * time.Hour).Format(time.RFC3339),
	}
	if licenseHeartbeatDue(state, 12*time.Hour) {
		t.Fatalf("heartbeat should not be due before interval")
	}

	state.UpdatedAt = time.Now().Add(-13 * time.Hour).Format(time.RFC3339)
	if !licenseHeartbeatDue(state, 12*time.Hour) {
		t.Fatalf("heartbeat should be due after interval")
	}
}

func TestLicenseHeartbeatEligibleRequiresActivationState(t *testing.T) {
	if licenseHeartbeatEligible(service.LicenseActivationState{DeviceID: "device-only"}) {
		t.Fatalf("device id alone should not trigger automatic license heartbeat")
	}
	if !licenseHeartbeatEligible(service.LicenseActivationState{LicenseKey: "MS-KEY"}) {
		t.Fatalf("stored license key should trigger automatic license heartbeat")
	}
	if !licenseHeartbeatEligible(service.LicenseActivationState{Valid: true}) {
		t.Fatalf("valid license state should trigger automatic license heartbeat")
	}
}
