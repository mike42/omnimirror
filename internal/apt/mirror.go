package apt

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/mike42/omnimirror/internal/download"
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

	// Phase 1: Stage metadata for all suites.
	for _, suite := range cfg.Suites {
		if err := stageSuiteMetadata(dm, baseURL, suite, cfg); err != nil {
			return fmt.Errorf("staging metadata for suite %q: %w", suite, err)
		}
	}

	// Phase 2: Content download (TODO: cross-suite .deb deduplication).

	// Phase 3: Commit all staged metadata.
	return dm.Commit()
}

func stageSuiteMetadata(dm *download.DirectoryManager, baseURL, suite string, cfg MirrorConfig) error {
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

	// We will download all metadata, TODO filter this..
	entries := release.SHA256

	for _, entry := range entries {
		relPath := fmt.Sprintf("dists/%s/%s", suite, entry.Filename)
		entryURL := suiteURL + "/" + entry.Filename
		log.Printf("Fetching %s", entryURL)

		body, err := httpGet(entryURL)
		if err != nil {
			log.Printf("Warning: could not fetch %s: %v", entryURL, err)
			continue
		}
		if err := dm.StageMetadata(relPath, body); err != nil {
			body.Close()
			return fmt.Errorf("staging %s: %w", relPath, err)
		}
		body.Close()

		_ = entry // TODO: verify file checksum after staging
	}

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
	if err := dm.StageMetadata(inReleasePath, strings.NewReader(string(inReleaseData))); err != nil {
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
		if err := dm.StageMetadata(relPath, body); err != nil {
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

// filterReleaseEntries returns the SHA256 entries that match the given components and architectures.
func filterReleaseEntries(entries []control.SHA256FileHash, components, archs []string) []control.SHA256FileHash {
	compSet := make(map[string]struct{}, len(components))
	for _, c := range components {
		compSet[c] = struct{}{}
	}
	archSet := make(map[string]struct{}, len(archs))
	for _, a := range archs {
		archSet[a] = struct{}{}
	}

	var result []control.SHA256FileHash
	for _, e := range entries {
		parts := strings.SplitN(e.Filename, "/", 2)
		if len(parts) < 2 {
			continue
		}
		comp := parts[0]
		rest := parts[1]

		// Component must match.
		if _, ok := compSet[comp]; !ok {
			continue
		}

		// Exclude source entries.
		// TODO should be configurable
		if strings.HasPrefix(rest, "source/") {
			continue
		}

		// Filter out architecture-specific entries for things we are not mirroring
		if strings.HasPrefix(rest, "binary-") {
			// Extract arch from "binary-{arch}/..."
			archAndFile := strings.TrimPrefix(rest, "binary-")
			slashIdx := strings.Index(archAndFile, "/")
			if slashIdx < 0 {
				continue
			}
			arch := archAndFile[:slashIdx]
			if _, ok := archSet[arch]; !ok {
				continue
			}
		}

		result = append(result, e)
	}
	return result
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
