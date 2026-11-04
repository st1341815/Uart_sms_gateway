package service

import (
	"strings"
	"testing"
)

// TestGenerateRandomPassword_MeetsPolicy 生成的口令必须能通过后端口令强度校验，
// 否则「短信发出去的新密码」在落库时会被 validatePasswordStrength 拒掉，
// 用户收到一条根本用不了的密码。
func TestGenerateRandomPassword_MeetsPolicy(t *testing.T) {
	for i := 0; i < 200; i++ {
		pw, err := GenerateRandomPassword(PasswordGenLength)
		if err != nil {
			t.Fatalf("生成口令失败: %v", err)
		}
		if err := validatePasswordStrength(pw); err != nil {
			t.Fatalf("生成的口令不满足强度校验: %q -> %v", pw, err)
		}
		if len(pw) != PasswordGenLength {
			t.Fatalf("长度不符: 期望 %d，实际 %d（%q）", PasswordGenLength, len(pw), pw)
		}
	}
}

// TestGenerateRandomPassword_NoAmbiguousChars 确认歧义字符没有混进口令。
// 这是手抄场景的硬性可读性要求，不是洁癖。
func TestGenerateRandomPassword_NoAmbiguousChars(t *testing.T) {
	const ambiguous = "0O1lIo"

	for i := 0; i < 200; i++ {
		pw, err := GenerateRandomPassword(PasswordGenLength)
		if err != nil {
			t.Fatalf("生成口令失败: %v", err)
		}
		if strings.ContainsAny(pw, ambiguous) {
			t.Fatalf("口令含易混淆字符 %q: %q", ambiguous, pw)
		}
	}
}

// TestGenerateRandomPassword_HasAllClasses 三类字符必须齐备
func TestGenerateRandomPassword_HasAllClasses(t *testing.T) {
	for i := 0; i < 100; i++ {
		pw, err := GenerateRandomPassword(PasswordGenLength)
		if err != nil {
			t.Fatalf("生成口令失败: %v", err)
		}
		var hasLower, hasUpper, hasDigit bool
		for _, r := range pw {
			switch {
			case r >= 'a' && r <= 'z':
				hasLower = true
			case r >= 'A' && r <= 'Z':
				hasUpper = true
			case r >= '0' && r <= '9':
				hasDigit = true
			}
		}
		if !hasLower || !hasUpper || !hasDigit {
			t.Fatalf("字符类别不全（lower=%v upper=%v digit=%v）: %q",
				hasLower, hasUpper, hasDigit, pw)
		}
	}
}

// TestGenerateRandomPassword_Distinct 连续生成不应重复。
// 重复意味着随机源没接上（例如误用了固定种子的 math/rand）。
func TestGenerateRandomPassword_Distinct(t *testing.T) {
	seen := make(map[string]struct{}, 500)
	for i := 0; i < 500; i++ {
		pw, err := GenerateRandomPassword(PasswordGenLength)
		if err != nil {
			t.Fatalf("生成口令失败: %v", err)
		}
		if _, dup := seen[pw]; dup {
			t.Fatalf("生成了重复口令: %q", pw)
		}
		seen[pw] = struct{}{}
	}
}

// TestGenerateRandomPassword_LengthBounds 长度边界处理
func TestGenerateRandomPassword_LengthBounds(t *testing.T) {
	cases := []struct {
		in   int
		want int
	}{
		{0, PasswordGenLength},   // 0 -> 默认
		{-5, PasswordGenLength},  // 负数 -> 默认
		{4, 8},                   // 过短 -> 下限 8
		{8, 8},                   // 边界
		{24, 24},                 // 正常指定
		{72, 72},                 // bcrypt 上限
	}
	for _, c := range cases {
		pw, err := GenerateRandomPassword(c.in)
		if err != nil {
			t.Fatalf("输入 %d 生成失败: %v", c.in, err)
		}
		if len(pw) != c.want {
			t.Fatalf("输入 %d：期望长度 %d，实际 %d", c.in, c.want, len(pw))
		}
		if err := validatePasswordStrength(pw); err != nil {
			t.Fatalf("输入 %d 生成的口令不合法: %v", c.in, err)
		}
	}
}
