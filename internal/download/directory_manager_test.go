package download

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestNewDirectoryManager(t *testing.T) {
	dir := t.TempDir()
	mirrorDir := filepath.Join(dir, "mirror")

	dm, err := NewDirectoryManager(mirrorDir)
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}
	// Mirror and staging directories should exist.
	if info, err := os.Stat(dm.mirrorDir); err != nil || !info.IsDir() {
		t.Fatalf("mirror directory not created")
	}
	if info, err := os.Stat(dm.stagingDir); err != nil || !info.IsDir() {
		t.Fatalf("staging directory not created")
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	// File does not exist.
	exists, err := dm.FileExists("pool/main/f/foo_1.0.deb", 100)
	if err != nil {
		t.Fatalf("FileExists: %v", err)
	}
	if exists {
		t.Fatal("expected file to not exist")
	}

	// Create the file with a known size.
	content := []byte("hello world")
	filePath := filepath.Join(dm.mirrorDir, "pool", "main", "f", "foo_1.0.deb")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Correct size.
	exists, err = dm.FileExists("pool/main/f/foo_1.0.deb", int64(len(content)))
	if err != nil {
		t.Fatalf("FileExists: %v", err)
	}
	if !exists {
		t.Fatal("expected file to exist")
	}

	// Wrong size.
	exists, err = dm.FileExists("pool/main/f/foo_1.0.deb", 999)
	if err != nil {
		t.Fatalf("FileExists: %v", err)
	}
	if exists {
		t.Fatal("expected file to not exist with wrong size")
	}
}

func TestStageMetadataAndCommit(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	// Stage two metadata files.
	inRelease := []byte("InRelease content")
	packages := []byte("Packages content")

	if err := dm.StageMetadata("dists/trixie/InRelease", "", bytes.NewReader(inRelease)); err != nil {
		t.Fatalf("StageMetadata InRelease: %v", err)
	}
	if err := dm.StageMetadata("dists/trixie/main/binary-amd64/Packages", "", bytes.NewReader(packages)); err != nil {
		t.Fatalf("StageMetadata Packages: %v", err)
	}

	// Staged files should exist in staging dir.
	stagedInRelease := filepath.Join(dm.stagingDir, "dists", "trixie", "InRelease")
	if _, err := os.Stat(stagedInRelease); err != nil {
		t.Fatalf("staged InRelease not found: %v", err)
	}

	// Files should not yet be in the mirror.
	mirrorInRelease := filepath.Join(dm.mirrorDir, "dists", "trixie", "InRelease")
	if _, err := os.Stat(mirrorInRelease); !os.IsNotExist(err) {
		t.Fatal("InRelease should not be in mirror before commit")
	}

	// Commit.
	if err := dm.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// Both files should now be in the mirror.
	got, err := os.ReadFile(mirrorInRelease)
	if err != nil {
		t.Fatalf("reading committed InRelease: %v", err)
	}
	if !bytes.Equal(got, inRelease) {
		t.Fatalf("InRelease content mismatch")
	}

	mirrorPackages := filepath.Join(dm.mirrorDir, "dists", "trixie", "main", "binary-amd64", "Packages")
	got, err = os.ReadFile(mirrorPackages)
	if err != nil {
		t.Fatalf("reading committed Packages: %v", err)
	}
	if !bytes.Equal(got, packages) {
		t.Fatalf("Packages content mismatch")
	}

	// Staging directory should be cleaned up.
	if _, err := os.Stat(dm.stagingDir); !os.IsNotExist(err) {
		t.Fatal("staging directory should be removed after commit")
	}
}

func TestCommitOrder(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	// Stage files in order: InRelease first, then Packages.
	if err := dm.StageMetadata("dists/trixie/InRelease", "", strings.NewReader("a")); err != nil {
		t.Fatal(err)
	}
	if err := dm.StageMetadata("dists/trixie/main/binary-amd64/Packages", "", strings.NewReader("b")); err != nil {
		t.Fatal(err)
	}

	// The staged list should reflect insertion order.
	staged := dm.StagedFiles()
	if len(staged) != 2 {
		t.Fatalf("expected 2 staged files, got %d", len(staged))
	}
	if staged[0].RelPath != "dists/trixie/InRelease" {
		t.Fatalf("expected InRelease first in staged list")
	}
	if staged[1].RelPath != "dists/trixie/main/binary-amd64/Packages" {
		t.Fatalf("expected Packages second in staged list")
	}

	// After commit, staged list should be empty.
	if err := dm.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(dm.staged) != 0 {
		t.Fatal("staged list should be empty after commit")
	}
}

func TestReadMetadataFile(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	content := []byte("staged content here")
	if err := dm.StageMetadata("dists/trixie/InRelease", "", bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}

	// Read it back.
	rc, err := dm.ReadMetadataFile("dists/trixie/InRelease")
	if err != nil {
		t.Fatalf("ReadMetadataFile: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}
}

func TestStageMetadataChecksum(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	content := []byte("verified content")
	checksum := sha256hex(content)

	// Correct checksum should succeed and be recorded.
	if err := dm.StageMetadata("dists/trixie/main/binary-amd64/Packages", checksum, bytes.NewReader(content)); err != nil {
		t.Fatalf("StageMetadata with valid checksum: %v", err)
	}
	staged := dm.StagedFiles()
	if len(staged) != 1 {
		t.Fatalf("expected 1 staged file, got %d", len(staged))
	}
	if staged[0].Checksum != checksum {
		t.Errorf("stored checksum = %q, want %q", staged[0].Checksum, checksum)
	}

	// Wrong checksum should fail.
	badChecksum := sha256hex([]byte("different"))
	err = dm.StageMetadata("dists/trixie/main/binary-arm64/Packages", badChecksum, bytes.NewReader(content))
	if err == nil {
		t.Fatal("expected error for bad checksum")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("unexpected error: %v", err)
	}

	// No checksum provided — should compute and store it.
	other := []byte("other content")
	if err := dm.StageMetadata("dists/trixie/InRelease", "", bytes.NewReader(other)); err != nil {
		t.Fatalf("StageMetadata without checksum: %v", err)
	}
	staged = dm.StagedFiles()
	if len(staged) != 2 {
		t.Fatalf("expected 2 staged files, got %d", len(staged))
	}
	expectedChecksum := sha256hex(other)
	if staged[1].Checksum != expectedChecksum {
		t.Errorf("computed checksum = %q, want %q", staged[1].Checksum, expectedChecksum)
	}
}

func TestReadMetadataFileRejectsUnknown(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	_, err = dm.ReadMetadataFile("dists/trixie/InRelease")
	if err == nil {
		t.Fatal("expected error for unknown file")
	}
}

func TestKeepExistingMetadata(t *testing.T) {
	dir := t.TempDir()
	mirrorDir := filepath.Join(dir, "mirror")
	dm, err := NewDirectoryManager(mirrorDir)
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	// Create a file in the mirror directory.
	content := []byte("existing Packages content")
	checksum := sha256hex(content)
	relPath := "dists/trixie/main/binary-amd64/Packages.xz"
	fullPath := filepath.Join(mirrorDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Matching checksum — should keep it.
	kept, err := dm.KeepExistingFile(relPath, checksum)
	if err != nil {
		t.Fatalf("KeepExistingFile: %v", err)
	}
	if !kept {
		t.Fatal("expected file to be kept")
	}
	if len(dm.ContentFiles()) != 1 {
		t.Fatalf("expected 1 content file, got %d", len(dm.ContentFiles()))
	}
	if dm.ContentFiles()[0].Checksum != checksum {
		t.Errorf("content checksum = %q, want %q", dm.ContentFiles()[0].Checksum, checksum)
	}

	// Wrong checksum — should not keep it.
	kept, err = dm.KeepExistingFile(relPath, sha256hex([]byte("wrong")))
	if err != nil {
		t.Fatalf("KeepExistingFile wrong checksum: %v", err)
	}
	if kept {
		t.Fatal("expected file not to be kept with wrong checksum")
	}

	// Empty checksum — should not keep it.
	kept, err = dm.KeepExistingFile(relPath, "")
	if err != nil {
		t.Fatalf("KeepExistingFile empty checksum: %v", err)
	}
	if kept {
		t.Fatal("expected file not to be kept with empty checksum")
	}

	// Missing file — should not keep it.
	kept, err = dm.KeepExistingFile("dists/trixie/nonexistent", checksum)
	if err != nil {
		t.Fatalf("KeepExistingFile missing: %v", err)
	}
	if kept {
		t.Fatal("expected missing file not to be kept")
	}
}

func TestReadMetadataFileFromContent(t *testing.T) {
	dir := t.TempDir()
	mirrorDir := filepath.Join(dir, "mirror")
	dm, err := NewDirectoryManager(mirrorDir)
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	// Create an existing file and keep it.
	content := []byte("kept metadata content")
	checksum := sha256hex(content)
	relPath := "dists/trixie/main/binary-amd64/Packages"
	fullPath := filepath.Join(mirrorDir, relPath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	kept, err := dm.KeepExistingFile(relPath, checksum)
	if err != nil || !kept {
		t.Fatalf("KeepExistingFile: kept=%v, err=%v", kept, err)
	}

	// ReadMetadataFile should find it via the content list.
	rc, err := dm.ReadMetadataFile(relPath)
	if err != nil {
		t.Fatalf("ReadMetadataFile: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content mismatch: got %q, want %q", got, content)
	}
}

func TestWriteContentFile(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	content := []byte("this is a fake .deb file")
	checksum := sha256hex(content)

	err = dm.WriteContentFile("pool/main/f/foo_1.0.deb", int64(len(content)), checksum, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("WriteContentFile: %v", err)
	}

	// File should be in the mirror directory.
	got, err := os.ReadFile(filepath.Join(dm.mirrorDir, "pool", "main", "f", "foo_1.0.deb"))
	if err != nil {
		t.Fatalf("reading content file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("content mismatch")
	}

	// No temp files should remain in staging.
	entries, err := os.ReadDir(dm.stagingDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWriteContentFileBadChecksum(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	content := []byte("this is a fake .deb file")
	badChecksum := sha256hex([]byte("different content"))

	err = dm.WriteContentFile("pool/main/f/foo_1.0.deb", int64(len(content)), badChecksum, bytes.NewReader(content))
	if err == nil {
		t.Fatal("expected error for bad checksum")
	}

	// File should not be in the mirror.
	mirrorPath := filepath.Join(dm.mirrorDir, "pool", "main", "f", "foo_1.0.deb")
	if _, statErr := os.Stat(mirrorPath); !os.IsNotExist(statErr) {
		t.Fatal("corrupt file should not be in mirror")
	}
}

func TestWriteContentFileBadSize(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	content := []byte("this is a fake .deb file")
	checksum := sha256hex(content)

	err = dm.WriteContentFile("pool/main/f/foo_1.0.deb", 999, checksum, bytes.NewReader(content))
	if err == nil {
		t.Fatal("expected error for wrong size")
	}

	mirrorPath := filepath.Join(dm.mirrorDir, "pool", "main", "f", "foo_1.0.deb")
	if _, statErr := os.Stat(mirrorPath); !os.IsNotExist(statErr) {
		t.Fatal("file with wrong size should not be in mirror")
	}
}

func TestPathTraversal(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	cases := []string{
		"../etc/passwd",
		"/etc/passwd",
		"foo/../../etc/passwd",
		"..",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			_, err := dm.FileExists(path, 0)
			if err == nil {
				t.Error("FileExists should reject path traversal")
			}
			err = dm.StageMetadata(path, "", strings.NewReader("x"))
			if err == nil {
				t.Error("StageMetadata should reject path traversal")
			}
			err = dm.WriteContentFile(path, 1, "abc", strings.NewReader("x"))
			if err == nil {
				t.Error("WriteContentFile should reject path traversal")
			}
		})
	}
}

func TestEmptyPath(t *testing.T) {
	dir := t.TempDir()
	dm, err := NewDirectoryManager(filepath.Join(dir, "mirror"))
	if err != nil {
		t.Fatalf("NewDirectoryManager: %v", err)
	}

	_, err = dm.FileExists("", 0)
	if err == nil {
		t.Error("FileExists should reject empty path")
	}
}
