package service

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// 随机口令字符集。
//
// 三个集合都刻意剔除了易混淆字符：
//   - 小写去掉了 l、o（分别与 1、0 难分）
//   - 大写去掉了 I、O（分别与 l/1、0 难分）
//   - 数字去掉了 0、1（与 O、l 难分）
//
// 因为口令要经短信下发、由用户从手机屏幕手抄到电脑上，
// 一次看错就是一次登录失败，这里宁可牺牲一点熵也不留歧义字符。
const (
	pwCharsetLower = "abcdefghijkmnpqrstuvwxyz"
	pwCharsetUpper = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	pwCharsetDigit = "23456789"
	pwCharsetAll   = pwCharsetLower + pwCharsetUpper + pwCharsetDigit
)

// PasswordGenLength 短信重置口令的默认长度。
// 16 位 × log2(57) ≈ 93 bit 熵，远超暴力破解可行范围。
const PasswordGenLength = 16

// randInt 返回 [0, max) 的密码学安全随机数。
// 用 crypto/rand 而非 math/rand：后者种子的可预测性会让口令可被推算。
func randInt(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("随机数上界非法: %d", max)
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, fmt.Errorf("读取随机数失败: %w", err)
	}
	return int(n.Int64()), nil
}

// randChar 从字符集中随机取一个字符
func randChar(set string) (byte, error) {
	if set == "" {
		return 0, fmt.Errorf("字符集为空")
	}
	idx, err := randInt(len(set))
	if err != nil {
		return 0, err
	}
	return set[idx], nil
}

// GenerateRandomPassword 生成一个满足本项目口令策略的随机口令。
//
// 保证同时包含大写、小写、数字（credential_service.go 的 validatePasswordStrength
// 要求「字母 + 数字」），并且长度 >= 8。做法是先各塞一个必需品，
// 再用全集补齐，最后 Fisher-Yates 洗牌——不做洗牌的话前缀永远是「小写大写数字」，
// 等于把结构泄露给了攻击者。
//
// length <= 0 时使用 PasswordGenLength；小于 8 时按 8 处理。
func GenerateRandomPassword(length int) (string, error) {
	if length <= 0 {
		length = PasswordGenLength
	}
	if length < 8 {
		length = 8
	}

	out := make([]byte, 0, length)

	// 1. 每类各保证至少一个
	for _, set := range []string{pwCharsetLower, pwCharsetUpper, pwCharsetDigit} {
		ch, err := randChar(set)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}

	// 2. 剩余位用全集补齐
	for len(out) < length {
		ch, err := randChar(pwCharsetAll)
		if err != nil {
			return "", err
		}
		out = append(out, ch)
	}

	// 3. Fisher-Yates 洗牌，打散位置规律
	for i := len(out) - 1; i > 0; i-- {
		j, err := randInt(i + 1)
		if err != nil {
			return "", err
		}
		out[i], out[j] = out[j], out[i]
	}

	return string(out), nil
}
