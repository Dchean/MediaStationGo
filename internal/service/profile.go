// Package service — user profile management.
package service

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// ProfileService handles non-credential user mutations.
type ProfileService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewProfileService is the constructor.
func NewProfileService(log *zap.Logger, repo *repository.Container) *ProfileService {
	return &ProfileService{log: log, repo: repo}
}

// ProfileUpdate is the patch object accepted by UpdateProfile. Empty
// fields are ignored so the same payload can be reused across screens.
type ProfileUpdate struct {
	Email     *string `json:"email,omitempty"`
	AvatarURL *string `json:"avatar_url,omitempty"`
}

// UpdateProfile applies a non-credential patch to the user.
func (p *ProfileService) UpdateProfile(ctx context.Context, userID string, patch ProfileUpdate) (*model.User, error) {
	if userID == "" {
		return nil, errors.New("missing user id")
	}
	updates := map[string]any{}
	if patch.Email != nil {
		v := strings.TrimSpace(*patch.Email)
		updates["email"] = v
	}
	if patch.AvatarURL != nil {
		updates["avatar_url"] = strings.TrimSpace(*patch.AvatarURL)
	}
	if len(updates) > 0 {
		if err := p.repo.DB.Model(&model.User{}).Where("id = ?", userID).
			Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return p.repo.User.FindByID(ctx, userID)
}

// AdminUpdateRole lets administrators promote / demote another user. The
// caller is expected to gate the route with AdminRequired.
func (p *ProfileService) AdminUpdateRole(ctx context.Context, userID, role string) (*model.User, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "admin" && role != "user" {
		return nil, errors.New("role must be admin or user")
	}
	if err := p.repo.DB.Model(&model.User{}).Where("id = ?", userID).
		Update("role", role).Error; err != nil {
		return nil, err
	}
	return p.repo.User.FindByID(ctx, userID)
}
