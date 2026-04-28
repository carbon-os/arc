package packaging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/carbon-os/arc/packaging/internal/archive"
	"github.com/carbon-os/arc/packaging/internal/sign"
)

func buildWindows(cfg PackagingConfig, appName string, opts BuildOptions) error {
	win := cfg.Windows

	if win.PackageID == "" {
		win.PackageID = strings.ReplaceAll(appName, " ", "")
	}
	if win.Version == "" {
		win.Version = "1.0.0.0"
	}
	if win.DisplayName == "" {
		win.DisplayName = appName
	}

	stageDir := filepath.Join(cfg.OutDir, "windows-stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(stageDir)

	// ── Binaries ──────────────────────────────────────────────────────────
	step("  copying Go binary → %s.exe", win.PackageID)
	exeDest := filepath.Join(stageDir, win.PackageID+".exe")
	if err := archive.CopyExec(cfg.BinaryPath, exeDest); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	rendererSrc, err := resolveRenderer(cfg.RendererBuildDir)
	if err != nil {
		return err
	}
	step("  copying renderer → renderer.exe")
	if err := archive.CopyExec(rendererSrc, filepath.Join(stageDir, "renderer.exe")); err != nil {
		return fmt.Errorf("copy renderer: %w", err)
	}

	// ── AppxManifest.xml ──────────────────────────────────────────────────
	step("  generating AppxManifest.xml")
	manifest := renderAppxManifest(appxManifestData{
		PackageID:   win.PackageID,
		Publisher:   win.Publisher,
		Version:     win.Version,
		DisplayName: win.DisplayName,
		AppName:     appName,
		Executable:  win.PackageID + ".exe",
	})
	if err := os.WriteFile(filepath.Join(stageDir, "AppxManifest.xml"), []byte(manifest), 0o644); err != nil {
		return err
	}

	// ── Pack ──────────────────────────────────────────────────────────────
	msixPath := filepath.Join(cfg.OutDir, win.PackageID+".msix")
	step("  packing → %s", filepath.Base(msixPath))
	if err := archive.PackMSIX(stageDir, msixPath); err != nil {
		return fmt.Errorf("pack msix: %w", err)
	}

	// ── Sign ──────────────────────────────────────────────────────────────
	if opts.SkipSign || win.CertPath == "" {
		fmt.Println("  ⚠️   skipping signtool (no cert or --skip-sign)")
	} else {
		step("  signing → %s", filepath.Base(msixPath))
		if err := sign.WindowsMSIX(msixPath, win.CertPath, win.CertPassword); err != nil {
			return err
		}
	}

	fmt.Printf("  📦  %s\n", msixPath)
	return nil
}

// ── AppxManifest template ─────────────────────────────────────────────────────

type appxManifestData struct {
	PackageID   string
	Publisher   string
	Version     string
	DisplayName string
	AppName     string
	Executable  string
}

const appxManifestTmpl = `<?xml version="1.0" encoding="utf-8"?>
<Package xmlns="http://schemas.microsoft.com/appx/manifest/foundation/windows10"
         xmlns:uap="http://schemas.microsoft.com/appx/manifest/uap/windows10"
         xmlns:rescap="http://schemas.microsoft.com/appx/manifest/foundation/windows10/restrictedcapabilities">
  <Identity Name="{{.PackageID}}"
            Publisher="{{.Publisher}}"
            Version="{{.Version}}"
            ProcessorArchitecture="x64" />
  <Properties>
    <DisplayName>{{.DisplayName}}</DisplayName>
    <PublisherDisplayName>{{.Publisher}}</PublisherDisplayName>
    <Logo>Assets\StoreLogo.png</Logo>
  </Properties>
  <Dependencies>
    <TargetDeviceFamily Name="Windows.Desktop"
                        MinVersion="10.0.17763.0"
                        MaxVersionTested="10.0.22621.0" />
  </Dependencies>
  <Resources>
    <Resource Language="en-us" />
  </Resources>
  <Applications>
    <Application Id="App" Executable="{{.Executable}}" EntryPoint="Windows.FullTrustApplication">
      <uap:VisualElements DisplayName="{{.AppName}}"
                          Description="{{.AppName}}"
                          BackgroundColor="transparent"
                          Square150x150Logo="Assets\Square150x150Logo.png"
                          Square44x44Logo="Assets\Square44x44Logo.png" />
    </Application>
  </Applications>
  <Capabilities>
    <rescap:Capability Name="runFullTrust" />
    <Capability Name="internetClient" />
  </Capabilities>
</Package>
`

func renderAppxManifest(data appxManifestData) string {
	t := template.Must(template.New("appx").Parse(appxManifestTmpl))
	var b strings.Builder
	_ = t.Execute(&b, data)
	return b.String()
}