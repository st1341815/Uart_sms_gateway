package service

import (
	"fmt"
	"sync"
	"time"
)

// 发信限流：防止单个设备被脚本刷爆话费。
//
// 设计取舍：
//   - 按「设备」维度限流，而不是全局。一张卡被刷不该连累另一张卡。
//   - 令牌桶：允许短时突发（用户连点两次），但长期速率受限。
//   - 两个限制同时生效：
//       ① 速率限制：每 interval 恢复一个令牌（capacity 个桶）
//       ② 最小间隔：两次发送之间至少间隔 minGap（防连点）
//
// 默认参数偏保守：每 3 秒 1 条、桶容量 3（允许 3 条突发），
// 与常见的短信发送节奏相符（设备本身也需要时间完成一次 AT 发送）。
const (
	smsRateInterval = 3 * time.Second // 令牌恢复间隔
	smsBucketSize   = 3               // 桶容量（突发上限）
	smsMinGap       = 1 * time.Second // 两次发送之间的最小间隔
)

// rateLimiter 简单令牌桶
type rateLimiter struct {
	mu        sync.Mutex
	tokens    float64
	capacity  float64
	perSecond float64
	last      time.Time
	// 最小间隔限制
	minGap  time.Duration
	lastHit time.Time
}

func newRateLimiter(capacity int, interval time.Duration, minGap time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:    float64(capacity),
		capacity:  float64(capacity),
		perSecond: 1.0 / interval.Seconds(),
		last:      time.Now(),
		minGap:    minGap,
	}
}

// Allow 判断此刻是否允许执行一次；允许则消耗一个令牌。
// 返回 (是否允许, 建议等待时长, 原因)。
func (l *rateLimiter) Allow() (bool, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// 最小间隔检查（防连点）
	if !l.lastHit.IsZero() {
		if elapsed := now.Sub(l.lastHit); elapsed < l.minGap {
			wait := l.minGap - elapsed
			return false, wait, fmt.Errorf("发送过于频繁，请等待 %s 后重试", wait.Round(time.Millisecond))
		}
	}

	// 令牌补充
	elapsed := now.Sub(l.last).Seconds()
	l.tokens += elapsed * l.perSecond
	if l.tokens > l.capacity {
		l.tokens = l.capacity
	}
	l.last = now

	if l.tokens < 1 {
		// 算出还需要多久才能攒够 1 个令牌
		need := (1 - l.tokens) / l.perSecond
		return false, time.Duration(need * float64(time.Second)), fmt.Errorf(
			"发送频率超限（每 %s 最多 1 条），请等待 %s 后重试",
			smsRateInterval, time.Duration(need*float64(time.Second)).Round(time.Millisecond))
	}

	l.tokens--
	l.lastHit = now
	return true, 0, nil
}

// sendRateLimiters 每设备一个限流器（由 DeviceManager 持有）
type sendRateLimiters struct {
	mu       sync.Mutex
	limiters map[string]*rateLimiter
}

func newSendRateLimiters() *sendRateLimiters {
	return &sendRateLimiters{limiters: make(map[string]*rateLimiter)}
}

func (s *sendRateLimiters) get(deviceID string) *rateLimiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.limiters[deviceID]
	if !ok {
		l = newRateLimiter(smsBucketSize, smsRateInterval, smsMinGap)
		s.limiters[deviceID] = l
	}
	return l
}
