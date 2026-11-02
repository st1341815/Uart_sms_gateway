package internal

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.uber.org/zap"
)

const (
	// JWT Secret 最短长度（HS256 建议 >= 32 字节熵）
	jwtSecretMinLen = 32
	// 密钥文件的默认位置（容器里 ./data 是持久卷）
	defaultJWTSecretFile = "./data/.jwt_secret"
	// 环境变量可覆盖密钥文件位置
	jwtSecretFileEnv = "JWT_SECRET_FILE"
)

// 这些子串出现时，视为「示例占位值」，不予采用
var jwtSecretPlaceholders = []string{
	"redacted", "changeme", "change-me", "example", "placeholder",
	"your-secret", "your_secret", "secret-here", "xxx", "todo",
}

func looksLikePlaceholderSecret(s string) bool {
	lower := strings.ToLower(s)
	for _, p := range jwtSecretPlaceholders {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// ensureJWTSecret 保证 JWT.Secret 是一个稳定可用的强随机值。
//
// 为什么不能像以前那样「为空就生成 uuid.NewString()」：
//   - JWT Secret 同时是 AK/SK 密文的派生源（见 service.NewAPIKeyService），
//     每次重启换一个值，会让库里所有 SecretEnc 永久解不开，
//     已发给调用方的 SecretKey 立刻全部失效，且无法恢复。
//   - 所以空/占位/过短时，改为生成一次并落盘，之后复用同一份。
//
// 判定顺序：
//  1. config.yaml 里已给了合法的 Secret（>=32 且非占位）→ 直接采用，不落盘；
//  2. 否则读 JWT_SECRET_FILE（默认 ./data/.jwt_secret）；
//  3. 仍没有 → 生成 32 字节随机值写入该文件（0600），下次启动复用。
func ensureJWTSecret(cfg *config.AppConfig, logger *zap.Logger) error {
	cur := strings.TrimSpace(cfg.JWT.Secret)
	if len(cur) >= jwtSecretMinLen && !looksLikePlaceholderSecret(cur) {
		return nil
	}
	if cur != "" {
		logger.Warn("配置中的 JWT Secret 过短或仍是示例值，不予采用，将改用持久化密钥文件",
			zap.Int("length", len(cur)),
			zap.String("hint", "openssl rand -base64 32"))
	}

	path := strings.TrimSpace(os.Getenv(jwtSecretFileEnv))
	if path == "" {
		path = defaultJWTSecretFile
	}

	// 2. 复用已有密钥文件
	if b, err := os.ReadFile(path); err == nil {
		if s := strings.TrimSpace(string(b)); len(s) >= jwtSecretMinLen && !looksLikePlaceholderSecret(s) {
			cfg.JWT.Secret = s
			logger.Info("已从密钥文件加载 JWT Secret", zap.String("path", path))
			return nil
		}
		logger.Warn("JWT 密钥文件内容过短，将重新生成", zap.String("path", path))
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("读取 JWT 密钥文件失败 %s: %w", path, err)
	}

	// 3. 生成并落盘
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("生成 JWT Secret 失败: %w", err)
	}
	secret := base64.StdEncoding.EncodeToString(buf) // 44 字符

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("创建 JWT 密钥目录失败 %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return fmt.Errorf("写入 JWT 密钥文件失败 %s: %w", path, err)
	}

	cfg.JWT.Secret = secret
	logger.Warn("未配置可用的 JWT Secret，已自动生成并持久化（请随 data 卷一并备份；丢失会导致 AK/SK 全部失效）",
		zap.String("path", path))
	return nil
}
