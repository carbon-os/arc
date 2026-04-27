//go:build windows

package runtime

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	webview2Version   = "146.0.3856.97"
	webview2PackageID = "WebView2.Runtime.X64"
	webview2NuGetBase = "https://www.nuget.org/api/v2/package/"
)

// EnsureWebView2 downloads and caches the fixed-version WebView2 runtime from
// NuGet and returns the path to the extracted runtime folder. On subsequent
// calls the cached copy is returned immediately — no network access.
//
// The returned path is set as WEBVIEW2_BROWSER_EXECUTABLE_FOLDER in the
// renderer's environment so WebView2 uses this specific runtime instead of
// whatever is installed system-wide.
func EnsureWebView2() (string, error) {
	dest, err := webview2CachedPath()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(filepath.Join(dest, "msedgewebview2.exe")); err == nil {
		return dest, nil
	}

	return downloadWebView2(dest)
}

// webview2CachedPath returns the directory where the WebView2 runtime is
// cached, nested inside the shared arc versioned cache directory so it sits
// alongside the renderer binary and WebView2Loader.dll.
//
//	%LOCALAPPDATA%\arc\v0.1.4\webview2\146.0.3856.97\
func webview2CachedPath() (string, error) {
	dir, err := CachedDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "webview2", webview2Version), nil
}

func downloadWebView2(dest string) (string, error) {
	url := webview2NuGetBase + webview2PackageID + "/" + webview2Version
	fmt.Fprintf(os.Stderr, "arc: fetching WebView2 runtime %s...\n", webview2Version)

	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", fmt.Errorf("arc: webview2 download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("arc: webview2 download: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "arc-webview2-*.nupkg")
	if err != nil {
		return "", fmt.Errorf("arc: webview2 temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("arc: webview2 write temp: %w", err)
	}
	tmp.Close()

	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return "", fmt.Errorf("arc: webview2 open zip: %w", err)
	}
	defer zr.Close()

	prefix, err := findWebView2Prefix(&zr.Reader)
	if err != nil {
		return "", err
	}

	if err := extractZipSubdir(&zr.Reader, prefix, dest); err != nil {
		return "", err
	}

	fmt.Fprintln(os.Stderr, "arc: WebView2 runtime ready")
	return dest, nil
}

var candidatePrefixes = []string{
	"WebView2/",
	"content/WebView2/",
	"contentFiles/any/any/WebView2/",
}

func findWebView2Prefix(zr *zip.Reader) (string, error) {
	for _, prefix := range candidatePrefixes {
		marker := prefix + "msedgewebview2.exe"
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, marker) {
				return prefix, nil
			}
		}
	}

	const exe = "msedgewebview2.exe"
	for _, f := range zr.File {
		lower := strings.ToLower(f.Name)
		if !strings.HasSuffix(lower, "/"+exe) && !strings.EqualFold(f.Name, exe) {
			continue
		}
		cut := strings.LastIndex(lower, "/"+exe)
		if cut < 0 {
			return "", nil
		}
		return f.Name[:cut+1], nil
	}

	return "", fmt.Errorf(
		"arc: msedgewebview2.exe not found in WebView2 package — " +
			"the NuGet layout may have changed for version " + webview2Version)
}

func extractZipSubdir(zr *zip.Reader, prefix, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("arc: webview2 mkdir: %w", err)
	}

	destClean := filepath.Clean(dest) + string(os.PathSeparator)

	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) {
			continue
		}
		rel := filepath.FromSlash(f.Name[len(prefix):])
		if rel == "" {
			continue
		}

		target := filepath.Join(dest, rel)

		if !strings.HasPrefix(target, destClean) {
			return fmt.Errorf("arc: webview2: unsafe path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(target, 0o755)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("arc: webview2 extract mkdir: %w", err)
		}

		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("arc: webview2 open entry %s: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("arc: webview2 create %s: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("arc: webview2 extract %s: %w", f.Name, err)
	}
	return nil
}