package update

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Install replaces the running program with the contents of a downloaded
// release archive, and reports the path to relaunch.
//
// Nothing is deleted until the replacement is in place: the old copy is moved
// aside and only removed on the next start, so a failure part-way through can
// be undone rather than leaving a machine with no working program on it.
//
// A useful side effect of updating this way: the file arrives over HTTP rather
// than through a browser, so it carries neither Windows' Mark of the Web nor
// macOS' quarantine flag. An updated copy therefore starts without the
// SmartScreen or Gatekeeper prompt the first download had to be clicked past.
func Install(archivePath string) (relaunch string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp("", "openipreporter-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	if err := unzip(archivePath, tmp); err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "darwin":
		return installBundle(tmp, exe)
	default:
		return installExecutable(tmp, exe)
	}
}

// installBundle swaps the whole .app, which is what macOS actually runs.
func installBundle(tmp, exe string) (string, error) {
	// .../OpenIPReporter.app/Contents/MacOS/OpenIPReporter -> .../OpenIPReporter.app
	current := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(current, ".app") {
		return "", fmt.Errorf("not running from an .app bundle, so there is nothing to replace")
	}

	fresh, err := findSuffix(tmp, ".app")
	if err != nil {
		return "", err
	}

	aside := current + ".old"
	os.RemoveAll(aside)
	if err := os.Rename(current, aside); err != nil {
		return "", fmt.Errorf("cannot replace %s: %w", filepath.Base(current), err)
	}
	if err := os.Rename(fresh, current); err != nil {
		os.Rename(aside, current) // put it back
		return "", err
	}
	return current, nil
}

// installExecutable swaps a single file. Windows will not let a running
// executable be deleted, but it will let it be renamed, which is enough.
func installExecutable(tmp, exe string) (string, error) {
	fresh, err := findFile(tmp, filepath.Base(exe))
	if err != nil {
		return "", err
	}

	aside := exe + ".old"
	os.Remove(aside)
	if err := os.Rename(exe, aside); err != nil {
		return "", fmt.Errorf("cannot replace %s: %w", filepath.Base(exe), err)
	}
	if err := copyFile(fresh, exe, 0o755); err != nil {
		os.Rename(aside, exe) // put it back
		return "", err
	}
	return exe, nil
}

// CleanUp removes the copy left behind by the last update. Called at startup,
// once the new version is demonstrably running.
func CleanUp() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return
	}
	os.Remove(exe + ".old")
	if runtime.GOOS == "darwin" {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if strings.HasSuffix(bundle, ".app") {
			os.RemoveAll(bundle + ".old")
		}
	}
}

func findSuffix(root, suffix string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), suffix) {
			return filepath.Join(root, e.Name()), nil
		}
	}
	return "", fmt.Errorf("the downloaded archive contains no %s", suffix)
}

func findFile(root, name string) (string, error) {
	var found string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || found != "" || info.IsDir() {
			return nil
		}
		if strings.EqualFold(info.Name(), name) {
			found = p
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("the downloaded archive contains no %s", name)
	}
	return found, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// unzip extracts an archive, refusing entries whose path would escape the
// destination.
func unzip(archive, dest string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Windows' Compress-Archive writes backslashes as separators.
		name := strings.ReplaceAll(f.Name, `\`, "/")
		target := filepath.Join(dest, filepath.FromSlash(name))

		// A crafted archive must not be able to write outside dest.
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("archive entry %q would write outside the target folder", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
