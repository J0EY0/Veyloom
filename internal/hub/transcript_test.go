package hub

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/J0EY0/veyloom/internal/engine"
)

func TestTranscript_WritesJSONLAndCreatesDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "turn-1.jsonl")

	tx, err := openTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	spec := engine.TurnSpec{Prompt: "hi"}
	tx.write(transcriptLine{Kind: "start", TurnID: "turn-1", Engine: "fake", Spec: &spec})
	tx.write(transcriptLine{Kind: "event", Event: &engine.Event{Kind: engine.EventText, Text: "chunk"}})
	tx.write(transcriptLine{Kind: "done", Result: &engine.Result{Output: "chunk"}})
	if err := tx.close(); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var kinds []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var line transcriptLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("line is not JSON: %s", scanner.Text())
		}
		if line.At.IsZero() {
			t.Error("every line is timestamped")
		}
		kinds = append(kinds, line.Kind)
	}
	if len(kinds) != 3 || kinds[0] != "start" || kinds[2] != "done" {
		t.Errorf("unexpected records: %v", kinds)
	}
}
