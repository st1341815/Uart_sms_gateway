package internal

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.uber.org/zap"
)

func newCfg(secret string) *config.AppConfig {
	return &config.AppConfig{JWT: config.JWTConfig{Secret: secret}}
}

func withSecretFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "secret", ".jwt_secret")
	t.Setenv(jwtSecretFileEnv, path)
	return path
}

// TestEnsureJWTSecretGeneratesAndReuses 空配置 → 生成并落盘；第二次调用复用同一值
func TestEnsureJWTSecretGeneratesAndReuses(t *testing.T) {
	path := withSecretFile(t)
	logger := zap.NewNop()

	cfg := newCfg("")
	if err := ensureJWTSecret(cfg, logger); err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if len(cfg.JWT.Secret) < jwtSecretMinLen {
		t.Fatalf("生成的密钥过短: %d", len(cfg.JWT.Secret))
	}
	// 必须是 base64 的 32 字节随机数
	if _, err := base64.StdEncoding.DecodeString(cfg.JWT.Secret); err != nil {
		t.Fatalf("生成的密钥不是合法 base64: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("密钥文件未生成: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("密钥文件权限应为 0600，实际 %v", fi.Mode().Perm())
	}

	// 模拟重启：新的空配置应读回同一个值
	first := cfg.JWT.Secret
	cfg2 := newCfg("")
	if err := ensureJWTSecret(cfg2, logger); err != nil {
		t.Fatalf("复用失败: %v", err)
	}
	if cfg2.JWT.Secret != first {
		t.Fatal("重启后密钥应保持不变（否则库里的 AK/SK 密文全部解不开）")
	}
}

// TestEnsureJWTSecretKeepsValidConfigured 用户显式配置了合法密钥 → 原样保留、不落盘
func TestEnsureJWTSecretKeepsValidConfigured(t *testing.T) {
	path := withSecretFile(t)
	configured := strings.Repeat("a1b2c3d4", 8) // 64 字符
	cfg := newCfg(configured)

	if err := ensureJWTSecret(cfg, zap.NewNop()); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if cfg.JWT.Secret != configured {
		t.Fatal("显式配置的合法密钥不应被覆盖")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("已有合法配置时不应生成密钥文件")
	}
}

// TestEnsureJWTSecretRejectsPlaceholder 占位符/过短 → 不采用，改用持久化密钥
func TestEnsureJWTSecretRejectsPlaceholder(t *testing.T) {
	_ = withSecretFile(t)
	logger := zap.NewNop()

	for _, bad := range []string{
		"***REDACTED***",
		"changeme",
		"short-secret", // 不足 32
		strings.Repeat("x", 40),
	} {
		cfg := newCfg(bad)
		if err := ensureJWTSecret(cfg, logger); err != nil {
			t.Fatalf("处理 %q 失败: %v", bad, err)
		}
		if cfg.JWT.Secret == bad {
			t.Fatalf("占位/弱密钥 %q 不应被采用", bad)
		}
		if len(cfg.JWT.Secret) < jwtSecretMinLen {
			t.Fatalf("替换后的密钥过短: %q", cfg.JWT.Secret)
		}
	}
}

// TestLooksLikeJWTPlaceholder 占位符识别
func TestLooksLikeJWTPlaceholder(t *testing.T) {
	cases := map[string]bool{
		"***REDACTED***":       true,
		"CHANGEME":             true,
		"your-secret-here":     true,
		"placeholder-value":    true,
		"9f8a7c6b5d4e3f2a1b0c": false,
	}
	for in, want := range cases {
		if got := looksLikePlaceholderSecret(in); got != want {
			t.Errorf("looksLikePlaceholderSecret(%q) = %v, want %v", in, got, want)
		}
	}
}
