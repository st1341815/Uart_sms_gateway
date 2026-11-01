package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 双维度限流默认参数
const (
	// 同一手机号两条之间的最小间隔
	PhoneMinInterval = 60 * time.Second
	// 同一手机号 24 小时内最多发送条数
	PhoneDailyLimit = 10
)

// 中国大陆手机号（宽松校验：11 位、1 开头）
var phoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

// OpenAPIService 对外开放短信接口的核心逻辑
type OpenAPIService struct {
	logger        *zap.Logger
	db            *gorm.DB
	deviceManager *DeviceManager
	templates     *TemplateService
	// 按 Key 的每分钟计数（内存滑动窗口，重启即重置；日维度由数据库兜底）
	keyWindows map[string][]time.Time
	mu         sync.Mutex
}

func NewOpenAPIService(logger *zap.Logger, db *gorm.DB, dm *DeviceManager, tpl *TemplateService) *OpenAPIService {
	return &OpenAPIService{
		logger:        logger,
		db:            db,
		deviceManager: dm,
		templates:     tpl,
		keyWindows:    make(map[string][]time.Time),
	}
}

// SendRequest 发送请求
type SendRequest struct {
	Phone      string            `json:"phone"`
	Template   string            `json:"template"`
	Params     map[string]string `json:"params"`
	// Content 不走模板时直接传内容（二选一）
	Content string `json:"content"`
}

// SendResult 发送结果
type SendResult struct {
	MessageID  string `json:"messageId"`
	Phone      string `json:"phone"`
	DeviceID   string `json:"deviceId"`
	Template   string `json:"template,omitempty"`
	Rendered   string `json:"rendered,omitempty"`
	Status     string `json:"status"`
}

// Send 执行发送：校验 → 限流 → 渲染模板 → 提交串口 → 落库
func (s *OpenAPIService) Send(ctx context.Context, key *models.APIKey, req SendRequest, clientIP string) (*SendResult, error) {
	// 1. 手机号校验
	phone := normalizePhone(req.Phone)
	if phone == "" {
		s.logRejected(ctx, key, req, clientIP, "手机号不能为空")
		return nil, NewAPIError(400, "INVALID_PHONE", "手机号不能为空")
	}
	if !phoneRe.MatchString(phone) {
		s.logRejected(ctx, key, req, clientIP, "手机号格式非法")
		return nil, NewAPIError(400, "INVALID_PHONE", "手机号格式非法（需为 11 位中国大陆手机号）")
	}

	// 2. 设备校验（Key 绑定的设备）
	if _, ok := s.deviceManager.GetDevice(key.DeviceID); !ok {
		s.logRejected(ctx, key, req, clientIP, "绑定设备不存在")
		return nil, NewAPIError(500, "DEVICE_UNAVAILABLE", "该 Key 绑定的设备不存在: "+key.DeviceID)
	}

	// 3. 内容准备（模板优先）
	var content, templateCode string
	if strings.TrimSpace(req.Template) != "" {
		tpl, err := s.templates.GetByCode(ctx, req.Template)
		if err != nil {
			s.logRejected(ctx, key, req, clientIP, err.Error())
			return nil, NewAPIError(400, "TEMPLATE_NOT_FOUND", err.Error())
		}
		rendered, err := RenderTemplate(tpl.Content, req.Params)
		if err != nil {
			s.logRejected(ctx, key, req, clientIP, err.Error())
			return nil, NewAPIError(400, "TEMPLATE_PARAM_MISSING", err.Error())
		}
		content = rendered
		templateCode = tpl.Code
	} else if strings.TrimSpace(req.Content) != "" {
		content = strings.TrimSpace(req.Content)
	} else {
		s.logRejected(ctx, key, req, clientIP, "缺少 template 或 content")
		return nil, NewAPIError(400, "EMPTY_CONTENT", "必须提供 template（+params）或 content")
	}

	// 短信长度保护
	if n := len([]rune(content)); n > 500 {
		s.logRejected(ctx, key, req, clientIP, "内容过长")
		return nil, NewAPIError(400, "CONTENT_TOO_LONG", fmt.Sprintf("短信内容过长（%d 字，最多 500 字）", n))
	}

	// 4. 双维度限流
	if err := s.checkPhoneLimit(ctx, phone); err != nil {
		s.logRejected(ctx, key, req, clientIP, err.Error())
		return nil, err
	}
	if err := s.checkKeyLimit(key); err != nil {
		s.logRejected(ctx, key, req, clientIP, err.Error())
		return nil, err
	}

	// 5. 提交串口
	msgID, err := s.deviceManager.SendSMS(key.DeviceID, phone, content)
	if err != nil {
		s.logRejected(ctx, key, req, clientIP, "提交失败: "+err.Error())
		// 按错误类型映射状态码，便于调用方决定是否重试
		switch {
		case isRateLimitErr(err):
			// 设备级令牌桶（防刷）触发
			return nil, NewAPIError(429, "SEND_RATE_LIMITED", err.Error())
		case isDeviceOfflineErr(err):
			return nil, NewAPIError(503, "DEVICE_OFFLINE",
				"发送设备当前不可用，请稍后重试")
		default:
			return nil, NewAPIError(500, "SEND_FAILED", "短信提交失败: "+err.Error())
		}
	}

	// 6. 记录（成功）
	s.recordSend(ctx, key, phone, templateCode, content, msgID, clientIP, "accepted", "")

	s.logger.Info("开放接口发送短信",
		zap.String("access_key", key.AccessKey),
		zap.String("phone", maskPhone(phone)),
		zap.String("device_id", key.DeviceID),
		zap.String("template", templateCode),
		zap.String("message_id", msgID))

	return &SendResult{
		MessageID: msgID,
		Phone:     maskPhone(phone),
		DeviceID:  key.DeviceID,
		Template:  templateCode,
		Rendered:  maskContent(content),
		Status:    "accepted",
	}, nil
}

// checkPhoneLimit 按手机号限流：60 秒 1 条 + 24 小时 10 条
func (s *OpenAPIService) checkPhoneLimit(ctx context.Context, phone string) error {
	now := time.Now()

	// 24 小时条数（查库，重启不失效）
	var daily int64
	if err := s.db.WithContext(ctx).Model(&models.APISendLog{}).
		Where("phone = ? AND status = ? AND created_at > ?",
			phone, "accepted", now.Add(-24*time.Hour).UnixMilli()).
		Count(&daily).Error; err != nil {
		s.logger.Error("统计手机号日发送量失败", zap.Error(err))
		// 统计失败时不阻断业务，但要记录
	}
	if daily >= PhoneDailyLimit {
		return NewAPIError(429, "PHONE_DAILY_LIMIT",
			fmt.Sprintf("该手机号 24 小时内发送次数已达上限（%d 条）", PhoneDailyLimit))
	}

	// 60 秒间隔
	var last models.APISendLog
	err := s.db.WithContext(ctx).
		Where("phone = ? AND status = ?", phone, "accepted").
		Order("created_at DESC").Limit(1).Find(&last).Error
	if err == nil && last.CreatedAt > 0 {
		elapsed := now.Sub(time.UnixMilli(last.CreatedAt))
		if elapsed < PhoneMinInterval {
			wait := PhoneMinInterval - elapsed
			return NewAPIError(429, "PHONE_TOO_FREQUENT",
				fmt.Sprintf("该手机号发送过于频繁，请 %d 秒后重试", int(wait.Seconds())+1))
		}
	}

	return nil
}

// checkKeyLimit 按 Key 限流：每分钟调用次数
func (s *OpenAPIService) checkKeyLimit(key *models.APIKey) error {
	limit := key.RateLimitPerMinute
	if limit <= 0 {
		limit = DefaultRateLimitPerMinute
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	// 滑动窗口：丢弃 1 分钟前的记录
	times := s.keyWindows[key.ID]
	kept := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}

	if len(kept) >= limit {
		s.keyWindows[key.ID] = kept
		return NewAPIError(429, "KEY_RATE_LIMIT",
			fmt.Sprintf("该 AccessKey 每分钟最多调用 %d 次，请稍后重试", limit))
	}

	s.keyWindows[key.ID] = append(kept, now)
	return nil
}

// ---- 记录 ----

func (s *OpenAPIService) recordSend(ctx context.Context, key *models.APIKey, phone, tpl, content, msgID, ip, status, reason string) {
	log := &models.APISendLog{
		ID:         uuid.NewString(),
		KeyID:      key.ID,
		AccessKey:  key.AccessKey,
		DeviceID:   key.DeviceID,
		Phone:      phone,
		TemplateID: tpl,
		Content:    content,
		Status:     status,
		Reason:     reason,
		MessageID:  msgID,
		ClientIP:   ip,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := s.db.WithContext(ctx).Create(log).Error; err != nil {
		s.logger.Error("保存开放接口发送记录失败", zap.Error(err))
	}
}

// logRejected 记录被拒绝的请求（便于排查调用方问题）
func (s *OpenAPIService) logRejected(ctx context.Context, key *models.APIKey, req SendRequest, ip, reason string) {
	phone := normalizePhone(req.Phone)
	s.recordSend(ctx, key, phone, req.Template, "", "", ip, "rejected", reason)

	s.logger.Warn("开放接口请求被拒绝",
		zap.String("access_key", key.AccessKey),
		zap.String("phone", maskPhone(phone)),
		zap.String("ip", ip),
		zap.String("reason", reason))
}

// ---- 辅助 ----

func normalizePhone(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, " ", "")
	p = strings.ReplaceAll(p, "-", "")
	// +86 / 86 前缀归一
	p = strings.TrimPrefix(p, "+86")
	if len(p) == 13 && strings.HasPrefix(p, "86") {
		p = p[2:]
	}
	return p
}

// maskPhone 日志脱敏：138****8000
func maskPhone(p string) string {
	if len(p) != 11 {
		return p
	}
	return p[:3] + "****" + p[7:]
}

// maskContent 内容脱敏：只保留长度信息，避免验证码进日志
func maskContent(c string) string {
	n := len([]rune(c))
	if n <= 8 {
		return strings.Repeat("*", n)
	}
	return fmt.Sprintf("%s…（共 %d 字）", strings.Repeat("*", 8), n)
}

// RecentLogs 最近的发送记录（含被拒绝的），供管理端排查
func (s *OpenAPIService) RecentLogs(ctx context.Context, limit int) ([]models.APISendLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	var logs []models.APISendLog
	if err := s.db.WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	if logs == nil {
		logs = []models.APISendLog{}
	}
	// 内容脱敏后再返回给前端，避免验证码在管理界面明文展示
	for i := range logs {
		logs[i].Content = maskContent(logs[i].Content)
		logs[i].Phone = maskPhone(logs[i].Phone)
	}
	return logs, nil
}

// isRateLimitErr 判断是否为设备级限流错误（防刷令牌桶）。
func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "发送过于频繁") || strings.Contains(msg, "发送频率超限")
}

// isDeviceOfflineErr 判断是否为「设备暂时不可用」类错误。
// 这类错误调用方稍后重试即可恢复，应当返回 503 而不是 500。
func isDeviceOfflineErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, kw := range []string{"串口未连接", "串口写入失败", "device not available"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}
