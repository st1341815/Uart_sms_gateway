package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

// OIDCService OIDC 认证服务
type OIDCService struct {
	logger       *zap.Logger
	config       *config.OIDCConfig
	provider     *oidc.Provider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
	stateStore   map[string]time.Time // 简单的 state 存储（生产环境应使用 Redis 等）
}

// NewOIDCService 创建 OIDC 服务
func NewOIDCService(logger *zap.Logger, appConfig *config.AppConfig) *OIDCService {
	if appConfig.OIDC == nil || !appConfig.OIDC.Enabled {
		logger.Info("OIDC 认证未启用")
		return &OIDCService{
			logger: logger,
			config: nil,
		}
	}

	oidcConfig := appConfig.OIDC

	// 验证配置
	if oidcConfig.Issuer == "" || oidcConfig.ClientID == "" || oidcConfig.ClientSecret == "" {
		logger.Error("OIDC 配置不完整，OIDC 认证将被禁用")
		return &OIDCService{
			logger: logger,
			config: nil,
		}
	}

	ctx := context.Background()

	// 初始化 OIDC Provider
	provider, err := oidc.NewProvider(ctx, oidcConfig.Issuer)
	if err != nil {
		logger.Error("初始化 OIDC Provider 失败，OIDC 认证将被禁用", zap.Error(err))
		return &OIDCService{
			logger: logger,
			config: nil,
		}
	}

	// 配置 OAuth2
	oauth2Config := oauth2.Config{
		ClientID:     oidcConfig.ClientID,
		ClientSecret: oidcConfig.ClientSecret,
		RedirectURL:  oidcConfig.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// 创建 ID Token 验证器
	verifier := provider.Verifier(&oidc.Config{ClientID: oidcConfig.ClientID})

	logger.Info("OIDC 服务初始化成功", zap.String("issuer", oidcConfig.Issuer))

	return &OIDCService{
		logger:       logger,
		config:       oidcConfig,
		provider:     provider,
		oauth2Config: oauth2Config,
		verifier:     verifier,
		stateStore:   make(map[string]time.Time),
	}
}

// IsEnabled 检查 OIDC 是否启用
func (s *OIDCService) IsEnabled() bool {
	return s.config != nil && s.config.Enabled
}

// GenerateAuthURL 生成认证 URL
func (s *OIDCService) GenerateAuthURL() (string, string, error) {
	if !s.IsEnabled() {
		return "", "", errors.New("OIDC 未启用")
	}

	// 生成随机 state
	state, err := s.generateState()
	if err != nil {
		return "", "", fmt.Errorf("生成 state 失败: %w", err)
	}

	// 存储 state（有效期 10 分钟）
	s.stateStore[state] = time.Now().Add(10 * time.Minute)

	// 清理过期的 state
	s.cleanExpiredStates()

	authURL := s.oauth2Config.AuthCodeURL(state)
	return authURL, state, nil
}

// ExchangeCode 交换授权码获取 token 和用户信息
func (s *OIDCService) ExchangeCode(ctx context.Context, code, state string) (string, string, error) {
	if !s.IsEnabled() {
		return "", "", errors.New("OIDC 未启用")
	}

	// 验证 state
	if !s.validateState(state) {
		return "", "", errors.New("无效的 state")
	}

	// 删除已使用的 state
	delete(s.stateStore, state)

	// 交换授权码
	oauth2Token, err := s.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return "", "", fmt.Errorf("交换授权码失败: %w", err)
	}

	// 提取 ID Token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		return "", "", errors.New("未获取到 ID Token")
	}

	// 验证 ID Token
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return "", "", fmt.Errorf("验证 ID Token 失败: %w", err)
	}

	// 提取用户信息
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}

	if err := idToken.Claims(&claims); err != nil {
		return "", "", fmt.Errorf("解析 claims 失败: %w", err)
	}

	// 确定用户标识（优先使用 email，其次 preferred_username，最后使用 subject）
	username := claims.Email
	if username == "" {
		username = claims.PreferredUsername
	}
	if username == "" {
		username = idToken.Subject
	}

	nickname := claims.Name
	if nickname == "" {
		nickname = username
	}

	s.logger.Info("OIDC 认证成功",
		zap.String("username", username),
		zap.String("nickname", nickname),
		zap.String("subject", idToken.Subject))

	return username, nickname, nil
}

// generateState 生成随机 state
func (s *OIDCService) generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// validateState 验证 state
func (s *OIDCService) validateState(state string) bool {
	expiresAt, exists := s.stateStore[state]
	if !exists {
		return false
	}
	return time.Now().Before(expiresAt)
}

// cleanExpiredStates 清理过期的 state
func (s *OIDCService) cleanExpiredStates() {
	now := time.Now()
	for state, expiresAt := range s.stateStore {
		if now.After(expiresAt) {
			delete(s.stateStore, state)
		}
	}
}
