package handler

import (
	"testing"

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
