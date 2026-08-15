package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AssetFor is the file to fetch for a platform, matching what the release
// workflow publishes.
func AssetFor(goos string) string {
	switch goos {
	case "windows":
		return "OpenIPReporter-windows-x64.zip"
	case "darwin":
		return "OpenIPReporter-macos-arm64.zip"
	}
	return ""
}

// checksumFile is published alongside every release.
const checksumFile = "SHA256SUMS.txt"

// Download fetches a release asset into dir and checks it against the
// SHA256SUMS.txt published with that same release.
//
// The verification is the point. This writes an executable that the machine
// will then run, so a truncated download or a proxy serving something else has
// to be caught here rather than discovered by running it.
func (c *Checker) Download(ctx context.Context, rel *Release, assetName, dir string) (string, error) {
	asset, ok := rel.Find(assetName)
	if !ok {
		return "", fmt.Errorf("release %s has no file called %s", rel.Version, assetName)
	}

	sums, ok := rel.Find(checksumFile)
	if !ok {
		return "", fmt.Errorf("release %s publishes no %s, so the download cannot be verified", rel.Version, checksumFile)
	}
	want, err := c.expectedSum(ctx, sums.URL, assetName)
	if err != nil {
		return "", err
	}

	path := filepath.Join(dir, assetName)
	got, err := c.fetchTo(ctx, asset.URL, path)
	if err != nil {
		return "", err
	}

	if !strings.EqualFold(got, want) {
		os.Remove(path)
		return "", fmt.Errorf("downloaded %s does not match its published checksum", assetName)
	}
	return path, nil
}

// expectedSum pulls the line for one file out of SHA256SUMS.txt.
func (c *Checker) expectedSum(ctx context.Context, url, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "OpenIPReporter")

	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetching %s: %s", checksumFile, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// sha256sum writes "<hash>  <name>", the name sometimes with a leading *.
		if strings.TrimPrefix(fields[1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%s has no entry for %s", checksumFile, name)
}

// fetchTo downloads to a path and returns the file's SHA-256.
func (c *Checker) fetchTo(ctx context.Context, url, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "OpenIPReporter")

	resp, err := c.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: %s", filepath.Base(path), resp.Status)
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, sum), resp.Body); err != nil {
		os.Remove(path)
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// client is the checker's HTTP client, or a long-timeout one for downloads.
// The four second timeout used for the version check is far too short for an
// eleven megabyte file.
func (c *Checker) client() *http.Client {
	if c.DownloadClient != nil {
		return c.DownloadClient
	}
	return &http.Client{Timeout: 10 * time.Minute}
}
