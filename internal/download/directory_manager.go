package download

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stagingDirName = ".staging"

// MirrorDirectoryEntry records a staged metadata file and its SHA-256 checksum.
type MirrorDirectoryEntry struct {
	RelPath  string
	Checksum string
}

// DirectoryManager manages a mirror directory with staging support.
//
// Metadata files are staged to a .staging/ subdirectory and committed in
// reverse order (last staged = first committed). Content files are written
// via staging with checksum verification, then moved directly into the mirror.
type DirectoryManager struct {
	mirrorDir  string
	stagingDir string
	staged     []MirrorDirectoryEntry
	content    []MirrorDirectoryEntry
}

// NewDirectoryManager creates a DirectoryManager for the given mirror directory.
// The mirror directory and staging subdirectory are created if they do not exist.
func NewDirectoryManager(mirrorDir string) (*DirectoryManager, error) {
	absDir, err := filepath.Abs(mirrorDir)
	if err != nil {
		return nil, fmt.Errorf("resolving mirror directory: %w", err)
	}
	stagingDir := filepath.Join(absDir, stagingDirName)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating staging directory: %w", err)
	}
	return &DirectoryManager{
		mirrorDir:  absDir,
		stagingDir: stagingDir,
	}, nil
}

// FileExists reports whether a file exists in the mirror with the expected size.
// This is much cheaper than a full checksum.
func (dm *DirectoryManager) FileExists(relPath string, size int64) (bool, error) {
	fullPath, err := dm.resolveMirrorPath(relPath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(fullPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking file %q: %w", relPath, err)
	}
	return info.Mode().IsRegular() && info.Size() == size, nil
}

// StageMetadata writes a metadata file to the staging directory.
// The file is added to a stack so that Commit moves files in reverse order.
// The SHA-256 checksum is always computed. If expectedChecksum is non-empty,
// the computed checksum is verified against it.
func (dm *DirectoryManager) StageMetadata(relPath string, expectedChecksum string, body io.Reader) error {
	fullPath, err := dm.resolveStagingPath(relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("creating staging subdirectory for %q: %w", relPath, err)
	}
	checksum, err := writeFileHashed(fullPath, expectedChecksum, body)
	if err != nil {
		return fmt.Errorf("staging metadata %q: %w", relPath, err)
	}
	dm.staged = append(dm.staged, MirrorDirectoryEntry{RelPath: relPath, Checksum: checksum})
	return nil
}

// StagedFiles returns the list of staged metadata entries with their checksums.
func (dm *DirectoryManager) StagedFiles() []MirrorDirectoryEntry {
	return dm.staged
}

// KeepExistingFile checks whether a file already exists in the
// mirror directory with the expected checksum. If it does, the file is recorded
// in the content list and true is returned, allowing the caller to skip
// re-downloading it. If expectedChecksum is empty, the file cannot be verified
// and false is returned.
func (dm *DirectoryManager) KeepExistingFile(relPath string, expectedChecksum string) (bool, error) {
	if expectedChecksum == "" {
		return false, nil
	}
	fullPath, err := dm.resolveMirrorPath(relPath)
	if err != nil {
		return false, err
	}
	checksum, err := checksumFile(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("checksumming %q: %w", relPath, err)
	}
	if !strings.EqualFold(checksum, expectedChecksum) {
		return false, nil
	}
	if dm.isContentFile(relPath) {
		// already on the list: the caller should not be re-checking the same file repeatedly.
		log.Printf("Warning: we checked whether file %q was already in the mirror more than once. This is probably a bug.", relPath)
		return true, nil
	}
	dm.content = append(dm.content, MirrorDirectoryEntry{RelPath: relPath, Checksum: checksum})
	return true, nil
}

// ContentFiles returns the list of pre-existing content entries that were kept.
func (dm *DirectoryManager) ContentFiles() []MirrorDirectoryEntry {
	return dm.content
}

// ReadMetadataFile opens a metadata file for reading. It first checks the
// staging directory (for newly staged files), then falls back to the mirror
// directory (for pre-existing files kept via KeepExistingFile).
func (dm *DirectoryManager) ReadMetadataFile(relPath string) (io.ReadCloser, error) {
	if dm.isStagedFile(relPath) {
		fullPath, err := dm.resolveStagingPath(relPath)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(fullPath)
		if err != nil {
			return nil, fmt.Errorf("opening staged file %q: %w", relPath, err)
		}
		return f, nil
	}
	if dm.isContentFile(relPath) {
		fullPath, err := dm.resolveMirrorPath(relPath)
		if err != nil {
			return nil, err
		}
		f, err := os.Open(fullPath)
		if err != nil {
			return nil, fmt.Errorf("opening existing file %q: %w", relPath, err)
		}
		return f, nil
	}
	return nil, fmt.Errorf("file %q is not staged or in content list", relPath)
}

// WriteContentFile writes a content file to the mirror directory.
// The file is first written to the staging directory, then its size and
// SHA-256 checksum are verified. If verification passes, the file is moved
// to its final location in the mirror. If verification fails, the temporary
// file is removed and an error is returned.
func (dm *DirectoryManager) WriteContentFile(relPath string, expectedSize int64, expectedChecksum string, body io.Reader) error {
	mirrorPath, err := dm.resolveMirrorPath(relPath)
	if err != nil {
		return err
	}
	// Write to a temp file in the staging directory.
	tmpFile, err := os.CreateTemp(dm.stagingDir, "content-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file for %q: %w", relPath, err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // clean up on any error path

	hasher := sha256.New()
	w := io.MultiWriter(tmpFile, hasher)
	n, err := io.Copy(w, body)
	if closeErr := tmpFile.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("writing content file %q: %w", relPath, err)
	}
	// Verify size.
	if n != expectedSize {
		return fmt.Errorf("content file %q: expected size %d, got %d", relPath, expectedSize, n)
	}
	// Verify checksum.
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return fmt.Errorf("content file %q: expected checksum %s, got %s", relPath, expectedChecksum, actualChecksum)
	}
	// Move into place.
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o755); err != nil {
		return fmt.Errorf("creating directory for %q: %w", relPath, err)
	}
	if err := os.Rename(tmpPath, mirrorPath); err != nil {
		return fmt.Errorf("moving content file %q into mirror: %w", relPath, err)
	}
	// Record on file list for mirror
	dm.content = append(dm.content, MirrorDirectoryEntry{RelPath: relPath, Checksum: actualChecksum})
	return nil
}

// Commit moves staged metadata files from the staging directory into the
// mirror directory. Files are committed in reverse stage order (last staged
// is moved first), so that top-level index files are updated last.
// After all files are copied into the mirror proper, a file is written containing
// SHA-256 checksums in sha256sum format, and the staging directory is removed.
func (dm *DirectoryManager) Commit() error {
	for i := len(dm.staged) - 1; i >= 0; i-- {
		relPath := dm.staged[i].RelPath
		srcPath, err := dm.resolveStagingPath(relPath)
		if err != nil {
			return err
		}
		dstPath, err := dm.resolveMirrorPath(relPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("creating directory for %q: %w", relPath, err)
		}
		if err := os.Rename(srcPath, dstPath); err != nil {
			return fmt.Errorf("committing %q: %w", relPath, err)
		}
	}
	if err := dm.writeMirrorChecksums(); err != nil {
		return fmt.Errorf("writing mirror checksums: %w", err)
	}
	dm.staged = nil
	// Best-effort removal of staging directory tree.
	_ = os.RemoveAll(dm.stagingDir)
	return nil
}

// writeMirrorChecksums writes a mirror.txt file in the mirror directory
// containing SHA-256 checksums for all staged and content files, in the
// format expected by the sha256sum program.
func (dm *DirectoryManager) writeMirrorChecksums() error {
	var lines []string
	lines = append(lines, "# updated: "+time.Now().UTC().Format(time.RFC3339))
	// TODO sort these by name so that they can be compared more easily via diff.
	for _, entry := range dm.staged {
		lines = append(lines, entry.Checksum+"  "+entry.RelPath)
	}
	for _, entry := range dm.content {
		lines = append(lines, entry.Checksum+"  "+entry.RelPath)
	}
	checksumPath := filepath.Join(dm.mirrorDir, "mirror.txt")
	data := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(checksumPath, []byte(data), 0o644); err != nil {
		return err
	}
	return nil
}

// resolveAndValidate resolves a relative path against a base directory
// and ensures the result is contained within it.
func resolveAndValidate(baseDir, relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("empty relative path")
	}
	cleaned := filepath.Clean(relPath)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) || cleaned == ".." {
		return "", fmt.Errorf("invalid relative path: %q", relPath)
	}
	full := filepath.Join(baseDir, cleaned)
	// Double-check containment after joining.
	if !strings.HasPrefix(full, baseDir+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes base directory", relPath)
	}
	return full, nil
}

func (dm *DirectoryManager) resolveMirrorPath(relPath string) (string, error) {
	return resolveAndValidate(dm.mirrorDir, relPath)
}

func (dm *DirectoryManager) resolveStagingPath(relPath string) (string, error) {
	return resolveAndValidate(dm.stagingDir, relPath)
}

func (dm *DirectoryManager) isStagedFile(relPath string) bool {
	for _, s := range dm.staged {
		if s.RelPath == relPath {
			return true
		}
	}
	return false
}

func (dm *DirectoryManager) isContentFile(relPath string) bool {
	for _, s := range dm.content {
		if s.RelPath == relPath {
			return true
		}
	}
	return false
}

// checksumFile computes the SHA-256 checksum of a file on disk.
func checksumFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// writeFileHashed creates or overwrites a file, computing its SHA-256 checksum.
// If expectedChecksum is non-empty, the computed checksum is verified against it
// and the file is removed on mismatch. Returns the hex-encoded SHA-256 checksum.
func writeFileHashed(path string, expectedChecksum string, r io.Reader) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	w := io.MultiWriter(f, hasher)
	_, err = io.Copy(w, r)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if expectedChecksum != "" && !strings.EqualFold(checksum, expectedChecksum) {
		os.Remove(path)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, checksum)
	}
	return checksum, nil
}
