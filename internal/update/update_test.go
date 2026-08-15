package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v2.3.0", "v2.2.0", true},
		{"v2.2.1", "v2.2.0", true},
		{"v3.0.0", "v2.9.9", true},
		{"v2.2.0", "v2.2.0", false},
		{"v2.1.0", "v2.2.0", false},
		{"2.3.0", "v2.2.0", true}, // the leading v is optional
		{"v2.10.0", "v2.9.0", true},
		{"v2.2", "v2.2.0", false}, // missing parts count as zero
		{"v2.2.0.1", "v2.2.0", true},
		{"v2.3.0-rc1", "v2.2.0", true}, // a suffix does not stop the compare
		// Anything unparseable must compare as older, or one bad tag on the
		// release page nags every user forever.
		{"latest", "v2.2.0", false},
		{"", "v2.2.0", false},
		{"v2.2.0", "", false},
		{"vX.Y.Z", "v2.2.0", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func serve(t *testing.T, status int, body string) *Checker {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent sent; GitHub rejects requests without one")
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Checker{Repo: "abriinii/open-ip-reporter", BaseURL: srv.URL, Client: srv.Client()}
}

func TestCheckReportsANewerRelease(t *testing.T) {
	c := serve(t, 200, `{"tag_name":"v2.3.0","body":"  Fixed a thing.  ","html_url":"https://example.test/r/v2.3.0"}`)
	rel, err := c.Check(context.Background(), "v2.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if rel == nil {
		t.Fatal("no release reported, want v2.3.0")
	}
	if !rel.Newer {
		t.Error("v2.3.0 not marked newer than v2.2.0")
	}
	if rel.Version != "v2.3.0" || rel.URL != "https://example.test/r/v2.3.0" {
		t.Errorf("got %+v", rel)
	}
	if rel.Notes != "Fixed a thing." {
		t.Errorf("notes = %q, want them trimmed", rel.Notes)
	}
}

// Still reported when up to date, so the notes for the running version can be
// read without having to fall behind first. Newer is what distinguishes them.
func TestCheckStillReportsTheReleaseWhenUpToDate(t *testing.T) {
	for _, tag := range []string{"v2.2.0", "v2.1.0"} {
		c := serve(t, 200, `{"tag_name":"`+tag+`","body":"Notes."}`)
		rel, err := c.Check(context.Background(), "v2.2.0")
		if err != nil {
			t.Fatalf("tag %s: %v", tag, err)
		}
		if rel == nil {
			t.Fatalf("tag %s: nothing reported", tag)
		}
		if rel.Newer {
			t.Errorf("tag %s marked newer than v2.2.0", tag)
		}
		if rel.Notes != "Notes." {
			t.Errorf("tag %s: notes = %q", tag, rel.Notes)
		}
	}
}

// Drafts and pre-releases are not what anyone walking a rack should be told to
// install.
func TestCheckIgnoresDraftsAndPrereleases(t *testing.T) {
	for _, body := range []string{
		`{"tag_name":"v9.0.0","draft":true}`,
		`{"tag_name":"v9.0.0","prerelease":true}`,
		`{"tag_name":""}`,
	} {
		c := serve(t, 200, body)
		if rel, _ := c.Check(context.Background(), "v2.2.0"); rel != nil {
			t.Errorf("%s: reported %+v, want nothing", body, rel)
		}
	}
}

// A local build has nothing meaningful to compare, and must not even make the
// request.
func TestCheckSkipsDevBuildsEntirely(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte(`{"tag_name":"v9.0.0"}`))
	}))
	defer srv.Close()
	c := &Checker{Repo: "x/y", BaseURL: srv.URL, Client: srv.Client()}

	for _, v := range []string{"dev", ""} {
		rel, err := c.Check(context.Background(), v)
		if rel != nil || err != nil {
			t.Errorf("version %q: got %+v, %v", v, rel, err)
		}
	}
	if called {
		t.Error("a dev build still called out to the network")
	}
}

// Being offline is the normal case on a miner network. It must be an ordinary
// error the caller ignores, never a crash or a hang.
func TestCheckFailsQuietlyWhenUnreachable(t *testing.T) {
	c := &Checker{
		Repo:    "x/y",
		BaseURL: "http://127.0.0.1:1", // nothing listens here
		Client:  &http.Client{Timeout: time.Second},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		rel, err := c.Check(context.Background(), "v2.2.0")
		if rel != nil {
			t.Errorf("reported %+v while offline", rel)
		}
		if err == nil {
			t.Error("no error returned for an unreachable host")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Check hung; it must never block the app from opening")
	}
}

func TestCheckHandlesRateLimitsAndGarbage(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{403, `{"message":"rate limit exceeded"}`},
		{404, `{"message":"Not Found"}`},
		{500, ``},
		{200, `not json at all`},
	} {
		c := serve(t, tc.status, tc.body)
		rel, err := c.Check(context.Background(), "v2.2.0")
		if rel != nil {
			t.Errorf("status %d: reported %+v, want nothing", tc.status, rel)
		}
		if err == nil {
			t.Errorf("status %d: no error returned", tc.status)
		}
	}
}
