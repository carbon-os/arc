package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"debug/elf"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	rendererWinPath   = "../renderer/build/bin/Release/renderer.exe"
	rendererUnixPath  = "../renderer/build/bin/renderer"
	webView2NuGetURL  = "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2"
)

func main() {
	log.SetFlags(0)
	log.Printf("Starting prebuild packager for %s/%s...", runtime.GOOS, runtime.GOARCH)

	if err := os.MkdirAll("dist", 0o755); err != nil {
		log.Fatalf("failed to create dist directory: %v", err)
	}

	var err error
	switch runtime.GOOS {
	case "windows":
		err = packageWindows()
	case "linux":
		err = packageLinux()
	case "darwin":
		err = packageDarwin()
	default:
		log.Fatalf("unsupported OS for prebuild: %s", runtime.GOOS)
	}

	if err != nil {
		log.Fatalf("❌ Build failed: %v", err)
	}
	log.Println("✅ Prebuild complete!")
}

// ── Windows ──────────────────────────────────────────────────────────────────

func packageWindows() error {
	outName := fmt.Sprintf("dist/prebuild-win-%s.zip", runtime.GOARCH)
	log.Printf("Packaging %s...", outName)

	outFile, err := os.Create(outName)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	if err := addFileToZip(zw, rendererWinPath, "arc-renderer.exe"); err != nil {
		return fmt.Errorf("adding renderer: %w", err)
	}

	log.Println("Downloading WebView2 NuGet package...")
	resp, err := http.Get(webView2NuGetURL)
	if err != nil {
		return fmt.Errorf("downloading webview2: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("reading nuget zip: %w", err)
	}

	archDir := "x64"
	if runtime.GOARCH == "arm64" {
		archDir = "arm64"
	}
	targetDll := fmt.Sprintf("build/native/%s/WebView2Loader.dll", archDir)

	found := false
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, targetDll) {
			log.Printf("Extracting %s from bundle...", f.Name)
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			w, err := zw.Create("WebView2Loader.dll")
			if err != nil {
				return err
			}
			if _, err := io.Copy(w, rc); err != nil {
				return err
			}
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("WebView2Loader.dll not found in NuGet package for arch %s", archDir)
	}

	return nil
}

// ── Linux ────────────────────────────────────────────────────────────────────

func packageLinux() error {
	outName := fmt.Sprintf("dist/prebuild-linux-%s.tar.gz", runtime.GOARCH)
	log.Printf("Packaging %s...", outName)

	outFile, err := os.Create(outName)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	if err := addFileToTar(tw, rendererUnixPath, "arc-renderer"); err != nil {
		return fmt.Errorf("adding renderer: %w", err)
	}

	log.Println("Resolving shared libraries via pure-Go ELF parser...")
	deps, err := resolveElfDependencies(rendererUnixPath)
	if err != nil {
		return fmt.Errorf("failed to parse elf dependencies: %w", err)
	}

	for _, dep := range deps {
		// Safety check: Avoid bundling core libc/ld.so as they are heavily tied
		// to the host system kernel and glibc version.
		if strings.Contains(dep, "libc.so") || strings.Contains(dep, "ld-linux") {
			continue
		}

		base := filepath.Base(dep)
		log.Printf("  -> bundling %s", base)
		if err := addFileToTar(tw, dep, base); err != nil {
			log.Printf("warning: failed to bundle %s: %v", dep, err)
		}
	}

	return nil
}

// resolveElfDependencies uses debug/elf to recursively find all shared
// library dependencies (.so) for a given binary, replicating 'ldd' natively.
func resolveElfDependencies(binPath string) ([]string, error) {
	visited := make(map[string]bool)
	var result []string

	// Common Linux library search paths
	searchPaths := []string{
		"/lib/x86_64-linux-gnu",
		"/usr/lib/x86_64-linux-gnu",
		"/lib/aarch64-linux-gnu",
		"/usr/lib/aarch64-linux-gnu",
		"/lib64",
		"/usr/lib64",
		"/lib",
		"/usr/lib",
	}

	var walk func(path string)
	walk = func(path string) {
		if visited[path] {
			return
		}
		visited[path] = true

		f, err := elf.Open(path)
		if err != nil {
			return // Skip if we can't open/parse it
		}
		defer f.Close()

		imported, err := f.ImportedLibraries()
		if err != nil {
			return
		}

		for _, libName := range imported {
			if strings.HasPrefix(libName, "linux-vdso") {
				continue // Virtual kernel object, doesn't exist on disk
			}

			// Find the absolute path of the library
			foundPath := ""
			for _, searchDir := range searchPaths {
				candidate := filepath.Join(searchDir, libName)
				if _, err := os.Stat(candidate); err == nil {
					foundPath = candidate
					break
				}
			}

			if foundPath != "" {
				if !visited[foundPath] {
					result = append(result, foundPath)
					walk(foundPath) // Recurse into the dependency
				}
			} else {
				log.Printf("  [!] warning: could not resolve path for %s", libName)
			}
		}
	}

	walk(binPath)
	return result, nil
}

// ── macOS ────────────────────────────────────────────────────────────────────

func packageDarwin() error {
	outName := fmt.Sprintf("dist/prebuild-mac-%s.tar.gz", runtime.GOARCH)
	log.Printf("Packaging %s...", outName)

	outFile, err := os.Create(outName)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	if err := addFileToTar(tw, rendererUnixPath, "arc-renderer"); err != nil {
		return fmt.Errorf("adding renderer: %w", err)
	}

	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func addFileToZip(zw *zip.Writer, srcPath, destName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = destName
	header.Method = zip.Deflate

	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, f)
	return err
}

func addFileToTar(tw *tar.Writer, srcPath, destName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, info.Name())
	if err != nil {
		return err
	}
	header.Name = destName

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}