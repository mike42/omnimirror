package apt

import (
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/mike42/omnimirror/internal/download"
	"github.com/ulikunitz/xz"
	"pault.ag/go/debian/control"
)

// MirrorConfig holds the configuration for an APT mirror download.
type MirrorConfig struct {
	URL        string
	OutputDir  string
	Suites     []string
	Components []string
	Archs      []string
	DryRun     bool
}

// Mirror downloads an APT repository based on the given configuration.
func Mirror(cfg MirrorConfig) error {
	baseURL := strings.TrimRight(cfg.URL, "/")

	dm, err := download.NewDirectoryManager(cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("creating directory manager: %w", err)
	}

	dl := download.NewDownloadList(dm)

	// Phase 1: Stage metadata for all suites.
	for _, suite := range cfg.Suites {
		if err := stageSuiteMetadata(dm, dl, baseURL, suite, cfg); err != nil {
			return fmt.Errorf("staging metadata for suite %q: %w", suite, err)
		}
	}

	// Phase 2: Download content files.
	log.Printf("Download list: %d files", dl.Len())

	// Phase 3: Commit all staged metadata.
	return dm.Commit()
}

func stageSuiteMetadata(dm *download.DirectoryManager, dl *download.DownloadList, baseURL, suite string, cfg MirrorConfig) error {
	suiteURL := baseURL + "/dists/" + suite

	// Stage release metadata files.
	log.Printf("Staging release files for suite %s", suite)
	release, err := fetchAndStageRelease(dm, suiteURL, suite)
	if err != nil {
		return err
	}

	// Determine which components and architectures to download.
	components := filterOrAll(cfg.Components, strings.Fields(release.Components))
	archs := filterOrAll(cfg.Archs, strings.Fields(release.Architectures))
	// Always include "all" architecture.
	archs = ensureContains(archs, "all")

	log.Printf("Staging metadata for %s: components=%v, architectures=%v", suite, components, archs)

	// Filter metadata entries for selected architectures and components.
	repoArchs := strings.Fields(release.Architectures)
	entries := filterReleaseEntries(release.SHA256, repoArchs, archs, false)
	entries = filterByComponent(entries, components)

	// Split entries into compressed and uncompressed files.
	// Download compressed files first so we can derive uncompressed files
	// via decompression instead of relying on the server to serve them.
	compressed, uncompressed := splitByCompression(entries)

	// Stage compressed files first.
	for _, entry := range compressed {
		relPath := fmt.Sprintf("dists/%s/%s", suite, entry.Filename)
		entryURL := suiteURL + "/" + entry.Filename
		log.Printf("Fetching %s", entryURL)

		body, err := httpGet(entryURL)
		if err != nil {
			log.Printf("Warning: could not fetch %s: %v", entryURL, err)
			continue
		}
		if err := dm.StageMetadata(relPath, entry.Hash, body); err != nil {
			body.Close()
			return fmt.Errorf("staging %s: %w", relPath, err)
		}
		body.Close()
	}

	// Stage uncompressed files: prefer decompressing an already-staged
	// compressed variant over downloading the uncompressed file directly.
	for _, entry := range uncompressed {
		relPath := fmt.Sprintf("dists/%s/%s", suite, entry.Filename)

		if stageFromCompressed(dm, relPath, entry.Hash, suite, entry.Filename) {
			continue
		}

		// Fall back to downloading directly.
		entryURL := suiteURL + "/" + entry.Filename
		log.Printf("Fetching %s", entryURL)
		body, err := httpGet(entryURL)
		if err != nil {
			log.Printf("Warning: could not fetch %s: %v", entryURL, err)
			continue
		}
		if err := dm.StageMetadata(relPath, entry.Hash, body); err != nil {
			body.Close()
			return fmt.Errorf("staging %s: %w", relPath, err)
		}
		body.Close()
	}

	// Parse Packages files and collect content to download.
	for _, comp := range components {
		for _, arch := range archs {
			packagesPath := fmt.Sprintf("dists/%s/%s/binary-%s/Packages", suite, comp, arch)
			if err := collectPackages(dm, dl, packagesPath); err != nil {
				return fmt.Errorf("collecting packages from %s: %w", packagesPath, err)
			}
		}
	}

	return nil
}

// compressedExts lists file extensions that indicate compressed metadata files.
var compressedExts = []string{".gz", ".xz", ".bz2"}

// isCompressedFile reports whether the filename has a known compression extension.
func isCompressedFile(filename string) bool {
	for _, ext := range compressedExts {
		if strings.HasSuffix(filename, ext) {
			return true
		}
	}
	return false
}

// splitByCompression partitions entries into compressed and uncompressed files.
func splitByCompression(entries []control.SHA256FileHash) (compressed, uncompressed []control.SHA256FileHash) {
	for _, entry := range entries {
		if isCompressedFile(entry.Filename) {
			compressed = append(compressed, entry)
		} else {
			uncompressed = append(uncompressed, entry)
		}
	}
	return
}

// stageFromCompressed attempts to produce an uncompressed staged file by
// decompressing an already-staged compressed variant. It tries .xz then .bz2
// then .gz. Returns true if decompression succeeded.
func stageFromCompressed(dm *download.DirectoryManager, relPath, expectedChecksum, suite, filename string) bool {
	// Try each compressed variant in preference order.
	for _, ext := range []string{".xz", ".bz2", ".gz"} {
		compressedRelPath := fmt.Sprintf("dists/%s/%s%s", suite, filename, ext)
		rc, err := dm.ReadStagedFile(compressedRelPath)
		if err != nil {
			continue
		}
		decompressor, err := newDecompressor(rc, ext)
		if err != nil {
			rc.Close()
			continue
		}
		if err := dm.StageMetadata(relPath, expectedChecksum, decompressor); err != nil {
			rc.Close()
			log.Printf("Warning: decompressing %s failed: %v", compressedRelPath, err)
			continue
		}
		rc.Close()
		log.Printf("Decompressed %s from %s", relPath, compressedRelPath)
		return true
	}
	return false
}

// newDecompressor wraps an io.Reader with the appropriate decompression
// based on the file extension.
func newDecompressor(r io.Reader, ext string) (io.Reader, error) {
	switch ext {
	case ".gz":
		return gzip.NewReader(r)
	case ".xz":
		xzReader, err := xz.NewReader(r)
		if err != nil {
			return nil, err
		}
		return xzReader, nil
	case ".bz2":
		return bzip2.NewReader(r), nil
	default:
		return nil, fmt.Errorf("unsupported compression: %s", ext)
	}
}

// collectPackages reads a staged Packages file and adds its entries to the download list.
func collectPackages(dm *download.DirectoryManager, dl *download.DownloadList, packagesPath string) error {
	rc, err := dm.ReadStagedFile(packagesPath)
	if err != nil {
		log.Printf("Warning: could not read staged %s: %v", packagesPath, err)
		return nil
	}
	defer rc.Close()

	pkgs, err := parsePackages(rc)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", packagesPath, err)
	}

	for _, pkg := range pkgs {
		if pkg.Filename == "" {
			continue
		}
		if _, err := dl.Add(pkg.Filename, pkg.Size, pkg.SHA256); err != nil {
			return fmt.Errorf("adding %s to download list: %w", pkg.Filename, err)
		}
	}

	log.Printf("Parsed %s: %d packages", packagesPath, len(pkgs))
	return nil
}

// fetchAndStageRelease downloads and stages the release metadata files for a suite.
// It fetches InRelease (required), plus Release and Release.gpg (optional).
// Returns the parsed release information from InRelease.
func fetchAndStageRelease(dm *download.DirectoryManager, suiteURL, suite string) (*Release, error) {
	// InRelease is required — this is our primary source of metadata.
	inReleaseURL := suiteURL + "/InRelease"
	log.Printf("Fetching %s", inReleaseURL)
	inReleaseBody, err := httpGet(inReleaseURL)
	if err != nil {
		return nil, fmt.Errorf("fetching InRelease: %w", err)
	}
	defer inReleaseBody.Close()

	inReleaseData, err := io.ReadAll(inReleaseBody)
	if err != nil {
		return nil, fmt.Errorf("reading InRelease: %w", err)
	}

	// Stage the raw InRelease file (with PGP signature intact).
	inReleasePath := fmt.Sprintf("dists/%s/InRelease", suite)
	if err := dm.StageMetadata(inReleasePath, "", strings.NewReader(string(inReleaseData))); err != nil {
		return nil, fmt.Errorf("staging InRelease: %w", err)
	}

	// Parse the release information (the control parser handles PGP stripping).
	release, err := parseRelease(inReleaseData)
	if err != nil {
		return nil, fmt.Errorf("parsing InRelease: %w", err)
	}

	// Release and Release.gpg are optional — stage them if available.
	for _, file := range []string{"Release", "Release.gpg"} {
		fileURL := suiteURL + "/" + file
		relPath := fmt.Sprintf("dists/%s/%s", suite, file)
		log.Printf("Fetching %s", fileURL)
		body, err := httpGet(fileURL)
		if err != nil {
			log.Printf("Optional file %s not available: %v", file, err)
			continue
		}
		if err := dm.StageMetadata(relPath, "", body); err != nil {
			body.Close()
			log.Printf("Warning: could not stage %s: %v", file, err)
			continue
		}
		body.Close()
	}

	return release, nil
}

// Release holds the parsed fields from an InRelease or Release file.
type Release struct {
	control.Paragraph
	Suite         string
	Codename      string
	Architectures string
	Components    string
	Date          string
	MD5Sum        []control.MD5FileHash    `delim:"\n" strip:"\n\r\t "`
	SHA256        []control.SHA256FileHash `delim:"\n" strip:"\n\r\t "`
}

// PackageEntry holds the fields from a Packages file entry that are needed for downloading.
type PackageEntry struct {
	Filename string
	Size     int64
	SHA256   string
}

// packageParagraph is used to decode a Packages file entry via the control decoder.
// Size is a string here because the control decoder does not support int64 directly.
type packageParagraph struct {
	Filename string
	Size     string
	SHA256   string
}

// parsePackages parses a Packages file and returns the list of package entries.
func parsePackages(r io.Reader) ([]PackageEntry, error) {
	decoder, err := control.NewDecoder(r, nil)
	if err != nil {
		return nil, fmt.Errorf("creating decoder: %w", err)
	}
	var entries []PackageEntry
	for {
		var para packageParagraph
		if err := decoder.Decode(&para); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decoding package entry: %w", err)
		}
		size, err := strconv.ParseInt(para.Size, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing size %q for %s: %w", para.Size, para.Filename, err)
		}
		entries = append(entries, PackageEntry{
			Filename: para.Filename,
			Size:     size,
			SHA256:   para.SHA256,
		})
	}
	return entries, nil
}

// parseRelease parses an InRelease or Release file, handling PGP signatures.
func parseRelease(data []byte) (*Release, error) {
	reader := strings.NewReader(string(data))
	decoder, err := control.NewDecoder(reader, nil)
	if err != nil {
		return nil, fmt.Errorf("creating decoder: %w", err)
	}
	var release Release
	if err := decoder.Decode(&release); err != nil {
		return nil, fmt.Errorf("decoding release: %w", err)
	}
	return &release, nil
}

// filterOrAll returns the filter list if non-empty, otherwise returns all available values.
func filterOrAll(filter, available []string) []string {
	if len(filter) == 0 {
		return available
	}
	return filter
}

// ensureContains appends val to the slice if not already present.
func ensureContains(slice []string, val string) []string {
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

// filterReleaseEntries filters metadata entries based on architecture selection.
// repoArchs is the full list of architectures in the repository.
// selectedArchs is the subset to include (should already contain "all").
// includeExtras controls whether source and debian-installer are mirrored
func filterReleaseEntries(entries []control.SHA256FileHash, repoArchs, selectedArchs []string, includeExtras bool) []control.SHA256FileHash {
	// Build list of architectures to exclude.
	selected := make(map[string]bool, len(selectedArchs))
	for _, a := range selectedArchs {
		selected[a] = true
	}
	var excluded []string
	for _, a := range repoArchs {
		if !selected[a] {
			excluded = append(excluded, a)
		}
	}
	// Work-around: Debian trixie has eg. 'contrib/dep11/Components-mipsel.yml', while 'mipsel' is not in the list of
	// architectures in the Release file.
	//   Architectures: all amd64 arm64 armel armhf i386 ppc64el riscv64 s390x
	for _, a := range [...]string{"mipsel", "mips64el"} {
		if !selected[a] {
			excluded = append(excluded, a)
		}
	}

	var result []control.SHA256FileHash
	for _, entry := range entries {
		path := entry.Filename

		// Skip extras (source, debian-installer, i18n) unless requested.
		if !includeExtras && isExtraEntry(path) {
			continue
		}

		// Skip entries for excluded architectures.
		if isExcludedByArch(path, excluded) {
			continue
		}

		result = append(result, entry)
	}
	return result
}

// isExtraEntry returns true if the path is a source, debian-installer, or i18n entry.
func isExtraEntry(path string) bool {
	// Split off the component prefix (e.g. "main/") to get the category path.
	_, after, _ := strings.Cut(path, "/")
	return strings.HasPrefix(after, "source/") ||
		strings.HasPrefix(after, "Contents-source") ||
		strings.HasPrefix(after, "debian-installer/") ||
		strings.HasPrefix(after, "Contents-udeb-")
}

// isExcludedByArch returns true if the path references one of the excluded architectures.
func isExcludedByArch(path string, excluded []string) bool {
	for _, arch := range excluded {
		if strings.Contains(path, "/binary-"+arch+"/") ||
			strings.Contains(path, "Contents-"+arch) ||
			strings.Contains(path, "installer-"+arch) ||
			strings.Contains(path, "Contents-udeb-"+arch) ||
			strings.Contains(path, "Components-"+arch+".") {
			return true
		}
	}
	return false
}

// filterByComponent returns only entries whose path starts with one of the given components.
func filterByComponent(entries []control.SHA256FileHash, components []string) []control.SHA256FileHash {
	var result []control.SHA256FileHash
	for _, entry := range entries {
		for _, c := range components {
			if strings.HasPrefix(entry.Filename, c+"/") {
				result = append(result, entry)
				break
			}
		}
	}
	return result
}

// httpGet performs an HTTP GET and returns the response body.
// The caller is responsible for closing the returned ReadCloser.
func httpGet(url string) (io.ReadCloser, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}
