package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	webview2Version   = "146.0.3856.97"
	webview2PackageID = "WebView2.Runtime.X64"
	webview2NuGetBase = "https://www.nuget.org/api/v2/package/"

	distDir = "dist"
)

func main() {
	if len(os.Args) < 3 {
		log.Fatalf("Usage: go run prebuild.go <os> <arch>\nExample: go run prebuild.go windows amd64")
	}
	targetOS := strings.ToLower(os.Args[1])
	targetArch := strings.ToLower(os.Args[2])

	if err := os.MkdirAll(distDir, 0755); err != nil {
		log.Fatalf("Failed to create dist dir: %v", err)
	}

	fmt.Printf("Packaging release for %s/%s...\n", targetOS, targetArch)

	switch targetOS {
	case "windows":
		if err := packageWindows(targetArch); err != nil {
			log.Fatalf("Windows packaging failed: %v", err)
		}
	case "darwin", "linux":
		if err := packageUnix(targetOS, targetArch); err != nil {
			log.Fatalf("%s packaging failed: %v", targetOS, err)
		}
	default:
		log.Fatalf("Unsupported OS: %s", targetOS)
	}

	fmt.Println("Done! Ready for GitHub Releases.")
}

// ── Windows (.zip with WebView2) ──────────────────────────────────────────────

func packageWindows(arch string) error {
	zipName := fmt.Sprintf("prebuild-win-%s.zip", arch)
	outPath := filepath.Join(distDir, zipName)

	// Adjust these paths to match your CMake output for Windows
	rendererPath := "../renderer/build/bin/Debug/renderer.exe"
	loaderPath := "../renderer/build/bin/Debug/WebView2Loader.dll"

	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	zw := zip.NewWriter(outFile)
	defer zw.Close()

	fmt.Println(" -> Adding renderer.exe")
	if err := addFileToZip(zw, rendererPath, "renderer.exe"); err != nil {
		return fmt.Errorf("renderer.exe: %w", err)
	}

	fmt.Println(" -> Adding WebView2Loader.dll")
	if err := addFileToZip(zw, loaderPath, "WebView2Loader.dll"); err != nil {
		return fmt.Errorf("WebView2Loader.dll: %w", err)
	}

	fmt.Println(" -> Fetching and bundling WebView2 Runtime...")
	if err := bundleWebView2(zw); err != nil {
		return fmt.Errorf("webview2 bundle: %w", err)
	}

	fmt.Printf("Created: %s\n", outPath)
	return nil
}

func bundleWebView2(zw *zip.Writer) error {
	url := webview2NuGetBase + webview2PackageID + "/" + webview2Version
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	nugetBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(bytes.NewReader(nugetBytes), int64(len(nugetBytes)))
	if err != nil {
		return err
	}

	prefix := findWebView2Prefix(zr)
	if prefix == "" {
		return fmt.Errorf("msedgewebview2.exe not found in nuget package")
	}

	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, prefix) || f.FileInfo().IsDir() {
			continue
		}

		relPath := f.Name[len(prefix):]
		if relPath == "" {
			continue
		}

		destName := filepath.ToSlash(filepath.Join("webview2", relPath))
		w, err := zw.Create(destName)
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(w, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func addFileToZip(zw *zip.Writer, srcPath, destName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w, err := zw.Create(destName)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, f)
	return err
}

func findWebView2Prefix(zr *zip.Reader) string {
	candidates := []string{
		"WebView2/",
		"content/WebView2/",
		"contentFiles/any/any/WebView2/",
	}
	for _, prefix := range candidates {
		for _, f := range zr.File {
			if strings.EqualFold(f.Name, prefix+"msedgewebview2.exe") {
				return prefix
			}
		}
	}
	return ""
}

// ── Mac & Linux (.tar.gz) ─────────────────────────────────────────────────────

func packageUnix(osName, arch string) error {
	osPrefix := osName
	if osName == "darwin" {
		osPrefix = "mac"
	}

	tarName := fmt.Sprintf("prebuild-%s-%s.tar.gz", osPrefix, arch)
	outPath := filepath.Join(distDir, tarName)

	// Adjust this path to match your CMake output for Unix
	rendererPath := "../renderer/build/bin/renderer"

	outFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	fmt.Println(" -> Adding renderer binary")
	if err := addFileToTar(tw, rendererPath, "renderer"); err != nil {
		return fmt.Errorf("renderer: %w", err)
	}

	fmt.Printf("Created: %s\n", outPath)
	return nil
}

func addFileToTar(tw *tar.Writer, srcPath, destName string) error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	hdr, err := tar.FileInfoHeader(stat, "")
	if err != nil {
		return err
	}
	
	hdr.Name = destName
	hdr.Mode = 0755 // Ensure it remains executable when extracted

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}