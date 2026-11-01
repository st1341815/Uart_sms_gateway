package service

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// mkFrame 生成一个设备帧
func mkFrame(json string) string {
	return smsPrefix + json + smsSuffix
}

// TestFrameReaderSimple 单帧正常读取
func TestFrameReaderSimple(t *testing.T) {
	src := mkFrame(`{"type":"heartbeat"}`)
	r := NewFrameReader(strings.NewReader(src))

	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame 错误: %v", err)
	}
	if string(frame) != `{"type":"heartbeat"}` {
		t.Fatalf("帧内容错误: %q", frame)
	}
}

// TestFrameReaderContentWithNewline 短信正文含换行——旧实现按行切分会错位
func TestFrameReaderContentWithNewline(t *testing.T) {
	payload := `{"type":"incoming_sms","from":"10010","content":"第一行\n第二行\n第三行"}`
	src := mkFrame(payload)
	r := NewFrameReader(strings.NewReader(src))

	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame 错误: %v", err)
	}
	if string(frame) != payload {
		t.Fatalf("含换行的帧被破坏:\n期望 %q\n实际 %q", payload, frame)
	}
}

// TestFrameReaderStickyPackets 粘包：一次 read 里有两帧
func TestFrameReaderStickyPackets(t *testing.T) {
	f1 := `{"type":"a"}`
	f2 := `{"type":"b"}`
	src := mkFrame(f1) + mkFrame(f2)
	r := NewFrameReader(strings.NewReader(src))

	got1, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("第1帧错误: %v", err)
	}
	got2, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("第2帧错误: %v", err)
	}
	if string(got1) != f1 || string(got2) != f2 {
		t.Fatalf("粘包解析错误: %q / %q", got1, got2)
	}
}

// TestFrameReaderHalfPackets 半包：每次只读 1 字节
func TestFrameReaderHalfPackets(t *testing.T) {
	payload := `{"type":"status_response","mobile":{"number":"+8615105513113"}}`
	src := mkFrame(payload)
	// iotest.OneByteReader 每次只返回 1 字节，模拟极慢的串口
	r := NewFrameReader(iotest.OneByteReader(strings.NewReader(src)))

	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame 错误: %v", err)
	}
	if string(frame) != payload {
		t.Fatalf("半包拼接错误:\n期望 %q\n实际 %q", payload, frame)
	}
}

// TestFrameReaderSkipsNoise 帧前有垃圾数据时能跳过
func TestFrameReaderSkipsNoise(t *testing.T) {
	payload := `{"type":"heartbeat"}`
	src := "随机噪声垃圾数据\r\n" + mkFrame(payload)
	r := NewFrameReader(strings.NewReader(src))

	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame 错误: %v", err)
	}
	if string(frame) != payload {
		t.Fatalf("噪声跳过失败: %q", frame)
	}
}

// TestFrameReaderEOF EOF 向上传递
func TestFrameReaderEOF(t *testing.T) {
	r := NewFrameReader(bytes.NewReader(nil))
	_, err := r.ReadFrame()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("期望 io.EOF，实际 %v", err)
	}
}

// TestFrameReaderParse 完整链路：帧 -> ParsedMessage
func TestFrameReaderParse(t *testing.T) {
	payload := `{"type":"incoming_sms","from":"10010","content":"a\nb"}`
	r := NewFrameReader(strings.NewReader(mkFrame(payload)))

	frame, err := r.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	msg, err := parseSMSFrame(string(frame))
	if err != nil {
		t.Fatalf("parseSMSFrame: %v", err)
	}
	if msg.Type != "incoming_sms" {
		t.Fatalf("type 错误: %s", msg.Type)
	}
	if msg.Payload["from"] != "10010" {
		t.Fatalf("from 错误: %v", msg.Payload["from"])
	}
	if msg.Payload["content"] != "a\nb" {
		t.Fatalf("content 换行未保留: %q", msg.Payload["content"])
	}
}

// TestParseSMSFrameLegacy 兼容传入整帧（含前后缀）
func TestParseSMSFrameLegacy(t *testing.T) {
	msg, err := parseSMSFrame(mkFrame(`{"type":"heartbeat"}`))
	if err != nil {
		t.Fatalf("整帧解析失败: %v", err)
	}
	if msg.Type != "heartbeat" {
		t.Fatalf("type 错误: %s", msg.Type)
	}
}

// TestParseSMSFrameMissingType type 缺失
func TestParseSMSFrameMissingType(t *testing.T) {
	_, err := parseSMSFrame(`{"from":"x"}`)
	if !errors.Is(err, errMissingType) {
		t.Fatalf("期望 errMissingType，实际 %v", err)
	}
}
