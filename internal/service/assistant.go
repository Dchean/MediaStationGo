// Package service — multi-turn AI assistant chat.
//
// AssistantService persists chat sessions / messages and forwards user
// turns to AIService.Chat() for the actual LLM call. When the AI is
// disabled we still keep the transcript so the UI doesn't lose state;
// the assistant simply replies with a deterministic offline note.
//
// The "operation" / "undo" surface from the upstream Python project is
// stubbed out: we accept the request, log it, and return a unique op
// ID so the UI's Undo affordance still renders. Full action execution
// would need a typed schema and side-effects we don't ship here.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

// AssistantService coordinates AssistantSession + AssistantMessage rows
// against the underlying AIService.
type AssistantService struct {
	log  *zap.Logger
	repo *repository.Container
	ai   *AIService
}

// NewAssistantService is the constructor.
func NewAssistantService(log *zap.Logger, repo *repository.Container, ai *AIService) *AssistantService {
	return &AssistantService{log: log, repo: repo, ai: ai}
}

// SessionView bundles the session header with its messages.
type SessionView struct {
	Session  model.AssistantSession   `json:"session"`
	Messages []model.AssistantMessage `json:"messages"`
}

// CreateSession opens a new chat thread.
func (s *AssistantService) CreateSession(ctx context.Context, userID, title string) (*model.AssistantSession, error) {
	if title == "" {
		title = "New chat"
	}
	sess := &model.AssistantSession{UserID: userID, Title: title}
	if err := s.repo.Assistant.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// ListSessions returns sessions for the user (or every session for
// admins when adminAll == true).
func (s *AssistantService) ListSessions(ctx context.Context, userID string, adminAll bool) ([]model.AssistantSession, error) {
	if adminAll {
		return s.repo.Assistant.ListSessions(ctx, "")
	}
	return s.repo.Assistant.ListSessions(ctx, userID)
}

// GetSession returns the full transcript for one session, after
// asserting ownership when the caller is not an admin.
func (s *AssistantService) GetSession(ctx context.Context, sessionID, userID string, isAdmin bool) (*SessionView, error) {
	sess, err := s.repo.Assistant.FindSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, errors.New("session not found")
	}
	if !isAdmin && sess.UserID != userID {
		return nil, errors.New("forbidden")
	}
	msgs, err := s.repo.Assistant.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return &SessionView{Session: *sess, Messages: msgs}, nil
}

// DeleteSession drops the session and its transcript.
func (s *AssistantService) DeleteSession(ctx context.Context, sessionID, userID string, isAdmin bool) error {
	sess, err := s.repo.Assistant.FindSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return errors.New("session not found")
	}
	if !isAdmin && sess.UserID != userID {
		return errors.New("forbidden")
	}
	return s.repo.Assistant.DeleteSession(ctx, sessionID)
}

// Chat appends a user turn, calls the AI, persists the assistant
// response, and returns both new messages.
func (s *AssistantService) Chat(ctx context.Context, sessionID, userID, content string, isAdmin bool) (*SessionView, error) {
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("content required")
	}
	sess, err := s.repo.Assistant.FindSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, errors.New("session not found")
	}
	if !isAdmin && sess.UserID != userID {
		return nil, errors.New("forbidden")
	}

	// Append the user turn.
	userMsg := &model.AssistantMessage{
		SessionID: sessionID,
		Role:      "user",
		Content:   strings.TrimSpace(content),
	}
	if err := s.repo.Assistant.AppendMessage(ctx, userMsg); err != nil {
		return nil, err
	}

	// Assemble history for the AI call.
	prior, _ := s.repo.Assistant.ListMessages(ctx, sessionID)
	history := make([]ChatTurn, 0, len(prior))
	for _, m := range prior {
		history = append(history, ChatTurn{Role: m.Role, Content: m.Content})
	}

	// Call the LLM (or fall back to a deterministic offline reply).
	reply, err := s.ai.Chat(ctx, history)
	if err != nil {
		s.log.Warn("assistant chat failed", zap.Error(err))
		reply = "(AI 暂未配置或调用失败,请稍后再试。)"
	}
	asstMsg := &model.AssistantMessage{
		SessionID: sessionID,
		Role:      "assistant",
		Content:   reply,
	}
	if err := s.repo.Assistant.AppendMessage(ctx, asstMsg); err != nil {
		return nil, err
	}
	return s.GetSession(ctx, sessionID, userID, isAdmin)
}

// Execute is the operation-execute stub. We log the proposed action
// and return a synthetic OpID so the UI's Undo button has something to
// reference.  Real execution would need a typed action schema we don't
// ship here.
func (s *AssistantService) Execute(ctx context.Context, sessionID, userID string, action map[string]any) (string, error) {
	if sessionID == "" {
		return "", errors.New("session_id required")
	}
	opID := uuid.NewString()
	s.log.Info("assistant.execute (stub)",
		zap.String("session_id", sessionID),
		zap.String("user_id", userID),
		zap.String("op_id", opID),
		zap.Any("action", action),
	)
	// Record the action in the transcript so it shows up in History.
	_ = s.repo.Assistant.AppendMessage(ctx, &model.AssistantMessage{
		SessionID:   sessionID,
		Role:        "system",
		Content:     "Action queued (no-op stub)",
		OperationID: opID,
	})
	return opID, nil
}

// Undo is the inverse stub; we just record the request.
func (s *AssistantService) Undo(ctx context.Context, opID string) error {
	s.log.Info("assistant.undo (stub)", zap.String("op_id", opID))
	return nil
}

// History returns the operations issued by the user, by walking the
// transcripts and filtering on OperationID. This is bounded to recent
// rows so the admin History pane stays responsive.
func (s *AssistantService) History(ctx context.Context, userID string, isAdmin bool) ([]map[string]any, error) {
	sessions, err := s.ListSessions(ctx, userID, isAdmin)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	cutoff := time.Now().AddDate(0, 0, -30)
	for _, sess := range sessions {
		msgs, _ := s.repo.Assistant.ListMessages(ctx, sess.ID)
		for _, m := range msgs {
			if m.OperationID == "" || m.CreatedAt.Before(cutoff) {
				continue
			}
			out = append(out, map[string]any{
				"op_id":      m.OperationID,
				"session":   sess.ID,
				"created_at": m.CreatedAt,
				"content":    m.Content,
			})
		}
	}
	return out, nil
}
