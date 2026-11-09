package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.bug.st/serial"
	"go.uber.org/zap"
)

// 设备探测（L1）
//
// L0 自检（selfcheck_service.go）能说清「宿主机有这张卡，但容器/配置里没有」，
// 但它**打不开串口**，所以拿不到运营商、号码、信号——用户加完卡还是不知道
// 这是哪张卡，起名只能靠人肉试。
//
// L1 补的就是这一环：对**未被任何配置引用**的物理端口，逐个接口试探，
// 发一次 get_status，谁回了 status_response，谁就是这张卡的 AT 口。
// 拿到 operator / number / iccid / 信号后，配置片段就能直接写上「中国移动 138xxxx」。
//
// 三条硬约束（每一条都有代价，违反就是线上事故）：
//
//	① 绝不动已配置端口。
//	   已经在跑的卡上做探测，会把它从「正在收发」打成「重连中」，
//	   掉线期间的短信进补发队列。所以 Configured == true 的端口一律拒绝。
//
//	② 探测必须可单测。
//	   真实串口在 CI/开发机上不存在，所以「打开串口」被抽成 PortOpener 注入，
//	   测试里换成内存假串口，验证试探顺序、切帧、解析、限频，不用接硬件。
//
//	③ 限频 + 串行化。
//	   同一端口 30 秒内只允许探测一次；全局一次只探测一个端口。
//	   否则用户连点按钮会在同一 UART 上开两个 fd，读到的是交错数据。
const (
	// ProbeReadTimeout 单次 Read 的超时。设小一点让循环有机会检查总时限。
	ProbeReadTimeout = 200 * time.Millisecond
	// ProbeWindow 单个接口等状态帧的总时长。
	//
	// 实测：发了 get_status 后模组 100~300ms 内回帧；给 2 秒是留给
	// 「刚上电、正在注册网络」的卡——它可能晚一点才回。
	ProbeWindow = 2 * time.Second
	// ProbeCooldown 同端口探测冷却期
	ProbeCooldown = 30 * time.Second
	// probeMaxBytes 单次探测的接收缓冲上限，防止脏数据把内存吃满
	probeMaxBytes = 64 * 1024
	// ProbeStatusType 状态帧的 type 值（与 main.lua 的 get_status 分支一致）
	ProbeStatusType = "status_response"
)

// probeInterfaceOrder 试探顺序：AirM2M 每个模组 3 个 ACM 口，实测短信走 :1.6。
//
// 1.6 放第一位是「大概率一次命中」；1.2 / 1.4 是兜底（不同固件版本可能挪位）。
// 其余接口按号排序跟在后面——不跳过，因为「1.6 才是 AT 口」只是实测规律、不是规范。
var probeInterfaceOrder = []string{"1.6", "1.2", "1.4"}

var (
	// ErrProbeNoTarget 宿主机上找不到这个 USB 端口
	ErrProbeNoTarget = errors.New("宿主机上找不到该 USB 端口")
	// ErrProbeConfigured 该端口已被 config.yaml 里的设备占用（拒绝探测，避免打扰在跑的卡）
	ErrProbeConfigured = errors.New("该端口已被已配置设备占用，不做探测")
	// ErrProbeCooldown 探测过于频繁
	ErrProbeCooldown = errors.New("探测过于频繁，请稍后再试")
	// ErrProbeBusy 已有探测在进行
	ErrProbeBusy = errors.New("已有探测正在进行，请稍后")
)

// ProbePort 探测用到的串口能力。
//
// 只声明必要方法，测试里用假实现替换；
// go.bug.st/serial 的 Port 接口天然满足它（Read/Write/Close/SetReadTimeout）。
type ProbePort interface {
	io.ReadWriteCloser
	SetReadTimeout(t time.Duration) error
}

// PortOpener 打开串口，便于测试注入
type PortOpener func(path string) (ProbePort, error)

// defaultPortOpener 与 SerialService.connectSerial 保持同一串口参数
// （115200 8N1）——参数不一致会让模组收不到命令，探测必然全失败。
func defaultPortOpener(path string) (ProbePort, error) {
	return serial.Open(path, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		StopBits: serial.OneStopBit,
		Parity:   serial.NoParity,
	})
}

// ProbeAttempt 单个接口的试探结果（失败时给用户看的排查线索）
type ProbeAttempt struct {
	IfKey       string `json:"if_key"`
	ByPath      string `json:"by_path"`
	Node        string `json:"node"`
	InContainer bool   `json:"in_container"`
	Opened      bool   `json:"opened"`
	GotStatus   bool   `json:"got_status"`
	Error       string `json:"error,omitempty"`
}

// ProbeResult 探测结论
type ProbeResult struct {
	UsbPort string `json:"usb_port"`
	Success bool   `json:"success"`
	// Message 一句话结论（中文）
	Message string `json:"message"`
	// NextStep 下一步该干什么
	NextStep string `json:"next_step,omitempty"`
	// Tried 试探过哪些接口（顺序即优先级，便于排查）
	Tried []ProbeAttempt `json:"tried"`

	// 以下字段仅在 Success 时有值
	AtPort string `json:"at_port,omitempty"` // 可直接写进 config.yaml 的 by-path
	Node   string `json:"node,omitempty"`
	IfKey  string `json:"if_key,omitempty"`
	// AtConfirmed 是否按 :1.6 规律命中（false = 靠试探确认的，结论更可信）
	AtConfirmed bool `json:"at_confirmed"`

	Operator   string `json:"operator,omitempty"`
	Number     string `json:"number,omitempty"`
	Iccid      string `json:"iccid,omitempty"`
	Imsi       string `json:"imsi,omitempty"`
	SimReady   bool   `json:"sim_ready"`
	Registered bool   `json:"is_registered"`
	Roaming    bool   `json:"is_roaming"`
	Csq        int    `json:"csq"`
	Rsrp       int    `json:"rsrp"`
	SignalDesc string `json:"signal_desc,omitempty"`
	LuaVersion string `json:"lua_version,omitempty"`

	// Name 建议的设备显示名（运营商 + 号码，可为空则前端自己填）
	Name string `json:"name,omitempty"`
	// ConfigSnippet 带实测信息的 config.yaml Devices 片段
	ConfigSnippet string `json:"config_snippet,omitempty"`
	// ProbedAt 探测时刻
	ProbedAt time.Time `json:"probed_at"`
}

// SelfCheckRunner 探测只需要「跑一次自检、拿到报告」这一项能力。
//
// 抽成接口不是为了抽象而抽象：真实自检要扫 /dev/serial/by-path 与 /sys/class/tty，
// 这些在 CI 和开发机上不存在；只有能替换成伪造报告，试探顺序 / 安全闸 / 限流才测得起来。
type SelfCheckRunner interface {
	Run(deviceConfigs []config.DeviceConfig) *SelfCheckReport
}

// DeviceRegistry 探测只需要「列出全部设备配置」这一项能力（安全闸要比对哪些端口已被占用）。
type DeviceRegistry interface {
	GetAllDeviceConfigs() []config.DeviceConfig
}

// ProbeService 探测服务
type ProbeService struct {
	logger    *zap.Logger
	selfCheck SelfCheckRunner
	devices   DeviceRegistry
	opener    PortOpener
	window    time.Duration
	cooldown  time.Duration

	// probeMu 串行化探测：同一时刻只探测一个端口
	probeMu sync.Mutex
	mu      sync.Mutex
	// lastProbeAt 端口级冷却记录（usbPort -> 上次探测时刻）
	lastProbeAt map[string]time.Time
}

// NewProbeService 创建探测服务
func NewProbeService(logger *zap.Logger, selfCheck SelfCheckRunner, devices DeviceRegistry) *ProbeService {
	return &ProbeService{
		logger:      logger,
		selfCheck:   selfCheck,
		devices:     devices,
		opener:      defaultPortOpener,
		window:      ProbeWindow,
		cooldown:    ProbeCooldown,
		lastProbeAt: make(map[string]time.Time),
	}
}

// Probe 探测某个 USB 端口上的模组
//
// usbPort 形如 "usb-0:1"（来自自检报告 modules[].usb_port）。
func (p *ProbeService) Probe(ctx context.Context, usbPort string) (*ProbeResult, error) {
	usbPort = strings.TrimSpace(usbPort)
	if usbPort == "" {
		return nil, ErrProbeNoTarget
	}

	// ① 串行化：探测会持续 1~6 秒，同一时刻只允许一个
	if !p.probeMu.TryLock() {
		return nil, ErrProbeBusy
	}
	defer p.probeMu.Unlock()

	// ② 重新扫描一次（而不是用缓存），保证看到的是「此刻的」设备树
	configs := p.devices.GetAllDeviceConfigs()
	report := p.selfCheck.Run(configs)

	module, ok := findModule(report.Modules, usbPort)
	if !ok {
		return nil, ErrProbeNoTarget
	}
	// ③ 安全闸：已配置的端口绝不探测
	if module.Configured {
		return nil, ErrProbeConfigured
	}

	// ④ 冷却检查放在「拿到目标」之后：
	//    输错端口号不该消耗冷却配额，否则用户改对之后还要多等 30 秒。
	if wait := p.checkCooldown(usbPort); wait > 0 {
		return nil, fmt.Errorf("%w（还需 %d 秒）", ErrProbeCooldown, int(wait.Seconds()+0.5))
	}

	result := &ProbeResult{
		UsbPort:  usbPort,
		Tried:    make([]ProbeAttempt, 0, len(module.Interfaces)),
		ProbedAt: time.Now(),
	}

	for _, iface := range sortProbeInterfaces(module.Interfaces) {
		attempt := ProbeAttempt{
			IfKey:       iface.IfKey,
			ByPath:      iface.ByPath,
			Node:        iface.Node,
			InContainer: iface.InContainer,
		}

		if !iface.InContainer {
			// 容器 /dev 里没有这个节点：连 open 都不用试。
			// 这是最常见的失败原因（compose 没映射 devices），要给出可执行的下一步。
			attempt.Error = "容器内没有该节点（需先补建 /dev 节点或改 compose devices）"
			result.Tried = append(result.Tried, attempt)
			continue
		}

		status, err := p.probeOnce(ctx, iface.ByPath)
		if err != nil {
			// 只有「连 open 都失败」才算口打不开；能开但读不到，是卡或固件的问题
			attempt.Opened = !errors.Is(err, errProbeOpen)
			attempt.Error = err.Error()
			result.Tried = append(result.Tried, attempt)
			p.logger.Debug("探测接口失败",
				zap.String("usb_port", usbPort),
				zap.String("by_path", iface.ByPath),
				zap.Error(err))
			continue
		}
		if status == nil {
			attempt.Error = "打开了串口但没等到 status_response"
			result.Tried = append(result.Tried, attempt)
			continue
		}

		// 命中：填结论并返回
		attempt.GotStatus = true
		result.Tried = append(result.Tried, attempt)
		p.fillResult(result, module, iface, status)
		p.markProbed(usbPort)

		p.logger.Info("模组探测成功",
			zap.String("usb_port", usbPort),
			zap.String("node", iface.Node),
			zap.String("if_key", iface.IfKey),
			zap.String("operator", result.Operator))

		return result, nil
	}

	p.markProbed(usbPort)
	result.Success = false
	result.Message = fmt.Sprintf("USB 端口 %s 上的 %d 个接口都没能拿到状态帧，卡可能未上电、SIM 未插好或固件没跑起来",
		usbPort, len(result.Tried))
	result.NextStep = "先确认该卡能在别的口上正常出状态（如容器外 / 串口工具），再排查是否接错口；若是容器内无节点，先补 compose devices 并重建容器"
	return result, nil
}

// fillResult 把状态帧的内容翻译成结论 + 配置片段
func (p *ProbeService) fillResult(result *ProbeResult, module *SelfCheckModule, iface SelfCheckInterface, status *StatusData) {
	result.Success = true
	result.AtPort = iface.ByPath
	result.Node = iface.Node
	result.IfKey = iface.IfKey
	result.AtConfirmed = iface.IfKey == AtInterfaceKey

	result.Operator = operatorFromIMSI(status.Mobile.Imsi)
	result.Number = status.Mobile.Number
	result.Iccid = status.Mobile.Iccid
	result.Imsi = status.Mobile.Imsi
	result.SimReady = status.Mobile.SimReady
	result.Registered = status.Mobile.IsRegistered
	result.Roaming = status.Mobile.IsRoaming
	result.Csq = status.Mobile.Csq
	result.Rsrp = status.Mobile.Rsrp
	result.SignalDesc = status.Mobile.SignalDesc
	result.LuaVersion = status.Version

	result.Name = probeDeviceName(result.Operator, result.Number, module)
	result.ConfigSnippet = p.configSnippet(result)

	// 号码读不到是常态（境外/漫游卡不写 SIM 卡 MSISDN 字段），不要当成失败
	parts := make([]string, 0, 3)
	if result.Operator != "" {
		parts = append(parts, result.Operator)
	}
	if result.Number != "" {
		parts = append(parts, result.Number)
	} else {
		parts = append(parts, "号码未写入 SIM 卡")
	}
	if result.Registered {
		parts = append(parts, "已注册网络")
	} else if result.SimReady {
		parts = append(parts, "SIM 就绪但未注册")
	} else {
		parts = append(parts, "SIM 未就绪")
	}
	result.Message = fmt.Sprintf("USB 端口 %s 的 AT 口是 %s：%s",
		result.UsbPort, displayOr(result.Node, result.IfKey), strings.Join(parts, " · "))
	result.NextStep = "把下面的 config 片段加进 config.yaml（compose 的 devices 若还没加这条节点，一并加），然后重建容器"
}

// probeOnce 打开一个接口、发一次 get_status、等到状态帧。
//
// 返回 (nil, nil) 表示「打开成功但没等到帧」，调用方据此区分
// 「口打不开」和「口能开但卡不回话」——这两种故障的下一步完全不同。
func (p *ProbeService) probeOnce(ctx context.Context, byPath string) (*StatusData, error) {
	port, err := p.opener(byPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errProbeOpen, err)
	}
	defer func() {
		_ = port.Close()
	}()

	// 读超时失败不致命：有的实现不认这个设置，那就会阻塞到 window 之后被 ctx 打断
	_ = port.SetReadTimeout(ProbeReadTimeout)

	message, _, err := buildCommandMessage(map[string]string{"action": "get_status"})
	if err != nil {
		return nil, err
	}
	if err := writeAll(port, message); err != nil {
		return nil, fmt.Errorf("写入 get_status 失败: %w", err)
	}

	deadline := time.Now().Add(p.window)
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 2048)

	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if !time.Now().Before(deadline) {
			return nil, nil
		}

		n, readErr := port.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if len(buf) > probeMaxBytes {
				buf = buf[:0]
			}
			for _, frame := range takeProbeFrames(&buf) {
				if status, ok := parseProbeStatus(frame); ok {
					return status, nil
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, fmt.Errorf("串口 EOF（设备可能已复位）")
			}
			// 读超时在不同平台有两种表现：返回 (0, nil) 或以错误形式返回。
			// go.bug.st/serial 没有统一哨兵，所以一律按「还在等」处理，让出一点 CPU。
			// 真的断了（设备复位/重新枚举）会在下一轮 Read 里露出 EOF 或硬错误。
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// configSnippet 生成可粘贴的 config.yaml Devices 片段
func (p *ProbeService) configSnippet(result *ProbeResult) string {
	if result.AtPort == "" {
		return ""
	}

	usedIDs := make(map[string]int, 16)
	for _, dc := range p.devices.GetAllDeviceConfigs() {
		usedIDs[dc.ID]++
	}

	id := suggestDeviceID(result.UsbPort)
	if usedIDs[id] > 0 {
		for i := 2; i < 100; i++ {
			candidate := fmt.Sprintf("%s_%d", id, i)
			if usedIDs[candidate] == 0 {
				id = candidate
				break
			}
		}
	}

	name := result.Name
	if name == "" {
		name = fmt.Sprintf("新卡（%s）", displayOr(result.UsbPort, UnknownUsbPort))
	}

	return fmt.Sprintf("      - ID: \"%s\"\n        Name: \"%s\"\n        Port: \"%s\"\n        Enabled: true\n",
		id, name, result.AtPort)
}

// checkCooldown 返回还需等待的时长（0 = 可以探测）
func (p *ProbeService) checkCooldown(usbPort string) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	last, ok := p.lastProbeAt[usbPort]
	if !ok {
		return 0
	}
	if elapsed := time.Since(last); elapsed < p.cooldown {
		return p.cooldown - elapsed
	}
	return 0
}

// markProbed 记录探测时刻
func (p *ProbeService) markProbed(usbPort string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastProbeAt[usbPort] = time.Now()
}

// findModule 从报告里找目标端口
func findModule(modules []SelfCheckModule, usbPort string) (*SelfCheckModule, bool) {
	for i := range modules {
		if modules[i].UsbPort == usbPort {
			return &modules[i], true
		}
	}
	return nil, false
}

// sortProbeInterfaces 按「1.6 → 1.2 → 1.4 → 其余按号」排序
func sortProbeInterfaces(ifaces []SelfCheckInterface) []SelfCheckInterface {
	out := make([]SelfCheckInterface, len(ifaces))
	copy(out, ifaces)

	rank := func(ifKey string) int {
		for i, k := range probeInterfaceOrder {
			if k == ifKey {
				return i
			}
		}
		return len(probeInterfaceOrder)
	}

	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i].IfKey), rank(out[j].IfKey)
		if ri != rj {
			return ri < rj
		}
		return out[i].IfKey < out[j].IfKey
	})
	return out
}

// takeProbeFrames 从缓冲区切出完整帧，并从缓冲区里消费掉它们。
//
// 与 FrameReader.extract 的思路一致（半包/粘包/噪声），但这里不能直接复用
// FrameReader：它的 ReadFrame 是阻塞语义，探测必须能在 2 秒后主动放弃。
func takeProbeFrames(buf *[]byte) [][]byte {
	frames := make([][]byte, 0, 2)

	for {
		start := bytes.Index(*buf, []byte(smsPrefix))
		if start < 0 {
			// 没有起始标记：清掉噪声，只留可能是标记前缀的尾巴
			if len(*buf) > len(smsPrefix) {
				*buf = append((*buf)[:0], (*buf)[len(*buf)-len(smsPrefix)+1:]...)
			}
			break
		}
		end := bytes.Index((*buf)[start:], []byte(smsSuffix))
		if end < 0 {
			// 帧未收全：保留从起始标记开始的部分，等下一轮 Read
			*buf = append((*buf)[:0], (*buf)[start:]...)
			break
		}

		body := (*buf)[start+len(smsPrefix) : start+end]
		frames = append(frames, append([]byte(nil), body...))
		*buf = append((*buf)[:0], (*buf)[start+end+len(smsSuffix):]...)
	}

	// 兜底：有些模拟器/老固件直接吐裸 JSON（不带 SMS_START 包裹）。
	// 只在没切出任何帧时尝试，避免把正常帧里的 JSON 重复解析一次。
	if len(frames) == 0 && len(*buf) > 0 {
		if start := bytes.IndexByte(*buf, '{'); start >= 0 {
			var raw json.RawMessage
			dec := json.NewDecoder(bytes.NewReader((*buf)[start:]))
			if err := dec.Decode(&raw); err == nil {
				frames = append(frames, raw)
				*buf = append((*buf)[:0], (*buf)[start+int(dec.InputOffset()):]...)
			}
		}
	}

	return frames
}

// parseProbeStatus 解析帧，只要 status_response
func parseProbeStatus(frame []byte) (*StatusData, bool) {
	msg, err := parseSMSFrame(string(frame))
	if err != nil || msg.Type != ProbeStatusType {
		return nil, false
	}
	var status StatusData
	if err := json.Unmarshal([]byte(msg.JSON), &status); err != nil {
		return nil, false
	}
	return &status, true
}

// probeDeviceName 建议的设备显示名
func probeDeviceName(operator, number string, module *SelfCheckModule) string {
	parts := make([]string, 0, 3)
	if operator != "" {
		parts = append(parts, operator)
	}
	if number != "" {
		parts = append(parts, number)
	}
	if len(parts) == 0 {
		// 什么都没读到：写上端口，至少和自检卡片里的名字对得上
		name := fmt.Sprintf("新卡（%s）", displayOr(module.UsbPort, UnknownUsbPort))
		if module.Device != "" {
			name = fmt.Sprintf("新卡（%s %s）", module.Device, module.UsbPort)
		}
		return name
	}
	return strings.Join(parts, " ")
}

// errProbeOpen 包内哨兵：区分「口打不开」和「口能开但不回话」，
// 这两种故障的下一步完全不同（前者补 /dev 节点或改 compose，后者查卡与供电）。
var errProbeOpen = errors.New("打开串口失败")
