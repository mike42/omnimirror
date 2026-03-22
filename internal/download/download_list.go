package download

// DownloadEntry represents a file to be downloaded.
type DownloadEntry struct {
	RelPath  string
	Size     int64
	Checksum string
}

// DownloadList collects files to download, deduplicating by path and skipping
// files that already exist in the mirror with the expected size.
type DownloadList struct {
	dm      *DirectoryManager
	entries []DownloadEntry
	seen    map[string]bool
}

// NewDownloadList creates a DownloadList backed by the given DirectoryManager.
func NewDownloadList(dm *DirectoryManager) *DownloadList {
	return &DownloadList{
		dm:   dm,
		seen: make(map[string]bool),
	}
}

// Add submits a file as a download candidate. The file is added only if it is
// not already on the list and does not already exist in the mirror with the
// expected size. Returns true if the file was added.
func (dl *DownloadList) Add(relPath string, size int64, checksum string) (bool, error) {
	if dl.seen[relPath] {
		return false, nil
	}
	exists, err := dl.dm.FileExists(relPath, size)
	if err != nil {
		return false, err
	}
	if exists {
		dl.seen[relPath] = true
		return false, nil
	}
	dl.seen[relPath] = true
	dl.entries = append(dl.entries, DownloadEntry{
		RelPath:  relPath,
		Size:     size,
		Checksum: checksum,
	})
	return true, nil
}

// Entries returns the list of files to download.
func (dl *DownloadList) Entries() []DownloadEntry {
	return dl.entries
}

// Len returns the number of files to download.
func (dl *DownloadList) Len() int {
	return len(dl.entries)
}
