package service

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// 同一 IP + 同一账号的连续失败上限
	loginUserMaxFails = 5
	// 同一 IP 跨账号的失败上限（挡扫号）
	loginIPMaxFails = 20
	// 触发上限后的锁定时长
	loginLockDuration = 5 * time.Minute
	// 失败计数与锁记录的保留时长（超过且已解锁则清理）
	loginFailDecay = 10 * time.Minute
)

// ThrottledError 登录因失败次数过多被限流。
// handler 用它区分「账号密码错」与「被限流」，后者回 429。
type ThrottledError struct {
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	mins := int(e.RetryAfter.Minutes())
	if mins < 1 {
		mins = 1
	}
	return fmt.Sprintf("登录失败次数过多，请 %d 分钟后再试", mins)
}

type loginFailEntry struct {
	count    int
	lockedTo time.Time
	lastFail time.Time
}

// LoginGuard 进程内的登录失败限流器。
//
// 设计取舍：
//   - 放内存即可：串口绑定单机，本来就是单实例部署；进程重启清空可以接受，
//     目标只是挡住在线爆破，不是做审计留痕。
//   - 双维度计数：ip|user（5 次）挡单账号爆破，ip（20 次）挡换账号扫号。
//     ip|user 维度不会因为「别人故意输错」而把某个账号永久锁死。
type LoginGuard struct {
	mu        sync.Mutex
	entries   map[string]*loginFailEntry
	lastClean time.Time
}

func NewLoginGuard() *LoginGuard {
	return &LoginGuard{
		entries:   make(map[string]*loginFailEntry),
		lastClean: time.Now(),
	}
}

func loginUserKey(ip, username string) string {
	return "u:" + ip + "|" + strings.ToLower(strings.TrimSpace(username))
}

func loginIPKey(ip string) string {
	return "i:" + ip
}

// Blocked 报告该 IP / 账号当前是否处于锁定状态。
// 返回 *ThrottledError 表示被限流。
func (g *LoginGuard) Blocked(ip, username string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.cleanupLocked(now)

	for _, key := range []string{loginUserKey(ip, username), loginIPKey(ip)} {
		if e := g.entries[key]; e != nil && now.Before(e.lockedTo) {
			return &ThrottledError{RetryAfter: e.lockedTo.Sub(now)}
		}
	}
	return nil
}

// Fail 记录一次失败
func (g *LoginGuard) Fail(ip, username string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()
	g.hitLocked(loginUserKey(ip, username), loginUserMaxFails, now)
	g.hitLocked(loginIPKey(ip), loginIPMaxFails, now)
}

// Reset 登录成功后清除该 IP / 账号的失败计数
func (g *LoginGuard) Reset(ip, username string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.entries, loginUserKey(ip, username))
	delete(g.entries, loginIPKey(ip))
}

// Size 当前跟踪的条目数（测试与运维观察用）
func (g *LoginGuard) Size() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.entries)
}

// hitLocked 调用方必须已持锁
func (g *LoginGuard) hitLocked(key string, max int, now time.Time) {
	e := g.entries[key]
	if e == nil {
		e = &loginFailEntry{}
		g.entries[key] = e
	}
	e.count++
	e.lastFail = now
	if e.count >= max {
		e.lockedTo = now.Add(loginLockDuration)
		e.count = 0 // 解锁后重新计数
	}
}

// cleanupLocked 惰性清理，最多每分钟一次。调用方必须已持锁。
func (g *LoginGuard) cleanupLocked(now time.Time) {
	if now.Sub(g.lastClean) < time.Minute {
		return
	}
	g.lastClean = now
	for key, e := range g.entries {
		if now.Before(e.lockedTo) {
			continue // 仍在锁定期
		}
		if now.Sub(e.lastFail) > loginFailDecay {
			delete(g.entries, key)
		}
	}
}
