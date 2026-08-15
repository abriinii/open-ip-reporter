package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// appName is the folder the application's files live under.
const appName = "OpenIPReporter"

// dataDir is where the can list, preferences and in-progress walks are kept.
//
// Deliberately not the working directory. A double-clicked executable inherits
// whatever directory it happened to be launched from, so running it out of
// Downloads scattered cans.json, settings.json and a sessions folder into
// Downloads — where they look like clutter and get tidied away. Sessions hold
// walks that are still in progress, so deleting them loses real work, and
// moving the executable made the application quietly start over with nothing.
//
//	Windows  %APPDATA%\OpenIPReporter
//	macOS    ~/Library/Application Support/OpenIPReporter
func dataDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		// No home directory to speak of. Falling back to the working directory
		// is the old behaviour, which is poor but better than not saving.
		return "."
	}
	dir := filepath.Join(base, appName)
	os.MkdirAll(dir, 0o755)
	return dir
}

// migrateFromWorkingDir moves files left beside the executable by earlier
// versions, so an upgrade does not appear to lose the can list and any walk
// that was part-finished.
//
// Only ever moves what it recognises, and never overwrites something already
// in the data directory.
func migrateFromWorkingDir(dst string) {
	if abs, err := filepath.Abs(dst); err == nil {
		if cwd, err := os.Getwd(); err == nil && abs == cwd {
			return // already the same place
		}
	}

	for _, name := range []string{"cans.json", "settings.json", "sessions"} {
		src := name
		if _, err := os.Stat(src); err != nil {
			continue
		}
		target := filepath.Join(dst, name)
		if _, err := os.Stat(target); err == nil {
			continue // do not clobber
		}
		os.Rename(src, target)
	}
}

// OpenDataFolder shows the folder in Explorer or Finder, so the can list can
// be found without anyone having to know where the operating system keeps it.
func (a *App) OpenDataFolder() {
	dir := a.dataDir
	if dir == "" {
		dir = "."
	}
	switch runtime.GOOS {
	case "windows":
		exec.Command("explorer", dir).Start()
	case "darwin":
		exec.Command("open", dir).Start()
	default:
		exec.Command("xdg-open", dir).Start()
	}
}

// DataFolder is the path itself, for showing on screen.
func (a *App) DataFolder() string { return a.dataDir }
