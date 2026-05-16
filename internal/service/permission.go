// Package service — per-user feature toggles.
//
// PermissionService persists model.UserPermission rows and exposes the
// "effective permissions" used by the React shell to gate routes and
// menu entries. Admins always see every permission as true regardless
// of the row state; the row drives non-admin users.
package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// PermissionService manages user permissions.
type PermissionService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewPermissionService is the constructor.
func NewPermissionService(log *zap.Logger, repo *repository.Container) *PermissionService {
	return &PermissionService{log: log, repo: repo}
}

// Defaults returns a non-admin's default permission set.
func DefaultPermissions(userID string) *model.UserPermission {
	return &model.UserPermission{
		UserID:                 userID,
		CanPlayMedia:           true,
		CanFavorite:            true,
		CanViewHistory:         true,
		CanViewDashboard:       true,
		CanViewDiscover:        true,
		CanCast:                true,
		CanManageDownloads:     false,
		CanManageSubscriptions: false,
		CanManageSites:         false,
		CanManageFiles:         false,
		CanManageSTRM:          false,
		CanUseAIAssistant:      false,
		CanAccessSettings:      false,
	}
}

// adminGrant returns the all-true permission set for admin users.
func adminGrant(userID string) *model.UserPermission {
	return &model.UserPermission{
		UserID:                 userID,
		CanPlayMedia:           true,
		CanFavorite:            true,
		CanViewHistory:         true,
		CanViewDashboard:       true,
		CanViewDiscover:        true,
		CanManageDownloads:     true,
		CanManageSubscriptions: true,
		CanManageSites:         true,
		CanManageFiles:         true,
		CanManageSTRM:          true,
		CanCast:                true,
		CanUseAIAssistant:      true,
		CanAccessSettings:      true,
	}
}

// Effective returns the permission set the React UI should consume.
// Admins skip the table entirely and get a synthetic all-grant row.
func (s *PermissionService) Effective(ctx context.Context, userID string) (*model.UserPermission, error) {
	u, err := s.repo.User.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	if u.Role == "admin" {
		return adminGrant(userID), nil
	}
	row, err := s.repo.Permission.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row != nil {
		return row, nil
	}
	// Seed defaults on first read so subsequent updates have a row to
	// patch.
	def := DefaultPermissions(userID)
	if err := s.repo.Permission.Save(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

// Save persists the user permission patch (admin only — caller checks).
func (s *PermissionService) Save(ctx context.Context, userID string, in *model.UserPermission) error {
	in.UserID = userID
	return s.repo.Permission.Save(ctx, in)
}

// Reset reverts to the non-admin defaults.
func (s *PermissionService) Reset(ctx context.Context, userID string) (*model.UserPermission, error) {
	def := DefaultPermissions(userID)
	if err := s.repo.Permission.Save(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}
