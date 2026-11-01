package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newAPIDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:apitest?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.APIKey{}, &models.SMSTemplate{}, &models.APISendLog{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	db.Exec("delete from api_keys")
	db.Exec("delete from sms_templates")
	db.Exec("delete from api_send_logs")
	return db
}

// ---------- API Key 服务 ----------

// TestCreateAPIKey Secret 可解密且与返回一致
func TestCreateAPIKey(t *testing.T) {
	db := newAPIDB(t)
	svc, err := NewAPIKeyService(zap.NewNop(), db, "test-jwt-secret")
	if err != nil {
		t.Fatalf("初始化失败: %v", err)
	}

	key, secret, err := svc.CreateAPIKey(context.Background(), "测试", "sms1", "", 10)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if !strings.HasPrefix(key.AccessKey, "ak_") {
		t.Fatalf("AK 前缀不对: %s", key.AccessKey)
	}
	if !strings.HasPrefix(secret, "sk_") {
		t.Fatalf("SK 前缀不对: %s", secret)
	}
	if key.SecretEnc == "" || strings.Contains(key.SecretEnc, secret) {
		t.Fatal("Secret 应加密存储，不能是明文")
	}
	// 能解密回原文
	plain, err := svc.decryptSecret(key.SecretEnc)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if plain != secret {
		t.Fatal("解密结果与创建时返回的 Secret 不一致")
	}
	// hint 只露末 4 位
	if !strings.HasSuffix(key.SecretHint, secret[len(secret)-4:]) {
		t.Fatalf("hint 不对: %s", key.SecretHint)
	}
}

// TestCreateAPIKeyRequiresDevice 必须绑定设备
func TestCreateAPIKeyRequiresDevice(t *testing.T) {
	db := newAPIDB(t)
	svc, _ := NewAPIKeyService(zap.NewNop(), db, "s")
	if _, _, err := svc.CreateAPIKey(context.Background(), "x", "", "", 0); err == nil {
		t.Fatal("未绑定设备应报错")
	}
}

// TestVerifyRequestSignature 正确签名通过、错误签名拒绝
func TestVerifyRequestSignature(t *testing.T) {
	db := newAPIDB(t)
	svc, _ := NewAPIKeyService(zap.NewNop(), db, "jwt")
	ctx := context.Background()

	key, secret, _ := svc.CreateAPIKey(ctx, "k", "sms1", "", 100)

	ts := time.Now().Unix()
	tsStr := time.Unix(ts, 0).Format("2006")
	_ = tsStr
	tsStr = itoa(ts)
	nonce := "nonce-abc"
	method, path, query, bodyHash := "POST", "/openapi/v1/sms/send", "", SHA256Hex([]byte(`{"a":1}`))

	goodSig := SignRequest(secret, method, path, query, bodyHash, tsStr, nonce)

	// 正确签名
	got, err := svc.VerifyRequest(ctx, key.AccessKey, goodSig, tsStr, nonce, method, path, query, bodyHash, "1.2.3.4")
	if err != nil {
		t.Fatalf("正确签名应通过: %v", err)
	}
	if got.ID != key.ID {
		t.Fatal("应返回对应的 Key")
	}

	// 同一 nonce 重放应被拒
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, goodSig, tsStr, nonce, method, path, query, bodyHash, "1.2.3.4"); err == nil {
		t.Fatal("nonce 重放应被拒")
	}

	// 错误签名（新 nonce，避免被 nonce 规则先拦）
	badSig := SignRequest("wrong-secret", method, path, query, bodyHash, tsStr, "nonce-2")
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, badSig, tsStr, "nonce-2", method, path, query, bodyHash, "1.2.3.4"); err == nil {
		t.Fatal("错误签名应被拒")
	}
}

// TestVerifyRequestExpiredTimestamp 超出时间窗口拒绝
func TestVerifyRequestExpiredTimestamp(t *testing.T) {
	db := newAPIDB(t)
	svc, _ := NewAPIKeyService(zap.NewNop(), db, "jwt")
	ctx := context.Background()
	key, secret, _ := svc.CreateAPIKey(ctx, "k", "sms1", "", 100)

	old := itoa(time.Now().Add(-10 * time.Minute).Unix())
	nonce := "n-old"
	sig := SignRequest(secret, "POST", "/p", "", SHA256Hex(nil), old, nonce)

	if _, err := svc.VerifyRequest(ctx, key.AccessKey, sig, old, nonce, "POST", "/p", "", SHA256Hex(nil), "1.2.3.4"); err == nil {
		t.Fatal("过期时间戳应被拒")
	} else if !strings.Contains(err.Error(), "过期") {
		t.Fatalf("错误信息应提示过期: %v", err)
	}
}

// TestVerifyRequestDisabledKey 停用的 Key 被拒
func TestVerifyRequestDisabledKey(t *testing.T) {
	db := newAPIDB(t)
	svc, _ := NewAPIKeyService(zap.NewNop(), db, "jwt")
	ctx := context.Background()
	key, secret, _ := svc.CreateAPIKey(ctx, "k", "sms1", "", 100)

	if err := svc.UpdateAPIKey(ctx, key.ID, "k", "sms1", "", 100, false); err != nil {
		t.Fatalf("停用失败: %v", err)
	}

	ts := itoa(time.Now().Unix())
	nonce := "n1"
	sig := SignRequest(secret, "POST", "/p", "", SHA256Hex(nil), ts, nonce)
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, sig, ts, nonce, "POST", "/p", "", SHA256Hex(nil), "1.2.3.4"); err == nil {
		t.Fatal("停用的 Key 应被拒")
	}
}

// TestVerifyRequestIPWhitelist IP 不在白名单被拒
func TestVerifyRequestIPWhitelist(t *testing.T) {
	db := newAPIDB(t)
	svc, _ := NewAPIKeyService(zap.NewNop(), db, "jwt")
	ctx := context.Background()
	key, secret, _ := svc.CreateAPIKey(ctx, "k", "sms1", "192.168.0.0/16", 100)

	ts := itoa(time.Now().Unix())
	sig := SignRequest(secret, "POST", "/p", "", SHA256Hex(nil), ts, "n1")

	// 白名单外
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, sig, ts, "n1", "POST", "/p", "", SHA256Hex(nil), "8.8.8.8"); err == nil {
		t.Fatal("白名单外 IP 应被拒")
	}
	// 白名单内（新 nonce）
	sig2 := SignRequest(secret, "POST", "/p", "", SHA256Hex(nil), ts, "n2")
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, sig2, ts, "n2", "POST", "/p", "", SHA256Hex(nil), "192.168.1.5"); err != nil {
		t.Fatalf("白名单内应通过: %v", err)
	}
}

// TestResetSecret 重置后旧 Secret 失效
func TestResetSecret(t *testing.T) {
	db := newAPIDB(t)
	svc, _ := NewAPIKeyService(zap.NewNop(), db, "jwt")
	ctx := context.Background()
	key, oldSecret, _ := svc.CreateAPIKey(ctx, "k", "sms1", "", 100)

	newSecret, err := svc.ResetSecret(ctx, key.ID)
	if err != nil {
		t.Fatalf("重置失败: %v", err)
	}
	if newSecret == oldSecret {
		t.Fatal("新 Secret 应与旧的不同")
	}

	ts := itoa(time.Now().Unix())
	nonce := "n1"

	// 旧 Secret 签名应失败
	oldSig := SignRequest(oldSecret, "POST", "/p", "", SHA256Hex(nil), ts, nonce)
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, oldSig, ts, nonce, "POST", "/p", "", SHA256Hex(nil), "1.2.3.4"); err == nil {
		t.Fatal("旧 Secret 应失效")
	}
	// 新 Secret 应通过
	newSig := SignRequest(newSecret, "POST", "/p", "", SHA256Hex(nil), ts, "n2")
	if _, err := svc.VerifyRequest(ctx, key.AccessKey, newSig, ts, "n2", "POST", "/p", "", SHA256Hex(nil), "1.2.3.4"); err != nil {
		t.Fatalf("新 Secret 应通过: %v", err)
	}
}

// ---------- 模板服务 ----------

// TestTemplateCRUD 增删改查 + 编码唯一
func TestTemplateCRUD(t *testing.T) {
	db := newAPIDB(t)
	svc := NewTemplateService(zap.NewNop(), db)
	ctx := context.Background()

	tpl, err := svc.Create(ctx, "verify_code", "验证码", "您的验证码是{code}", "登录用", true)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	// 编码重复
	if _, err := svc.Create(ctx, "verify_code", "x", "y", "", true); err == nil {
		t.Fatal("重复编码应报错")
	}

	// 按编码取
	got, err := svc.GetByCode(ctx, "verify_code")
	if err != nil || got.ID != tpl.ID {
		t.Fatalf("按编码查询失败: %v", err)
	}

	// 更新
	if err := svc.Update(ctx, tpl.ID, "verify_code", "验证码2", "新内容{code}", "", true); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	// 停用后取不到
	if err := svc.Update(ctx, tpl.ID, "verify_code", "验证码2", "新内容{code}", "", false); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if _, err := svc.GetByCode(ctx, "verify_code"); err == nil {
		t.Fatal("停用模板不应可取到")
	}

	// 删除
	if err := svc.Delete(ctx, tpl.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if err := svc.Delete(ctx, tpl.ID); err == nil {
		t.Fatal("重复删除应报错")
	}
}

// TestTemplateCodeValidation 编码格式校验
func TestTemplateCodeValidation(t *testing.T) {
	db := newAPIDB(t)
	svc := NewTemplateService(zap.NewNop(), db)
	ctx := context.Background()

	bad := []string{"", "a", "中文", "a b", "a/b", strings.Repeat("x", 65)}
	for _, code := range bad {
		if _, err := svc.Create(ctx, code, "n", "c", "", true); err == nil {
			t.Fatalf("非法编码 %q 应被拒", code)
		}
	}
	good := []string{"ab", "a_b-c", "VerifyCode1", strings.Repeat("x", 64)}
	for _, code := range good {
		if _, err := svc.Create(ctx, code, "n", "c", "", true); err != nil {
			t.Fatalf("合法编码 %q 应通过: %v", code, err)
		}
	}
}

// TestTemplateTooLong 内容超长拒绝
func TestTemplateTooLong(t *testing.T) {
	db := newAPIDB(t)
	svc := NewTemplateService(zap.NewNop(), db)
	long := strings.Repeat("字", 501)
	if _, err := svc.Create(context.Background(), "t1", "n", long, "", true); err == nil {
		t.Fatal("超 500 字应被拒")
	}
}

// ---------- 限流 ----------

// TestKeyRateLimit 每分钟上限
func TestKeyRateLimit(t *testing.T) {
	db := newAPIDB(t)
	tpl := NewTemplateService(zap.NewNop(), db)
	dm := newTestDM()
	svc := NewOpenAPIService(zap.NewNop(), db, dm, tpl)

	key := &models.APIKey{ID: "k1", AccessKey: "ak_1", DeviceID: "sms1", RateLimitPerMinute: 3}

	for i := 0; i < 3; i++ {
		if err := svc.checkKeyLimit(key); err != nil {
			t.Fatalf("第 %d 次应通过: %v", i+1, err)
		}
	}
	if err := svc.checkKeyLimit(key); err == nil {
		t.Fatal("超过每分钟上限应被拒")
	} else if !strings.Contains(err.Error(), "每分钟最多") {
		t.Fatalf("错误信息应说明限流: %v", err)
	}
}

// TestPhoneDailyLimit 手机号 24 小时上限
func TestPhoneDailyLimit(t *testing.T) {
	db := newAPIDB(t)
	svc := NewOpenAPIService(zap.NewNop(), db, newTestDM(), NewTemplateService(zap.NewNop(), db))
	ctx := context.Background()

	phone := "13800138000"
	// 造 10 条最近的 accepted 记录
	now := time.Now().UnixMilli()
	for i := 0; i < PhoneDailyLimit; i++ {
		db.Create(&models.APISendLog{
			ID: itoa(int64(i)), Phone: phone, Status: "accepted",
			// 错开到 61 秒以外，避免触发「过于频繁」
			CreatedAt: now - int64(i+2)*61*1000,
		})
	}

	err := svc.checkPhoneLimit(ctx, phone)
	if err == nil {
		t.Fatal("达到日上限应被拒")
	}
	if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("错误信息应说明上限: %v", err)
	}
}

// TestPhoneMinInterval 同号码 60 秒内拒绝
func TestPhoneMinInterval(t *testing.T) {
	db := newAPIDB(t)
	svc := NewOpenAPIService(zap.NewNop(), db, newTestDM(), NewTemplateService(zap.NewNop(), db))
	ctx := context.Background()

	phone := "13900139000"
	db.Create(&models.APISendLog{
		ID: "x", Phone: phone, Status: "accepted", CreatedAt: time.Now().UnixMilli(),
	})

	err := svc.checkPhoneLimit(ctx, phone)
	if err == nil {
		t.Fatal("60 秒内应被拒")
	}
	if !strings.Contains(err.Error(), "频繁") {
		t.Fatalf("错误信息应说明频繁: %v", err)
	}
}

// TestPhoneLimitDifferentPhonesIndependent 不同号码互不影响
func TestPhoneLimitDifferentPhonesIndependent(t *testing.T) {
	db := newAPIDB(t)
	svc := NewOpenAPIService(zap.NewNop(), db, newTestDM(), NewTemplateService(zap.NewNop(), db))
	ctx := context.Background()

	db.Create(&models.APISendLog{
		ID: "x", Phone: "13800138000", Status: "accepted", CreatedAt: time.Now().UnixMilli(),
	})
	// 另一个号码应不受影响
	if err := svc.checkPhoneLimit(ctx, "13900139000"); err != nil {
		t.Fatalf("不同号码应互不影响: %v", err)
	}
}

// TestRejectedLogsNotCounted 被拒绝的记录不计入限流
func TestRejectedLogsNotCounted(t *testing.T) {
	db := newAPIDB(t)
	svc := NewOpenAPIService(zap.NewNop(), db, newTestDM(), NewTemplateService(zap.NewNop(), db))
	ctx := context.Background()

	phone := "13700137000"
	now := time.Now().UnixMilli()
	for i := 0; i < 20; i++ {
		db.Create(&models.APISendLog{
			ID: "r" + itoa(int64(i)), Phone: phone, Status: "rejected",
			CreatedAt: now - int64(i+2)*61*1000,
		})
	}
	// 全是 rejected，不该触发限流
	if err := svc.checkPhoneLimit(ctx, phone); err != nil {
		t.Fatalf("rejected 记录不应计入限流: %v", err)
	}
}

// itoa 避免引入 strconv 只为转字符串
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
