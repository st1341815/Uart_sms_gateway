package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	// 单帧上限：设备固件单帧远小于此值，超过视为脏数据并丢弃，
	// 防止缓冲区被无限增长的数据撑爆内存。
	maxFrameBytes = 64 * 1024
	// 读块大小
	readChunk = 4 * 1024
)

var (
	// ErrFrameTooLarge 帧超过上限
	ErrFrameTooLarge = errors.New("串口帧超过大小上限")
	// ErrReaderClosed 读取器已关闭
	ErrReaderClosed = errors.New("串口读取器已关闭")
)

// FrameReader 按 SMS_START:/SMS_END 边界扫描串口数据流。
//
// 为什么不用 bufio.Reader.ReadString('\n')：
//   - 设备帧以 SMS_START: 开头、:SMS_END 结尾，
//     JSON 内的短信正文可以包含 \n（多行短信/换行符），
//     按行切分会把一帧劈成两半，之后所有数据都错位，
//     表现就是「某张卡突然不再收短信」，且永不恢复。
//   - 也可能出现半包（一帧跨多次 Read）和粘包（两帧在一次 Read 里）。
//
// 本实现把收到的字节累积到缓冲区，不断用边界定位完整帧，
// 天然处理半包/粘包，并跳过帧外的噪声数据。
type FrameReader struct {
	r   io.Reader
	buf []byte
}

// NewFrameReader 创建帧读取器
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{
		r:   r,
		buf: make([]byte, 0, readChunk),
	}
}

// ReadFrame 阻塞读取一个完整帧。
// 返回的 []byte 是**帧内容**（不含 SMS_START:/SMS_END 标记，但含其中的 JSON）。
//   - 设备断开/IO 错误：返回该错误
//   - 帧超长：返回 ErrFrameTooLarge（调用方通常应重连串口）
func (f *FrameReader) ReadFrame() ([]byte, error) {
	chunk := make([]byte, readChunk)

	for {
		// 先尝试从已有缓冲里切出完整帧
		if frame, ok, err := f.extract(); err != nil {
			return nil, err
		} else if ok {
			return frame, nil
		}

		// 缓冲里还没有完整帧，继续读
		n, err := f.r.Read(chunk)
		if n > 0 {
			f.buf = append(f.buf, chunk[:n]...)
			// 防止无边界垃圾数据无限累积
			if len(f.buf) > maxFrameBytes && !bytes.Contains(f.buf, []byte(smsPrefix)) {
				// 完全没有起始标记，整段丢弃
				f.buf = f.buf[:0]
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, err
		}
		if n == 0 {
			// 读超时的实现可能返回 (0, nil)；继续循环等待
			continue
		}
	}
}

// extract 从缓冲区中切出一个完整帧；没有则返回 ok=false。
func (f *FrameReader) extract() ([]byte, bool, error) {
	start := bytes.Index(f.buf, []byte(smsPrefix))
	if start < 0 {
		// 没有起始标记：保留尾部（可能是下一个标记的前缀），其余丢弃
		if len(f.buf) > len(smsPrefix) {
			f.buf = append(f.buf[:0], f.buf[len(f.buf)-len(smsPrefix)+1:]...)
		}
		return nil, false, nil
	}
	// 丢弃起始标记之前的噪声
	if start > 0 {
		f.buf = f.buf[start:]
	}

	end := bytes.Index(f.buf, []byte(smsSuffix))
	if end < 0 {
		// 帧未收全
		if len(f.buf) > maxFrameBytes {
			f.buf = f.buf[:0]
			return nil, false, ErrFrameTooLarge
		}
		return nil, false, nil
	}

	// 帧内容 = SMS_START: 之后 到 :SMS_END 之前
	bodyStart := len(smsPrefix)
	frame := make([]byte, end-bodyStart)
	copy(frame, f.buf[bodyStart:end])

	// 消费掉这一帧（含后缀）
	consumed := end + len(smsSuffix)
	f.buf = append(f.buf[:0], f.buf[consumed:]...)

	return frame, true, nil
}

// parseSMSFrame 解析一帧内容（不含前缀/后缀）为 ParsedMessage。
//
// 注意：与旧实现的区别——入参是**帧内容**而非整行，
// 因此不再要求以 SMS_START 开头。为兼容旧调用（传入完整帧字符串），
// 这里也接受带前后缀的输入。
func parseSMSFrame(data string) (*ParsedMessage, error) {
	jsonData := trimFrame(data)
	if jsonData == "" {
		return nil, errNotSMSFrame
	}

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

// trimFrame 去掉可能存在的 SMS_START:/SMS_END 包裹与首尾空白。
func trimFrame(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, smsPrefix) {
		s = s[len(smsPrefix):]
	}
	if strings.HasSuffix(s, smsSuffix) {
		s = s[:len(s)-len(smsSuffix)]
	}
	return strings.TrimSpace(s)
}
