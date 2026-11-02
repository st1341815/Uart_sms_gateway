package service

import (
	"testing"
	"time"
)

// TestLoginGuardLocksSameAccount 同一 IP + 账号失败到上限后应被锁
func TestLoginGuardLocksSameAccount(t *testing.T) {
	g := NewLoginGuard()
	ip, user := "10.0.0.1", "admin"

	for i := 0; i < loginUserMaxFails-1; i++ {
		if err := g.Blocked(ip, user); err != nil {
			t.Fatalf("第 %d 次失败前不应被锁: %v", i+1, err)
		}
		g.Fail(ip, user)
	}
	// 上限触发
	g.Fail(ip, user)

	if err := g.Blocked(ip, user); err == nil {
		t.Fatal("达到失败上限后应被锁定")
	}
}

// TestLoginGuardOtherAccountStillAllowed 锁的是 ip+账号，不是整个 IP（避免被恶意锁死账号）
func TestLoginGuardOtherAccountStillAllowed(t *testing.T) {
	g := NewLoginGuard()
	ip := "10.0.0.2"

	for i := 0; i < loginUserMaxFails; i++ {
		g.Fail(ip, "admin")
	}
	if err := g.Blocked(ip, "admin"); err == nil {
		t.Fatal("admin 应被锁")
	}
	// 同一 IP 换了账号，仍可尝试（IP 维度要高得多才触发）
	if err := g.Blocked(ip, "other"); err != nil {
		t.Fatalf("同 IP 其它账号不应被连坐: %v", err)
	}
}

// TestLoginGuardIPWideLock 同一 IP 跨账号扫号到达上限后被锁
func TestLoginGuardIPWideLock(t *testing.T) {
	g := NewLoginGuard()
	ip := "10.0.0.3"

	for i := 0; i < loginIPMaxFails; i++ {
		g.Fail(ip, "user"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	if err := g.Blocked(ip, "brand-new-name"); err == nil {
		t.Fatal("同一 IP 大量失败后应触发 IP 维度锁定")
	}
}

// TestLoginGuardResetClears 登录成功后计数应清零
func TestLoginGuardResetClears(t *testing.T) {
	g := NewLoginGuard()
	ip, user := "10.0.0.4", "admin"

	for i := 0; i < loginUserMaxFails-1; i++ {
		g.Fail(ip, user)
	}
	g.Reset(ip, user)

	// 清零后再失败一次不应被锁
	g.Fail(ip, user)
	if err := g.Blocked(ip, user); err != nil {
		t.Fatalf("Reset 后不应立刻被锁: %v", err)
	}
	if n := g.Size(); n == 0 {
		t.Fatal("IP 维度计数应仍然存在（Reset 后马上重新累计）")
	}
}

// TestThrottledErrorCarriesRetryAfter 错误里要带剩余时长，供 429 的 Retry-After 用
func TestThrottledErrorCarriesRetryAfter(t *testing.T) {
	g := NewLoginGuard()
	ip, user := "10.0.0.5", "admin"
	for i := 0; i < loginUserMaxFails; i++ {
		g.Fail(ip, user)
	}
	err := g.Blocked(ip, user)
	te, ok := err.(*ThrottledError)
	if !ok {
		t.Fatalf("应返回 *ThrottledError，实际 %T", err)
	}
	if te.RetryAfter <= 0 || te.RetryAfter > loginLockDuration {
		t.Fatalf("RetryAfter 取值异常: %v", te.RetryAfter)
	}
	if te.Error() == "" {
		t.Fatal("Error() 不应为空")
	}
	_ = time.Now()
}
