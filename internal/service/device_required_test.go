package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/dushixiang/uart_sms_forwarder/internal/repo"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"go.uber.org/zap"
	"gorm.io/gorm/logger"
)

// newTestDM 构造一个含 2 台假设备的 DeviceManager（不连串口）
func newTestDM() *DeviceManager {
	dm := &DeviceManager{
		devices:      map[string]*SerialService{},
		deviceConfig: map[string]config.DeviceConfig{},
		order:        []string{},
		limiters:     newSendRateLimiters(),
	}
	for _, id := range []string{"sms1", "sms2"} {
		dm.devices[id] = &SerialService{deviceID: id}
		dm.deviceConfig[id] = config.DeviceConfig{ID: id, Name: id}
		dm.order = append(dm.order, id)
	}
	return dm
}

// TestSendSMSRequiresDeviceID device_id 必填（本次需求）
func TestSendSMSRequiresDeviceID(t *testing.T) {
	dm := newTestDM()

	_, err := dm.SendSMS("", "10086", "hi")
	if err == nil {
		t.Fatal("device_id 为空时应报错，实际通过了")
	}
	if !strings.Contains(err.Error(), "device_id") {
		t.Fatalf("错误信息应提示 device_id，实际: %v", err)
	}
}

// TestSetFlymodeRequiresDeviceID 飞行模式必须指定设备
func TestSetFlymodeRequiresDeviceID(t *testing.T) {
	dm := newTestDM()
	if err := dm.SetFlymode("", true); err == nil {
		t.Fatal("device_id 为空时应报错")
	}
}

// TestRebootRequiresDeviceID 重启必须指定设备
func TestRebootRequiresDeviceID(t *testing.T) {
	dm := newTestDM()
	if err := dm.RebootMcu(""); err == nil {
		t.Fatal("device_id 为空时应报错")
	}
}

// TestUnknownDeviceRejected 不存在的设备被拒，且提示可选项
func TestUnknownDeviceRejected(t *testing.T) {
	dm := newTestDM()
	_, err := dm.SendSMS("nope", "10086", "hi")
	if err == nil {
		t.Fatal("不存在的设备应报错")
	}
	if !strings.Contains(err.Error(), "sms1") {
		t.Fatalf("错误信息应列出可选设备，实际: %v", err)
	}
}

// TestSchedulerHasDeviceEmpty device_id 为空不再视为合法（本次需求）
func TestSchedulerHasDeviceEmpty(t *testing.T) {
	s := &SchedulerService{deviceManager: newTestDM()}
	if s.HasDevice("") {
		t.Fatal("device_id 为空时 HasDevice 应返回 false")
	}
	if !s.HasDevice("sms1") {
		t.Fatal("sms1 应存在")
	}
	if s.HasDevice("nope") {
		t.Fatal("nope 不应存在")
	}
}

// newTestDB 建一个内存 SQLite 库
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	if err := db.AutoMigrate(&models.TextMessage{}); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}
	return db
}

// TestExpireStaleSending 卡在 sending 的记录超时后置为 unknown
func TestExpireStaleSending(t *testing.T) {
	db := newTestDB(t)
	svc := &TextMessageService{
		repo:   repo.NewTextMessageRepo(db),
		logger: testLogger(),
	}

	ctx := context.Background()
	old := time.Now().Add(-10 * time.Minute).UnixMilli()
	fresh := time.Now().UnixMilli()

	// 一条很旧的 sending（应被置 unknown）
	mustSave(t, db, &models.TextMessage{
		ID: "old-sending", Status: models.MessageStatusSending, CreatedAt: old,
	})
	// 一条刚发出的 sending（不应被动）
	mustSave(t, db, &models.TextMessage{
		ID: "fresh-sending", Status: models.MessageStatusSending, CreatedAt: fresh,
	})
	// 一条已 sent（不应被动）
	mustSave(t, db, &models.TextMessage{
		ID: "old-sent", Status: models.MessageStatusSent, CreatedAt: old,
	})

	n, err := svc.ExpireStaleSending(ctx, 90*time.Second)
	if err != nil {
		t.Fatalf("ExpireStaleSending 错误: %v", err)
	}
	if n != 1 {
		t.Fatalf("应更新 1 条，实际 %d 条", n)
	}

	assertStatus(t, db, "old-sending", models.MessageStatusUnknown)
	assertStatus(t, db, "fresh-sending", models.MessageStatusSending)
	assertStatus(t, db, "old-sent", models.MessageStatusSent)
}

// TestUpdateStatusIfSendingNoOverwrite 已 sent 的记录不会被超时扫描覆盖
func TestUpdateStatusIfSendingNoOverwrite(t *testing.T) {
	db := newTestDB(t)
	svc := &TextMessageService{repo: repo.NewTextMessageRepo(db), logger: testLogger()}
	ctx := context.Background()

	mustSave(t, db, &models.TextMessage{
		ID: "m1", Status: models.MessageStatusSent, CreatedAt: time.Now().UnixMilli(),
	})

	n, err := svc.UpdateStatusIfSending(ctx, "m1", models.MessageStatusUnknown)
	if err != nil {
		t.Fatalf("错误: %v", err)
	}
	if n != 0 {
		t.Fatalf("已是 sent，不应被更新，实际影响 %d 行", n)
	}
	assertStatus(t, db, "m1", models.MessageStatusSent)
}

// ---------- 测试辅助 ----------

func testLogger() *zap.Logger { return zap.NewNop() }

func mustSave(t *testing.T, db *gorm.DB, m *models.TextMessage) {
	t.Helper()
	if err := db.Create(m).Error; err != nil {
		t.Fatalf("插入测试数据失败: %v", err)
	}
}

func assertStatus(t *testing.T, db *gorm.DB, id string, want models.MessageStatus) {
	t.Helper()
	var got models.TextMessage
	if err := db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("查询 %s 失败: %v", id, err)
	}
	if got.Status != want {
		t.Fatalf("%s 状态 = %q，期望 %q", id, got.Status, want)
	}
}
