package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// releaseServer serves a fake release: one asset and a SHA256SUMS.txt whose
// contents the test controls, so a mismatch can be forced.
func releaseServer(t *testing.T, assetBody []byte, sumOverride string) (*Checker, *Release) {
	t.Helper()
	const name = "OpenIPReporter-windows-x64.zip"

	sum := sha256.Sum256(assetBody)
	published := hex.EncodeToString(sum[:])
	if sumOverride != "" {
		published = sumOverride
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) { w.Write(assetBody) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  %s\n%s  capture-tool-windows-x64.exe\n", published, name, strings.Repeat("0", 64))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rel := &Release{Version: "v9.0.0", Assets: []Asset{
		{Name: name, URL: srv.URL + "/asset"},
		{Name: checksumFile, URL: srv.URL + "/sums"},
	}}
	return &Checker{DownloadClient: srv.Client()}, rel
}

func TestDownloadVerifiesTheChecksum(t *testing.T) {
	body := []byte("pretend this is an eleven megabyte zip")
	c, rel := releaseServer(t, body, "")

	dir := t.TempDir()
	path, err := c.Download(context.Background(), rel, "OpenIPReporter-windows-x64.zip", dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Error("downloaded file does not match what was served")
	}
}

// The whole point of verifying: this writes something the machine will then
// run, so a file that does not match must never survive.
func TestDownloadRefusesAMismatchedFile(t *testing.T) {
	c, rel := releaseServer(t, []byte("the real file"), strings.Repeat("a", 64))

	dir := t.TempDir()
	_, err := c.Download(context.Background(), rel, "OpenIPReporter-windows-x64.zip", dir)
	if err == nil {
		t.Fatal("a file that failed its checksum was accepted")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error %q does not say the checksum failed", err)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "*")); len(entries) != 0 {
		t.Errorf("the bad download was left on disk: %v", entries)
	}
}

func TestDownloadRefusesWhenNothingPublishesAChecksum(t *testing.T) {
	rel := &Release{Version: "v9.0.0", Assets: []Asset{{Name: "OpenIPReporter-windows-x64.zip", URL: "http://example.invalid"}}}
	_, err := (&Checker{}).Download(context.Background(), rel, "OpenIPReporter-windows-x64.zip", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "verified") {
		t.Errorf("err = %v, want a refusal to install something unverifiable", err)
	}
}

func TestDownloadRefusesAnAssetTheReleaseDoesNotHave(t *testing.T) {
	c, rel := releaseServer(t, []byte("x"), "")
	if _, err := c.Download(context.Background(), rel, "not-published.zip", t.TempDir()); err == nil {
		t.Error("downloaded a file the release does not contain")
	}
}

func TestAssetForKnowsBothPlatforms(t *testing.T) {
	if AssetFor("windows") != "OpenIPReporter-windows-x64.zip" {
		t.Error("wrong Windows asset")
	}
	if AssetFor("darwin") != "OpenIPReporter-macos-arm64.zip" {
		t.Error("wrong macOS asset")
	}
	if AssetFor("plan9") != "" {
		t.Error("an unsupported platform should have no asset")
	}
}

// A crafted archive must not be able to write outside the folder it is being
// extracted into.
func TestUnzipRefusesToEscapeItsFolder(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "evil.zip")

	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("../escaped.txt")
	w.Write([]byte("should never be written"))
	zw.Close()
	f.Close()

	dest := filepath.Join(dir, "dest")
	os.MkdirAll(dest, 0o755)
	if err := unzip(archive, dest); err == nil {
		t.Fatal("an archive escaping its folder was extracted")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err == nil {
		t.Error("the archive wrote outside the target folder")
	}
}

// Windows writes backslash separators, which must still extract as folders.
func TestUnzipHandlesWindowsSeparators(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "win.zip")

	f, _ := os.Create(archive)
	zw := zip.NewWriter(f)
	w, _ := zw.Create(`OpenIPReporter\OpenIPReporter.exe`)
	w.Write([]byte("MZ"))
	zw.Close()
	f.Close()

	dest := filepath.Join(dir, "dest")
	if err := unzip(archive, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "OpenIPReporter", "OpenIPReporter.exe")); err != nil {
		t.Errorf("backslash paths did not become folders: %v", err)
	}
}
