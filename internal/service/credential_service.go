package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// dummyHash 用于「用户不存在」分支的等时比较。
//
// 不硬编码任何真实账号的哈希（那会泄露口令指纹进仓库），
// 而是在进程内按同样 cost 现算一个随机口令的哈希，缓存后复用。
// 这样既能保持 bcrypt 比较的真实耗时，又不含任何敏感信息。
var (
	dummyHashOnce sync.Once
	dummyHashVal  []byte
	dummyHashCost = 12
)

func dummyHash() []byte {
	dummyHashOnce.Do(func() {
		// 随机口令本身不参与任何校验，仅用于生成等 cost 的哈希
		seed, err := bcrypt.GenerateFromPassword(
			[]byte(fmt.Sprintf("dummy-%d-%d", time.Now().UnixNano(), os.Getpid())),
			dummyHashCost,
		)
		if err != nil {
			// 极端情况下回退：用零值让比较必然失败，但不会 panic
			dummyHashVal = []byte("$2a$12$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalidinv")
			return
		}
		dummyHashVal = seed
	})
	return dummyHashVal
}

type CredentialService struct {
	logger *zap.Logger
	db     *gorm.DB

	mu sync.RWMutex
	// 内存缓存：username -> bcrypt hash
	hashes map[string]string
}

// NewCredentialService 创建口令服务，并用 config 里的 Users 做种子
func NewCredentialService(logger *zap.Logger, db *gorm.DB, seedUsers map[string]string) (*CredentialService, error) {
	s := &CredentialService{
		logger: logger,
		db:     db,
		hashes: make(map[string]string),
	}

	// 1. 载入库中已有口令
	var rows []models.UserCredential
	if err := db.WithContext(context.Background()).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取口令失败: %w", err)
	}
	for _, r := range rows {
		s.hashes[r.Username] = r.PasswordHash
	}

	// 2. 用 config.yaml 的 Users 做种子（仅当库中不存在该用户）
	seeded := 0
	for username, hash := range seedUsers {
		if username == "" || hash == "" {
			continue
		}
		if _, exists := s.hashes[username]; exists {
			continue
		}
		// 校验一下种子哈希是否合法，非法就跳过（避免把坏数据写进库）
		if _, err := bcrypt.Cost([]byte(hash)); err != nil {
			logger.Warn("配置中的口令哈希格式非法，已跳过",
				zap.String("username", username), zap.Error(err))
			continue
		}
		c := &models.UserCredential{
			Username:     username,
			PasswordHash: hash,
			CreatedAt:    time.Now().UnixMilli(),
			UpdatedAt:    time.Now().UnixMilli(),
		}
		if err := db.WithContext(context.Background()).Create(c).Error; err != nil {
			return nil, fmt.Errorf("初始化口令失败: %w", err)
		}
		s.hashes[username] = hash
		seeded++
	}

	if seeded > 0 {
		logger.Info("已从配置文件导入口令到数据库",
			zap.Int("count", seeded),
			zap.Int("total_users", len(s.hashes)))
	}

	if len(s.hashes) == 0 {
		logger.Warn("当前没有任何可用口令，将无法登录；请在 config.yaml 的 Users 中配置")
	}

	return s, nil
}

// HasUser 是否已有该用户
func (s *CredentialService) HasUser(username string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.hashes[username]
	return ok
}

// UserCount 用户数量
func (s *CredentialService) UserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.hashes)
}

// Verify 校验口令
func (s *CredentialService) Verify(username, password string) error {
	s.mu.RLock()
	hash, ok := s.hashes[username]
	s.mu.RUnlock()

	if !ok {
		// 用户不存在时也做一次 bcrypt 比较，避免通过响应时间探测用户是否存在。
		// 这里必须用一个「真实但无关」的哈希：先按 cost=12 现算一次再比对，
		// 保证耗时与正常分支一致；绝不能硬编码任何真实账号的哈希。
		_ = bcrypt.CompareHashAndPassword(
			dummyHash(),
			[]byte(password))
		return fmt.Errorf("用户名或密码错误")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return fmt.Errorf("用户名或密码错误")
	}
	return nil
}

// ChangePassword 修改口令。
// 会先校验旧口令，再生成新哈希写入数据库并同步更新内存，
// 因此修改后立即生效且重启后依然有效。
func (s *CredentialService) ChangePassword(ctx context.Context, username, oldPassword, newPassword string) error {
	// 1. 校验旧口令
	if err := s.Verify(username, oldPassword); err != nil {
		return fmt.Errorf("当前密码错误")
	}

	// 2. 校验新口令强度
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}

	// 3. 新旧不能相同
	if oldPassword == newPassword {
		return fmt.Errorf("新密码不能与当前密码相同")
	}

	// 4. 落库 + 刷内存
	if err := s.persistPassword(ctx, username, newPassword); err != nil {
		return err
	}

	s.logger.Info("管理员口令已修改", zap.String("username", username))
	return nil
}

// ResetPassword 直接设置新口令，不校验旧口令。
//
// 仅供「已通过其他途径确认过身份」的路径使用，目前唯一调用方是
// PasswordResetService（短信重置：要求持有已登录会话 + 手机能收到短信）。
// 普通改密必须走 ChangePassword —— 那条路会校验旧口令。
func (s *CredentialService) ResetPassword(ctx context.Context, username, newPassword string) error {
	if !s.HasUser(username) {
		return fmt.Errorf("用户不存在")
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	return s.persistPassword(ctx, username, newPassword)
}

// persistPassword 生成哈希、写库、更新内存缓存。
// ChangePassword 与 ResetPassword 共用，避免两处落库逻辑各写一遍后走样。
func (s *CredentialService) persistPassword(ctx context.Context, username, newPassword string) error {
	// 1. 生成新哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("生成密码哈希失败: %w", err)
	}

	// 2. 写库（存在则更新）
	now := time.Now().UnixMilli()
	row := &models.UserCredential{
		Username:     username,
		PasswordHash: string(hash),
		UpdatedAt:    now,
	}
	if err := s.db.WithContext(ctx).
		Where("username = ?", username).
		Assign(map[string]interface{}{
			"password_hash": string(hash),
			"updated_at":    now,
		}).
		FirstOrCreate(row).Error; err != nil {
		return fmt.Errorf("保存新密码失败: %w", err)
	}

	// 3. 更新内存缓存
	s.mu.Lock()
	s.hashes[username] = string(hash)
	s.mu.Unlock()

	return nil
}

// bcryptCost 与既有哈希保持一致（$2y$12$ / $2a$12$ 都是 cost 12）
const bcryptCost = 12

// validatePasswordStrength 口令强度校验
func validatePasswordStrength(pw string) error {
	if len(pw) < 8 {
		return fmt.Errorf("新密码长度至少 8 位")
	}
	if len(pw) > 72 {
		// bcrypt 只取前 72 字节，超出部分会被静默丢弃
		return fmt.Errorf("新密码不能超过 72 位")
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("新密码需同时包含字母和数字")
	}
	return nil
}
