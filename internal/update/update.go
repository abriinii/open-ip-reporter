// Package update checks whether a newer release exists on GitHub.
//
// It only ever reports. It does not download, install, or execute anything —
// the operator is told a version exists and given a link. An updater that
// replaces a running binary on a locked-down work laptop is a much larger
// promise than this tool needs to make.
//
// This is the only outbound network traffic in the entire program. Everything
// else listens.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// timeout is deliberately short. The machine walking a rack is on the miner
// network, which usually has no route to the internet at all, so the common
// case is not "slow" but "never answers" — and that must not be noticeable.
const timeout = 4 * time.Second

// Release is the latest published version, whether or not it is newer than the
// one running.
type Release struct {
	Version string  `json:"version"` // e.g. "v2.3.0"
	Notes   string  `json:"notes"`   // the release body, as written on the tag
	URL     string  `json:"url"`     // where a human goes to get it
	Newer   bool    `json:"newer"`   // true when it is ahead of the running build
	Assets  []Asset `json:"assets"`  // the published files
}

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Find returns the asset with this name.
func (r *Release) Find(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Checker looks up the latest release. The API base is a field so tests can
// point it at a local server instead of GitHub.
type Checker struct {
	Repo    string // "owner/name"
	BaseURL string // defaults to GitHub's API
	Client  *http.Client

	// DownloadClient fetches release files. Separate from Client because the
	// version check gives up after four seconds, which is far too short for an
	// eleven megabyte download.
	DownloadClient *http.Client
}

// NewChecker returns a checker for a repository.
func NewChecker(repo string) *Checker {
	return &Checker{
		Repo:    repo,
		BaseURL: "https://api.github.com",
		Client:  &http.Client{Timeout: timeout},
	}
}

// Check returns the latest published release, with Newer saying whether it is
// ahead of the running build.
//
// It deliberately reports the release even when the running build is already
// current, so the notes for the version in front of you can be read without
// having to be out of date first.
//
// A network failure is returned as an error for logging, but callers are
// expected to ignore it: being offline is normal here, not a fault.
func (c *Checker) Check(ctx context.Context, current string) (*Release, error) {
	// A locally built binary has no version to compare against, and telling a
	// developer their own build is out of date is noise.
	if current == "" || current == "dev" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(c.BaseURL, "/"), c.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "OpenIPReporter/"+current)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github returned %s", resp.Status)
	}

	var body struct {
		TagName    string `json:"tag_name"`
		Body       string `json:"body"`
		HTMLURL    string `json:"html_url"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	if body.Draft || body.Prerelease || body.TagName == "" {
		return nil, nil
	}

	rel := &Release{
		Version: body.TagName,
		Notes:   strings.TrimSpace(body.Body),
		URL:     body.HTMLURL,
		Newer:   Newer(body.TagName, current),
	}
	for _, a := range body.Assets {
		rel.Assets = append(rel.Assets, Asset{Name: a.Name, URL: a.URL, Size: a.Size})
	}
	return rel, nil
}

// Newer reports whether version a is later than version b. Both are compared
// as dot-separated numbers, ignoring a leading "v".
//
// Anything unparseable compares as older, so a malformed tag on the release
// page can never nag every user into thinking they are out of date.
func Newer(a, b string) bool {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := at(pa, i), at(pb, i)
		if x != y {
			return x > y
		}
	}
	return false
}

func parse(v string) ([]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	if v == "" {
		return nil, false
	}
	// Drop any pre-release or build suffix: 1.2.3-rc1 compares as 1.2.3.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

func at(v []int, i int) int {
	if i < len(v) {
		return v[i]
	}
	return 0
}
