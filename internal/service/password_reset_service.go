package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PropertyIDPasswordResetPhone 「安全手机号」的 property 键。
//
// 为什么不放 config.yaml：容器内 config.yaml 是只读挂载，
// 程序写不回去（详见 models/user_credential.go 的注释）。
// 口令相关的可变状态一律落 properties 表。
const PropertyIDPasswordResetPhone = "password_reset_phone"

// 短信重置的最小间隔，防止误点/脚本刷短信把话费打光。
const passwordResetCooldown = 60 * time.Second

// phonePattern 手机号校验。
//
// 故意放宽到 8-15 位数字并允许 + 国际区号：本项目的场景里存在境外漫游卡
// （见 device_manager.go 的注释），管理员手机未必是 +86。
// 格式合法性交给运营商判断，这里只挡住明显是垃圾输入的情况。
var phonePattern = regexp.MustCompile(`^\+?\d{8,15}$`)

// SMSSender 短信重置所依赖的最小发送能力。
//
// 抽成接口而不是直接吃 *DeviceManager，是为了让「发送失败时口令必须保持不变」
// 这条安全承诺可以被单元测试覆盖——真机发短信没法在测试里跑。
// *DeviceManager 天然满足该接口。
type SMSSender interface {
	SendSMS(deviceID, to, content string) (string, error)
	GetDeviceStatus(id string) (*DeviceStatus, error)
	GetAllDeviceIDs() []string
}

// PasswordResetService 通过短信把新口令下发给管理员。
type PasswordResetService struct {
	logger          *zap.Logger
	credentials     *CredentialService
	sms             SMSSender
	propertyService *PropertyService

	mu     sync.Mutex
	lastAt map[string]time.Time // username -> 上次重置时间

	cooldown time.Duration
}

// NewPasswordResetService 创建短信重置服务
func NewPasswordResetService(
	logger *zap.Logger,
	credentials *CredentialService,
	sms SMSSender,
	propertyService *PropertyService,
) *PasswordResetService {
	return &PasswordResetService{
		logger:          logger,
		credentials:     credentials,
		sms:             sms,
		propertyService: propertyService,
		lastAt:          make(map[string]time.Time),
		cooldown:        passwordResetCooldown,
	}
}

// PasswordResetResult 重置结果（不含明文口令——口令只走短信通道）
type PasswordResetResult struct {
	MaskedPhone string `json:"masked_phone"` // 脱敏后的接收号码，如 138****8000
	DeviceID    string `json:"device_id"`
	DeviceName  string `json:"device_name"`
	SentAt      int64  `json:"sent_at"`
}

// normalizeAndValidatePhone 归一化并校验手机号。
//
// 归一化复用 openapi_service.go 的 normalizePhone（去空格/横线、+86 归一），
// 避免两套规则——否则同一条号码在开放接口和密码重置里会被判成两种样子。
func normalizeAndValidatePhone(raw string) (string, error) {
	cleaned := normalizePhone(raw)
	if cleaned == "" {
		return "", fmt.Errorf("接收手机号不能为空")
	}
	if !phonePattern.MatchString(cleaned) {
		return "", fmt.Errorf("手机号格式不正确：需 8-15 位数字，可带 + 国际区号")
	}
	return cleaned, nil
}

// MaskPhone 打码显示，仅保留头部和尾部，用于回显给前端。
// 完整号码不返回给前端也不写日志——它本身也是敏感信息。
//
// 注意与 openapi_service.go 的 maskPhone 的区别：那个在号码不是 11 位时**原样返回**，
// 是给开放接口日志用的（短号/测试号保留可读性）。这里的规则更严，任何长度都打码，
// 因为它是回显给浏览器的，一旦原样返回就等于把安全手机号泄露在页面上。
func MaskPhone(phone string) string {
	if phone == "" {
		return ""
	}
	if len(phone) <= 7 {
		return strings.Repeat("*", len(phone)-1) + phone[len(phone)-1:]
	}
	return phone[:3] + "****" + phone[len(phone)-4:]
}

// isNotFound 判断是否为「记录不存在」。
//
// orz 的 Repository.FindById 可能直接返回 gorm.ErrRecordNotFound，
// 也可能在中间层包一层，因此同时用 errors.Is 和字符串兜底。
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "record not found")
}

// GetPhone 读取已保存的安全手机号；未设置时返回空串而非报错。
func (s *PasswordResetService) GetPhone(ctx context.Context) (string, error) {
	var phone string
	if err := s.propertyService.GetValue(ctx, PropertyIDPasswordResetPhone, &phone); err != nil {
		if isNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("读取安全手机号失败: %w", err)
	}
	return phone, nil
}

// SetPhone 保存安全手机号
func (s *PasswordResetService) SetPhone(ctx context.Context, raw string) (string, error) {
	phone, err := normalizeAndValidatePhone(raw)
	if err != nil {
		return "", err
	}
	if err := s.propertyService.Set(ctx, PropertyIDPasswordResetPhone, "密码重置安全手机号", phone); err != nil {
		return "", fmt.Errorf("保存安全手机号失败: %w", err)
	}
	s.logger.Info("安全手机号已更新", zap.String("masked_phone", MaskPhone(phone)))
	return phone, nil
}

// checkCooldown 检查冷却期，返回剩余秒数
func (s *PasswordResetService) checkCooldown(username string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	last, ok := s.lastAt[username]
	if !ok {
		return 0, nil
	}
	if remain := s.cooldown - time.Since(last); remain > 0 {
		return remain, fmt.Errorf("操作过于频繁，请 %d 秒后再试", int(remain.Seconds())+1)
	}
	return 0, nil
}

// markCooldown 记录本次操作时间
func (s *PasswordResetService) markCooldown(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAt[username] = time.Now()
}

// buildResetMessage 拼装下发新口令的短信正文。
//
// 控制在 70 字以内（中文按 UCS2 编码计 70 字/条），
// 避免一条重置短信被拆成多条计费。
func buildResetMessage(username, password string) string {
	return fmt.Sprintf(
		"【UART短信网关】管理员密码已重置\n账号：%s\n新密码：%s\n请用新密码登录，登录后请立即修改。",
		username, password,
	)
}

// ResetBySMS 生成新口令、短信下发、然后落库。
//
// 顺序很关键：**先发短信，发送成功才落库**。
// 反过来的话，一旦短信发不出去（设备离线/欠费/号码填错），
// 旧口令已经被覆盖，用户就被彻底锁在门外了。
// 当前实现里发送失败 = 口令保持原样，用户仍可用旧密码登录。
func (s *PasswordResetService) ResetBySMS(ctx context.Context, username, deviceID, rawPhone string) (*PasswordResetResult, error) {
	if username == "" {
		return nil, fmt.Errorf("未登录")
	}
	if s.credentials == nil {
		return nil, fmt.Errorf("口令服务未初始化")
	}
	if s.sms == nil {
		return nil, fmt.Errorf("设备服务未初始化")
	}

	// 1. 确定接收号码：入参优先，否则用已保存的
	phone := rawPhone
	if strings.TrimSpace(phone) == "" {
		stored, err := s.GetPhone(ctx)
		if err != nil {
			return nil, err
		}
		phone = stored
	}
	phone, err := normalizeAndValidatePhone(phone)
	if err != nil {
		return nil, err
	}

	// 2. 冷却期检查（在真正发短信之前，省下话费）
	if _, err := s.checkCooldown(username); err != nil {
		return nil, err
	}

	// 3. 设备校验。不提供「默认设备」兜底：多卡场景下随便挑一张
	//    可能是境外漫游卡，发一条短信就是一条国际资费（见 device_manager.go）。
	if deviceID == "" {
		return nil, fmt.Errorf("必须指定发送设备")
	}
	ids := s.sms.GetAllDeviceIDs()
	if !slices.Contains(ids, deviceID) {
		return nil, fmt.Errorf("设备不存在: %s（可选: %v）", deviceID, ids)
	}
	deviceName := deviceID
	if status, err := s.sms.GetDeviceStatus(deviceID); err == nil {
		if status.Name != "" {
			deviceName = status.Name
		}
		if !status.Connected {
			return nil, fmt.Errorf("设备「%s」当前离线，无法发送短信", deviceName)
		}
	}

	// 4. 生成新口令
	newPassword, err := GenerateRandomPassword(PasswordGenLength)
	if err != nil {
		return nil, fmt.Errorf("生成新密码失败: %w", err)
	}

	// 5. 先发短信
	msgID, err := s.sms.SendSMS(deviceID, phone, buildResetMessage(username, newPassword))
	if err != nil {
		s.logger.Error("短信重置密码：短信发送失败，口令保持不变",
			zap.String("username", username),
			zap.String("device_id", deviceID),
			zap.String("masked_phone", MaskPhone(phone)),
			zap.Error(err))
		return nil, fmt.Errorf("短信发送失败，密码未修改（可继续使用原密码）：%w", err)
	}

	// 6. 短信已发出，再落库
	if err := s.credentials.ResetPassword(ctx, username, newPassword); err != nil {
		// 极端情况：短信带着一个实际没生效的新密码飞出去了。
		// 明确告诉调用方「旧密码仍然有效」，别让用户以为被改了。
		s.logger.Error("短信重置密码：短信已发出但口令落库失败",
			zap.String("username", username),
			zap.String("msg_id", msgID),
			zap.Error(err))
		return nil, fmt.Errorf("短信已发出，但新密码写入失败，请继续使用原密码：%w", err)
	}

	s.markCooldown(username)

	// 记住本次使用的号码，省得下次再手输一遍。
	// 失败不影响重置结果——密码已经生效了，保存号码只是便利性问题。
	if saved, _ := s.GetPhone(ctx); saved != phone {
		if err := s.propertyService.Set(ctx, PropertyIDPasswordResetPhone, "密码重置安全手机号", phone); err != nil {
			s.logger.Warn("重置成功后保存安全手机号失败", zap.Error(err))
		}
	}

	s.logger.Info("短信重置密码成功",
		zap.String("username", username),
		zap.String("device_id", deviceID),
		zap.String("device_name", deviceName),
		zap.String("masked_phone", MaskPhone(phone)),
		zap.String("msg_id", msgID))

	return &PasswordResetResult{
		MaskedPhone: MaskPhone(phone),
		DeviceID:    deviceID,
		DeviceName:  deviceName,
		SentAt:      time.Now().UnixMilli(),
	}, nil
}
