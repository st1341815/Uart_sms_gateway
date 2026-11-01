package service

import (
	"testing"
)

// TestCfgStringNoPanic 验证配置缺字段/类型不符时不会 panic。
// 这是 P1 修复点：原来 config["apiToken"].(string) 在 goroutine 里 panic 会杀进程。
func TestCfgStringNoPanic(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		key    string
		want   string
	}{
		{"字段缺失", map[string]interface{}{}, "apiToken", ""},
		{"值为 nil", map[string]interface{}{"apiToken": nil}, "apiToken", ""},
		{"正常字符串", map[string]interface{}{"apiToken": "abc"}, "apiToken", "abc"},
		{"数字", map[string]interface{}{"apiToken": float64(123)}, "apiToken", "123"},
		{"布尔", map[string]interface{}{"apiToken": true}, "apiToken", "true"},
		{"类型不符（map）", map[string]interface{}{"apiToken": map[string]string{"a": "b"}}, "apiToken", ""},
		{"类型不符（数组）", map[string]interface{}{"apiToken": []string{"a"}}, "apiToken", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 关键：不 panic
			got := cfgString(c.config, c.key)
			if got != c.want {
				t.Fatalf("cfgString = %q, 期望 %q", got, c.want)
			}
		})
	}
}

// TestCfgBoolNoPanic 验证布尔取值兼容多种写法。
func TestCfgBoolNoPanic(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]interface{}
		want   bool
	}{
		{"字段缺失", map[string]interface{}{}, false},
		{"值为 nil", map[string]interface{}{"proxyEnabled": nil}, false},
		{"true", map[string]interface{}{"proxyEnabled": true}, true},
		{"false", map[string]interface{}{"proxyEnabled": false}, false},
		{"字符串 true", map[string]interface{}{"proxyEnabled": "true"}, true},
		{"字符串 1", map[string]interface{}{"proxyEnabled": "1"}, true},
		{"字符串 false", map[string]interface{}{"proxyEnabled": "false"}, false},
		{"数字 1", map[string]interface{}{"proxyEnabled": float64(1)}, true},
		{"数字 0", map[string]interface{}{"proxyEnabled": float64(0)}, false},
		{"类型不符", map[string]interface{}{"proxyEnabled": []string{}}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cfgBool(c.config, "proxyEnabled")
			if got != c.want {
				t.Fatalf("cfgBool = %v, 期望 %v", got, c.want)
			}
		})
	}
}
