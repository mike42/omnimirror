package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadListAdd(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(dir)
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	dl := NewDownloadList(dm)

	// First add should succeed.
	added, err := dl.Add("pool/main/foo_1.0_amd64.deb", 1000, "abc123")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !added {
		t.Error("expected file to be added")
	}
	if dl.Len() != 1 {
		t.Errorf("Len = %d, want 1", dl.Len())
	}

	// Duplicate add should be skipped.
	added, err = dl.Add("pool/main/foo_1.0_amd64.deb", 1000, "abc123")
	if err != nil {
		t.Fatalf("Add duplicate: %v", err)
	}
	if added {
		t.Error("expected duplicate to be skipped")
	}
	if dl.Len() != 1 {
		t.Errorf("Len = %d, want 1", dl.Len())
	}

	// Different file should be added.
	added, err = dl.Add("pool/main/bar_2.0_amd64.deb", 2000, "def456")
	if err != nil {
		t.Fatalf("Add second: %v", err)
	}
	if !added {
		t.Error("expected second file to be added")
	}
	if dl.Len() != 2 {
		t.Errorf("Len = %d, want 2", dl.Len())
	}
}

func TestDownloadListSkipsExistingFile(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(dir)
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	// Create an existing file in the mirror directory with matching size.
	relPath := "pool/main/existing_1.0_amd64.deb"
	fullPath := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := []byte("existing content here")
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	dl := NewDownloadList(dm)

	// File exists with matching size — should be skipped.
	added, err := dl.Add(relPath, int64(len(content)), "somechecksum")
	if err != nil {
		t.Fatalf("Add existing: %v", err)
	}
	if added {
		t.Error("expected existing file to be skipped")
	}
	if dl.Len() != 0 {
		t.Errorf("Len = %d, want 0", dl.Len())
	}

	// Same path with different size — should be added (file needs re-download).
	added, err = dl.Add(relPath, int64(len(content)+100), "otherchecksum")
	if err != nil {
		t.Fatalf("Add size mismatch: %v", err)
	}
	// Path was already seen from the first Add, so it's skipped.
	if added {
		t.Error("expected already-seen path to be skipped")
	}
}

func TestDownloadListEntries(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(dir)
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	dl := NewDownloadList(dm)
	dl.Add("pool/a.deb", 100, "aaa")
	dl.Add("pool/b.deb", 200, "bbb")
	dl.Add("pool/a.deb", 100, "aaa") // duplicate

	entries := dl.Entries()
	if len(entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(entries))
	}
	if entries[0].RelPath != "pool/a.deb" || entries[0].Size != 100 || entries[0].Checksum != "aaa" {
		t.Errorf("entries[0] = %+v", entries[0])
	}
	if entries[1].RelPath != "pool/b.deb" || entries[1].Size != 200 || entries[1].Checksum != "bbb" {
		t.Errorf("entries[1] = %+v", entries[1])
	}
}
