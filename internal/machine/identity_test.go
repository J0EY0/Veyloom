package machine

import (
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

func TestFileIdentity_MissingFileMeansNoID(t *testing.T) {
	f := FileIdentity{Path: filepath.Join(t.TempDir(), "machine.json")}

	id, err := f.Load()
	if err != nil {
		t.Fatalf("Load on a missing file should not fail: %v", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty", id)
	}
}

func TestFileIdentity_RoundTripCreatesDirectories(t *testing.T) {
	// The parent directories do not exist yet; Save must create them.
	path := filepath.Join(t.TempDir(), "nested", "state", "machine.json")
	f := FileIdentity{Path: path}

	if err := f.Save("abc-123"); err != nil {
		t.Fatal(err)
	}
	id, err := f.Load()
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc-123" {
		t.Errorf("id = %q, want abc-123", id)
	}

	if goruntime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("identity file mode = %o, want 600", perm)
		}
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temporary file should not linger after Save")
	}
}

func TestFileIdentity_SaveOverwrites(t *testing.T) {
	f := FileIdentity{Path: filepath.Join(t.TempDir(), "machine.json")}
	if err := f.Save("first"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save("second"); err != nil {
		t.Fatal(err)
	}

	if id, _ := f.Load(); id != "second" {
		t.Errorf("id = %q, want second", id)
	}
}

func TestFileIdentity_CorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "machine.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (FileIdentity{Path: path}).Load(); err == nil {
		t.Error("a corrupt identity file must not be silently treated as empty")
	}
}

func TestMemoryIdentity(t *testing.T) {
	var m MemoryIdentity
	if id, _ := m.Load(); id != "" {
		t.Errorf("fresh MemoryIdentity should be empty, got %q", id)
	}
	if err := m.Save("x"); err != nil {
		t.Fatal(err)
	}
	if id, _ := m.Load(); id != "x" {
		t.Errorf("id = %q, want x", id)
	}
}
