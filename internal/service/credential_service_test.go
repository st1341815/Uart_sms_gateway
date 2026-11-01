package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newCredDB 建一个带 user_credentials 表的内存库
func newCredDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:credtest?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.UserCredential{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	// 清理，避免同一测试进程内多次使用共享内存库互相污染
	db.Exec("delete from user_credentials")
	return db
}

func mkHash(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成哈希失败: %v", err)
	}
	return string(h)
}

// TestCredentialSeedFromConfig config 里的 Users 会作为种子导入
func TestCredentialSeedFromConfig(t *testing.T) {
	db := newCredDB(t)
	seed := map[string]string{"admin": mkHash(t, "OldPass123")}

	svc, err := NewCredentialService(zap.NewNop(), db, seed)
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	if !svc.HasUser("admin") {
		t.Fatal("admin 应已导入")
	}
	if svc.UserCount() != 1 {
		t.Fatalf("用户数 = %d，期望 1", svc.UserCount())
	}
	// 数据库里确实有记录
	var n int64
	db.Model(&models.UserCredential{}).Count(&n)
	if n != 1 {
		t.Fatalf("库中记录 = %d，期望 1", n)
	}
}

// TestCredentialVerify 旧口令可登录、错口令被拒
func TestCredentialVerify(t *testing.T) {
	db := newCredDB(t)
	svc, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})

	if err := svc.Verify("admin", "OldPass123"); err != nil {
		t.Fatalf("正确口令应通过: %v", err)
	}
	if err := svc.Verify("admin", "wrong"); err == nil {
		t.Fatal("错误口令应被拒")
	}
	if err := svc.Verify("nobody", "OldPass123"); err == nil {
		t.Fatal("不存在的用户应被拒")
	}
}

// TestCredentialChangePassword 改密码后立即生效并落库
func TestCredentialChangePassword(t *testing.T) {
	db := newCredDB(t)
	svc, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})
	ctx := context.Background()

	if err := svc.ChangePassword(ctx, "admin", "OldPass123", "NewPass456"); err != nil {
		t.Fatalf("改密码失败: %v", err)
	}

	// 旧口令失效
	if err := svc.Verify("admin", "OldPass123"); err == nil {
		t.Fatal("旧口令应已失效")
	}
	// 新口令生效
	if err := svc.Verify("admin", "NewPass456"); err != nil {
		t.Fatalf("新口令应可用: %v", err)
	}

	// 库里存的是新哈希
	var row models.UserCredential
	db.First(&row, "username = ?", "admin")
	if err := bcrypt.CompareHashAndPassword([]byte(row.PasswordHash), []byte("NewPass456")); err != nil {
		t.Fatal("数据库中应保存新口令的哈希")
	}
}

// TestCredentialChangePasswordSurvivesRestart 模拟重启：新建服务实例仍用新口令
func TestCredentialChangePasswordSurvivesRestart(t *testing.T) {
	db := newCredDB(t)
	ctx := context.Background()

	svc1, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})
	if err := svc1.ChangePassword(ctx, "admin", "OldPass123", "NewPass456"); err != nil {
		t.Fatalf("改密码失败: %v", err)
	}

	// 重启：用**旧的 config.yaml 种子**再初始化一次（模拟容器重启）
	svc2, err := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})
	if err != nil {
		t.Fatalf("重启初始化失败: %v", err)
	}

	if err := svc2.Verify("admin", "NewPass456"); err != nil {
		t.Fatalf("重启后新口令应仍有效: %v", err)
	}
	if err := svc2.Verify("admin", "OldPass123"); err == nil {
		t.Fatal("重启后旧口令不应复活")
	}
}

// TestChangePasswordRequiresCorrectOld 旧口令错误时拒绝修改
func TestChangePasswordRequiresCorrectOld(t *testing.T) {
	db := newCredDB(t)
	svc, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})

	err := svc.ChangePassword(context.Background(), "admin", "WrongOld", "NewPass456")
	if err == nil {
		t.Fatal("旧口令错误应拒绝修改")
	}
	if !strings.Contains(err.Error(), "当前密码错误") {
		t.Fatalf("错误信息应指出旧密码错误，实际: %v", err)
	}
	// 原口令仍有效
	if err := svc.Verify("admin", "OldPass123"); err != nil {
		t.Fatal("修改失败后原口令应仍有效")
	}
}

// TestChangePasswordStrength 口令强度校验
func TestChangePasswordStrength(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		ok   bool
		msg  string
	}{
		{"太短", "Ab1", false, "至少 8 位"},
		{"纯数字", "12345678", false, "字母和数字"},
		{"纯字母", "abcdefgh", false, "字母和数字"},
		{"超长", strings.Repeat("A1", 40), false, "72"},
		{"合法", "Abcd1234", true, ""},
		{"合法带符号", "Abcd!234", true, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validatePasswordStrength(c.pw)
			if c.ok && err != nil {
				t.Fatalf("应通过，实际报错: %v", err)
			}
			if !c.ok {
				if err == nil {
					t.Fatal("应被拒绝")
				}
				if !strings.Contains(err.Error(), c.msg) {
					t.Fatalf("错误信息应含 %q，实际: %v", c.msg, err)
				}
			}
		})
	}
}

// TestChangePasswordSameAsOld 新旧相同应拒绝
func TestChangePasswordSameAsOld(t *testing.T) {
	db := newCredDB(t)
	svc, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})

	err := svc.ChangePassword(context.Background(), "admin", "OldPass123", "OldPass123")
	if err == nil || !strings.Contains(err.Error(), "不能与当前密码相同") {
		t.Fatalf("新旧相同应拒绝，实际: %v", err)
	}
}

// TestSeedSkipsExisting 库里已存在的用户不会被 config 覆盖
func TestSeedSkipsExisting(t *testing.T) {
	db := newCredDB(t)
	ctx := context.Background()

	// 先建一个实例并把密码改成新的
	svc1, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})
	if err := svc1.ChangePassword(ctx, "admin", "OldPass123", "NewPass456"); err != nil {
		t.Fatalf("改密码失败: %v", err)
	}

	// 再用不同种子的 config 启动：不能覆盖已有记录
	svc2, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "AnotherPw789")})
	if err := svc2.Verify("admin", "NewPass456"); err != nil {
		t.Fatal("已有记录不应被配置种子覆盖")
	}
}

// TestSeedInvalidHashSkipped 配置里的非法哈希被跳过而不是写进库
func TestSeedInvalidHashSkipped(t *testing.T) {
	db := newCredDB(t)
	svc, err := NewCredentialService(zap.NewNop(), db, map[string]string{
		"admin": mkHash(t, "OldPass123"),
		"bad":   "not-a-bcrypt-hash",
	})
	if err != nil {
		t.Fatalf("初始化不应因非法哈希失败: %v", err)
	}
	if svc.HasUser("bad") {
		t.Fatal("非法哈希的用户不应被导入")
	}
	if !svc.HasUser("admin") {
		t.Fatal("合法用户应被导入")
	}
}

// TestChangePasswordUpdatesTimestamp 更新时间戳
func TestChangePasswordUpdatesTimestamp(t *testing.T) {
	db := newCredDB(t)
	svc, _ := NewCredentialService(zap.NewNop(), db, map[string]string{"admin": mkHash(t, "OldPass123")})

	var before models.UserCredential
	db.First(&before, "username = ?", "admin")

	time.Sleep(5 * time.Millisecond)
	if err := svc.ChangePassword(context.Background(), "admin", "OldPass123", "NewPass456"); err != nil {
		t.Fatalf("改密码失败: %v", err)
	}

	var after models.UserCredential
	db.First(&after, "username = ?", "admin")
	if after.UpdatedAt <= before.UpdatedAt {
		t.Fatalf("updated_at 应更新: before=%d after=%d", before.UpdatedAt, after.UpdatedAt)
	}
	if after.CreatedAt != before.CreatedAt {
		t.Fatal("created_at 不应变化")
	}
}
