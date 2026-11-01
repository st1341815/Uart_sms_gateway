package service

import (
	"sync"
	"time"
)

// NonceCache 记录已使用过的 nonce，防重放攻击。
//
// 为什么用内存而不是数据库：
//   - nonce 只在签名有效期（5 分钟）内有意义，过期即无价值；
//   - 写库会带来不必要的 IO，且需要清理任务。
//
// 重启后缓存清空的影响：最多允许 5 分钟内已用过的 nonce 被重放一次，
// 而重放还需要配合有效签名与时间窗口，实际风险可接受。
type NonceCache struct {
	mu   sync.Mutex
	ttl  time.Duration
	data map[string]time.Time
	// 上次清理时间，避免每次 Use 都全量扫描
	lastClean time.Time
}

func NewNonceCache(ttl time.Duration) *NonceCache {
	return &NonceCache{
		ttl:       ttl,
		data:      make(map[string]time.Time),
		lastClean: time.Now(),
	}
}

// Use 尝试占用 nonce。返回 true 表示首次使用（通过），false 表示重复（拒绝）。
func (c *NonceCache) Use(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// 惰性清理：最多每分钟一次
	if now.Sub(c.lastClean) > time.Minute {
		for k, exp := range c.data {
			if now.After(exp) {
				delete(c.data, k)
			}
		}
		c.lastClean = now
	}

	if exp, ok := c.data[key]; ok && now.Before(exp) {
		return false
	}
	c.data[key] = now.Add(c.ttl)
	return true
}

// Clear 清空缓存（Key 配置变更时调用）
func (c *NonceCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = make(map[string]time.Time)
}

// Size 当前缓存条目数（测试与监控用）
func (c *NonceCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.data)
}
