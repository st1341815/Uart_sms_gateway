package service

import (
	"strings"
	"testing"
	"time"
)

// ---------- 签名 ----------

// TestSignRequestDeterministic 相同输入签名一致
func TestSignRequestDeterministic(t *testing.T) {
	a := SignRequest("secret", "POST", "/openapi/v1/sms/send", "a=1&b=2", "hash", "1700000000", "nonce1")
	b := SignRequest("secret", "POST", "/openapi/v1/sms/send", "a=1&b=2", "hash", "1700000000", "nonce1")
	if a != b {
		t.Fatal("相同输入签名应一致")
	}
	if len(a) != 64 {
		t.Fatalf("HMAC-SHA256 十六进制应为 64 字符，实际 %d", len(a))
	}
}

// TestSignRequestSensitiveToEachField 任一字段变化都改变签名
func TestSignRequestSensitiveToEachField(t *testing.T) {
	base := SignRequest("secret", "POST", "/p", "q=1", "h", "100", "n")

	cases := []struct {
		name string
		got  string
	}{
		{"secret", SignRequest("secret2", "POST", "/p", "q=1", "h", "100", "n")},
		{"method", SignRequest("secret", "GET", "/p", "q=1", "h", "100", "n")},
		{"path", SignRequest("secret", "POST", "/p2", "q=1", "h", "100", "n")},
		{"query", SignRequest("secret", "POST", "/p", "q=2", "h", "100", "n")},
		{"body", SignRequest("secret", "POST", "/p", "q=1", "h2", "100", "n")},
		{"timestamp", SignRequest("secret", "POST", "/p", "q=1", "h", "101", "n")},
		{"nonce", SignRequest("secret", "POST", "/p", "q=1", "h", "100", "n2")},
	}
	for _, c := range cases {
		if c.got == base {
			t.Fatalf("改动 %s 后签名不应相同", c.name)
		}
	}
}

// TestSortedQuery 查询参数排序
func TestSortedQuery(t *testing.T) {
	got := SortedQuery(map[string][]string{
		"b": {"2", "1"},
		"a": {"3"},
	})
	want := "a=3&b=1&b=2"
	if got != want {
		t.Fatalf("SortedQuery = %q，期望 %q", got, want)
	}
	if SortedQuery(nil) != "" {
		t.Fatal("空参数应返回空串")
	}
}

// TestSHA256Hex 空 body 也有哈希
func TestSHA256Hex(t *testing.T) {
	empty := SHA256Hex(nil)
	if len(empty) != 64 {
		t.Fatalf("应为 64 位十六进制，实际 %d", len(empty))
	}
	// 与已知值对比（空字符串 sha256）
	if empty != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("空串 sha256 不对: %s", empty)
	}
	if SHA256Hex([]byte("a")) == empty {
		t.Fatal("不同内容哈希应不同")
	}
}

// ---------- Nonce 防重放 ----------

// TestNonceCacheRejectsReplay 同一 nonce 第二次被拒
func TestNonceCacheRejectsReplay(t *testing.T) {
	c := NewNonceCache(time.Minute)
	if !c.Use("n1") {
		t.Fatal("首次使用应通过")
	}
	if c.Use("n1") {
		t.Fatal("重复使用应被拒")
	}
	if !c.Use("n2") {
		t.Fatal("不同 nonce 应通过")
	}
}

// TestNonceCacheExpiry 过期后同一 nonce 可再用（模拟超出时间窗口）
func TestNonceCacheExpiry(t *testing.T) {
	c := NewNonceCache(50 * time.Millisecond)
	c.Use("n1")
	time.Sleep(80 * time.Millisecond)
	if !c.Use("n1") {
		t.Fatal("TTL 过期后应允许再次使用")
	}
}

// TestNonceCacheClear 清空
func TestNonceCacheClear(t *testing.T) {
	c := NewNonceCache(time.Minute)
	c.Use("n1")
	c.Clear()
	if c.Size() != 0 {
		t.Fatal("Clear 后应为空")
	}
	if !c.Use("n1") {
		t.Fatal("Clear 后同一 nonce 应可再用")
	}
}

// ---------- IP 白名单 ----------

// TestIPAllowed 单 IP / CIDR / 空配置
func TestIPAllowed(t *testing.T) {
	s := &APIKeyService{}
	cases := []struct {
		name      string
		whitelist string
		ip        string
		want      bool
	}{
		{"未配置-放行", "", "1.2.3.4", true},
		{"单IP命中", "1.2.3.4", "1.2.3.4", true},
		{"单IP不命中", "1.2.3.4", "1.2.3.5", false},
		{"CIDR命中", "192.168.0.0/16", "192.168.11.21", true},
		{"CIDR不命中", "192.168.0.0/16", "10.0.0.1", false},
		{"多条目命中第二个", "8.8.8.8, 192.168.0.0/16", "192.168.1.1", true},
		{"非法IP", "1.2.3.4", "not-an-ip", false},
		{"空白符容错", " 1.2.3.4 , 5.6.7.8 ", "5.6.7.8", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := s.ipAllowed(c.whitelist, c.ip); got != c.want {
				t.Fatalf("ipAllowed(%q, %q) = %v，期望 %v", c.whitelist, c.ip, got, c.want)
			}
		})
	}
}

// ---------- 模板渲染 ----------

// TestExtractVars 变量提取去重排序
func TestExtractVars(t *testing.T) {
	vars := ExtractVars("您的验证码是{code}，{code} 5分钟内有效，请勿泄露。{name}")
	got := strings.Join(vars, ",")
	if got != "code,name" {
		t.Fatalf("ExtractVars = %q，期望 code,name", got)
	}
	if len(ExtractVars("无变量")) != 0 {
		t.Fatal("无变量应返回空")
	}
}

// TestRenderTemplate 正常替换
func TestRenderTemplate(t *testing.T) {
	out, err := RenderTemplate("您的验证码是{code}，5分钟内有效。", map[string]string{"code": "123456"})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "您的验证码是123456，5分钟内有效。" {
		t.Fatalf("渲染结果不对: %q", out)
	}
}

// TestRenderTemplateMissingVar 缺变量必须报错（否则验证码会是空的）
func TestRenderTemplateMissingVar(t *testing.T) {
	_, err := RenderTemplate("验证码{code}，{name}您好", map[string]string{"code": "1"})
	if err == nil {
		t.Fatal("缺变量应报错")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Fatalf("错误应指出缺失的变量名，实际: %v", err)
	}
}

// TestRenderTemplateExtraParams 多余参数被忽略
func TestRenderTemplateExtraParams(t *testing.T) {
	out, err := RenderTemplate("{a}", map[string]string{"a": "1", "b": "2"})
	if err != nil || out != "1" {
		t.Fatalf("多余参数应被忽略，got=%q err=%v", out, err)
	}
}

// TestRenderTemplateSpecialChars 变量值含特殊字符不做转义（短信是纯文本）
func TestRenderTemplateSpecialChars(t *testing.T) {
	out, err := RenderTemplate("{v}", map[string]string{"v": "a&b<c>\"d'"})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if out != "a&b<c>\"d'" {
		t.Fatalf("特殊字符应原样输出，实际 %q", out)
	}
}

// ---------- 手机号归一与脱敏 ----------

// TestNormalizePhone 各种写法归一
func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"13800138000":        "13800138000",
		"+8613800138000":     "13800138000",
		"8613800138000":      "13800138000",
		" 138 0013 8000 ":    "13800138000",
		"138-0013-8000":      "13800138000",
		"":                   "",
	}
	for in, want := range cases {
		if got := normalizePhone(in); got != want {
			t.Fatalf("normalizePhone(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestMaskPhone 脱敏
func TestMaskPhone(t *testing.T) {
	if got := maskPhone("13800138000"); got != "138****8000" {
		t.Fatalf("maskPhone = %q", got)
	}
	// 非 11 位原样返回
	if got := maskPhone("123"); got != "123" {
		t.Fatalf("短号码应原样返回，实际 %q", got)
	}
}

// TestMaskContent 内容脱敏（验证码不能进日志）
func TestMaskContent(t *testing.T) {
	out := maskContent("您的验证码是123456")
	if strings.Contains(out, "123456") {
		t.Fatalf("脱敏后不应包含原文: %q", out)
	}
	if !strings.Contains(out, "字") {
		t.Fatalf("应包含长度信息: %q", out)
	}
}
