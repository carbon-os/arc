package runtime

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	rendererVersion = "v1.0.0"
	baseURL         = "https://github.com/carbon-os/arc/releases/download/"
)

// checksums should now map to the SHA256 of the *archives* (.zip or .tar.gz)
var checksums = map[string]string{
	"darwin/amd64":  "",
	"darwin/arm64":  "",
	"linux/amd64":   "",
	"linux/arm64":   "",
	"windows/amd64": "", // Add your v1.0.0 zip sha256 here if you'd like
}

// EnsureRenderer guarantees the renderer binary and its dependencies are
// present in the cache directory, returning the path to the executable.
func EnsureRenderer(localPath string, prebuilt bool) (string, error) {
	cacheDir, err := CachedDir()
	if err != nil {
		return "", err
	}

	binPath := filepath.Join(cacheDir, rendererBinary())

	// 1. If a local path is provided (dev mode), sync it directly.
	if localPath != "" {
		return binPath, syncLocalRenderer(localPath, binPath)
	}

	// 2. Already cached?
	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	// 3. Fail if prebuilt is not enabled
	if !prebuilt {
		return "", fmt.Errorf(
			"arc: renderer not found.\n" +
				"     Set Renderer.Prebuilt: true to download automatically,\n" +
				"     or set Renderer.Path to your cmake build output.")
	}

	// 4. Download and extract the prebuilt archive
	return downloadAndExtract(cacheDir)
}

// CachedDir returns the versioned cache directory.
func CachedDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("arc: cache dir: %w", err)
	}
	return filepath.Join(dir, "arc", rendererVersion), nil
}

func rendererBinary() string {
	if runtime.GOOS == "windows" {
		return "renderer.exe"
	}
	return "renderer"
}

// syncLocalRenderer copies the local dev binary and DLLs into the cache.
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

func copyFileIfChanged(src, dst string, mode os.FileMode) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if di, err := os.Stat(dst); err == nil {
		if di.Size() == si.Size() && !si.ModTime().After(di.ModTime()) {
			return nil
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

// ── Download and Extraction ───────────────────────────────────────────────────

func downloadAndExtract(destDir string) (string, error) {
	osPrefix := runtime.GOOS
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		osPrefix = "win"
		ext = ".zip"
	} else if runtime.GOOS == "darwin" {
		osPrefix = "mac"
	}

	archiveName := fmt.Sprintf("prebuild-%s-%s%s", osPrefix, runtime.GOARCH, ext)
	url := fmt.Sprintf("%s%s/%s", baseURL, rendererVersion, archiveName)

	key := runtime.GOOS + "/" + runtime.GOARCH
	expectedChecksum := checksums[key]

	fmt.Fprintf(os.Stderr, "arc: downloading %s...\n", archiveName)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("arc: download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("arc: download HTTP %d (%s)", resp.StatusCode, url)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("arc: cache mkdir: %w", err)
	}

	tmpArchive := filepath.Join(destDir, archiveName+".tmp")
	f, err := os.OpenFile(tmpArchive, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("arc: temp file write: %w", err)
	}

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		os.Remove(tmpArchive)
		return "", fmt.Errorf("arc: download stream error: %w", err)
	}
	f.Close()
	defer os.Remove(tmpArchive) // Clean up the archive once we're done extracting

	if expectedChecksum != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if got != expectedChecksum {
			return "", fmt.Errorf("arc: checksum mismatch (got %s, want %s)", got, expectedChecksum)
		}
	}

	fmt.Fprintf(os.Stderr, "arc: extracting %s...\n", archiveName)

	if ext == ".zip" {
		if err := extractZip(tmpArchive, destDir); err != nil {
			return "", err
		}
	} else {
		if err := extractTarGz(tmpArchive, destDir); err != nil {
			return "", err
		}
	}

	fmt.Fprintln(os.Stderr, "arc: renderer ready")
	return filepath.Join(destDir, rendererBinary()), nil
}

func extractZip(zipPath, destDir string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("arc: open zip: %w", err)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if err := extractZipFile(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, destDir string) error {
	// Defend against ZipSlip
	cleanName := filepath.Clean(f.Name)
	if strings.Contains(cleanName, "..") {
		return fmt.Errorf("arc: unsafe zip extraction path")
	}

	outPath := filepath.Join(destDir, cleanName)
	if f.FileInfo().IsDir() {
		return os.MkdirAll(outPath, 0o755)
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	mode := f.Mode()
	if strings.HasSuffix(cleanName, ".exe") || cleanName == "renderer" {
		mode = 0o755
	}

	outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, rc)
	return err
}

func extractTarGz(tarPath, destDir string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("arc: open tar: %w", err)
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("arc: gzip reader: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("arc: tar read: %w", err)
		}

		cleanName := filepath.Clean(header.Name)
		if strings.Contains(cleanName, "..") {
			return fmt.Errorf("arc: unsafe tar extraction path")
		}

		outPath := filepath.Join(destDir, cleanName)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(outPath, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
				return err
			}

			mode := header.FileInfo().Mode()
			if cleanName == "renderer" {
				mode = 0o755 // ensure executable permissions
			}

			outFile, err := os.OpenFile(outPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}
	return nil
}