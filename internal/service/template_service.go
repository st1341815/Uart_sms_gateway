package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dushixiang/uart_sms_forwarder/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 模板变量占位符：{变量名}
var templateVarRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// TemplateService 短信模板管理
type TemplateService struct {
	logger *zap.Logger
	db     *gorm.DB
}

func NewTemplateService(logger *zap.Logger, db *gorm.DB) *TemplateService {
	return &TemplateService{logger: logger, db: db}
}

// ExtractVars 提取模板中的所有变量名（去重、排序）
func ExtractVars(content string) []string {
	matches := templateVarRe.FindAllStringSubmatch(content, -1)
	seen := make(map[string]bool, len(matches))
	vars := make([]string, 0, len(matches))
	for _, m := range matches {
		v := m[1]
		if !seen[v] {
			seen[v] = true
			vars = append(vars, v)
		}
	}
	sort.Strings(vars)
	return vars
}

// RenderTemplate 用参数替换模板变量。
// 缺少变量时报错（而不是留个空占位符发出去——那对验证码是致命的）。
func RenderTemplate(content string, params map[string]string) (string, error) {
	var missing []string
	result := templateVarRe.ReplaceAllFunc([]byte(content), func(b []byte) []byte {
		name := templateVarRe.FindSubmatch(b)[1]
		val, ok := params[string(name)]
		if !ok {
			missing = append(missing, string(name))
			return b
		}
		return []byte(val)
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("缺少模板变量: %s", strings.Join(dedupe(missing), ", "))
	}
	return string(result), nil
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// ---- CRUD ----

func (s *TemplateService) Create(ctx context.Context, code, name, content, description string, enabled bool) (*models.SMSTemplate, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)

	if code == "" {
		return nil, fmt.Errorf("模板编码不能为空")
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{2,64}$`).MatchString(code) {
		return nil, fmt.Errorf("模板编码只能包含字母、数字、下划线和短横线（2-64 位）")
	}
	if name == "" {
		return nil, fmt.Errorf("模板名称不能为空")
	}
	if content == "" {
		return nil, fmt.Errorf("模板内容不能为空")
	}
	// 短信长度保护：中文按 70 字/条计，超过 500 字基本无意义且易被拆条
	if len([]rune(content)) > 500 {
		return nil, fmt.Errorf("模板内容过长（最多 500 字）")
	}

	// 编码唯一
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&models.SMSTemplate{}).Where("code = ?", code).Count(&cnt).Error; err != nil {
		return nil, err
	}
	if cnt > 0 {
		return nil, fmt.Errorf("模板编码已存在: %s", code)
	}

	now := time.Now().UnixMilli()
	t := &models.SMSTemplate{
		ID:          uuid.NewString(),
		Code:        code,
		Name:        name,
		Content:     content,
		Description: description,
		Enabled:     enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.db.WithContext(ctx).Create(t).Error; err != nil {
		return nil, fmt.Errorf("保存模板失败: %w", err)
	}
	return t, nil
}

func (s *TemplateService) List(ctx context.Context) ([]models.SMSTemplate, error) {
	var list []models.SMSTemplate
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *TemplateService) Update(ctx context.Context, id, code, name, content, description string, enabled bool) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("模板内容不能为空")
	}
	if len([]rune(content)) > 500 {
		return fmt.Errorf("模板内容过长（最多 500 字）")
	}
	// 编码改了要查重
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&models.SMSTemplate{}).
		Where("code = ? AND id <> ?", code, id).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return fmt.Errorf("模板编码已被其他模板占用: %s", code)
	}

	res := s.db.WithContext(ctx).Model(&models.SMSTemplate{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"code":        code,
			"name":        name,
			"content":     content,
			"description": description,
			"enabled":     enabled,
			"updated_at":  time.Now().UnixMilli(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("模板不存在")
	}
	return nil
}

func (s *TemplateService) Delete(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Delete(&models.SMSTemplate{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("模板不存在")
	}
	return nil
}

// GetByCode 按编码取启用中的模板（发送时用）
func (s *TemplateService) GetByCode(ctx context.Context, code string) (*models.SMSTemplate, error) {
	var t models.SMSTemplate
	if err := s.db.WithContext(ctx).Where("code = ?", code).First(&t).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("模板不存在: %s", code)
		}
		return nil, err
	}
	if !t.Enabled {
		return nil, fmt.Errorf("模板已停用: %s", code)
	}
	return &t, nil
}
