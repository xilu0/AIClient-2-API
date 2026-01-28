// Package unit contains unit tests for the Kiro server.
package unit

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/anthropics/AIClient-2-API/internal/kiro"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAWSEventMessage(t *testing.T) {
	// Create a valid AWS event stream message
	payload := []byte(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}`)

	msg := createTestEventMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
		":content-type": "application/json",
	}, payload)

	parser := kiro.NewEventStreamParser()
	events, err := parser.Parse(msg)
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, "event", events[0].Headers[":message-type"].Value)
	assert.Equal(t, "chunk", events[0].Headers[":event-type"].Value)
	assert.Equal(t, payload, events[0].Payload)
}

func TestParseMultipleMessages(t *testing.T) {
	payload1 := []byte(`{"type":"message_start"}`)
	payload2 := []byte(`{"type":"content_block_delta"}`)

	msg1 := createTestEventMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, payload1)

	msg2 := createTestEventMessage(t, map[string]string{
		":message-type": "event",
		":event-type":   "chunk",
	}, payload2)

	// Concatenate messages
	combined := append(msg1, msg2...)

	parser := kiro.NewEventStreamParser()
	events, err := parser.Parse(combined)
	require.NoError(t, err)
	require.Len(t, events, 2)

	assert.Equal(t, payload1, events[0].Payload)
	assert.Equal(t, payload2, events[1].Payload)
}

func TestParseInvalidCRC(t *testing.T) {
	payload := []byte(`{"type":"test"}`)
	msg := createTestEventMessage(t, map[string]string{
		":message-type": "event",
	}, payload)

	// Corrupt the message CRC (last 4 bytes)
	msg[len(msg)-1] ^= 0xFF

	parser := kiro.NewEventStreamParser()
	_, err := parser.Parse(msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CRC")
}

func TestParseTruncatedMessage(t *testing.T) {
	payload := []byte(`{"type":"test"}`)
	msg := createTestEventMessage(t, map[string]string{
		":message-type": "event",
	}, payload)

	// Truncate the message
	truncated := msg[:len(msg)/2]

	parser := kiro.NewEventStreamParser()
	events, err := parser.Parse(truncated)
	// Parser buffers incomplete messages, so no error but no events
	assert.NoError(t, err)
	assert.Empty(t, events, "truncated message should not produce events")
}

func TestParseEmptyInput(t *testing.T) {
	parser := kiro.NewEventStreamParser()
	events, err := parser.Parse([]byte{})
	require.NoError(t, err)
	assert.Empty(t, events)
}

func TestParseExceptionMessage(t *testing.T) {
	payload := []byte(`{"message":"Rate limit exceeded"}`)
	msg := createTestEventMessage(t, map[string]string{
		":message-type": "exception",
		":exception-type": "throttlingException",
	}, payload)

	parser := kiro.NewEventStreamParser()
	events, err := parser.Parse(msg)
	require.NoError(t, err)
	require.Len(t, events, 1)

	assert.Equal(t, "exception", events[0].Headers[":message-type"].Value)
}

// createTestEventMessage creates a valid AWS event stream message for testing.
func createTestEventMessage(t *testing.T, headers map[string]string, payload []byte) []byte {
	t.Helper()

	// Build headers
	var headerBuf bytes.Buffer
	for name, value := range headers {
		// Name length (1 byte)
		headerBuf.WriteByte(byte(len(name)))
		// Name
		headerBuf.WriteString(name)
		// Type (1 byte) - 7 for string
		headerBuf.WriteByte(7)
		// Value length (2 bytes, big endian)
		binary.Write(&headerBuf, binary.BigEndian, uint16(len(value)))
		// Value
		headerBuf.WriteString(value)
	}
	headerBytes := headerBuf.Bytes()

	// Calculate lengths
	preludeLen := 12 // total length (4) + header length (4) + prelude CRC (4)
	headersLen := len(headerBytes)
	payloadLen := len(payload)
	messageCRCLen := 4
	totalLen := preludeLen + headersLen + payloadLen + messageCRCLen

	// Build message
	var msg bytes.Buffer

	// Prelude
	binary.Write(&msg, binary.BigEndian, uint32(totalLen))
	binary.Write(&msg, binary.BigEndian, uint32(headersLen))

	// Calculate prelude CRC
	preludeBytes := msg.Bytes()
	preludeCRC := crc32.ChecksumIEEE(preludeBytes)
	binary.Write(&msg, binary.BigEndian, preludeCRC)

	// Headers
	msg.Write(headerBytes)

	// Payload
	msg.Write(payload)

	// Calculate message CRC (entire message except the CRC itself)
	messageBytes := msg.Bytes()
	messageCRC := crc32.ChecksumIEEE(messageBytes)
	binary.Write(&msg, binary.BigEndian, messageCRC)

	return msg.Bytes()
}
