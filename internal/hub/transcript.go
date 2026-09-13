package hub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/store"
)

// transcriptLine is one JSONL record of a turn's transcript. The first
// line is the start record with the spec, then one line per event (an
// approval request is an event), one per approval decision, then the done
// record.
type transcriptLine struct {
	Kind     string              `json:"kind"`
	At       time.Time           `json:"at"`
	TurnID   string              `json:"turn_id,omitempty"`
	Engine   string              `json:"engine,omitempty"`
	Spec     *engine.TurnSpec    `json:"spec,omitempty"`
	Event    *engine.Event       `json:"event,omitempty"`
	Approval *transcriptApproval `json:"approval,omitempty"`
	Result   *engine.Result      `json:"result,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// transcriptApproval records how an approval request was settled.
type transcriptApproval struct {
	ID        string               `json:"id"`
	RequestID string               `json:"request_id"`
	Status    store.ApprovalStatus `json:"status"`
	Message   string               `json:"message,omitempty"`
	DecidedBy string               `json:"decided_by,omitempty"`
}

// transcript appends a turn's records to a JSONL file. Writes go through a
// buffer that is flushed on close, so a chatty turn costs a handful of
// syscalls rather than one per event. Callers serialise access.
type transcript struct {
	path string
	file *os.File
	buf  *bufio.Writer
	enc  *json.Encoder
}

// openTranscript creates the file, and its directory if needed.
func openTranscript(path string) (*transcript, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create transcript dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create transcript: %w", err)
	}
	buf := bufio.NewWriter(file)
	enc := json.NewEncoder(buf)
	// Prompts and replies are full of <, > and &; keep them readable.
	enc.SetEscapeHTML(false)
	return &transcript{path: path, file: file, buf: buf, enc: enc}, nil
}

// write appends one record.
func (t *transcript) write(line transcriptLine) error {
	if line.At.IsZero() {
		line.At = time.Now()
	}
	if err := t.enc.Encode(line); err != nil {
		return fmt.Errorf("write transcript %s: %w", t.path, err)
	}
	return nil
}

// close flushes and closes the file.
func (t *transcript) close() error {
	if err := t.buf.Flush(); err != nil {
		t.file.Close()
		return fmt.Errorf("flush transcript %s: %w", t.path, err)
	}
	if err := t.file.Close(); err != nil {
		return fmt.Errorf("close transcript %s: %w", t.path, err)
	}
	return nil
}
