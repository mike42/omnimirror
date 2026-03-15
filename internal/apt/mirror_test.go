package apt

import (
	"os"
	"testing"
)

func TestParseInRelease(t *testing.T) {
	data, err := os.ReadFile("../../tests/resources/apt/mozilla_InRelease")
	if err != nil {
		t.Fatalf("reading test file: %v", err)
	}

	release, err := parseRelease(data)
	if err != nil {
		t.Fatalf("parseRelease: %v", err)
	}

	if release.Suite != "mozilla" {
		t.Errorf("Suite = %q, want %q", release.Suite, "mozilla")
	}
	if release.Codename != "mozilla" {
		t.Errorf("Codename = %q, want %q", release.Codename, "mozilla")
	}
	if release.Architectures != "all amd64 arm64 i386" {
		t.Errorf("Architectures = %q", release.Architectures)
	}
	if release.Components != "main" {
		t.Errorf("Components = %q", release.Components)
	}

	// Check SHA256 hashes were parsed.
	if len(release.SHA256) == 0 {
		t.Fatal("no SHA256 hashes parsed")
	}

	hash, ok := findSHA256(release.SHA256, "main/binary-amd64/Packages")
	if !ok {
		t.Fatal("SHA256 entry for main/binary-amd64/Packages not found")
	}
	if hash.Size != 88708 {
		t.Errorf("Size = %d, want 88708", hash.Size)
	}
}

func TestParseRelease(t *testing.T) {
	data, err := os.ReadFile("../../tests/resources/apt/mozilla_Release")
	if err != nil {
		t.Fatalf("reading test file: %v", err)
	}

	release, err := parseRelease(data)
	if err != nil {
		t.Fatalf("parseRelease: %v", err)
	}

	if release.Suite != "mozilla" {
		t.Errorf("Suite = %q, want %q", release.Suite, "mozilla")
	}
	if len(release.SHA256) != 4 {
		t.Errorf("got %d SHA256 entries, want 4", len(release.SHA256))
	}
}

func TestFilterOrAll(t *testing.T) {
	available := []string{"main", "contrib", "non-free"}

	// No filter returns all.
	got := filterOrAll(nil, available)
	if len(got) != 3 {
		t.Errorf("expected all 3, got %d", len(got))
	}

	// With filter returns only filtered.
	got = filterOrAll([]string{"main"}, available)
	if len(got) != 1 || got[0] != "main" {
		t.Errorf("expected [main], got %v", got)
	}
}

func TestEnsureContains(t *testing.T) {
	slice := []string{"amd64", "arm64"}

	// Already present.
	got := ensureContains(slice, "amd64")
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}

	// Not present.
	got = ensureContains(slice, "all")
	if len(got) != 3 || got[2] != "all" {
		t.Errorf("expected [amd64 arm64 all], got %v", got)
	}
}
