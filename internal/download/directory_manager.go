package download

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const stagingDirName = ".staging"

// DirectoryManager manages a mirror directory with staging support.
//
// Metadata files are staged to a .staging/ subdirectory and committed in
// reverse order (last staged = first committed). Content files are written
// via staging with checksum verification, then moved directly into the mirror.
type DirectoryManager struct {
	mirrorDir  string
	stagingDir string
	staged     []string // stack of staged metadata relative paths
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
// If expectedChecksum is non-empty, the SHA-256 of the written data is verified.
func (dm *DirectoryManager) StageMetadata(relPath string, expectedChecksum string, body io.Reader) error {
	fullPath, err := dm.resolveStagingPath(relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return fmt.Errorf("creating staging subdirectory for %q: %w", relPath, err)
	}
	if expectedChecksum == "" {
		if err := writeFile(fullPath, body); err != nil {
			return fmt.Errorf("staging metadata %q: %w", relPath, err)
		}
	} else {
		if err := writeFileVerified(fullPath, expectedChecksum, body); err != nil {
			return fmt.Errorf("staging metadata %q: %w", relPath, err)
		}
	}
	dm.staged = append(dm.staged, relPath)
	return nil
}

// ReadStagedFile opens a previously staged metadata file for reading.
// The relative path must match a file that was staged via StageMetadata;
// this prevents path traversal through arbitrary paths.
func (dm *DirectoryManager) ReadStagedFile(relPath string) (io.ReadCloser, error) {
	if !dm.isStagedFile(relPath) {
		return nil, fmt.Errorf("file %q was not staged", relPath)
	}
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
	return nil
}

// Commit moves staged metadata files from the staging directory into the
// mirror directory. Files are committed in reverse stage order (last staged
// is moved first), so that top-level index files are updated last.
// The staging directory is removed after all files are committed.
func (dm *DirectoryManager) Commit() error {
	for i := len(dm.staged) - 1; i >= 0; i-- {
		relPath := dm.staged[i]
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
	dm.staged = nil
	// Best-effort removal of staging directory tree.
	_ = os.RemoveAll(dm.stagingDir)
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
		if s == relPath {
			return true
		}
	}
	return false
}

// writeFile creates or overwrites a file with the contents of r.
func writeFile(path string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	return err
}

// writeFileVerified creates or overwrites a file, verifying its SHA-256 checksum.
// If the checksum does not match, the file is removed and an error is returned.
func writeFileVerified(path string, expectedChecksum string, r io.Reader) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	w := io.MultiWriter(f, hasher)
	_, err = io.Copy(w, r)
	if closeErr := f.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	actualChecksum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		os.Remove(path)
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}
	return nil
}
