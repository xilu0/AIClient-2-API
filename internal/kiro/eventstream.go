// Package kiro provides AWS event stream parsing for Kiro API responses.
package kiro

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"sync"
)

var (
	// ErrInvalidPreludeCRC indicates the prelude CRC doesn't match.
	ErrInvalidPreludeCRC = errors.New("invalid prelude CRC")
	// ErrInvalidMessageCRC indicates the message CRC doesn't match.
	ErrInvalidMessageCRC = errors.New("invalid message CRC")
	// ErrTruncatedMessage indicates the message data is incomplete.
	ErrTruncatedMessage = errors.New("truncated message")
	// ErrInvalidHeaderType indicates an unsupported header type.
	ErrInvalidHeaderType = errors.New("invalid header type")
	// ErrBufferOverflow indicates the buffer exceeded maximum size.
	ErrBufferOverflow = errors.New("event stream buffer overflow")
)

const (
	// Initial buffer capacity for event stream parsing
	initialBufferCap = 8192
	// Maximum buffer size to prevent unbounded memory growth (1MB)
	maxBufferSize = 1024 * 1024
)

// parserPool provides reusable EventStreamParser instances to reduce GC pressure.
var parserPool = sync.Pool{
	New: func() interface{} {
		return &EventStreamParser{
			buffer: make([]byte, 0, initialBufferCap),
		}
	},
}

// GetEventStreamParser gets a parser from the pool.
// Call ReleaseEventStreamParser when done.
func GetEventStreamParser() *EventStreamParser {
	return parserPool.Get().(*EventStreamParser)
}

// ReleaseEventStreamParser returns a parser to the pool.
func ReleaseEventStreamParser(p *EventStreamParser) {
	p.Reset()
	parserPool.Put(p)
}

// EventStreamParser parses AWS event stream binary format.
type EventStreamParser struct {
	buffer []byte
}

// NewEventStreamParser creates a new event stream parser.
// Prefer GetEventStreamParser/ReleaseEventStreamParser for better performance.
func NewEventStreamParser() *EventStreamParser {
	return &EventStreamParser{
		buffer: make([]byte, 0, initialBufferCap),
	}
}

// Parse parses the input data and returns complete event messages.
// Partial messages are buffered for the next call.
func (p *EventStreamParser) Parse(data []byte) ([]*AWSEventMessage, error) {
	// Check for buffer overflow before appending
	if len(p.buffer)+len(data) > maxBufferSize {
		return nil, ErrBufferOverflow
	}

	// Append to buffer with capacity check for efficient growth
	p.buffer = append(p.buffer, data...)

	var messages []*AWSEventMessage

	for len(p.buffer) >= 12 { // Minimum message size: prelude (12) + message CRC (4) = 16, but we need at least prelude
		// Read prelude
		totalLength := binary.BigEndian.Uint32(p.buffer[0:4])
		headersLength := binary.BigEndian.Uint32(p.buffer[4:8])
		preludeCRC := binary.BigEndian.Uint32(p.buffer[8:12])

		// Validate prelude CRC
		calculatedPreludeCRC := crc32.ChecksumIEEE(p.buffer[0:8])
		if preludeCRC != calculatedPreludeCRC {
			return messages, fmt.Errorf("%w: expected %x, got %x", ErrInvalidPreludeCRC, calculatedPreludeCRC, preludeCRC)
		}

		// Check if we have the complete message
		bufLen := len(p.buffer)
		if bufLen < 0 || uint32(bufLen) < totalLength { //nolint:gosec // bufLen is always >= 0
			// Wait for more data
			break
		}

		// Extract the complete message
		messageData := p.buffer[:totalLength]
		p.buffer = p.buffer[totalLength:]

		// Validate message CRC (last 4 bytes)
		messageCRC := binary.BigEndian.Uint32(messageData[totalLength-4:])
		calculatedMessageCRC := crc32.ChecksumIEEE(messageData[:totalLength-4])
		if messageCRC != calculatedMessageCRC {
			return messages, fmt.Errorf("%w: expected %x, got %x", ErrInvalidMessageCRC, calculatedMessageCRC, messageCRC)
		}

		// Parse headers
		headersStart := uint32(12)
		headersEnd := headersStart + headersLength
		headers, err := parseHeaders(messageData[headersStart:headersEnd])
		if err != nil {
			return messages, fmt.Errorf("failed to parse headers: %w", err)
		}

		// Extract payload
		payloadStart := headersEnd
		payloadEnd := totalLength - 4 // Exclude message CRC
		payload := messageData[payloadStart:payloadEnd]

		msg := &AWSEventMessage{
			TotalLength:   totalLength,
			HeadersLength: headersLength,
			PreludeCRC:    preludeCRC,
			Headers:       headers,
			Payload:       payload,
			MessageCRC:    messageCRC,
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// parseHeaders parses the headers section of an AWS event stream message.
func parseHeaders(data []byte) (map[string]HeaderValue, error) {
	headers := make(map[string]HeaderValue)
	reader := bytes.NewReader(data)

	for reader.Len() > 0 {
		// Read name length (1 byte)
		nameLenByte, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("failed to read header name length: %w", err)
		}
		nameLen := int(nameLenByte)

		// Read name
		name := make([]byte, nameLen)
		if _, err := reader.Read(name); err != nil {
			return nil, fmt.Errorf("failed to read header name: %w", err)
		}

		// Read type (1 byte)
		headerType, err := reader.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("failed to read header type: %w", err)
		}

		// Parse value based on type
		var value string
		switch headerType {
		case HeaderTypeString:
			// Read value length (2 bytes, big endian)
			var valueLen uint16
			if err := binary.Read(reader, binary.BigEndian, &valueLen); err != nil {
				return nil, fmt.Errorf("failed to read header value length: %w", err)
			}

			// Read value
			valueBytes := make([]byte, valueLen)
			if _, err := reader.Read(valueBytes); err != nil {
				return nil, fmt.Errorf("failed to read header value: %w", err)
			}
			value = string(valueBytes)

		default:
			return nil, fmt.Errorf("%w: %d", ErrInvalidHeaderType, headerType)
		}

		headers[string(name)] = HeaderValue{
			Type:  headerType,
			Value: value,
		}
	}

	return headers, nil
}

// GetMessageType returns the message type from the headers.
func (m *AWSEventMessage) GetMessageType() string {
	if h, ok := m.Headers[HeaderMessageType]; ok {
		return h.Value
	}
	return ""
}

// GetEventType returns the event type from the headers.
func (m *AWSEventMessage) GetEventType() string {
	if h, ok := m.Headers[HeaderEventType]; ok {
		return h.Value
	}
	return ""
}

// GetContentType returns the content type from the headers.
func (m *AWSEventMessage) GetContentType() string {
	if h, ok := m.Headers[HeaderContentType]; ok {
		return h.Value
	}
	return ""
}

// IsEvent returns true if this is an event message (not an exception).
func (m *AWSEventMessage) IsEvent() bool {
	return m.GetMessageType() == MessageTypeEvent
}

// IsException returns true if this is an exception message.
func (m *AWSEventMessage) IsException() bool {
	return m.GetMessageType() == MessageTypeException
}

// Reset clears the parser buffer while retaining capacity for reuse.
func (p *EventStreamParser) Reset() {
	// Keep buffer capacity but reset length to avoid allocations on reuse
	if cap(p.buffer) > maxBufferSize {
		// If buffer grew too large, allocate a fresh smaller one
		p.buffer = make([]byte, 0, initialBufferCap)
	} else {
		p.buffer = p.buffer[:0]
	}
}
