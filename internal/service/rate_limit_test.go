package service

import (
	"testing"
	"time"
)

// TestRateLimiterBurst 允许突发到桶容量
func TestRateLimiterBurst(t *testing.T) {
	// 大容量桶 + 无最小间隔，验证容量逻辑
	l := newRateLimiter(3, time.Hour, 0)

	allowed := 0
	for i := 0; i < 5; i++ {
		if ok, _, _ := l.Allow(); ok {
			allowed++
		}
	}
	if allowed != 3 {
		t.Fatalf("桶容量 3，应允许 3 次突发，实际 %d 次", allowed)
	}
}

// TestRateLimiterRejects 超过容量后拒绝，并给出等待时间
func TestRateLimiterRejects(t *testing.T) {
	l := newRateLimiter(1, time.Second, 0)

	if ok, _, _ := l.Allow(); !ok {
		t.Fatal("第一次应允许")
	}
	ok, wait, err := l.Allow()
	if ok {
		t.Fatal("第二次应立即被拒绝")
	}
	if err == nil {
		t.Fatal("拒绝时应返回错误说明")
	}
	if wait <= 0 {
		t.Fatalf("应给出正的等待时长，实际 %v", wait)
	}
}

// TestRateLimiterMinGap 最小间隔限制防连点
func TestRateLimiterMinGap(t *testing.T) {
	// 容量很大，唯一限制是 minGap
	l := newRateLimiter(100, time.Millisecond, 200*time.Millisecond)

	if ok, _, _ := l.Allow(); !ok {
		t.Fatal("第一次应允许")
	}
	// 立即再发，应因 minGap 被拒
	if ok, _, _ := l.Allow(); ok {
		t.Fatal("最小间隔内应被拒绝")
	}
	// 等待超过 minGap 后应通过
	time.Sleep(210 * time.Millisecond)
	if ok, _, err := l.Allow(); !ok {
		t.Fatalf("超过最小间隔后应允许，实际被拒: %v", err)
	}
}

// TestRateLimiterRecovery 令牌随时间恢复
func TestRateLimiterRecovery(t *testing.T) {
	l := newRateLimiter(1, 100*time.Millisecond, 0)

	if ok, _, _ := l.Allow(); !ok {
		t.Fatal("第一次应允许")
	}
	time.Sleep(120 * time.Millisecond)
	if ok, _, err := l.Allow(); !ok {
		t.Fatalf("令牌应已恢复，实际被拒: %v", err)
	}
}

// TestSendRateLimitersPerDevice 每个设备独立计数
func TestSendRateLimitersPerDevice(t *testing.T) {
	ls := newSendRateLimiters()

	l1 := ls.get("sms1")
	l2 := ls.get("sms2")

	// 同一设备拿到同一个限流器
	if ls.get("sms1") != l1 {
		t.Fatal("同一设备应复用限流器")
	}
	// 不同设备是不同实例
	if l1 == l2 {
		t.Fatal("不同设备应有各自的限流器")
	}
}
