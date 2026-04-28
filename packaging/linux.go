package packaging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/carbon-os/arc/packaging/internal/archive"
)

func buildLinux(cfg PackagingConfig, appName string, opts BuildOptions) error {
	lin := cfg.Linux

	if lin.Name == "" {
		lin.Name = strings.ToLower(strings.ReplaceAll(appName, " ", "-"))
	}
	if lin.Version == "" {
		lin.Version = "1.0.0"
	}

	if !lin.Deb && !lin.AppImage {
		return fmt.Errorf("LinuxPackage: set Deb: true and/or AppImage: true")
	}

	if lin.Deb {
		step("  building .deb")
		if err := buildDeb(cfg, appName, lin); err != nil {
			return fmt.Errorf("deb: %w", err)
		}
	}

	if lin.AppImage {
		step("  building .AppImage")
		if err := buildAppImage(cfg, appName, lin); err != nil {
			return fmt.Errorf("appimage: %w", err)
		}
	}

	return nil
}

// ── .deb ─────────────────────────────────────────────────────────────────────

func buildDeb(cfg PackagingConfig, appName string, lin *LinuxPackage) error {
	pkgDir  := filepath.Join(cfg.OutDir, "deb-stage")
	binDir  := filepath.Join(pkgDir, "usr", "bin")
	debCtrl := filepath.Join(pkgDir, "DEBIAN")

	for _, d := range []string{binDir, debCtrl} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	defer os.RemoveAll(pkgDir)

	// Binary
	step("    copying Go binary → usr/bin/%s", lin.Name)
	if err := archive.CopyExec(cfg.BinaryPath, filepath.Join(binDir, lin.Name)); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	// Renderer alongside binary
	rendererSrc, err := resolveRenderer(cfg.RendererBuildDir)
	if err != nil {
		return err
	}
	step("    copying renderer → usr/bin/arc-renderer")
	if err := archive.CopyExec(rendererSrc, filepath.Join(binDir, "arc-renderer")); err != nil {
		return fmt.Errorf("copy renderer: %w", err)
	}

	// DEBIAN/control
	ctrl := renderDebControl(debControlData{
		Name:        lin.Name,
		Version:     lin.Version,
		Maintainer:  lin.Maintainer,
		Description: lin.Description,
		Homepage:    lin.Homepage,
	})
	if err := os.WriteFile(filepath.Join(debCtrl, "control"), []byte(ctrl), 0o644); err != nil {
		return err
	}

	debName := fmt.Sprintf("%s_%s_amd64.deb", lin.Name, lin.Version)
	debPath := filepath.Join(cfg.OutDir, debName)

	if err := archive.PackDeb(pkgDir, debPath); err != nil {
		return err
	}

	fmt.Printf("    📦  %s\n", debPath)
	return nil
}

type debControlData struct {
	Name        string
	Version     string
	Maintainer  string
	Description string
	Homepage    string
}

const debControlTmpl = `Package: {{.Name}}
Version: {{.Version}}
Architecture: amd64
Maintainer: {{.Maintainer}}
Description: {{.Description}}
{{- if .Homepage}}
Homepage: {{.Homepage}}
{{- end}}
`

func renderDebControl(data debControlData) string {
	t := template.Must(template.New("ctrl").Parse(debControlTmpl))
	var b strings.Builder
	_ = t.Execute(&b, data)
	return b.String()
}

// ── .AppImage ─────────────────────────────────────────────────────────────────

func buildAppImage(cfg PackagingConfig, appName string, lin *LinuxPackage) error {
	appDir := filepath.Join(cfg.OutDir, "appimage-stage", appName+".AppDir")
	usrBin := filepath.Join(appDir, "usr", "bin")

	for _, d := range []string{usrBin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	defer os.RemoveAll(filepath.Dir(appDir))

	// Binary
	step("    copying Go binary → AppDir/usr/bin/%s", lin.Name)
	if err := archive.CopyExec(cfg.BinaryPath, filepath.Join(usrBin, lin.Name)); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	// Renderer
	rendererSrc, err := resolveRenderer(cfg.RendererBuildDir)
	if err != nil {
		return err
	}
	step("    copying renderer → AppDir/usr/bin/arc-renderer")
	if err := archive.CopyExec(rendererSrc, filepath.Join(usrBin, "arc-renderer")); err != nil {
		return fmt.Errorf("copy renderer: %w", err)
	}

	// AppRun symlink
	if err := os.Symlink(filepath.Join("usr", "bin", lin.Name), filepath.Join(appDir, "AppRun")); err != nil && !os.IsExist(err) {
		return err
	}

	// .desktop file
	desktop := renderDesktopFile(desktopData{
		Name:       appName,
		Exec:       lin.Name,
		Comment:    lin.Description,
	})
	desktopPath := filepath.Join(appDir, lin.Name+".desktop")
	if err := os.WriteFile(desktopPath, []byte(desktop), 0o644); err != nil {
		return err
	}

	outName := fmt.Sprintf("%s-%s.AppImage", lin.Name, lin.Version)
	outPath := filepath.Join(cfg.OutDir, outName)

	step("    running appimagetool")
	if err := archive.PackAppImage(appDir, outPath); err != nil {
		return err
	}

	fmt.Printf("    📦  %s\n", outPath)
	return nil
}

type desktopData struct {
	Name    string
	Exec    string
	Comment string
}

const desktopTmpl = `[Desktop Entry]
Type=Application
Name={{.Name}}
Exec={{.Exec}}
Comment={{.Comment}}
Categories=Utility;
Terminal=false
`

func renderDesktopFile(data desktopData) string {
	t := template.Must(template.New("desktop").Parse(desktopTmpl))
	var b strings.Builder
	_ = t.Execute(&b, data)
	return b.String()
}