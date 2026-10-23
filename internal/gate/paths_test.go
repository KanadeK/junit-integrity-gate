package gate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestAndFileLimits(t *testing.T) {
	body := `{"version":1,"files":["report.xml"]}`
	manifest := writeFixture(t, map[string]string{"report.xml": healthyXML}, body)
	if r := Check(manifest, Options{Limits: Limits{MaxManifestBytes: int64(len(body)), MaxFileBytes: int64(len(healthyXML))}}); !r.OK {
		t.Fatalf("exact byte limits rejected: %+v", r)
	}
	requireCode(t, Check(manifest, Options{Limits: Limits{MaxManifestBytes: int64(len(body) - 1)}}), "manifest_too_large", 2)
	requireCode(t, Check(manifest, Options{Limits: Limits{MaxFileBytes: int64(len(healthyXML) - 1)}}), "xml_limit_bytes", 1)
	manifest = writeFixture(t, map[string]string{"one.xml": healthyXML, "two.xml": healthyXML}, `{"version":1,"files":["one.xml","two.xml"]}`)
	requireCode(t, Check(manifest, Options{Limits: Limits{MaxFiles: 1}}), "manifest_files_limit", 2)
	requireCode(t, Check(filepath.Join(t.TempDir(), "missing.json"), Options{}), "manifest_read_error", 2)
	requireCode(t, Check(t.TempDir(), Options{}), "manifest_not_regular", 2)
	manifest = writeFixture(t, nil, `{"version":1,"files":["folder"]}`)
	if err := os.Mkdir(filepath.Join(filepath.Dir(manifest), "folder"), 0700); err != nil {
		t.Fatal(err)
	}
	requireCode(t, Check(manifest, Options{}), "report_not_regular", 1)
	if err := os.WriteFile(manifest, []byte("{\"version\":1,\"files\":[\"\xff\"]}"), 0600); err != nil {
		t.Fatal(err)
	}
	requireCode(t, Check(manifest, Options{}), "manifest_invalid_json", 2)
}

func TestNoSymlinkFilesOrTraversalBelowRoot(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "report.xml"), []byte(healthyXML), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := writeFixture(t, nil, `{"version":1,"files":["linked.xml"]}`)
	dir := filepath.Dir(manifest)
	if err := os.Symlink(filepath.Join(outside, "report.xml"), filepath.Join(dir, "linked.xml")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	requireCode(t, Check(manifest, Options{}), "report_unsafe_path", 1)
	if err := os.Symlink(outside, filepath.Join(dir, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"version":1,"files":["linked-dir/report.xml"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	requireCode(t, Check(manifest, Options{}), "report_unsafe_path", 1)
	manifestLink := filepath.Join(dir, "manifest-link.json")
	if err := os.Symlink(manifest, manifestLink); err != nil {
		t.Fatal(err)
	}
	requireCode(t, Check(manifestLink, Options{}), "manifest_unsafe_path", 2)
}

func TestTrustedManifestParentAlias(t *testing.T) {
	manifest := writeFixture(t, map[string]string{"report.xml": healthyXML}, `{"version":1,"files":["report.xml"]}`)
	alias := filepath.Join(t.TempDir(), "chosen-root")
	if err := os.Symlink(filepath.Dir(manifest), alias); err != nil {
		t.Skipf("directory aliases unavailable: %v", err)
	}
	r := Check(filepath.Join(alias, filepath.Base(manifest)), Options{})
	if !r.OK {
		t.Fatalf("caller-selected root alias should canonicalize: %+v", r)
	}
}

func TestOpenedFileAndPathReplacement(t *testing.T) {
	manifest := writeFixture(t, map[string]string{"report.xml": healthyXML}, `{"version":1,"files":["report.xml"]}`)
	dir := filepath.Dir(manifest)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, before, guard, code := openReport(root, "report.xml")
	if code != "" {
		t.Fatal(code)
	}
	defer f.Close()
	path := filepath.Join(dir, "report.xml")
	if err := os.Rename(path, filepath.Join(dir, "previous.xml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`<testsuite tests="0"/>`), 0600); err != nil {
		t.Fatal(err)
	}
	if guard.unchanged() {
		t.Fatal("ordinary pathname replacement was not detected")
	}
	after, err := f.Stat()
	if err != nil || !sameSnapshot(before, after) {
		t.Fatal("held handle unexpectedly refers to replacement")
	}
	buf := make([]byte, len(healthyXML))
	n, err := f.Read(buf)
	if err != nil || string(buf[:n]) != healthyXML {
		t.Fatal("held read followed replacement pathname")
	}
}

func TestInPlaceMutationAndManifestGuard(t *testing.T) {
	manifest := writeFixture(t, map[string]string{"report.xml": healthyXML}, `{"version":1,"files":["report.xml"]}`)
	f, root, manifestGuard, code := openManifest(manifest)
	if code != "" {
		t.Fatal(code)
	}
	defer f.Close()
	defer root.Close()
	file, before, guard, code := openReport(root, "report.xml")
	if code != "" {
		t.Fatal(code)
	}
	defer file.Close()
	path := filepath.Join(filepath.Dir(manifest), "report.xml")
	if err := os.WriteFile(path, []byte(healthyXML+" "), 0600); err != nil {
		t.Fatal(err)
	}
	after, err := file.Stat()
	if err != nil || sameSnapshot(before, after) || guard.unchanged() {
		t.Fatal("observable in-place change not detected")
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(manifest, future, future); err != nil {
		t.Fatal(err)
	}
	if manifestGuard.unchanged() {
		t.Fatal("manifest modification not detected")
	}
}

func TestManifestInputIsNotPublicOutput(t *testing.T) {
	secret := "PRIVATE_MANIFEST_FIELD_SENTINEL"
	manifest := writeFixture(t, nil, `{"version":1,"files":["`+secret+`/missing.xml"]}`)
	r := Check(manifest, Options{})
	requireCode(t, r, "report_missing", 1)
	data, err := json.Marshal(r)
	if err != nil || strings.Contains(string(data), secret) || strings.Contains(string(data), filepath.Dir(manifest)) {
		t.Fatalf("manifest contents/path exposed: %s %v", data, err)
	}
}
