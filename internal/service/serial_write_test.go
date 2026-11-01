package service

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dushixiang/uart_sms_forwarder/config"
)

// shortWriter 模拟串口「短写」：每次最多写 max 字节且 err == nil。
type shortWriter struct {
	max   int
	got   []byte
	calls int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	w.calls++
	n := len(p)
	if n > w.max {
		n = w.max
	}
	w.got = append(w.got, p[:n]...)
	return n, nil
}

// failWriter 第一次写一半，第二次报错。
type failWriter struct {
	calls int
}

func (w *failWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 1 {
		return len(p) / 2, nil
	}
	return 0, errors.New("boom")
}

// TestWriteAllHandlesShortWrite 验证短写会被循环补齐。
// 这是 P0 修复点：原来直接丢弃 Write 的返回值，命令会被截断。
func TestWriteAllHandlesShortWrite(t *testing.T) {
	payload := []byte(`CMD_START:{"action":"send_sms","to":"10086","content":"hello"}:CMD_END` + "\r\n")
	w := &shortWriter{max: 3}

	if err := writeAll(w, payload); err != nil {
		t.Fatalf("writeAll 返回错误: %v", err)
	}
	if string(w.got) != string(payload) {
		t.Fatalf("写出内容不完整:\n期望 %q\n实际 %q", payload, w.got)
	}
	if w.calls < 2 {
		t.Fatalf("期望多次写入（短写），实际只调用了 %d 次", w.calls)
	}
}

// TestWriteAllPropagatesError 验证写入错误会向上传递。
func TestWriteAllPropagatesError(t *testing.T) {
	w := &failWriter{}
	err := writeAll(w, []byte("0123456789"))
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
}

// TestWriteAllZeroProgress 验证 n<=0 被当作短写错误，避免死循环。
func TestWriteAllZeroProgress(t *testing.T) {
	w := &shortWriter{max: 0}
	err := writeAll(w, []byte("abc"))
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("期望 io.ErrShortWrite，实际 %v", err)
	}
}

// TestGetFirstEnabledDeviceIsDeterministic 验证「第一个设备」按配置声明顺序返回，
// 不会因为 Go map 遍历顺序随机而随机选卡（P0 修复点）。
func TestGetFirstEnabledDeviceIsDeterministic(t *testing.T) {
	dm := &DeviceManager{
		devices:      map[string]*SerialService{},
		deviceConfig: map[string]config.DeviceConfig{},
		order:        []string{},
	}

	// 模拟配置顺序：sms1 先声明，sms2 后声明
	declared := []string{"sms1", "sms2", "sms3"}
	for _, id := range declared {
		dm.devices[id] = &SerialService{deviceID: id}
		dm.deviceConfig[id] = config.DeviceConfig{ID: id, Name: id}
		dm.order = append(dm.order, id)
	}

	// 连续调用 200 次，必须永远是 sms1
	for i := 0; i < 200; i++ {
		id, ok := dm.GetFirstEnabledDeviceID()
		if !ok {
			t.Fatal("GetFirstEnabledDeviceID 返回 false")
		}
		if id != "sms1" {
			t.Fatalf("第 %d 次返回 %q，期望恒为 sms1（配置声明顺序）", i, id)
		}
	}

	// GetAllDeviceIDs 也必须保持声明顺序
	ids := dm.GetAllDeviceIDs()
	if strings.Join(ids, ",") != "sms1,sms2,sms3" {
		t.Fatalf("设备顺序错误: %v", ids)
	}

	// GetAllDeviceConfigs 同理
	cfgIDs := []string{}
	for _, dc := range dm.GetAllDeviceConfigs() {
		cfgIDs = append(cfgIDs, dc.ID)
	}
	if strings.Join(cfgIDs, ",") != "sms1,sms2,sms3" {
		t.Fatalf("设备配置顺序错误: %v", cfgIDs)
	}
}

// TestGetFirstEnabledDeviceEmpty 验证没有设备时安全返回。
func TestGetFirstEnabledDeviceEmpty(t *testing.T) {
	dm := &DeviceManager{
		devices:      map[string]*SerialService{},
		deviceConfig: map[string]config.DeviceConfig{},
		order:        []string{},
	}
	if id, ok := dm.GetFirstEnabledDeviceID(); ok || id != "" {
		t.Fatalf("空设备列表应返回 (\"\", false)，实际 (%q, %v)", id, ok)
	}
	if ss, id := dm.GetFirstEnabledDevice(); ss != nil || id != "" {
		t.Fatalf("空设备列表 GetFirstEnabledDevice 应返回 (nil, \"\")")
	}
}
