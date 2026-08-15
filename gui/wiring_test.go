package main

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The window talks to Go by name, through strings. Nothing in the compiler
// checks those names, so a method that is renamed, or a call site that quietly
// fails to be added, produces a feature that simply does nothing — which is
// exactly how the update check shipped doing nothing at all.
//
// These tests read the frontend that gets embedded in the binary and check it
// against the real methods on App.

var callRe = regexp.MustCompile(`\bcall\("([A-Za-z_][A-Za-z0-9_]*)"`)

func frontend(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("frontend/dist/app.js")
	if err != nil {
		t.Fatalf("cannot read the embedded frontend: %v", err)
	}
	return string(data)
}

func appMethods() map[string]bool {
	m := map[string]bool{}
	rt := reflect.TypeOf(&App{})
	for i := 0; i < rt.NumMethod(); i++ {
		m[rt.Method(i).Name] = true
	}
	return m
}

// Every name the window calls must exist on App, or the button does nothing.
func TestEveryFrontendCallExists(t *testing.T) {
	methods := appMethods()
	var missing []string
	seen := map[string]bool{}

	for _, m := range callRe.FindAllStringSubmatch(frontend(t), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !methods[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("the window calls methods that do not exist on App: %v", missing)
	}
	if len(seen) == 0 {
		t.Error("found no backend calls at all; the regex or the frontend has moved")
	}
}

// Features that are only ever triggered from the frontend are invisible when
// the call site is missing: nothing errors, nothing logs, the feature just
// never happens. Each of these shipped broken exactly once.
func TestFeaturesAreActuallyWiredUp(t *testing.T) {
	js := frontend(t)
	required := map[string]string{
		`call("Layout")`:    "the Cans editor opens empty",
		`call("SaveLayout"`: "the Cans editor cannot save",
		`call("Skip")`:      "the spacebar does nothing",
		`call("Export")`:    "the Export button does nothing",
		`call("Undo")`:      "undo does nothing",
		`call("CopyIP"`:     "copying an IP does nothing",
		`call("CopyMAC"`:    "copying a MAC does nothing",
	}
	for snippet, consequence := range required {
		if !strings.Contains(js, snippet) {
			t.Errorf("missing %s — %s", snippet, consequence)
		}
	}
}

// Events go the other way, and fail just as silently.
func TestEveryEmittedEventIsListenedFor(t *testing.T) {
	js := frontend(t)
	appSrc, err := os.ReadFile("app.go")
	if err != nil {
		t.Fatal(err)
	}

	emitRe := regexp.MustCompile(`a\.emit\("([a-z-]+)"`)
	seen := map[string]bool{}
	for _, m := range emitRe.FindAllStringSubmatch(string(appSrc), -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !strings.Contains(js, `EventsOn("`+name+`"`) {
			t.Errorf("app.go emits %q but the window never listens for it", name)
		}
	}
	if len(seen) == 0 {
		t.Error("found no emitted events; the regex or app.go has moved")
	}
}
