package packaging

import (
	"fmt"
	"os"
	"strings"
)

// BuildOptions are derived from CLI flags at startup.
type BuildOptions struct {
	SkipSign bool
}

// ParseFlags inspects os.Args for --package[=target] and --skip-sign.
// Returns (targets, opts, true) when packaging was requested,
// or (nil, {}, false) for a normal application run.
func ParseFlags() ([]string, BuildOptions, bool) {
	var targets []string
	var opts BuildOptions
	requested := false

	for _, arg := range os.Args[1:] {
		switch {
		case arg == "--package":
			requested = true

		case strings.HasPrefix(arg, "--package="):
			raw := strings.TrimPrefix(arg, "--package=")
			for _, t := range strings.Split(raw, ",") {
				if t = strings.TrimSpace(t); t != "" {
					targets = append(targets, t)
				}
			}
			requested = true

		case arg == "--skip-sign":
			opts.SkipSign = true
		}
	}

	return targets, opts, requested
}

// Build runs the full packaging pipeline for all non-nil platform configs
// and returns when all targets are complete, or on first error.
// Called by arc.App.Run — the renderer process is never spawned, so this
// is safe in headless / CI environments.
func Build(cfg PackagingConfig, appName string, targets []string, opts BuildOptions) error {
	if cfg.OutDir == "" {
		cfg.OutDir = "dist"
	}

	if cfg.BinaryPath == "" {
		self, err := os.Executable()
		if err != nil {
			return fmt.Errorf("packaging: cannot resolve executable path: %w", err)
		}
		cfg.BinaryPath = self
	}

	if err := os.MkdirAll(cfg.OutDir, 0o755); err != nil {
		return fmt.Errorf("packaging: create output dir: %w", err)
	}

	// Build only the platforms that are configured and match the requested
	// targets filter (empty targets = build everything configured).
	built := 0

	if cfg.MacOS != nil && wantTarget(targets, "macos") {
		fmt.Printf("\n▶  macOS\n")
		if err := buildMacOS(cfg, appName, opts); err != nil {
			return fmt.Errorf("packaging [macos]: %w", err)
		}
		built++
	}

	if cfg.Windows != nil && wantTarget(targets, "windows") {
		fmt.Printf("\n▶  Windows\n")
		if err := buildWindows(cfg, appName, opts); err != nil {
			return fmt.Errorf("packaging [windows]: %w", err)
		}
		built++
	}

	if cfg.Linux != nil && wantTarget(targets, "linux") {
		fmt.Printf("\n▶  Linux\n")
		if err := buildLinux(cfg, appName, opts); err != nil {
			return fmt.Errorf("packaging [linux]: %w", err)
		}
		built++
	}

	if built == 0 {
		return fmt.Errorf("packaging: no targets configured — set MacOS, Windows, or Linux in PackagingConfig")
	}

	fmt.Printf("\n✅  packages written to %s/\n", cfg.OutDir)
	return nil
}

// wantTarget reports whether t should be built given an explicit filter list.
// An empty filter means "build all".
func wantTarget(filter []string, t string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		if strings.EqualFold(f, t) {
			return true
		}
	}
	return false
}