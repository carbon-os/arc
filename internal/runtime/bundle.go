package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

const (
	rendererVersion = "v0.1.4"
	baseURL         = "https://github.com/carbon-os/arc/releases/download/"
)

var checksums = map[string]string{
	"darwin/amd64":  "...",
	"darwin/arm64":  "...",
	"linux/amd64":   "...",
	"linux/arm64":   "...",
	"windows/amd64": "...",
}

// EnsureRenderer guarantees the renderer binary (and on Windows,
// WebView2Loader.dll) are present in the cache directory, then returns
// the path to the cached binary.
//
// If localPath is non-empty (dev / cmake build) the binary and any sibling
// DLLs are copied into the cache. If localPath is empty and prebuilt is true
// the binary is downloaded from GitHub Releases. Either way the caller always
// gets back the standardised cache path.
func EnsureRenderer(localPath string, prebuilt bool) (string, error) {
	dest, err := cachedRendererPath()
	if err != nil {
		return "", err
	}

	if localPath != "" {
		return dest, syncLocalRenderer(localPath, dest)
	}

	// Already cached?
	if _, err := os.Stat(dest); err == nil {
		return dest, nil
	}

	if !prebuilt {
		return "", fmt.Errorf(
			"arc: renderer not found.\n" +
				"     Set Renderer.Prebuilt: true to download automatically,\n" +
				"     or set Renderer.Path to your cmake build output.")
	}

	return downloadRenderer(dest)
}

// cachedRendererPath returns the expected location of the renderer binary
// inside the user cache directory for the current platform.
func cachedRendererPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("arc: cache dir: %w", err)
	}
	return filepath.Join(dir, "arc", rendererVersion, rendererBinary()), nil
}

// CachedDir returns the versioned cache directory (used by EnsureWebView2
// so everything lands in one place).
func CachedDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("arc: cache dir: %w", err)
	}
	return filepath.Join(dir, "arc", rendererVersion), nil
}

func rendererBinary() string {
	if runtime.GOOS == "windows" {
		return "arc-renderer.exe"
	}
	return "arc-renderer"
}

// syncLocalRenderer copies the renderer binary from src into destBin.
// On Windows it also copies WebView2Loader.dll from the same source
// directory into the cache directory alongside the binary.
// If the cached copy is already up-to-date (same size + mtime) the copy
// is skipped so repeated runs are fast.
func syncLocalRenderer(src, destBin string) error {
	if err := os.MkdirAll(filepath.Dir(destBin), 0o755); err != nil {
		return fmt.Errorf("arc: cache dir: %w", err)
	}

	if err := copyFileIfChanged(src, destBin, 0o755); err != nil {
		return fmt.Errorf("arc: copy renderer: %w", err)
	}

	if runtime.GOOS == "windows" {
		srcDir := filepath.Dir(src)
		destDir := filepath.Dir(destBin)
		dlls := []string{"WebView2Loader.dll"}
		for _, dll := range dlls {
			s := filepath.Join(srcDir, dll)
			d := filepath.Join(destDir, dll)
			if _, err := os.Stat(s); err == nil {
				if err := copyFileIfChanged(s, d, 0o644); err != nil {
					return fmt.Errorf("arc: copy %s: %w", dll, err)
				}
			}
		}
	}

	return nil
}

// copyFileIfChanged copies src → dst only when src is newer or sizes differ.
func copyFileIfChanged(src, dst string, mode os.FileMode) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}

	if di, err := os.Stat(dst); err == nil {
		if di.Size() == si.Size() && !si.ModTime().After(di.ModTime()) {
			return nil // already up to date
		}
	}

	fmt.Fprintf(os.Stderr, "arc: caching %s\n", filepath.Base(src))
	return atomicCopy(src, dst, mode)
}

func atomicCopy(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()

	return os.Rename(tmp, dst)
}

func downloadRenderer(dest string) (string, error) {
	key := runtime.GOOS + "/" + runtime.GOARCH
	expected, ok := checksums[key]
	if !ok {
		return "", fmt.Errorf("arc: no prebuilt renderer for %s", key)
	}

	url := fmt.Sprintf("%s%s/arc-renderer-%s-%s",
		baseURL, rendererVersion, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		url += ".exe"
	}

	fmt.Fprintf(os.Stderr, "arc: downloading renderer %s (%s/%s)...\n",
		rendererVersion, runtime.GOOS, runtime.GOARCH)

	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("arc: download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("arc: download: HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("arc: cache dir: %w", err)
	}

	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("arc: cache write: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("arc: download write: %w", err)
	}
	f.Close()

	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		os.Remove(tmp)
		return "", fmt.Errorf("arc: checksum mismatch (got %s, want %s)", got, expected)
	}

	if err := os.Rename(tmp, dest); err != nil {
		return "", fmt.Errorf("arc: cache install: %w", err)
	}

	fmt.Fprintln(os.Stderr, "arc: renderer ready")
	return dest, nil
}