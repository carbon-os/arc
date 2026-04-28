package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// CopyExec copies src to dst with executable permissions.
func CopyExec(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// PackMSIX zips the contents of stageDir into an .msix file.
// Tries makeappx first; falls back to a pure-Go zip writer.
func PackMSIX(stageDir, outPath string) error {
	if path, err := exec.LookPath("makeappx"); err == nil {
		cmd := exec.Command(path, "pack", "/d", stageDir, "/p", outPath, "/nv")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return zipDir(stageDir, outPath)
}

// PackDeb builds a .deb using dpkg-deb when available.
func PackDeb(stageDir, outPath string) error {
	if path, err := exec.LookPath("dpkg-deb"); err == nil {
		cmd := exec.Command(path, "--build", stageDir, outPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("dpkg-deb not found — install dpkg or build on a Debian/Ubuntu host")
}

// PackAppImage runs appimagetool to produce an AppImage.
func PackAppImage(appDir, outPath string) error {
	tool, err := exec.LookPath("appimagetool")
	if err != nil {
		return fmt.Errorf("appimagetool not found — download from https://github.com/AppImage/AppImageKit/releases")
	}
	cmd := exec.Command(tool, appDir, outPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// zipDir zips all files under root into outPath (fallback for PackMSIX).
func zipDir(root, outPath string) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(w, in)
		return err
	})
}