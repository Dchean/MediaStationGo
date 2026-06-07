// Package service — 双令牌认证服务。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/ShukeBta/MediaStationGo/internal/config"
	"github.com/ShukeBta/MediaStationGo/internal/model"
	"github.com/ShukeBta/MediaStationGo/internal/repository"
)

const (
	// AccessTokenDuration Access Token 有效期（60分钟）
	AccessTokenDuration = 60 * time.Minute
	// RefreshTokenDuration Refresh Token 有效期（30天）
	RefreshTokenDuration = 30 * 24 * time.Hour
	// RefreshTokenLength Refresh Token 随机字节长度
	RefreshTokenLength = 32
)

// Claims 是 JWT 载荷（复制自 middleware 以避免循环导入）。
type Claims struct {
	UserID string `json:"uid"`
	Role   string `json:"role"`
	Tier   string `json:"tier,omitempty"`
	jwt.RegisteredClaims
}

// TokenService 处理双令牌认证（Access Token + Refresh Token）。
type TokenService struct {
	cfg  *config.Config
	log  *zap.Logger
	repo *repository.Container
}

// NewTokenService 创建令牌服务实例。
func NewTokenService(cfg *config.Config, log *zap.Logger, repo *repository.Container) *TokenService {
	return &TokenService{cfg: cfg, log: log, repo: repo}
}

// TokenPair 包含访问令牌和刷新令牌。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // 秒
	TokenType    string `json:"token_type"`
}

// TokenService 错误定义。
var (
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrTokenExpired        = errors.New("token expired")
	ErrTokenRevoked        = errors.New("token revoked")
)

// IssuePair 为用户签发新的令牌对。
func (s *TokenService) IssuePair(ctx context.Context, userID, role, tier string) (*TokenPair, error) {
	// 生成 Access Token
	accessToken, err := s.issueAccessToken(userID, role, tier)
	if err != nil {
		return nil, err
	}

	// 生成 Refresh Token
	refreshToken, err := s.generateRefreshToken()
	if err != nil {
		return nil, err
	}

	// 存储 Refresh Token 哈希
	tokenHash := repository.HashToken(refreshToken)
	rt := &model.RefreshToken{
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(RefreshTokenDuration),
	}
	if err := s.repo.RefreshToken.Create(ctx, rt); err != nil {
		return nil, err
	}
	if err := s.repo.RefreshToken.RevokeOldestActiveByUserID(ctx, userID, s.maxActiveRefreshTokens(ctx)); err != nil {
		s.log.Warn("failed to enforce refresh token session limit", zap.String("user_id", userID), zap.Error(err))
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(AccessTokenDuration.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

func (s *TokenService) maxActiveRefreshTokens(ctx context.Context) int {
	cfg := loadBotConfig(ctx, s.repo)
	if cfg.MaxLoggedClients < 1 {
		return defaultBotConfig().MaxLoggedClients
	}
	return cfg.MaxLoggedClients
}

// issueAccessToken 签发 JWT Access Token（HS256，60分钟有效期）。
func (s *TokenService) issueAccessToken(userID, role, tier string) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		Tier:   tier,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenDuration)),
			Issuer:    "mediastationgo",
			Subject:   userID,
		},
	}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return t.SignedString([]byte(s.cfg.Secrets.JWTSecret))
}

// generateRefreshToken 生成安全的随机 Refresh Token。
func (s *TokenService) generateRefreshToken() (string, error) {
	buf := make([]byte, RefreshTokenLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// Refresh 使用 Refresh Token 轮换获取新的令牌对。
func (s *TokenService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	tokenHash := repository.HashToken(refreshToken)

	// 查找 Refresh Token 记录
	rt, err := s.repo.RefreshToken.FindByHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, ErrInvalidRefreshToken
	}

	// 检查是否已撤销
	if rt.Revoked {
		return nil, ErrTokenRevoked
	}

	// 检查是否过期
	if rt.IsExpired() {
		return nil, ErrTokenExpired
	}

	// 获取用户信息
	user, err := s.repo.User.FindByID(ctx, rt.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrInvalidRefreshToken
	}
	if !user.IsActive {
		return nil, ErrUserInactive
	}
	if user.ExpiredAt != nil && time.Now().After(*user.ExpiredAt) {
		return nil, ErrUserExpired
	}

	// 撤销旧的 Refresh Token
	if err := s.repo.RefreshToken.Revoke(ctx, tokenHash); err != nil {
		s.log.Warn("failed to revoke old refresh token", zap.Error(err))
	}

	// 签发新的令牌对
	return s.IssuePair(ctx, user.ID, user.Role, user.Tier)
}

// RevokeAll 撤销用户的所有 Refresh Token（用于登出）。
func (s *TokenService) RevokeAll(ctx context.Context, userID string) error {
	return s.repo.RefreshToken.RevokeByUserID(ctx, userID)
}

// ValidateAccessToken 验证 Access Token 并返回 Claims。
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(s.cfg.Secrets.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

// CleanupExpired 清理过期的 Refresh Token。
func (s *TokenService) CleanupExpired(ctx context.Context) error {
	return s.repo.RefreshToken.DeleteExpired(ctx)
}
