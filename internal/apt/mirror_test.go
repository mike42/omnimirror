package apt

import (
	"os"
	"strings"
	"testing"

	"pault.ag/go/debian/control"
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

	// Verify architecture filtering via filterReleaseEntries.
	repoArchs := []string{"all", "amd64", "arm64", "i386"}
	selectedArchs := []string{"amd64", "all"}
	entries := filterReleaseEntries(release.SHA256, repoArchs, selectedArchs, false)
	found := false
	for _, e := range entries {
		if e.Filename == "main/binary-amd64/Packages" {
			if e.Size != 88708 {
				t.Errorf("Size = %d, want 88708", e.Size)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("SHA256 entry for main/binary-amd64/Packages not found")
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
	if len(release.SHA256) == 0 {
		t.Error("no SHA256 entries parsed")
	}
}

func TestFilterReleaseEntries(t *testing.T) {
	data, err := os.ReadFile("../../tests/resources/apt/debian_InRelease")
	if err != nil {
		t.Fatalf("reading test file: %v", err)
	}

	release, err := parseRelease(data)
	if err != nil {
		t.Fatalf("parseRelease: %v", err)
	}

	repoArchs := strings.Fields(release.Architectures) // all amd64 arm64 armel armhf i386 ppc64el riscv64 s390x
	allEntries := release.SHA256

	t.Run("binaries only for amd64", func(t *testing.T) {
		entries := filterReleaseEntries(allEntries, repoArchs, []string{"amd64", "all"}, false)

		assertContains(t, entries, "main/binary-amd64/Packages")
		assertContains(t, entries, "main/binary-all/Packages")
		assertContains(t, entries, "contrib/binary-amd64/Packages")
		assertContains(t, entries, "main/Contents-amd64")
		assertContains(t, entries, "main/dep11/Components-amd64.yml")
		assertContains(t, entries, "main/dep11/icons-48x48.tar")

		assertNotContains(t, entries, "main/binary-arm64/Packages")
		assertNotContains(t, entries, "main/binary-i386/Packages")
		assertNotContains(t, entries, "main/Contents-arm64")
		assertNotContains(t, entries, "main/dep11/Components-arm64.yml")
		assertNotContains(t, entries, "main/source/Sources")
		assertNotContains(t, entries, "main/Contents-source")
		assertNotContains(t, entries, "main/i18n/Translation-en")
		assertNotContains(t, entries, "main/debian-installer/binary-amd64/Packages")
		assertNotContains(t, entries, "main/Contents-udeb-amd64")
	})

	t.Run("binaries only for amd64 and arm64", func(t *testing.T) {
		entries := filterReleaseEntries(allEntries, repoArchs, []string{"amd64", "arm64", "all"}, false)

		assertContains(t, entries, "main/binary-amd64/Packages")
		assertContains(t, entries, "main/binary-arm64/Packages")
		assertContains(t, entries, "main/binary-all/Packages")

		assertNotContains(t, entries, "main/binary-i386/Packages")
		assertNotContains(t, entries, "main/binary-armel/Packages")
	})

	t.Run("with extras includes source and d-i and i18n", func(t *testing.T) {
		entries := filterReleaseEntries(allEntries, repoArchs, []string{"amd64", "all"}, true)

		assertContains(t, entries, "main/binary-amd64/Packages")
		assertContains(t, entries, "main/source/Sources")
		assertContains(t, entries, "main/Contents-source")
		assertContains(t, entries, "main/i18n/Translation-en")
		assertContains(t, entries, "main/debian-installer/binary-amd64/Packages")
		assertContains(t, entries, "main/debian-installer/binary-all/Packages")
		assertContains(t, entries, "main/Contents-udeb-amd64")

		// d-i entries for excluded archs are still filtered out.
		assertNotContains(t, entries, "main/debian-installer/binary-arm64/Packages")
		assertNotContains(t, entries, "main/Contents-udeb-arm64")
	})
}

func assertContains(t *testing.T, entries []control.SHA256FileHash, filename string) {
	t.Helper()
	for _, e := range entries {
		if e.Filename == filename {
			return
		}
	}
	t.Errorf("expected entry %q not found", filename)
}

func assertNotContains(t *testing.T, entries []control.SHA256FileHash, filename string) {
	t.Helper()
	for _, e := range entries {
		if e.Filename == filename {
			t.Errorf("unexpected entry %q found", filename)
			return
		}
	}
}

func TestFilterByComponent(t *testing.T) {
	data, err := os.ReadFile("../../tests/resources/apt/debian_InRelease")
	if err != nil {
		t.Fatalf("reading test file: %v", err)
	}

	release, err := parseRelease(data)
	if err != nil {
		t.Fatalf("parseRelease: %v", err)
	}

	entries := filterByComponent(release.SHA256, []string{"main"})
	for _, e := range entries {
		if !strings.HasPrefix(e.Filename, "main/") {
			t.Errorf("unexpected component in %q", e.Filename)
		}
	}

	if len(entries) == len(release.SHA256) {
		t.Error("expected component filter to reduce entry count")
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
