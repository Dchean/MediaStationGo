// Package service — audit log helper.
//
// AuditService writes structured AccessLog rows for sensitive actions
// (login, library CRUD, scrape / scan triggers, download enqueue, etc).
// It deliberately swallows write errors so audit failures never bubble
// up to the caller.
package service

import (
	"context"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// AuditService is the only sanctioned writer for the access_logs table.
type AuditService struct {
	log  *zap.Logger
	repo *repository.Container
}

// NewAuditService is the constructor.
func NewAuditService(log *zap.Logger, repo *repository.Container) *AuditService {
	return &AuditService{log: log, repo: repo}
}

// Record persists one audit row.
func (a *AuditService) Record(ctx context.Context, userID, action, target, ip, detail string) {
	row := &model.AccessLog{
		UserID: userID,
		Action: action,
		Target: target,
		IP:     ip,
		Detail: detail,
	}
	if err := a.repo.Log.Create(ctx, row); err != nil {
		a.log.Debug("audit write failed", zap.Error(err))
	}
}
