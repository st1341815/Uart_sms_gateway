package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
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

const testOldPassword = "OldPass123"

// fakeSMS 假的短信发送器：只记录调用，不碰真机。
type fakeSMS struct {
	sent     []fakeSent
	sendErr  error
	statuses map[string]*DeviceStatus
}

type fakeSent struct {
	DeviceID string
	To       string
	Content  string
}

func (f *fakeSMS) SendSMS(deviceID, to, content string) (string, error) {
	if f.sendErr != nil {
		return "", f.sendErr
	}
	f.sent = append(f.sent, fakeSent{DeviceID: deviceID, To: to, Content: content})
	return "msg-fake-1", nil
}

func (f *fakeSMS) GetDeviceStatus(id string) (*DeviceStatus, error) {
	if st, ok := f.statuses[id]; ok {
		return st, nil
	}
	return nil, fmt.Errorf("设备不存在: %s", id)
}

func (f *fakeSMS) GetAllDeviceIDs() []string {
	ids := make([]string, 0, len(f.statuses))
	for id := range f.statuses {
		ids = append(ids, id)
	}
	return ids
}

// newResetEnv 搭一套隔离的测试环境（独立内存库 + 假短信通道）。
func newResetEnv(t *testing.T) (*PasswordResetService, *CredentialService, *fakeSMS) {
	t.Helper()

	// 每个用例一个独立的内存库，避免 cache=shared 互相串数据
	dbName := fmt.Sprintf("file:reset_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.Property{}, &models.UserCredential{}); err != nil {
		t.Fatalf("建表失败: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(testOldPassword), bcryptCost)
	if err != nil {
		t.Fatalf("生成初始哈希失败: %v", err)
	}

	log := zap.NewNop()
	credentials, err := NewCredentialService(log, db, map[string]string{"admin": string(hash)})
	if err != nil {
		t.Fatalf("初始化口令服务失败: %v", err)
	}
	propertyService := NewPropertyService(log, db)

	sms := &fakeSMS{
		statuses: map[string]*DeviceStatus{
			"sms1": {ID: "sms1", Name: "联通151", Connected: true},
			"sms2": {ID: "sms2", Name: "英国漫游卡", Connected: false},
		},
	}

	return NewPasswordResetService(log, credentials, sms, propertyService), credentials, sms
}

// newPasswordFromMessage 从下发的短信正文里取出新口令。
// 测试要验证「用户真能拿到那个密码」，所以按用户视角解析，而不是读内部变量。
var passwordInMessage = regexp.MustCompile(`新密码：(\S+)`)

func newPasswordFromMessage(t *testing.T, content string) string {
	t.Helper()
	m := passwordInMessage.FindStringSubmatch(content)
	if len(m) < 2 {
		t.Fatalf("短信正文里没找到新密码: %q", content)
	}
	return m[1]
}

// TestResetBySMSSuccess 正常路径：短信发出 → 旧密码失效、短信里的新密码可登录。
func TestResetBySMSSuccess(t *testing.T) {
	svc, cred, sms := newResetEnv(t)
	ctx := context.Background()

	result, err := svc.ResetBySMS(ctx, "admin", "sms1", "+8613800138000")
	if err != nil {
		t.Fatalf("重置失败: %v", err)
	}

	// 号码应被归一化（+86 去掉）
	if len(sms.sent) != 1 {
		t.Fatalf("应发出 1 条短信，实际 %d", len(sms.sent))
	}
	if got := sms.sent[0].To; got != "13800138000" {
		t.Fatalf("接收号码 = %q，期望归一化为 13800138000", got)
	}
	if sms.sent[0].DeviceID != "sms1" {
		t.Fatalf("发送设备 = %q", sms.sent[0].DeviceID)
	}

	newPassword := newPasswordFromMessage(t, sms.sent[0].Content)
	if newPassword == testOldPassword {
		t.Fatal("新密码不能等于旧密码")
	}

	// 旧密码必须失效
	if err := cred.Verify("admin", testOldPassword); err == nil {
		t.Fatal("旧密码仍然可用，重置没生效")
	}
	// 短信里的新密码必须可用
	if err := cred.Verify("admin", newPassword); err != nil {
		t.Fatalf("短信下发的新密码不可用: %v", err)
	}

	if result.MaskedPhone != "138****8000" {
		t.Fatalf("脱敏号码 = %q", result.MaskedPhone)
	}
	if result.DeviceName != "联通151" {
		t.Fatalf("设备名 = %q", result.DeviceName)
	}
}

// TestResetBySMSKeepsPasswordWhenSendFails 核心安全承诺：
// 短信发不出去时，口令必须保持原样，用户不能被锁死。
func TestResetBySMSKeepsPasswordWhenSendFails(t *testing.T) {
	svc, cred, sms := newResetEnv(t)
	ctx := context.Background()
	sms.sendErr = errors.New("串口写超时")

	_, err := svc.ResetBySMS(ctx, "admin", "sms1", "13800138000")
	if err == nil {
		t.Fatal("发送失败时不应返回成功")
	}
	if !strings.Contains(err.Error(), "密码未修改") {
		t.Fatalf("错误信息应明确告知密码未修改，实际: %v", err)
	}

	// 旧密码必须仍然有效
	if err := cred.Verify("admin", testOldPassword); err != nil {
		t.Fatalf("发送失败却把旧密码弄坏了: %v", err)
	}
}

// TestResetBySMSRejectsOfflineDevice 离线设备要挡在发短信之前。
func TestResetBySMSRejectsOfflineDevice(t *testing.T) {
	svc, cred, sms := newResetEnv(t)

	_, err := svc.ResetBySMS(context.Background(), "admin", "sms2", "13800138000")
	if err == nil {
		t.Fatal("离线设备不应通过")
	}
	if !strings.Contains(err.Error(), "离线") {
		t.Fatalf("错误信息应说明设备离线，实际: %v", err)
	}
	if len(sms.sent) != 0 {
		t.Fatal("离线设备不应真的发出短信")
	}
	if err := cred.Verify("admin", testOldPassword); err != nil {
		t.Fatalf("口令不应被改动: %v", err)
	}
}

// TestResetBySMSRejectsUnknownDevice 不存在的设备要报错，且不能兜底到别的卡。
func TestResetBySMSRejectsUnknownDevice(t *testing.T) {
	svc, _, _ := newResetEnv(t)

	_, err := svc.ResetBySMS(context.Background(), "admin", "sms9", "13800138000")
	if err == nil {
		t.Fatal("未知设备不应通过")
	}
	if !strings.Contains(err.Error(), "设备不存在") {
		t.Fatalf("错误信息应说明设备不存在，实际: %v", err)
	}
}

// TestResetBySMSRequiresDevice 不指定设备时必须拒绝——绝不自动挑一张卡。
func TestResetBySMSRequiresDevice(t *testing.T) {
	svc, _, sms := newResetEnv(t)

	_, err := svc.ResetBySMS(context.Background(), "admin", "", "13800138000")
	if err == nil {
		t.Fatal("未指定设备不应通过")
	}
	if len(sms.sent) != 0 {
		t.Fatal("未指定设备时不应发出短信")
	}
}

// TestResetBySMSRejectsInvalidPhone 号码格式非法时不发短信、不改密码。
func TestResetBySMSRejectsInvalidPhone(t *testing.T) {
	svc, cred, sms := newResetEnv(t)

	for _, bad := range []string{"", "  ", "abc", "123", "1380013800012345678"} {
		_, err := svc.ResetBySMS(context.Background(), "admin", "sms1", bad)
		if err == nil {
			t.Fatalf("非法号码 %q 不应通过", bad)
		}
	}
	if len(sms.sent) != 0 {
		t.Fatal("非法号码时不应发出短信")
	}
	if err := cred.Verify("admin", testOldPassword); err != nil {
		t.Fatalf("口令不应被改动: %v", err)
	}
}

// TestResetBySMSCooldown 冷却期内重复请求要被拦住，省话费。
func TestResetBySMSCooldown(t *testing.T) {
	svc, _, sms := newResetEnv(t)
	ctx := context.Background()

	if _, err := svc.ResetBySMS(ctx, "admin", "sms1", "13800138000"); err != nil {
		t.Fatalf("首次重置应成功: %v", err)
	}
	_, err := svc.ResetBySMS(ctx, "admin", "sms1", "13800138000")
	if err == nil {
		t.Fatal("冷却期内应被拦截")
	}
	if !strings.Contains(err.Error(), "频繁") {
		t.Fatalf("错误信息应说明过于频繁，实际: %v", err)
	}
	if len(sms.sent) != 1 {
		t.Fatalf("冷却期内不应重复发短信，实际发出 %d 条", len(sms.sent))
	}
}

// TestSetAndGetPhone 安全手机号要能落库并读回；未设置时返回空串而不是报错。
func TestSetAndGetPhone(t *testing.T) {
	svc, _, _ := newResetEnv(t)
	ctx := context.Background()

	// 初始未设置
	got, err := svc.GetPhone(ctx)
	if err != nil {
		t.Fatalf("未设置时不应报错: %v", err)
	}
	if got != "" {
		t.Fatalf("未设置时应返回空串，实际 %q", got)
	}

	if _, err := svc.SetPhone(ctx, "138 0013 8000"); err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	got, err = svc.GetPhone(ctx)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got != "13800138000" {
		t.Fatalf("读回号码 = %q，期望归一化后的 13800138000", got)
	}
}

// TestResetBySMSUsesSavedPhone 未显式传号码时，使用已保存的安全手机号。
func TestResetBySMSUsesSavedPhone(t *testing.T) {
	svc, _, sms := newResetEnv(t)
	ctx := context.Background()

	if _, err := svc.SetPhone(ctx, "13900139000"); err != nil {
		t.Fatalf("保存号码失败: %v", err)
	}
	if _, err := svc.ResetBySMS(ctx, "admin", "sms1", ""); err != nil {
		t.Fatalf("重置失败: %v", err)
	}
	if len(sms.sent) != 1 || sms.sent[0].To != "13900139000" {
		t.Fatalf("应发往已保存的号码，实际 %+v", sms.sent)
	}
}

// TestMaskPhoneForReset 脱敏规则：任何长度的号码都不能原样返回。
func TestMaskPhoneForReset(t *testing.T) {
	cases := map[string]string{
		"":              "",
		"13800138000":   "138****8000",
		"13900139000":   "139****9000",
		"+447700900123": "+44****0123",
	}
	for in, want := range cases {
		if got := MaskPhone(in); got != want {
			t.Fatalf("MaskPhone(%q) = %q，期望 %q", in, got, want)
		}
	}

	// 短号也必须打码，不能明文回显
	if got := MaskPhone("12345"); strings.Contains(got, "12345") {
		t.Fatalf("短号未打码: %q", got)
	}
}




