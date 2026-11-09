package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/config"
	"go.uber.org/zap"
)

// 设备自检（L0 一致性体检）
//
// 为什么需要它：新增一张卡时，宿主机枚举正常、但应用完全看不到——根因通常是
// 「config.yaml 没写」加「容器 devices 没映射」，两件事都要人手改宿主机文件。
// 程序自己不会报，只能靠人肉 ls / docker exec 排查。
//
// 本服务的做法（全部只读，绝不打开串口，因此不会打扰正在收发短信的设备）：
//
//	① 读 /dev/serial/by-path（宿主机挂载进来的目录，容器内能看到**全部**软链）
//	② 按 usb-<bus>:<port> 分组 —— 同一物理 USB 口 = 一个模组。
//	   三个 AirM2M 模组的 VID/PID/SerialNumber 完全相同，by-id 不可用，
//	   端口号是唯一能区分它们的东西。
//	③ 与 config.yaml 的 Devices 交叉比对：
//	     配置有 + 宿主机有 + 容器有     → ok
//	     宿主机有 + 容器 /dev 里没有    → not_mapped（需要在 compose 里加 devices）
//	     宿主机都没有                   → missing_on_host（卡被拔了 / by-path 变了）
//	     端口没被任何配置引用           → 新模块（新插的卡）
//	④ 顺带校验配置本身：ID 重复、两台设备抢同一个口、Port 没指向 AT 口。
//
// 注意 /dev 与 /sys 的可见性差异，这是整个方案成立的前提：
//   - 容器 /dev 是创建时固定的（未映射的 ttyACM 根本不存在）
//   - 但 /sys/class/tty 是宿主机内核视图，容器里能看到全部 ttyACM*
//   - /dev/serial/by-path 是挂载进来的宿主机目录，软链一条不少
//
// 所以「容器看不到但知道它存在」是可实现的。
const (
	// DefaultByPathDir 宿主机 /dev/serial/by-path 在容器内的位置
	DefaultByPathDir = "/dev/serial/by-path"
	// DefaultSysTTYDir 内核 tty 类目录（容器内可见宿主机全部节点）
	DefaultSysTTYDir = "/sys/class/tty"
	// DefaultDevDir 容器自己的 /dev
	DefaultDevDir = "/dev"
	// AtInterfaceKey 程序实际使用的接口号。
	//
	// AirM2M 每个模组暴露 3 个 ACM 口，短信走第 6 号接口（by-path 名字以 :1.6 结尾）。
	// 这是实测规律、不是规范，所以只用来「提示可疑配置」，不用来决定能不能用。
	AtInterfaceKey = "1.6"
	// UnknownUsbPort 无法解析端口号时的分组键
	UnknownUsbPort = "unknown"
)

// SelfCheckItem 单条体检结论
type SelfCheckItem struct {
	// Level ok | info | warn | error
	Level string `json:"level"`
	// Code 机器可读的问题码，便于前端高亮与后续自动化
	Code string `json:"code"`
	// Message 给人看的结论（中文，一句话说清「哪里不对」）
	Message string `json:"message"`
	// NextStep 可执行的下一步（命令或操作），没有则为空
	NextStep string `json:"next_step,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	UsbPort  string `json:"usb_port,omitempty"`
}

// SelfCheckDevice 已配置设备的体检结果
type SelfCheckDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Port string `json:"port"`
	// Enabled 来自 config.yaml；禁用的设备不参与收发，但仍会体检
	Enabled bool `json:"enabled"`
	// UsbPort 解析出的物理端口（usb-<bus>:<port>）
	UsbPort string `json:"usb_port,omitempty"`
	// Node 解析出的 ttyACM 节点名
	Node string `json:"node,omitempty"`
	// OnHost by-path 软链在宿主机上存在
	OnHost bool `json:"on_host"`
	// InContainer 节点在容器 /dev 里存在（= 打不打得开的前提）
	InContainer bool `json:"in_container"`
	// Status 状态码：ok | not_mapped | missing_on_host | node_only | disabled | auto | invalid
	Status string `json:"status"`
	// StatusText 状态的中文说明
	StatusText string `json:"status_text"`
}

// SelfCheckInterface 一个 by-path 接口
type SelfCheckInterface struct {
	// ByPath 完整路径，可直接写进 config.yaml 的 Port
	ByPath string `json:"by_path"`
	// IfKey 接口号，如 "1.6"
	IfKey string `json:"if_key"`
	// Node ttyACM 节点名
	Node string `json:"node"`
	// InContainer 该节点在容器 /dev 里是否存在
	InContainer bool `json:"in_container"`
}

// SelfCheckModule 一个物理模组（按 USB 端口分组）
type SelfCheckModule struct {
	UsbPort string `json:"usb_port"`
	// Device 型号名（来自 /sys 的 manufacturer/product，取不到为空）
	Device    string `json:"device,omitempty"`
	IDVendor  string `json:"id_vendor,omitempty"`
	IDProduct string `json:"id_product,omitempty"`
	// Interfaces 该端口下的全部 by-path 接口
	Interfaces []SelfCheckInterface `json:"interfaces"`
	// CandidatePort 候选 AT 口（优先 :1.6），可直接写进 config.yaml
	CandidatePort string `json:"candidate_port,omitempty"`
	// CandidateNode 候选 AT 口对应的 ttyACM 节点名
	CandidateNode string `json:"candidate_node,omitempty"`
	// AtConfirmed 候选口是否按 :1.6 规律识别（false = 只是猜的，需要人工确认）
	AtConfirmed bool `json:"at_confirmed"`
	// InContainer 候选口节点在容器 /dev 里是否存在
	InContainer bool `json:"in_container"`
	// Configured 该端口是否已被 config.yaml 的某个设备引用
	Configured bool `json:"configured"`
	// DeviceIDs 引用该端口的已配置设备 ID
	DeviceIDs []string `json:"device_ids,omitempty"`
	// ComposeSnippet 可直接贴进 docker-compose.yml 的 devices 片段
	ComposeSnippet string `json:"compose_snippet,omitempty"`
	// ConfigSnippet 可直接贴进 config.yaml 的 Devices 片段
	ConfigSnippet string `json:"config_snippet,omitempty"`
}

// SelfCheckSummary 汇总计数
type SelfCheckSummary struct {
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Error int `json:"error"`
	Info  int `json:"info"`
}

// SelfCheckReport 体检报告
type SelfCheckReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	// ByPathAvailable 宿主机 by-path 目录是否可读（未挂载时为 false，只能做容器内检查）
	ByPathAvailable bool `json:"by_path_available"`
	// SysAvailable /sys/class/tty 是否可读
	SysAvailable bool `json:"sys_available"`
	Devices      []SelfCheckDevice  `json:"devices"`
	Modules      []SelfCheckModule  `json:"modules"`
	Items        []SelfCheckItem    `json:"items"`
	Summary      SelfCheckSummary   `json:"summary"`
	// UnconfiguredCount 未被任何配置引用的模组数量（= 新卡数量）
	UnconfiguredCount int `json:"unconfigured_count"`
}

// SelfCheckService 设备自检服务
type SelfCheckService struct {
	logger    *zap.Logger
	byPathDir string
	sysTTYDir string
	devDir    string

	mu     sync.Mutex
	last   *SelfCheckReport
	lastAt time.Time
}

// NewSelfCheckService 创建自检服务（默认路径适配容器部署）
func NewSelfCheckService(logger *zap.Logger) *SelfCheckService {
	return &SelfCheckService{
		logger:    logger,
		byPathDir: DefaultByPathDir,
		sysTTYDir: DefaultSysTTYDir,
		devDir:    DefaultDevDir,
	}
}

// byPathEntry 一条 by-path 软链
type byPathEntry struct {
	name    string
	usbPort string
	ifKey   string
	node    string
}

// Run 执行一次体检。
//
// deviceConfigs 直接来自 DeviceManager.GetAllDeviceConfigs()，
// 这样「界面上看到的设备列表」与「体检用的设备列表」保证是同一份。
func (s *SelfCheckService) Run(deviceConfigs []config.DeviceConfig) *SelfCheckReport {
	report := &SelfCheckReport{
		GeneratedAt: time.Now(),
		Devices:     make([]SelfCheckDevice, 0, len(deviceConfigs)),
		Modules:     make([]SelfCheckModule, 0),
		Items:       make([]SelfCheckItem, 0),
	}

	entries, err := s.scanByPath()
	if err != nil {
		report.ByPathAvailable = false
		report.Items = append(report.Items, SelfCheckItem{
			Level:    "warn",
			Code:     "BYPATH_UNAVAILABLE",
			Message:  fmt.Sprintf("读不到宿主机串口目录 %s：%v", s.byPathDir, err),
			NextStep: "确认 docker-compose.yml 里有 volumes: - /dev/serial:/dev/serial:ro，然后重建容器",
		})
	} else {
		report.ByPathAvailable = true
	}

	sysNodes, sysErr := s.scanSysTTY()
	if sysErr != nil {
		report.SysAvailable = false
	} else {
		report.SysAvailable = true
	}

	// ① 按物理端口分组
	modules := s.groupModules(entries)
	// ② 已配置设备逐个体检（同时标记哪些端口被引用）
	portOwners := make(map[string][]string)
	usedIDs := make(map[string]int)
	usedPorts := make(map[string][]string)
	for _, dc := range deviceConfigs {
		usedIDs[dc.ID]++
		if dc.Port != "" {
			usedPorts[dc.Port] = append(usedPorts[dc.Port], dc.ID)
		}

		d := s.checkDevice(dc, entries)
		report.Devices = append(report.Devices, d)
		if d.UsbPort != "" {
			portOwners[d.UsbPort] = append(portOwners[d.UsbPort], dc.ID)
		}
	}

	// ③ 模块侧：标记是否被引用，给未引用的生成修复片段
	unconfigured := 0
	for i := range modules {
		m := &modules[i]
		owners, ok := portOwners[m.UsbPort]
		if ok {
			m.Configured = true
			m.DeviceIDs = owners
		} else {
			m.Configured = false
			unconfigured++
			m.ComposeSnippet = s.composeSnippet(m.CandidateNode)
			m.ConfigSnippet = s.configSnippet(m, usedIDs)
		}
	}

	report.Modules = modules
	report.UnconfiguredCount = unconfigured

	// ④ 结论清单
	report.Items = append(report.Items, s.buildDeviceItems(report.Devices, usedIDs, usedPorts)...)
	report.Items = append(report.Items, s.buildModuleItems(modules)...)

	// ⑤ 汇总 + 全局降级提示
	for _, it := range report.Items {
		switch it.Level {
		case "ok":
			report.Summary.OK++
		case "warn":
			report.Summary.Warn++
		case "error":
			report.Summary.Error++
		default:
			report.Summary.Info++
		}
	}
	if !report.SysAvailable && sysErr != nil {
		report.Items = append(report.Items, SelfCheckItem{
			Level:   "info",
			Code:    "SYS_UNAVAILABLE",
			Message: fmt.Sprintf("读不到 %s：%v（不影响体检主流程）", s.sysTTYDir, sysErr),
		})
	}
	if len(sysNodes) > 0 {
		// 只用于日志：把「内核里有多少个 ttyACM」打出来，方便对照
		s.logger.Debug("自检：宿主机 tty 节点", zap.Int("count", len(sysNodes)))
	}

	// ⑥ 缓存 + 日志摘要
	s.mu.Lock()
	s.last = report
	s.lastAt = report.GeneratedAt
	s.mu.Unlock()

	s.logger.Info("设备自检完成",
		zap.Int("devices", len(report.Devices)),
		zap.Int("modules", len(report.Modules)),
		zap.Int("unconfigured", report.UnconfiguredCount),
		zap.Int("warn", report.Summary.Warn),
		zap.Int("error", report.Summary.Error))

	return report
}

// Last 返回上一次体检结果（还没跑过则返回 nil）
func (s *SelfCheckService) Last() *SelfCheckReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// scanByPath 枚举宿主机 by-path 软链
func (s *SelfCheckService) scanByPath() ([]byPathEntry, error) {
	dentries, err := os.ReadDir(s.byPathDir)
	if err != nil {
		return nil, err
	}

	out := make([]byPathEntry, 0, len(dentries))
	for _, de := range dentries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		link, err := os.Readlink(filepath.Join(s.byPathDir, name))
		if err != nil {
			// 软链坏掉（目标节点已被移除）时也要保留信息，只是 node 拿不到
			link = ""
		}
		e := byPathEntry{name: name, node: filepath.Base(link)}
		if link == "" {
			e.node = ""
		}
		e.usbPort, e.ifKey = parseByPathName(name)
		out = append(out, e)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// scanSysTTY 列出内核里全部 ttyACM 节点（容器内可见宿主机全部设备）
func (s *SelfCheckService) scanSysTTY() ([]string, error) {
	dentries, err := os.ReadDir(s.sysTTYDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(dentries))
	for _, de := range dentries {
		n := de.Name()
		if strings.HasPrefix(n, "ttyACM") {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out, nil
}

// groupModules 把 by-path 接口按物理 USB 端口聚合成「模组」
func (s *SelfCheckService) groupModules(entries []byPathEntry) []SelfCheckModule {
	byPort := make(map[string][]byPathEntry)
	order := make([]string, 0, len(entries))
	for _, e := range entries {
		port := e.usbPort
		if port == "" {
			port = UnknownUsbPort
		}
		if _, ok := byPort[port]; !ok {
			order = append(order, port)
		}
		byPort[port] = append(byPort[port], e)
	}

	modules := make([]SelfCheckModule, 0, len(order))
	for _, port := range order {
		list := byPort[port]
		m := SelfCheckModule{
			UsbPort:    port,
			Interfaces: make([]SelfCheckInterface, 0, len(list)),
		}

		for _, e := range list {
			m.Interfaces = append(m.Interfaces, SelfCheckInterface{
				ByPath:      filepath.Join(s.byPathDir, e.name),
				IfKey:       e.ifKey,
				Node:        e.node,
				InContainer: s.nodeInContainer(e.node),
			})
		}

		// 候选 AT 口：优先接口号 1.6（实测规律），否则取第一个可用接口并标记「待确认」
		var candidate *SelfCheckInterface
		for i := range m.Interfaces {
			if m.Interfaces[i].IfKey == AtInterfaceKey {
				candidate = &m.Interfaces[i]
				break
			}
		}
		if candidate == nil && len(m.Interfaces) > 0 {
			candidate = &m.Interfaces[0]
		}
		if candidate != nil {
			m.CandidatePort = candidate.ByPath
			m.CandidateNode = candidate.Node
			m.AtConfirmed = candidate.IfKey == AtInterfaceKey
			m.InContainer = candidate.InContainer
			// 型号信息来自 /sys，取不到就留空（不影响其他判定）
			m.IDVendor, m.IDProduct, m.Device = s.sysIdentity(candidate.Node)
		}

		modules = append(modules, m)
	}
	return modules
}

// checkDevice 单个已配置设备体检
func (s *SelfCheckService) checkDevice(dc config.DeviceConfig, entries []byPathEntry) SelfCheckDevice {
	d := SelfCheckDevice{
		ID:      dc.ID,
		Name:    dc.Name,
		Port:    dc.Port,
		Enabled: dc.Enabled,
	}

	if dc.ID == "" {
		d.Status = "invalid"
		d.StatusText = "设备 ID 为空"
		return d
	}
	if dc.Port == "" {
		// 旧配置：不写 Port 表示自动检测（config.Serial.Port 兼容模式）
		d.Status = "auto"
		d.StatusText = "未指定串口（自动检测）"
		return d
	}

	// 先按 by-path 精确匹配，再退化到「直接写了 ttyACM 名」
	var matched *byPathEntry
	base := filepath.Base(dc.Port)
	for i := range entries {
		if entries[i].name == base || (entries[i].node != "" && entries[i].node == base) {
			matched = &entries[i]
			break
		}
	}

	if matched != nil {
		d.UsbPort = matched.usbPort
		d.Node = matched.node
		d.OnHost = true
		d.InContainer = s.nodeInContainer(matched.node)
	} else {
		// by-path 里找不到：也许人直接写了 /dev/ttyACMx，且容器里恰好有
		d.Node = base
		d.OnHost = fileExists(dc.Port)
		d.InContainer = s.nodeInContainer(d.Node)
	}

	switch {
	case !dc.Enabled && d.OnHost && d.InContainer:
		d.Status = "disabled"
		d.StatusText = "已禁用（串口正常，启用后即可收发）"
	case !dc.Enabled:
		d.Status = "disabled"
		d.StatusText = "已禁用"
	case d.OnHost && d.InContainer:
		d.Status = "ok"
		d.StatusText = "正常"
	case d.OnHost && !d.InContainer:
		d.Status = "not_mapped"
		d.StatusText = "宿主机有该串口，但容器内没有节点"
	case !d.OnHost && d.InContainer:
		d.Status = "node_only"
		d.StatusText = "容器内有节点，但 by-path 已不存在（可能换了 USB 口）"
	default:
		d.Status = "missing_on_host"
		d.StatusText = "宿主机上找不到该串口"
	}

	return d
}

// buildDeviceItems 已配置设备的问题清单
func (s *SelfCheckService) buildDeviceItems(devices []SelfCheckDevice, usedIDs map[string]int, usedPorts map[string][]string) []SelfCheckItem {
	items := make([]SelfCheckItem, 0, len(devices)+4)

	// 配置本身的冲突：ID 重复 / 端口抢占
	for id, n := range usedIDs {
		if n > 1 {
			items = append(items, SelfCheckItem{
				Level:    "error",
				Code:     "CONFIG_DUP_ID",
				Message:  fmt.Sprintf("设备 ID「%s」在 config.yaml 里出现了 %d 次，只有一条能生效", id, n),
				NextStep: "改 config.yaml，把重复的 ID 换成唯一值后重建容器",
				DeviceID: id,
			})
		}
	}
	for port, ids := range usedPorts {
		if len(ids) > 1 {
			items = append(items, SelfCheckItem{
				Level:    "error",
				Code:     "CONFIG_DUP_PORT",
				Message:  fmt.Sprintf("串口 %s 被多个设备同时占用：%s", port, strings.Join(ids, ", ")),
				NextStep: "每张卡必须对应独立的 by-path，改 config.yaml 后重建容器",
			})
		}
	}

	for _, d := range devices {
		switch d.Status {
		case "ok":
			items = append(items, SelfCheckItem{
				Level:    "ok",
				Code:     "DEVICE_OK",
				Message:  fmt.Sprintf("%s（%s）串口正常", displayOr(d.Name, d.ID), d.Port),
				DeviceID: d.ID,
				UsbPort:  d.UsbPort,
			})
		case "not_mapped":
			items = append(items, SelfCheckItem{
				Level:    "error",
				Code:     "DEVICE_NOT_MAPPED",
				Message:  fmt.Sprintf("%s（%s）在宿主机上存在，但容器内没有 /dev/%s，程序打不开这个口", displayOr(d.Name, d.ID), d.Port, d.Node),
				NextStep: fmt.Sprintf("在 docker-compose.yml 的 devices 里加一行 - /dev/%s:/dev/%s，再执行 docker compose up -d --force-recreate（会中断在跑的卡几十秒）", d.Node, d.Node),
				DeviceID: d.ID,
				UsbPort:  d.UsbPort,
			})
		case "missing_on_host":
			items = append(items, SelfCheckItem{
				Level:    "error",
				Code:     "DEVICE_MISSING_ON_HOST",
				Message:  fmt.Sprintf("%s（%s）的串口在宿主机上不存在，卡可能被拔掉或换了 USB 口", displayOr(d.Name, d.ID), d.Port),
				NextStep: "重新插好卡后用 ls -l /dev/serial/by-path/ 核对，把新的 by-path 写回 config.yaml",
				DeviceID: d.ID,
			})
		case "node_only":
			items = append(items, SelfCheckItem{
				Level:    "warn",
				Code:     "DEVICE_PORT_CHANGED",
				Message:  fmt.Sprintf("%s（%s）的 by-path 已失效，但容器里仍有 /dev/%s；配置和实际设备可能对不上", displayOr(d.Name, d.ID), d.Port, d.Node),
				NextStep: "核对 ls -l /dev/serial/by-path/，确认这台设备现在对应哪个 by-path",
				DeviceID: d.ID,
			})
		case "auto":
			items = append(items, SelfCheckItem{
				Level:    "warn",
				Code:     "DEVICE_AUTO_PORT",
				Message:  fmt.Sprintf("%s 没有写 Port，多卡场景下会自动挑一个串口，可能落到非预期的卡上", displayOr(d.Name, d.ID)),
				NextStep: "给该设备补上明确的 by-path",
				DeviceID: d.ID,
			})
		case "invalid":
			items = append(items, SelfCheckItem{
				Level:    "error",
				Code:     "DEVICE_INVALID_ID",
				Message:  "config.yaml 里有设备的 ID 为空",
				NextStep: "补上唯一 ID 后重建容器",
			})
		}

		// 指向的不是 AT 口：能开但不一定能收发短信，属于「可疑但不致命」
		if d.OnHost && d.UsbPort != "" && !strings.HasSuffix(d.Port, ":"+AtInterfaceKey) && d.Port != "" {
			items = append(items, SelfCheckItem{
				Level:    "info",
				Code:     "DEVICE_NON_AT_PORT",
				Message:  fmt.Sprintf("%s 的 Port 不是以 :%s 结尾（实测短信走该接口），若收发异常请先核对这里", displayOr(d.Name, d.ID), AtInterfaceKey),
				DeviceID: d.ID,
				UsbPort:  d.UsbPort,
			})
		}
	}

	return items
}

// buildModuleItems 模组（含未配置的新卡）的问题清单
func (s *SelfCheckService) buildModuleItems(modules []SelfCheckModule) []SelfCheckItem {
	items := make([]SelfCheckItem, 0, len(modules))
	for _, m := range modules {
		if m.Configured {
			continue
		}

		name := m.Device
		if name == "" {
			name = "型号未识别"
		}
		msg := fmt.Sprintf("发现未配置的模组：USB 端口 %s（%s）", displayOr(m.UsbPort, UnknownUsbPort), name)
		if m.CandidateNode != "" {
			msg += fmt.Sprintf("，候选 AT 口 %s", m.CandidateNode)
		}
		if !m.AtConfirmed {
			msg += "（未按 :1.6 规律匹配，需人工确认哪个是 AT 口）"
		}

		next := ""
		if !m.InContainer {
			next = "该口在宿主机存在但容器内缺失：先按 compose 片段加 devices 并重建容器，再把它加进 config.yaml"
		} else {
			next = "该口容器内可见：把 config 片段加进 config.yaml 后重建容器即可启用"
		}

		items = append(items, SelfCheckItem{
			Level:    "warn",
			Code:     "MODULE_UNCONFIGURED",
			Message:  msg,
			NextStep: next,
			UsbPort:  m.UsbPort,
		})
	}
	return items
}

// composeSnippet 生成 docker-compose.yml 的 devices 片段（缩进按仓库里的写法：6 空格）
func (s *SelfCheckService) composeSnippet(node string) string {
	if node == "" {
		return ""
	}
	return fmt.Sprintf("      - /dev/%s:/dev/%s\n", node, node)
}

// configSnippet 生成 config.yaml 的 Devices 条目（缩进按 config.example.yaml：6 空格）
func (s *SelfCheckService) configSnippet(m *SelfCheckModule, usedIDs map[string]int) string {
	if m.CandidatePort == "" {
		return ""
	}

	id := suggestDeviceID(m.UsbPort)
	// 避免和已有 ID 撞车：撞了就加后缀
	if usedIDs[id] > 0 {
		for i := 2; i < 100; i++ {
			candidate := fmt.Sprintf("%s_%d", id, i)
			if usedIDs[candidate] == 0 {
				id = candidate
				break
			}
		}
	}

	name := fmt.Sprintf("新卡（%s）", displayOr(m.UsbPort, UnknownUsbPort))
	if m.Device != "" {
		name = fmt.Sprintf("新卡（%s %s）", m.Device, m.UsbPort)
	}

	return fmt.Sprintf("      - ID: \"%s\"\n        Name: \"%s\"\n        Port: \"%s\"\n        Enabled: true\n",
		id, name, m.CandidatePort)
}

// sysIdentity 从 /sys 向上读模组的 VID/PID/型号。
//
// ttyACMx/device 指向 USB interface，idVendor 在它的父目录（USB device）里；
// 不同内核版本层级略有差异，所以向上找几层，读不到就返回空——纯展示用途，不能因此报错。
func (s *SelfCheckService) sysIdentity(node string) (vendor, product, device string) {
	if node == "" {
		return "", "", ""
	}

	dir := filepath.Join(s.sysTTYDir, node, "device")
	for i := 0; i < 5; i++ {
		if v, err := os.ReadFile(filepath.Join(dir, "idVendor")); err == nil {
			vendor = strings.TrimSpace(string(v))
		}
		if p, err := os.ReadFile(filepath.Join(dir, "idProduct")); err == nil {
			product = strings.TrimSpace(string(p))
		}
		if m, err := os.ReadFile(filepath.Join(dir, "manufacturer")); err == nil {
			device = strings.TrimSpace(string(m))
		}
		if vendor != "" || device != "" {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "/" {
			break
		}
		dir = parent
	}

	// 厂商名常常是空的，退一步用 product 字段
	if device == "" {
		if p, err := os.ReadFile(filepath.Join(s.sysTTYDir, node, "device", "product")); err == nil {
			device = strings.TrimSpace(string(p))
		}
	}
	return vendor, product, device
}

// nodeInContainer 判断节点是否出现在容器自己的 /dev 里
func (s *SelfCheckService) nodeInContainer(node string) bool {
	if node == "" {
		return false
	}
	return fileExists(filepath.Join(s.devDir, node))
}

// parseByPathName 从 by-path 名字解析物理端口与接口号。
//
// 典型名字：pci-0000:07:1b.0-usb-0:1:1.6
//
//	→ usbPort = "usb-0:1"（同一物理口 = 同一模组）
//	→ ifKey   = "1.6"    （第 1 个配置的第 6 号接口）
//
// 解析不出来时返回空串，调用方会归入 unknown 组（只做展示，不参与新卡判定）。
func parseByPathName(name string) (usbPort, ifKey string) {
	idx := strings.LastIndex(name, "-usb-")
	tail := name
	if idx >= 0 {
		tail = name[idx+len("-usb-"):]
	} else if !strings.HasPrefix(name, "usb-") {
		return "", ""
	}

	parts := strings.Split(tail, ":")
	if len(parts) < 2 {
		return "", ""
	}

	usbPort = "usb-" + parts[0] + ":" + parts[1]
	last := parts[len(parts)-1]
	if strings.Contains(last, ".") {
		ifKey = last
	}
	return usbPort, ifKey
}

// suggestDeviceID 用端口号派生设备 ID（usb-0:1 → usb01）。
//
// 刻意不用递增序号：同一张卡插同一个 USB 口时 ID 稳定，
// 不会因为「先加哪张卡」而变号，配置和 API 调用方都好对齐。
func suggestDeviceID(usbPort string) string {
	tail := strings.TrimPrefix(usbPort, "usb-")
	replacer := strings.NewReplacer(":", "", ".", "", "-", "")
	cleaned := replacer.Replace(tail)
	if cleaned == "" {
		return "usbX"
	}
	return "usb" + cleaned
}

// fileExists 节点是否存在（不用 os.Stat 的错误类型判断，软链坏掉也算不存在）
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// displayOr 空值兜底
func displayOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
