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

	for _, suite := range cfg.Suites {
		if err := mirrorSuite(dm, baseURL, suite, cfg); err != nil {
			return fmt.Errorf("mirroring suite %q: %w", suite, err)
		}
	}

	return nil
}

func mirrorSuite(dm *download.DirectoryManager, baseURL, suite string, cfg MirrorConfig) error {
	suiteURL := baseURL + "/dists/" + suite

	// Stage release metadata files.
	release, err := fetchAndStageRelease(dm, suiteURL, suite)
	if err != nil {
		return err
	}

	// Determine which components and architectures to download.
	components := filterOrAll(cfg.Components, strings.Fields(release.Components))
	archs := filterOrAll(cfg.Archs, strings.Fields(release.Architectures))
	// Always include "all" architecture.
	archs = ensureContains(archs, "all")

	log.Printf("Suite %s: components=%v, architectures=%v", suite, components, archs)

	// Stage Packages index files for each component/arch combination.
	for _, comp := range components {
		for _, arch := range archs {
			indexPath := fmt.Sprintf("%s/binary-%s/Packages", comp, arch)
			relPath := fmt.Sprintf("dists/%s/%s", suite, indexPath)

			// Look up the expected checksum from the Release file.
			hash, ok := findSHA256(release.SHA256, indexPath)
			if !ok {
				log.Printf("Skipping %s (not listed in Release)", relPath)
				continue
			}

			indexURL := suiteURL + "/" + indexPath
			log.Printf("Fetching %s", indexURL)

			body, err := httpGet(indexURL)
			if err != nil {
				log.Printf("Warning: could not fetch %s: %v", indexURL, err)
				continue
			}
			if err := dm.StageMetadata(relPath, body); err != nil {
				body.Close()
				return fmt.Errorf("staging %s: %w", relPath, err)
			}
			body.Close()

			_ = hash // TODO: verify Packages file checksum after staging
		}
	}

	if cfg.DryRun {
		log.Printf("Dry run: skipping content download for suite %s", suite)
		return dm.Commit()
	}

	// TODO: implement content download logic.
	// - Open each staged Packages file with dm.ReadStagedFile()
	// - Parse with control.ParseBinaryIndex() to get []BinaryIndex
	// - For each package, check dm.FileExists(pkg.Filename, pkg.Size)
	// - Download missing .deb files with dm.WriteContentFile()
	// - Use errgroup with bounded concurrency for parallel downloads

	return dm.Commit()
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
	MD5Sum        []control.MD5FileHash  `delim:"\n" strip:"\n\r\t "`
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

// findSHA256 looks up a file's SHA256 hash entry in the release metadata.
func findSHA256(hashes []control.SHA256FileHash, path string) (control.SHA256FileHash, bool) {
	for _, h := range hashes {
		if h.Filename == path {
			return h, true
		}
	}
	return control.SHA256FileHash{}, false
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
