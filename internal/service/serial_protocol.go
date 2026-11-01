package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	smsPrefix = "SMS_START:"
	smsSuffix = ":SMS_END"
)

var (
	errNotSMSFrame = errors.New("not sms frame")
	errMissingType = errors.New("message type missing")
)

type ParsedMessage struct {
	JSON    string
	Type    string
	Payload map[string]interface{}
}

func parseSMSFrame(data string) (*ParsedMessage, error) {
	if !strings.HasPrefix(data, smsPrefix) || !strings.HasSuffix(data, smsSuffix) {
		return nil, errNotSMSFrame
	}

	jsonData := data[len(smsPrefix) : len(data)-len(smsSuffix)]
	payload := make(map[string]interface{})
	if err := json.Unmarshal([]byte(jsonData), &payload); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %w", err)
	}

	msgType, ok := payload["type"].(string)
	if !ok || msgType == "" {
		return nil, errMissingType
	}

	return &ParsedMessage{
		JSON:    jsonData,
		Type:    msgType,
		Payload: payload,
	}, nil
}

func buildCommandMessage(cmd any) ([]byte, string, error) {
	jsonData, err := json.Marshal(cmd)
	if err != nil {
		return nil, "", fmt.Errorf("JSON编码失败: %w", err)
	}

	message := fmt.Sprintf("CMD_START:%s:CMD_END\r\n", string(jsonData))
	return []byte(message), string(jsonData), nil
}

// isValidResponse 检查响应是否有效
func isValidResponse(response string) bool {
	// 检查是否包含基本的JSON结构
	if !strings.Contains(response, "{") || !strings.Contains(response, "}") {
		return false
	}

	// 尝试解析JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(response), &jsonData); err == nil {
		if _, hasType := jsonData["type"]; hasType {
			return true
		}
		if _, hasTimestamp := jsonData["timestamp"]; hasTimestamp {
			return true
		}
		if len(jsonData) > 0 {
			return true
		}
	}

	// 检查是否包含Lua脚本的标准格式
	if strings.Contains(response, smsPrefix) && strings.Contains(response, smsSuffix) {
		return true
	}

	// 检查是否包含状态信息关键词
	keywords := []string{"status_response", "mobile_info", "heartbeat", "system_ready"}
	for _, keyword := range keywords {
		if strings.Contains(response, keyword) {
			return true
		}
	}

	return false
}
