package service

import (
	"bytes"
	"os"
	"testing"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.uber.org/zap"
)

// TestDeviceManagerDisplayNamePrecedence 覆盖三级优先级：
// DB 覆盖名 > config.yaml 的 Name > 设备 ID。
func TestDeviceManagerDisplayNamePrecedence(t *testing.T) {
	cases := []struct {
		name       string
		id         string
		configName string
		override   string
		hasOver    bool
		want       string
	}{
		{
			name:       "无覆盖时用 config 名",
			id:         "sms1",
			configName: "移动新卡",
			want:       "移动新卡",
		},
		{
			name:       "有覆盖时用覆盖名",
			id:         "sms1",
			configName: "移动新卡",
			override:   "联通151命名卡",
			hasOver:    true,
			want:       "联通151命名卡",
		},
		{
			name:       "覆盖名为空串视为未覆盖",
			id:         "sms1",
			configName: "移动新卡",
			override:   "",
			hasOver:    true,
			want:       "移动新卡",
		},
		{
			name: "config 名也为空时回退设备 ID",
			id:   "auto",
			want: "auto",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dm := NewDeviceManager(zap.NewNop(), config.SerialConfig{}, nil, nil, nil)
			if c.hasOver {
				dm.nameOverrides[c.id] = c.override
			}
			if got := dm.displayName(c.id, c.configName); got != c.want {
				t.Fatalf("displayName(%q, %q) = %q, want %q", c.id, c.configName, got, c.want)
			}
		})
	}
}

// TestSerialServiceDisplayNameFollowsRename 复现并锁定 v1.6.0 的 bug：
// 界面改名只写 properties 表（内存里是 dm.nameOverrides），而 SerialService
// 手里那份 config.DeviceConfig 是启动快照。所有推送（短信/来电/SIM 上下线/
// 补发失败）都必须走 displayName()，改名后立刻生效。
func TestSerialServiceDisplayNameFollowsRename(t *testing.T) {
	dc := config.DeviceConfig{ID: "sms3", Name: "移动新卡", Port: "/dev/ttyUSB2", Enabled: true}
	serialCfg := config.SerialConfig{Devices: []config.DeviceConfig{dc}}

	ss := NewSerialServiceWithDeviceID(
		zap.NewNop(), dc.ID,
		config.SerialConfig{Port: dc.Port},
		dc, nil, nil, nil,
	)

	// 未注入解析器（例如单测里直接构造）：回退到 config 名，保持老行为。
	if got := ss.displayName(); got != "移动新卡" {
		t.Fatalf("未注入解析器时 displayName() = %q, want %q", got, "移动新卡")
	}

	dm := NewDeviceManager(zap.NewNop(), serialCfg, nil, nil, nil)
	ss.SetDisplayNameResolver(func() string { return dm.displayName(dc.ID, dc.Name) })

	// 注入后、尚未改名：仍是 config 名。
	if got := ss.displayName(); got != "移动新卡" {
		t.Fatalf("注入后未改名 displayName() = %q, want %q", got, "移动新卡")
	}

	// 用户在界面上改名 —— 落 properties 表，内存里就是这份覆盖层。
	dm.nameMu.Lock()
	dm.nameOverrides[dc.ID] = "联通151命名卡"
	dm.nameMu.Unlock()

	if got := ss.displayName(); got != "联通151命名卡" {
		t.Fatalf("改名后 displayName() = %q, want %q", got, "联通151命名卡")
	}
	// Handler / 设备列表走的另一条路径，两边必须一致。
	if got := dm.GetDeviceDisplayName(dc.ID); got != "联通151命名卡" {
		t.Fatalf("GetDeviceDisplayName(%q) = %q, want %q", dc.ID, got, "联通151命名卡")
	}
	// 覆盖只发生在内存层，SerialService 的快照本身不该被改动。
	if got := ss.GetDeviceConfig().Name; got != "移动新卡" {
		t.Fatalf("DeviceConfig 快照被篡改：Name = %q, want %q", got, "移动新卡")
	}
}

// TestSerialServiceDisplayNameFallsBackToID 防御空 Name 场景：
// 旧配置里往往只写 Port 不写 Name。
func TestSerialServiceDisplayNameFallsBackToID(t *testing.T) {
	ss := &SerialService{deviceID: "auto"}
	if got := ss.displayName(); got != "auto" {
		t.Fatalf("displayName() = %q, want %q", got, "auto")
	}
}

// TestNotificationSitesUseDisplayName 这是给 v1.6.0 那个 bug 上的护栏。
//
// 当时的形态是 5 处推送各自写 `DeviceName: s.deviceConfig.Name`，谁都不知道
// 还有第二份「自定义名」存在。以后有人再加通知，也很容易顺手又抄一份快照。
// 所以直接扫源码：这些文件里不允许再出现取 config 快照的写法。
func TestNotificationSitesUseDisplayName(t *testing.T) {
	files := []string{
		"serial_handlers_sms.go",
		"serial_handlers_call.go",
		"serial_handlers_sim.go",
		"serial_pending.go",
	}
	banned := []string{
		"DeviceName: s.deviceConfig.Name",
		`zap.String("device_name", s.deviceConfig.Name)`,
	}

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		for _, b := range banned {
			if bytes.Contains(src, []byte(b)) {
				t.Errorf("%s 仍在使用 %s —— 推送设备名必须走 s.displayName()，"+
					"否则界面改名后通知里还是开机默认名", f, b)
			}
		}
	}
}
