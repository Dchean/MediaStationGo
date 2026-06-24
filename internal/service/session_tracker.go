package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/model"
)

const (
	realtimeSessionTTL       = 30 * time.Minute
	realtimeSessionOnlineTTL = 5 * time.Minute
)

func RealtimeDeletionGuardWindow() time.Duration {
	return realtimeSessionTTL
}

// RealtimeSession is an in-memory Emby-compatible session view. It mirrors the
// information reported by Emby clients through AuthenticateByName and
// /Sessions/Playing/* without requiring Playback Reporting persistence.
type RealtimeSession struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	UserName       string     `json:"user_name,omitempty"`
	DeviceID       string     `json:"device_id"`
	DeviceName     string     `json:"device_name,omitempty"`
	Client         string     `json:"client,omitempty"`
	RemoteEndPoint string     `json:"remote_end_point,omitempty"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	ItemID         string     `json:"item_id,omitempty"`
	PositionTicks  int64      `json:"position_ticks,omitempty"`
	RuntimeTicks   int64      `json:"runtime_ticks,omitempty"`
	IsPlaying      bool       `json:"is_playing"`
	IsPaused       bool       `json:"is_paused"`
	LastPlaybackAt *time.Time `json:"last_playback_at,omitempty"`
}

type realtimeSessionInput struct {
	UserID         string
	UserName       string
	DeviceID       string
	DeviceName     string
	Client         string
	RemoteEndPoint string
	ItemID         string
	PositionTicks  int64
	RuntimeTicks   int64
	IsPlaying      bool
	IsPaused       bool
	PlaybackUpdate bool
}

type userRealtimeActivity struct {
	LastActivityAt    *time.Time
	ActiveDeviceCount int
	Online            bool
}

// SessionTrackerService keeps recent client state in memory. It is intentionally
// not durable: process restart clears transient online status, while normal
// login/playback requests repopulate it immediately.
type SessionTrackerService struct {
	log *zap.Logger

	mu       sync.RWMutex
	sessions map[string]RealtimeSession
	activity map[string]time.Time
	now      func() time.Time
}

func NewSessionTrackerService(log *zap.Logger) *SessionTrackerService {
	return &SessionTrackerService{
		log:      log,
		sessions: make(map[string]RealtimeSession),
		activity: make(map[string]time.Time),
		now:      time.Now,
	}
}

func (s *SessionTrackerService) RecordLogin(ctx context.Context, userID, userName, deviceID, deviceName, client, remoteEndPoint string) {
	s.RecordActivity(ctx, userID, userName, deviceID, deviceName, client, remoteEndPoint)
}

func (s *SessionTrackerService) RecordActivity(ctx context.Context, userID, userName, deviceID, deviceName, client, remoteEndPoint string) {
	if s == nil {
		return
	}
	s.upsert(ctx, realtimeSessionInput{
		UserID:         userID,
		UserName:       userName,
		DeviceID:       deviceID,
		DeviceName:     deviceName,
		Client:         client,
		RemoteEndPoint: remoteEndPoint,
	})
}

func (s *SessionTrackerService) RecordPlayback(ctx context.Context, userID, userName, deviceID, deviceName, client, remoteEndPoint, itemID string, positionTicks, runtimeTicks int64, stopped bool) {
	if s == nil {
		return
	}
	s.upsert(ctx, realtimeSessionInput{
		UserID:         userID,
		UserName:       userName,
		DeviceID:       deviceID,
		DeviceName:     deviceName,
		Client:         client,
		RemoteEndPoint: remoteEndPoint,
		ItemID:         itemID,
		PositionTicks:  positionTicks,
		RuntimeTicks:   runtimeTicks,
		IsPlaying:      !stopped,
		PlaybackUpdate: true,
	})
}

func (s *SessionTrackerService) Logout(ctx context.Context, userID, deviceID, remoteEndPoint string) {
	if s == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	deviceID = strings.TrimSpace(deviceID)
	remoteEndPoint = strings.TrimSpace(remoteEndPoint)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activity == nil {
		s.activity = make(map[string]time.Time)
	}
	s.activity[userID] = s.now()
	for key, sess := range s.sessions {
		if sess.UserID != userID {
			continue
		}
		if deviceID != "" && sess.DeviceID != deviceID {
			continue
		}
		if deviceID == "" && remoteEndPoint != "" && sess.RemoteEndPoint != remoteEndPoint {
			continue
		}
		delete(s.sessions, key)
	}
}

func (s *SessionTrackerService) List(ctx context.Context) []RealtimeSession {
	if s == nil {
		return nil
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	out := make([]RealtimeSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastActivityAt.After(out[j].LastActivityAt)
	})
	return out
}

func (s *SessionTrackerService) ListByUser(ctx context.Context, userID string) []RealtimeSession {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	all := s.List(ctx)
	out := make([]RealtimeSession, 0, len(all))
	for _, sess := range all {
		if sess.UserID == userID {
			out = append(out, sess)
		}
	}
	return out
}

func (s *SessionTrackerService) ApplyToUsers(ctx context.Context, users []model.User) {
	if s == nil || len(users) == 0 {
		return
	}
	activity := s.activityByUser(ctx)
	for i := range users {
		a, ok := activity[users[i].ID]
		if !ok {
			continue
		}
		if a.LastActivityAt != nil && (users[i].LastLoginAt == nil || a.LastActivityAt.After(*users[i].LastLoginAt)) {
			t := *a.LastActivityAt
			users[i].LastLoginAt = &t
		}
		users[i].RealtimeOnline = a.Online
		users[i].RealtimeDeviceCount = a.ActiveDeviceCount
	}
}

func (s *SessionTrackerService) UserRecentlyActive(ctx context.Context, userID string, within time.Duration) bool {
	if s == nil || within <= 0 {
		return false
	}
	activity := s.activityByUser(ctx)[strings.TrimSpace(userID)]
	return activity.LastActivityAt != nil && activity.LastActivityAt.After(s.now().Add(-within))
}

func (s *SessionTrackerService) activityByUser(ctx context.Context) map[string]userRealtimeActivity {
	sessions := s.List(ctx)
	now := s.now()
	out := make(map[string]userRealtimeActivity)
	for userID, lastActivity := range s.userActivitySnapshot() {
		if strings.TrimSpace(userID) == "" {
			continue
		}
		a := out[userID]
		if a.LastActivityAt == nil || lastActivity.After(*a.LastActivityAt) {
			t := lastActivity
			a.LastActivityAt = &t
		}
		out[userID] = a
	}
	seenDevices := make(map[string]map[string]struct{})
	for _, sess := range sessions {
		if strings.TrimSpace(sess.UserID) == "" {
			continue
		}
		a := out[sess.UserID]
		if a.LastActivityAt == nil || sess.LastActivityAt.After(*a.LastActivityAt) {
			t := sess.LastActivityAt
			a.LastActivityAt = &t
		}
		if sess.LastActivityAt.After(now.Add(-realtimeSessionOnlineTTL)) {
			a.Online = true
		}
		if seenDevices[sess.UserID] == nil {
			seenDevices[sess.UserID] = map[string]struct{}{}
		}
		seenDevices[sess.UserID][sessionDeviceKey(sess)] = struct{}{}
		a.ActiveDeviceCount = len(seenDevices[sess.UserID])
		out[sess.UserID] = a
	}
	return out
}

func (s *SessionTrackerService) userActivitySnapshot() map[string]time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]time.Time, len(s.activity))
	for userID, lastActivity := range s.activity {
		out[userID] = lastActivity
	}
	return out
}

func (s *SessionTrackerService) upsert(ctx context.Context, in realtimeSessionInput) {
	userID := strings.TrimSpace(in.UserID)
	if userID == "" {
		return
	}
	now := s.now()
	in.DeviceID = strings.TrimSpace(in.DeviceID)
	in.DeviceName = strings.TrimSpace(in.DeviceName)
	in.Client = strings.TrimSpace(in.Client)
	in.RemoteEndPoint = strings.TrimSpace(in.RemoteEndPoint)
	if in.DeviceID == "" {
		in.DeviceID = fallbackSessionDeviceID(in.DeviceName, in.Client, in.RemoteEndPoint)
	}
	key := userID + "\x00" + in.DeviceID
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if s.activity == nil {
		s.activity = make(map[string]time.Time)
	}
	s.activity[userID] = now
	existing, existed := s.sessions[key]
	if strings.TrimSpace(in.UserName) == "" {
		in.UserName = existing.UserName
	}
	if in.DeviceName == "" {
		in.DeviceName = existing.DeviceName
	}
	if in.Client == "" {
		in.Client = existing.Client
	}
	if in.RemoteEndPoint == "" {
		in.RemoteEndPoint = existing.RemoteEndPoint
	}
	lastPlaybackAt := existing.LastPlaybackAt
	itemID := existing.ItemID
	positionTicks := existing.PositionTicks
	runtimeTicks := existing.RuntimeTicks
	isPlaying := existing.IsPlaying
	isPaused := existing.IsPaused
	if in.PlaybackUpdate {
		itemID = firstNonEmptyString(in.ItemID, existing.ItemID)
		if in.ItemID != "" || in.PositionTicks != 0 {
			positionTicks = in.PositionTicks
		}
		if in.ItemID != "" || in.RuntimeTicks != 0 {
			runtimeTicks = in.RuntimeTicks
		}
		isPlaying = in.IsPlaying
		isPaused = in.IsPaused
	}
	if in.PlaybackUpdate && (in.ItemID != "" || in.IsPlaying) {
		t := now
		lastPlaybackAt = &t
	}
	s.sessions[key] = RealtimeSession{
		ID:             key,
		UserID:         userID,
		UserName:       strings.TrimSpace(in.UserName),
		DeviceID:       in.DeviceID,
		DeviceName:     in.DeviceName,
		Client:         in.Client,
		RemoteEndPoint: in.RemoteEndPoint,
		LastActivityAt: now,
		ItemID:         itemID,
		PositionTicks:  positionTicks,
		RuntimeTicks:   runtimeTicks,
		IsPlaying:      isPlaying,
		IsPaused:       isPaused,
		LastPlaybackAt: lastPlaybackAt,
	}
	if !existed && s.log != nil {
		s.log.Debug("realtime session started",
			zap.String("user_id", userID),
			zap.String("device_id", in.DeviceID),
			zap.String("client", in.Client),
			zap.String("remote", in.RemoteEndPoint),
		)
	}
}

func (s *SessionTrackerService) pruneLocked(now time.Time) {
	expiresBefore := now.Add(-realtimeSessionTTL)
	for key, sess := range s.sessions {
		if sess.LastActivityAt.Before(expiresBefore) {
			delete(s.sessions, key)
		}
	}
	for userID, lastActivity := range s.activity {
		if lastActivity.Before(expiresBefore) {
			delete(s.activity, userID)
		}
	}
}

func fallbackSessionDeviceID(deviceName, client, remoteEndPoint string) string {
	parts := []string{strings.TrimSpace(deviceName), strings.TrimSpace(client), strings.TrimSpace(remoteEndPoint)}
	joined := strings.Trim(strings.Join(parts, "|"), "|")
	if joined == "" {
		joined = "unknown"
	}
	return "rt-" + fingerprint(client, joined)
}

func sessionDeviceKey(sess RealtimeSession) string {
	if strings.TrimSpace(sess.DeviceID) != "" {
		return strings.TrimSpace(sess.DeviceID)
	}
	return fallbackSessionDeviceID(sess.DeviceName, sess.Client, sess.RemoteEndPoint)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
