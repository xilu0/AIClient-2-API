// Package debug provides request/response dumping for debugging.
package debug

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// DefaultDumpDir is the default directory for debug dumps.
	DefaultDumpDir = "/tmp/kiro-debug"
)

// Dumper handles request/response dumping for debugging.
type Dumper struct {
	enabled bool
	baseDir string
	mu      sync.Mutex
}

// Metadata contains debug metadata for a request.
type Metadata struct {
	SessionID     string    `json:"session_id"`
	RequestID     string    `json:"request_id,omitempty"`
	AccountUUID   string    `json:"account_uuid,omitempty"`
	Model         string    `json:"model,omitempty"`
	StartTime     time.Time `json:"start_time"`
	EndTime       time.Time `json:"end_time,omitempty"`
	StatusCode    int       `json:"status_code,omitempty"`
	Error         string    `json:"error,omitempty"`
	TriedAccounts []string  `json:"tried_accounts,omitempty"`
}

// Session represents a debug session for a single request.
type Session struct {
	dumper    *Dumper
	sessionID string
	dir       string
	metadata  *Metadata
	mu        sync.Mutex
	closed    bool
}

// NewDumper creates a new debug dumper.
// Enable with GO_KIRO_DEBUG_DUMP=true environment variable.
func NewDumper() *Dumper {
	enabled := os.Getenv("GO_KIRO_DEBUG_DUMP") == "true"
	baseDir := os.Getenv("GO_KIRO_DEBUG_DIR")
	if baseDir == "" {
		baseDir = DefaultDumpDir
	}

	if enabled {
		// Ensure base directory exists
		_ = os.MkdirAll(baseDir, 0755)
	}

	return &Dumper{
		enabled: enabled,
		baseDir: baseDir,
	}
}

// Enabled returns whether debug dumping is enabled.
func (d *Dumper) Enabled() bool {
	return d.enabled
}

// NewSession creates a new debug session.
// Returns nil if dumping is disabled.
func (d *Dumper) NewSession(sessionID string) *Session {
	if !d.enabled {
		return nil
	}

	dir := filepath.Join(d.baseDir, sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil
	}

	return &Session{
		dumper:    d,
		sessionID: sessionID,
		dir:       dir,
		metadata: &Metadata{
			SessionID: sessionID,
			StartTime: time.Now(),
		},
	}
}

// SetRequestID sets the request ID in metadata.
func (s *Session) SetRequestID(requestID string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.RequestID = requestID
}

// SetAccountUUID sets the current account UUID in metadata.
func (s *Session) SetAccountUUID(uuid string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.AccountUUID = uuid
}

// AddTriedAccount adds an account to the tried accounts list.
func (s *Session) AddTriedAccount(uuid string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.TriedAccounts = append(s.metadata.TriedAccounts, uuid)
}

// SetModel sets the model in metadata.
func (s *Session) SetModel(model string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.Model = model
}

// SetError sets the error in metadata.
func (s *Session) SetError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.Error = err.Error()
}

// SetStatusCode sets the status code in metadata.
func (s *Session) SetStatusCode(code int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.StatusCode = code
}

// DumpRequest writes the request body to request.json.
func (s *Session) DumpRequest(body []byte) {
	if s == nil {
		return
	}
	go s.writeFile("request.json", body)
}

// DumpRequestJSON writes the request as formatted JSON.
func (s *Session) DumpRequestJSON(v interface{}) {
	if s == nil {
		return
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	go s.writeFile("request.json", data)
}

// DumpResponse writes the response body to response.json.
func (s *Session) DumpResponse(body []byte) {
	if s == nil {
		return
	}
	go s.writeFile("response.json", body)
}

// DumpKiroRequest writes the Kiro request body to kiro_request.json.
func (s *Session) DumpKiroRequest(body []byte) {
	if s == nil {
		return
	}
	go s.writeFile("kiro_request.json", body)
}

// DumpKiroResponse writes the Kiro response to kiro_response.json.
func (s *Session) DumpKiroResponse(body []byte) {
	if s == nil {
		return
	}
	go s.writeFile("kiro_response.json", body)
}

// AppendKiroChunk appends a chunk to kiro_chunks.jsonl (JSON Lines format).
func (s *Session) AppendKiroChunk(chunk []byte) {
	if s == nil {
		return
	}
	s.appendToFile("kiro_chunks.jsonl", chunk)
}

// AppendClaudeChunk appends a converted Claude SSE event to claude_chunks.jsonl.
func (s *Session) AppendClaudeChunk(eventType string, data interface{}) {
	if s == nil {
		return
	}
	// Format as SSE-like JSON for easy comparison
	entry := map[string]interface{}{
		"event": eventType,
		"data":  data,
	}
	chunk, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.appendToFile("claude_chunks.jsonl", chunk)
}

// appendToFile appends data to a file in the session directory.
func (s *Session) appendToFile(name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	path := filepath.Join(s.dir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)
	f.Write([]byte("\n"))
}

// writeFile writes data to a file in the session directory.
func (s *Session) writeFile(name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	path := filepath.Join(s.dir, name)
	_ = os.WriteFile(path, data, 0644)
}

// Success marks the session as successful and cleans up.
func (s *Session) Success() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	// Remove the debug directory on success
	_ = os.RemoveAll(s.dir)
}

// Fail marks the session as failed and writes metadata.
func (s *Session) Fail(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	// Update metadata
	s.metadata.EndTime = time.Now()
	if err != nil {
		s.metadata.Error = err.Error()
	}

	// Write metadata file
	data, _ := json.MarshalIndent(s.metadata, "", "  ")
	path := filepath.Join(s.dir, "metadata.json")
	_ = os.WriteFile(path, data, 0644)
}

// Close closes the session. If not explicitly marked as success/fail,
// treats as failure and preserves files.
func (s *Session) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Default to failure if not explicitly closed
	s.Fail(fmt.Errorf("session closed without explicit success/fail"))
}

// ResponseWriter wraps an io.Writer to capture response data.
type ResponseWriter struct {
	writer  io.Writer
	session *Session
	buffer  []byte
	mu      sync.Mutex
}

// NewResponseWriter creates a writer that captures data for debugging.
func (s *Session) NewResponseWriter(w io.Writer) *ResponseWriter {
	if s == nil {
		return &ResponseWriter{writer: w}
	}
	return &ResponseWriter{
		writer:  w,
		session: s,
		buffer:  make([]byte, 0, 4096),
	}
}

// Write implements io.Writer.
func (rw *ResponseWriter) Write(p []byte) (n int, err error) {
	n, err = rw.writer.Write(p)
	if rw.session != nil && n > 0 {
		rw.mu.Lock()
		rw.buffer = append(rw.buffer, p[:n]...)
		rw.mu.Unlock()
	}
	return
}

// Bytes returns the captured data.
func (rw *ResponseWriter) Bytes() []byte {
	if rw == nil {
		return nil
	}
	rw.mu.Lock()
	defer rw.mu.Unlock()
	return rw.buffer
}

// Flush writes captured data to the session.
func (rw *ResponseWriter) Flush() {
	if rw == nil || rw.session == nil {
		return
	}
	rw.mu.Lock()
	data := rw.buffer
	rw.mu.Unlock()

	if len(data) > 0 {
		rw.session.DumpResponse(data)
	}
}
