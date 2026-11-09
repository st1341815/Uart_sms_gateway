package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.uber.org/zap"
)

// 探测（L1）的单测。
//
// 这里没有任何真实串口：PortOpener / SelfCheckRunner / DeviceRegistry 三处都被替换成
// 内存替身。测试要做的判断是**决策逻辑**——先试哪个口、什么时候拒绝探测、
// 结论怎么翻译成配置片段——而不是「这台机器上有没有插卡」。

const testUsbPort = "usb-0:1"

const (
	path16 = "/dev/serial/by-path/usb-0:1:1.6"
	path12 = "/dev/serial/by-path/usb-0:1:1.2"
	path14 = "/dev/serial/by-path/usb-0:1:1.4"
)

// fakePort 假串口：把预设字节流按序吐出，读完就空转（睡眠 1ms 让出 CPU）。
//
// 「读完不返回错误、只是没数据」正是真实模组不回话的形态：
// 若这里返回 EOF，测的就不是超时路径而是错误路径了。
type fakePort struct {
	data    []byte
	pos     int
	written bytes.Buffer
	closed  bool
	timeout time.Duration
}

func (f *fakePort) Read(p []byte) (int, error) {
	if f.pos >= len(f.data) {
		time.Sleep(time.Millisecond)
		return 0, nil
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func (f *fakePort) Write(p []byte) (int, error) { return f.written.Write(p) }

func (f *fakePort) Close() error {
	f.closed = true
	return nil
}

func (f *fakePort) SetReadTimeout(d time.Duration) error {
	f.timeout = d
	return nil
}

// stubSelfCheck 直接返回构造好的自检报告
type stubSelfCheck struct {
	report *SelfCheckReport
	// calls 记录被调用次数，用来断言「探测前重新扫过一遍、而不是吃缓存」
	calls int
}

func (s *stubSelfCheck) Run([]config.DeviceConfig) *SelfCheckReport {
	s.calls++
	return s.report
}

// stubDevices 设备配置来源
type stubDevices struct {
	configs []config.DeviceConfig
}

func (s *stubDevices) GetAllDeviceConfigs() []config.DeviceConfig { return s.configs }

// testStatusFrame 构造一帧 status_response（含前后缀）
func testStatusFrame(imsi, number string) []byte {
	payload := fmt.Sprintf(
		`{"type":"status_response","version":"1.3.0","mobile":{`+
			`"is_registered":true,"is_roaming":false,"sim_ready":true,`+
			`"iccid":"89860012345678901234","imsi":"%s","number":"%s",`+
			`"csq":22,"rsrp":-88,"signal_desc":"信号良好"}}`,
		imsi, number)
	return []byte(smsPrefix + payload + smsSuffix)
}

// probeReport 造一个「宿主机上有一张未配置模组、三个 ACM 口」的报告
func probeReport(configured bool) *SelfCheckReport {
	return &SelfCheckReport{
		Modules: []SelfCheckModule{{
			UsbPort:    testUsbPort,
			Device:     "AirM2M",
			Configured: configured,
			Interfaces: []SelfCheckInterface{
				{ByPath: path14, IfKey: "1.4", Node: "ttyACM2", InContainer: true},
				{ByPath: path12, IfKey: "1.2", Node: "ttyACM0", InContainer: true},
				{ByPath: path16, IfKey: "1.6", Node: "ttyACM1", InContainer: true},
			},
		}},
	}
}

// newTestProbe 造一个探测服务，opener 换成内存实现
func newTestProbe(report *SelfCheckReport, opener PortOpener, devices *stubDevices) *ProbeService {
	if devices == nil {
		devices = &stubDevices{}
	}
	p := NewProbeService(zap.NewNop(), &stubSelfCheck{report: report}, devices)
	p.opener = opener
	p.window = 120 * time.Millisecond
	p.cooldown = time.Hour // 除专测冷却的用例外，不触发冷却
	return p
}

// fixedOpener 按 by-path 返回预置字节；没预置的口返回「能打开但没数据」
func fixedOpener(frames map[string][]byte, opened *[]string) PortOpener {
	return func(path string) (ProbePort, error) {
		*opened = append(*opened, path)
		return &fakePort{data: frames[path]}, nil
	}
}

// TestProbeFallsBackToSecondInterface 验证兜底路径：
// :1.6 不回话就继续试 :1.2，命中后 AtConfirmed=false（不是靠规律猜的）。
func TestProbeFallsBackToSecondInterface(t *testing.T) {
	frames := map[string][]byte{path12: testStatusFrame("460001234567890", "13800138000")}
	var opened []string
	p := newTestProbe(probeReport(false), fixedOpener(frames, &opened), nil)

	result, err := p.Probe(context.Background(), testUsbPort)
	if err != nil {
		t.Fatalf("探测不该报错: %v", err)
	}
	if !result.Success {
		t.Fatalf("应判定成功，实际 Message=%q", result.Message)
	}
	if len(result.Tried) != 2 {
		t.Fatalf("应先试 :1.6 再试 :1.2，实际试探 %d 个接口: %+v", len(result.Tried), result.Tried)
	}
	if result.Tried[0].IfKey != "1.6" {
		t.Errorf("试探顺序错：第一个应是 1.6，实际 %s", result.Tried[0].IfKey)
	}
	if result.Tried[0].GotStatus {
		t.Error(":1.6 没吐帧，不该被标记为命中")
	}
	if !result.Tried[1].GotStatus || result.Tried[1].IfKey != "1.2" {
		t.Errorf(":1.2 应命中，实际 %+v", result.Tried[1])
	}

	if result.AtPort != path12 || result.Node != "ttyACM0" || result.IfKey != "1.2" {
		t.Errorf("AT 口定位错: port=%s node=%s if=%s", result.AtPort, result.Node, result.IfKey)
	}
	if result.AtConfirmed {
		t.Error(":1.2 不是按 :1.6 规律命中的，AtConfirmed 应为 false")
	}
	if result.Operator != operatorFromIMSI("460001234567890") {
		t.Errorf("运营商应沿用 operatorFromIMSI，实际 %q", result.Operator)
	}
	if result.Number != "13800138000" || !result.Registered || !result.SimReady {
		t.Errorf("SIM/号码字段没填对: %+v", result)
	}
	if result.Name == "" {
		t.Error("建议设备名不应为空（否则用户还得自己起名）")
	}
	if !strings.Contains(result.ConfigSnippet, fmt.Sprintf("Port: %q", path12)) {
		t.Errorf("配置片段里应含实测 AT 口，实际:\n%s", result.ConfigSnippet)
	}
}

// TestProbeRejectsConfiguredInterface 已配置的端口绝不探测：
// 在跑的卡上做探测会把它打成「重连中」，掉线期间的短信只能进补发队列。
func TestProbeRejectsConfiguredInterface(t *testing.T) {
	var opened []string
	p := newTestProbe(probeReport(true), fixedOpener(nil, &opened), nil)

	_, err := p.Probe(context.Background(), testUsbPort)
	if !errors.Is(err, ErrProbeConfigured) {
		t.Fatalf("应拒绝探测已配置端口，实际 err=%v", err)
	}
	if len(opened) != 0 {
		t.Fatalf("被拒绝时不该打开任何串口，实际打开了 %v", opened)
	}
}

// TestProbeUnknownPort 报告里没有这个端口 → 找不到目标
func TestProbeUnknownPort(t *testing.T) {
	p := newTestProbe(&SelfCheckReport{}, fixedOpener(nil, new([]string)), nil)
	if _, err := p.Probe(context.Background(), "usb-9:9"); !errors.Is(err, ErrProbeNoTarget) {
		t.Fatalf("应返回 ErrProbeNoTarget，实际 %v", err)
	}
	if _, err := p.Probe(context.Background(), "   "); !errors.Is(err, ErrProbeNoTarget) {
		t.Fatalf("空端口号应返回 ErrProbeNoTarget，实际 %v", err)
	}
}

// TestProbeCooldown 同一端口冷却：连点两次，第二次被拒
func TestProbeCooldown(t *testing.T) {
	frames := map[string][]byte{path16: testStatusFrame("460001234567890", "")}
	var opened []string
	p := newTestProbe(probeReport(false), fixedOpener(frames, &opened), nil)

	if _, err := p.Probe(context.Background(), testUsbPort); err != nil {
		t.Fatalf("第一次探测应成功: %v", err)
	}
	_, err := p.Probe(context.Background(), testUsbPort)
	if !errors.Is(err, ErrProbeCooldown) {
		t.Fatalf("第二次应被冷却拦住，实际 %v", err)
	}
	// 错误信息要带还差多少秒，前端直接展示，不该自己去算
	if !strings.Contains(err.Error(), "秒") {
		t.Errorf("冷却错误应提示剩余秒数，实际 %q", err.Error())
	}
}

// TestProbeBusy 全局串行：已有探测占着锁时，第二个请求立刻被拒而不是排队
func TestProbeBusy(t *testing.T) {
	p := newTestProbe(probeReport(false), fixedOpener(nil, new([]string)), nil)

	p.probeMu.Lock()
	defer p.probeMu.Unlock()

	if _, err := p.Probe(context.Background(), testUsbPort); !errors.Is(err, ErrProbeBusy) {
		t.Fatalf("已有探测进行中应返回 ErrProbeBusy，实际 %v", err)
	}
}

// TestProbeNodeMissingInContainer 容器里没有 /dev 节点：
// 不该白费 2 秒去 open，而要直接给出「补 compose devices」的下一步。
func TestProbeNodeMissingInContainer(t *testing.T) {
	report := probeReport(false)
	for i := range report.Modules[0].Interfaces {
		report.Modules[0].Interfaces[i].InContainer = false
	}
	var opened []string
	p := newTestProbe(report, fixedOpener(nil, &opened), nil)

	result, err := p.Probe(context.Background(), testUsbPort)
	if err != nil {
		t.Fatalf("不该报错: %v", err)
	}
	if result.Success {
		t.Fatal("容器内没有节点，不可能探测成功")
	}
	if len(opened) != 0 {
		t.Fatalf("节点不存在时不该尝试打开串口，实际打开了 %v", opened)
	}
	if !strings.Contains(result.Tried[0].Error, "容器内没有该节点") {
		t.Errorf("应说明容器内缺节点，实际 %q", result.Tried[0].Error)
	}
	if result.NextStep == "" {
		t.Error("失败结论必须带下一步，否则用户不知道从哪查起")
	}
}

// TestProbeOnceOpenError 打不开串口应该能和「能开但不回话」区分开
func TestProbeOnceOpenError(t *testing.T) {
	p := newTestProbe(probeReport(false), func(string) (ProbePort, error) {
		return nil, errors.New("permission denied")
	}, nil)

	status, err := p.probeOnce(context.Background(), path16)
	if status != nil {
		t.Fatalf("打开失败不该返回状态: %+v", status)
	}
	if !errors.Is(err, errProbeOpen) {
		t.Fatalf("应包裹成 errProbeOpen，实际 %v", err)
	}
}

// TestProbeOnceNoFrame 口能打开但卡不回话 → (nil, nil)，不当作错误
func TestProbeOnceNoFrame(t *testing.T) {
	p := newTestProbe(probeReport(false), func(string) (ProbePort, error) {
		return &fakePort{}, nil
	}, nil)

	status, err := p.probeOnce(context.Background(), path16)
	if status != nil || err != nil {
		t.Fatalf("无帧时应返回 (nil, nil)，实际 status=%+v err=%v", status, err)
	}
}

// TestProbeOnceWritesCommand 确认探测真的发了 get_status（而不是只读不写）
func TestProbeOnceWritesCommand(t *testing.T) {
	var port *fakePort
	p := newTestProbe(probeReport(false), func(string) (ProbePort, error) {
		port = &fakePort{data: testStatusFrame("460001234567890", "")}
		return port, nil
	}, nil)

	status, err := p.probeOnce(context.Background(), path16)
	if err != nil || status == nil {
		t.Fatalf("应拿到状态帧，实际 status=%+v err=%v", status, err)
	}
	written := port.written.String()
	if !strings.Contains(written, "get_status") {
		t.Errorf("应写入 get_status 命令，实际写入 %q", written)
	}
	if !strings.HasPrefix(written, "CMD_START:") || !strings.HasSuffix(written, ":CMD_END\r\n") {
		t.Errorf("命令帧格式不对: %q", written)
	}
	if !port.closed {
		t.Error("探测结束必须关闭串口，否则下一次 open 会撞上占用")
	}
	if port.timeout != ProbeReadTimeout {
		t.Errorf("读超时应设为 %v，实际 %v", ProbeReadTimeout, port.timeout)
	}
}

// TestTakeProbeFrames 切帧：噪声、粘包、半包、裸 JSON
func TestTakeProbeFrames(t *testing.T) {
	t.Run("丢弃噪声并切出完整帧", func(t *testing.T) {
		buf := []byte("garbage" + smsPrefix + `{"type":"status_response"}` + smsSuffix + "tail")
		frames := takeProbeFrames(&buf)
		if len(frames) != 1 {
			t.Fatalf("应切出 1 帧，实际 %d: %q", len(frames), frames)
		}
		if strings.Contains(string(frames[0]), "garbage") {
			t.Errorf("帧里混进了噪声: %q", frames[0])
		}
	})

	t.Run("半包时保留起始标记等下一轮", func(t *testing.T) {
		half := smsPrefix + `{"type":"status_re`
		buf := []byte(half)
		if frames := takeProbeFrames(&buf); len(frames) != 0 {
			t.Fatalf("半包不该产出帧，实际 %q", frames)
		}
		if !strings.HasPrefix(string(buf), smsPrefix) {
			t.Errorf("半包应保留起始标记，实际 %q", buf)
		}
	})

	t.Run("老固件吐裸 JSON 也能认", func(t *testing.T) {
		buf := []byte(`{"type":"status_response","mobile":{}}`)
		frames := takeProbeFrames(&buf)
		if len(frames) != 1 {
			t.Fatalf("裸 JSON 应被兜底识别，实际 %d", len(frames))
		}
	})
}

// TestParseProbeStatus 只认 status_response，别的类型一律当没拿到
func TestParseProbeStatus(t *testing.T) {
	if _, ok := parseProbeStatus([]byte(`{"type":"heartbeat"}`)); ok {
		t.Error("heartbeat 不该被当成状态帧")
	}
	if _, ok := parseProbeStatus([]byte(`not json`)); ok {
		t.Error("非 JSON 不该被当成状态帧")
	}
	status, ok := parseProbeStatus(testStatusFrame("460001234567890", "13800138000")[len(smsPrefix) : len(smsPrefix)+len(testStatusFrame("460001234567890", "13800138000"))-len(smsPrefix)-len(smsSuffix)])
	if !ok {
		t.Fatal("合法 status_response 应解析成功")
	}
	if status.Mobile.Number != "13800138000" {
		t.Errorf("号码解析错: %q", status.Mobile.Number)
	}
}

// TestSortProbeInterfaces 试探顺序：1.6 → 1.2 → 1.4 → 其余按号
func TestSortProbeInterfaces(t *testing.T) {
	in := []SelfCheckInterface{
		{IfKey: "1.8"}, {IfKey: "1.4"}, {IfKey: "1.2"}, {IfKey: "1.6"},
	}
	got := sortProbeInterfaces(in)
	want := []string{"1.6", "1.2", "1.4", "1.8"}
	for i, k := range want {
		if got[i].IfKey != k {
			t.Fatalf("第 %d 个应是 %s，实际 %s（整体 %v）", i, k, got[i].IfKey, got)
		}
	}
	// 不能就地改调用方切片
	if in[0].IfKey != "1.8" {
		t.Error("排序不应修改入参切片")
	}
}

// TestConfigSnippetUniqueID 设备 ID 冲突时要顺延，不能生成重复 ID
func TestConfigSnippetUniqueID(t *testing.T) {
	devices := &stubDevices{configs: []config.DeviceConfig{{ID: "usb01"}}}
	p := newTestProbe(probeReport(false), fixedOpener(nil, new([]string)), devices)

	snippet := p.configSnippet(&ProbeResult{UsbPort: testUsbPort, AtPort: path16, Name: "中国移动 13800138000"})
	if !strings.Contains(snippet, `ID: "usb01_2"`) {
		t.Errorf("ID 冲突应顺延成 usb01_2，实际:\n%s", snippet)
	}
	if !strings.Contains(snippet, "Enabled: true") {
		t.Errorf("新增设备默认应启用，实际:\n%s", snippet)
	}

	if s := p.configSnippet(&ProbeResult{UsbPort: testUsbPort}); s != "" {
		t.Errorf("没定位到 AT 口时不该给配置片段，实际 %q", s)
	}
}

// TestOperatorFromIMSI 运营商推断是共享逻辑（状态帧与探测同源）
func TestOperatorFromIMSI(t *testing.T) {
	if got := operatorFromIMSI(""); got != "" {
		t.Errorf("空 IMSI 应返回空串，实际 %q", got)
	}
	if got := operatorFromIMSI("4600"); got != "" {
		t.Errorf("不足 5 位应返回空串，实际 %q", got)
	}
	if got := operatorFromIMSI("460001234567890"); got == "" {
		t.Error("已知 PLMN 不该返回空串")
	}
	if got := operatorFromIMSI("999991234567890"); got != "99999" {
		t.Errorf("未知 PLMN 应原样返回，实际 %q", got)
	}
	if got := operatorFromIMSI("460001234567890"); got != OperData["46000"] && OperData["46000"] != "" {
		t.Errorf("应查 OperData，实际 %q", got)
	}
}
