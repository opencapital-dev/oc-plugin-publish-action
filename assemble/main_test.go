package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writePlugin lays out a minimal plugin dir: a built dist/ (plugin.json + a
// darwin-arm64 backend binary) plus repo-root dashboards/ and library-panels/.
func writePlugin(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dist := filepath.Join(dir, "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	must := func(p, body string) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must(filepath.Join(dist, "plugin.json"), `{"id":"oc-core-app","type":"app"}`)
	must(filepath.Join(dist, "module.js"), "// built frontend")
	must(filepath.Join(dist, "gpx_core-app_darwin_arm64"), "fake-backend")
	// Repo-root artifacts the SDK build does not copy into dist.
	must(filepath.Join(dir, "dashboards", "overview.json"), `{"title":"Overview"}`)
	must(filepath.Join(dir, "library-panels", "nav.json"), `{"title":"Nav"}`)
	return dir
}

// tarEntries packs dist/ the way the assembler does and returns the set of
// archive entry names.
func tarEntries(t *testing.T, pluginDir string) map[string]bool {
	t.Helper()
	art, err := packagePlugin(pluginDir, "oc-core-app", "v0.1.3", "darwin-arm64")
	if err != nil {
		t.Fatalf("packagePlugin: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(art.tarball) })

	f, err := os.Open(art.tarball)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	names := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[hdr.Name] = true
	}
	return names
}

// Without staging, the tarball omits the repo-root artifact dirs — this is the
// bug: the desktop app has no dashboards/library panels to provision.
func TestPackage_WithoutStaging_OmitsRootArtifacts(t *testing.T) {
	dir := writePlugin(t)
	names := tarEntries(t, dir)
	if !names["plugin.json"] || !names["gpx_core-app_darwin_arm64"] {
		t.Fatalf("expected base bundle files, got %v", names)
	}
	if names["dashboards/overview.json"] || names["library-panels/nav.json"] {
		t.Fatalf("root artifacts unexpectedly present without staging: %v", names)
	}
}

// After staging, the tarball carries dashboards/ and library-panels/ at top
// level, where instance-bootstrap reads them.
func TestStageRootArtifacts_ThenPackage_IncludesThem(t *testing.T) {
	dir := writePlugin(t)
	if err := stageRootArtifacts(dir); err != nil {
		t.Fatalf("stageRootArtifacts: %v", err)
	}
	names := tarEntries(t, dir)
	for _, want := range []string{
		"plugin.json",
		"gpx_core-app_darwin_arm64",
		"dashboards/overview.json",
		"library-panels/nav.json",
	} {
		if !names[want] {
			t.Errorf("missing %q in tarball; got %v", want, names)
		}
	}
}

// A plugin shipping neither dir (e.g. a datasource) stages cleanly as a no-op.
func TestStageRootArtifacts_NoDirs_IsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := stageRootArtifacts(dir); err != nil {
		t.Fatalf("stageRootArtifacts no-op: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "dashboards")); !os.IsNotExist(err) {
		t.Fatalf("expected no dashboards dir staged, err=%v", err)
	}
}
