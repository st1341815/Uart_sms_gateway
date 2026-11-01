package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	// 签名时间戳允许的偏差（防重放）
	SignatureTTL = 5 * time.Minute
	// nonce 缓存时长，需 >= SignatureTTL
	nonceTTL = 10 * time.Minute
	// 默认每分钟调用上限
	DefaultRateLimitPerMinute = 20
)

// APIKeyService 管理开放接口凭证与签名校验
type APIKeyService struct {
	logger *zap.Logger
	db     *gorm.DB
	aead   cipher.AEAD
	// nonce 缓存，防重放
	nonces *NonceCache
}

// NewAPIKeyService 创建服务。secretEncKey 用于加密 SecretKey（由 JWT Secret 派生）。
func NewAPIKeyService(logger *zap.Logger, db *gorm.DB, secretEncKey string) (*APIKeyService, error) {
	if secretEncKey == "" {
		return nil, fmt.Errorf("加密密钥不能为空")
	}
	// 用 SHA-256 派生 32 字节 AES 密钥
	sum := sha256.Sum256([]byte("sms-api-secret-encryption:" + secretEncKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, fmt.Errorf("初始化加密器失败: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化 GCM 失败: %w", err)
	}
	return &APIKeyService{
		logger: logger,
		db:     db,
		aead:   aead,
		nonces: NewNonceCache(nonceTTL),
	}, nil
}

// ---- 密钥加解密 ----

func (s *APIKeyService) encryptSecret(plain string) (string, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := s.aead.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out), nil
}

func (s *APIKeyService) decryptSecret(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	ns := s.aead.NonceSize()
	if len(raw) < ns {
		return "", fmt.Errorf("密文长度非法")
	}
	plain, err := s.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ---- Key 管理 ----

// CreateAPIKey 创建 Key，返回**明文 Secret（仅此一次）**
func (s *APIKeyService) CreateAPIKey(ctx context.Context, name, deviceID, allowedIPs string, ratePerMinute int) (*models.APIKey, string, error) {
	if strings.TrimSpace(name) == "" {
		return nil, "", fmt.Errorf("名称不能为空")
	}
	if strings.TrimSpace(deviceID) == "" {
		return nil, "", fmt.Errorf("必须绑定设备")
	}
	if ratePerMinute <= 0 {
		ratePerMinute = DefaultRateLimitPerMinute
	}

	ak, err := randomToken("ak_", 16)
	if err != nil {
		return nil, "", err
	}
	sk, err := randomToken("sk_", 24)
	if err != nil {
		return nil, "", err
	}
	enc, err := s.encryptSecret(sk)
	if err != nil {
		return nil, "", fmt.Errorf("加密 Secret 失败: %w", err)
	}

	now := time.Now().UnixMilli()
	k := &models.APIKey{
		ID:                 uuid.NewString(),
		Name:               name,
		AccessKey:          ak,
		SecretEnc:          enc,
		SecretHint:         "****" + sk[len(sk)-4:],
		DeviceID:           deviceID,
		AllowedIPs:         strings.TrimSpace(allowedIPs),
		RateLimitPerMinute: ratePerMinute,
		Enabled:            true,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.db.WithContext(ctx).Create(k).Error; err != nil {
		return nil, "", fmt.Errorf("保存 Key 失败: %w", err)
	}
	s.logger.Info("已创建 API Key", zap.String("name", name), zap.String("access_key", ak))
	return k, sk, nil
}

// ListAPIKeys 列出所有 Key
func (s *APIKeyService) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	var list []models.APIKey
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// UpdateAPIKey 更新 Key 配置（不含 Secret）
func (s *APIKeyService) UpdateAPIKey(ctx context.Context, id, name, deviceID, allowedIPs string, ratePerMinute int, enabled bool) error {
	if ratePerMinute <= 0 {
		ratePerMinute = DefaultRateLimitPerMinute
	}
	res := s.db.WithContext(ctx).Model(&models.APIKey{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"name":                  name,
			"device_id":             deviceID,
			"allowed_ips":           strings.TrimSpace(allowedIPs),
			"rate_limit_per_minute": ratePerMinute,
			"enabled":               enabled,
			"updated_at":            time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("Key 不存在")
	}
	// 配置变更后清空 nonce 缓存（避免旧 nonce 与新配置混用）
	s.nonces.Clear()
	return nil
}

// DeleteAPIKey 删除 Key
func (s *APIKeyService) DeleteAPIKey(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Delete(&models.APIKey{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("Key 不存在")
	}
	return nil
}

// ResetSecret 重置 Secret，返回新的明文（仅此一次）
func (s *APIKeyService) ResetSecret(ctx context.Context, id string) (string, error) {
	sk, err := randomToken("sk_", 24)
	if err != nil {
		return "", err
	}
	enc, err := s.encryptSecret(sk)
	if err != nil {
		return "", err
	}
	res := s.db.WithContext(ctx).Model(&models.APIKey{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"secret_enc":  enc,
			"secret_hint": "****" + sk[len(sk)-4:],
			"updated_at":  time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		return "", fmt.Errorf("Key 不存在")
	}
	return sk, nil
}

// FindByAccessKey 按 AK 查（验签时用）
func (s *APIKeyService) FindByAccessKey(ctx context.Context, ak string) (*models.APIKey, error) {
	var k models.APIKey
	if err := s.db.WithContext(ctx).Where("access_key = ?", ak).First(&k).Error; err != nil {
		return nil, err
	}
	return &k, nil
}

// ---- 验签 ----

// SignRequest 计算签名。
// StringToSign 结构（换行分隔）：
//
//	METHOD
//	PATH
//	SORTED_QUERY        // a=1&b=2，按 key 升序
//	BODY_SHA256_HEX     // 无 body 时为空字符串的 sha256
//	TIMESTAMP
//	NONCE
func SignRequest(secret, method, path, sortedQuery, bodySHA256, timestamp, nonce string) string {
	sts := strings.Join([]string{
		strings.ToUpper(method),
		path,
		sortedQuery,
		bodySHA256,
		timestamp,
		nonce,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(sts))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyRequest 完整校验：AK 存在且启用 → 时间戳 → IP 白名单 → 签名 → nonce 防重放
func (s *APIKeyService) VerifyRequest(ctx context.Context, ak, signature, timestamp, nonce, method, path, sortedQuery, bodySHA256, clientIP string) (*models.APIKey, error) {
	if ak == "" || signature == "" || timestamp == "" || nonce == "" {
		return nil, fmt.Errorf("缺少认证参数（X-Api-Key / X-Signature / X-Timestamp / X-Nonce）")
	}

	// 1. 时间戳窗口
	ts, err := parseUnixSeconds(timestamp)
	if err != nil {
		return nil, fmt.Errorf("时间戳格式非法")
	}
	skew := time.Since(ts)
	if skew < 0 {
		skew = -skew
	}
	if skew > SignatureTTL {
		return nil, fmt.Errorf("请求已过期（时间偏差超过 %s），请校准服务器时间", SignatureTTL)
	}

	// 2. 查 Key
	k, err := s.FindByAccessKey(ctx, ak)
	if err != nil {
		// 不区分「不存在」和「已禁用」，避免探测
		return nil, fmt.Errorf("认证失败")
	}
	if !k.Enabled {
		return nil, fmt.Errorf("该 AccessKey 已被停用")
	}

	// 3. IP 白名单
	if !s.ipAllowed(k.AllowedIPs, clientIP) {
		s.logger.Warn("API 调用来源 IP 不在白名单",
			zap.String("access_key", ak), zap.String("ip", clientIP))
		return nil, fmt.Errorf("来源 IP 不在白名单: %s", clientIP)
	}

	// 4. 验签
	secret, err := s.decryptSecret(k.SecretEnc)
	if err != nil {
		s.logger.Error("解密 Secret 失败", zap.String("access_key", ak), zap.Error(err))
		return nil, fmt.Errorf("认证失败")
	}
	expected := SignRequest(secret, method, path, sortedQuery, bodySHA256, timestamp, nonce)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(strings.ToLower(signature))) != 1 {
		return nil, fmt.Errorf("签名校验失败")
	}

	// 5. 防重放：nonce 只能用一次
	if !s.nonces.Use(ak + ":" + nonce) {
		return nil, fmt.Errorf("重复请求（nonce 已使用）")
	}

	// 6. 记录使用时间
	_ = s.db.WithContext(ctx).Model(&models.APIKey{}).Where("id = ?", k.ID).
		Update("last_used_at", time.Now().UnixMilli()).Error

	return k, nil
}

// ipAllowed 校验 IP 是否在白名单内（支持单 IP 与 CIDR）
func (s *APIKeyService) ipAllowed(whitelist, clientIP string) bool {
	whitelist = strings.TrimSpace(whitelist)
	if whitelist == "" {
		return true // 未配置 = 不限制
	}
	ip := net.ParseIP(strings.TrimSpace(clientIP))
	if ip == nil {
		return false
	}
	for _, item := range strings.Split(whitelist, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			if _, cidr, err := net.ParseCIDR(item); err == nil && cidr.Contains(ip) {
				return true
			}
			continue
		}
		if parsed := net.ParseIP(item); parsed != nil && parsed.Equal(ip) {
			return true
		}
	}
	return false
}

// ---- 辅助 ----

func randomToken(prefix string, nbytes int) (string, error) {
	b := make([]byte, nbytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机串失败: %w", err)
	}
	return prefix + hex.EncodeToString(b), nil
}

func parseUnixSeconds(s string) (time.Time, error) {
	var sec int64
	if _, err := fmt.Sscanf(s, "%d", &sec); err != nil {
		return time.Time{}, err
	}
	return time.Unix(sec, 0), nil
}

// SortedQuery 把 query 参数按 key 升序拼成 a=1&b=2
func SortedQuery(params map[string][]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vals := params[k]
		sort.Strings(vals)
		for _, v := range vals {
			parts = append(parts, k+"="+v)
		}
	}
	return strings.Join(parts, "&")
}

// SHA256Hex 计算 body 的 sha256 十六进制（空 body 也返回空串的哈希）
func SHA256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
