package protocol

import (
	"encoding/binary"
	"errors"
	"time"
)

// 消息类型
const (
	MsgTypeData      = 0x01 // 数据消息
	MsgTypeHeartbeat = 0x02 // 心跳消息
	MsgTypeStatus    = 0x03 // 状态消息
	MsgTypeError     = 0x04 // 错误消息
)

// MessageHeader 消息头
type MessageHeader struct {
	Type      uint8
	Length    uint32
	Timestamp uint64
	Sequence  uint32
	Reserved  [16]byte
}

// NewMessageHeader 创建新的消息头
func NewMessageHeader(msgType uint8, dataLen uint32) *MessageHeader {
	return &MessageHeader{
		Type:      msgType,
		Length:    dataLen,
		Timestamp: uint64(time.Now().UnixNano()),
		Sequence:  generateSequence(),
	}
}

// Encode 编码消息头
func (h *MessageHeader) Encode() []byte {
	buf := make([]byte, 29) // 1 + 4 + 8 + 4 + 12 = 29字节
	buf[0] = h.Type
	binary.BigEndian.PutUint32(buf[1:5], h.Length)
	binary.BigEndian.PutUint64(buf[5:13], h.Timestamp)
	binary.BigEndian.PutUint32(buf[13:17], h.Sequence)
	copy(buf[17:], h.Reserved[:12])
	return buf
}

// DecodeHeader 解码消息头
func DecodeHeader(data []byte) (*MessageHeader, error) {
	if len(data) < 29 {
		return nil, ErrInvalidHeader
	}

	return &MessageHeader{
		Type:      data[0],
		Length:    binary.BigEndian.Uint32(data[1:5]),
		Timestamp: binary.BigEndian.Uint64(data[5:13]),
		Sequence:  binary.BigEndian.Uint32(data[13:17]),
	}, nil
}

var sequence uint32 = 0

func generateSequence() uint32 {
	sequence++
	return sequence
}

// Message 完整的消息
type Message struct {
	Header *MessageHeader
	Data   []byte
}

// Encode 编码完整消息
func (m *Message) Encode() []byte {
	headerBytes := m.Header.Encode()
	if m.Header.Length == 0 {
		return headerBytes
	}
	result := make([]byte, len(headerBytes)+len(m.Data))
	copy(result, headerBytes)
	copy(result[len(headerBytes):], m.Data)
	return result
}

var ErrInvalidHeader = errors.New("invalid message header")
