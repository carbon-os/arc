package cli

import (
	"os"
	"path/filepath"
	"strings"
)

type buildConfig struct {
	binaryName string
	goFlags    []string
	goPackage  string
	arcJSON    string

	wd            string
	projectDir    string // <wd>/arc-project/
	libarcRepoDir string // <projectDir>/libarc-repo/   ← cloned libarc
	vcpkgDir      string // <projectDir>/vcpkg/          ← cloned vcpkg
	libarcDestDir string // <projectDir>/libarc/         (copied headers)
	moduleLib     string
	libarcLib     string
	stubPath      string
}

func ParseBuildArgs(args []string) (*buildConfig, error) {
	cfg := &buildConfig{goPackage: "."}

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			i++
			cfg.binaryName = args[i]
		case strings.HasPrefix(args[i], "-o="):
			cfg.binaryName = strings.TrimPrefix(args[i], "-o=")

		case args[i] == "--config" && i+1 < len(args):
			i++
			cfg.arcJSON = args[i]
		case strings.HasPrefix(args[i], "--config="):
			cfg.arcJSON = strings.TrimPrefix(args[i], "--config=")

		case !strings.HasPrefix(args[i], "-"):
			cfg.goPackage = args[i]
		default:
			cfg.goFlags = append(cfg.goFlags, args[i])
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg.wd = wd

	if cfg.binaryName == "" {
		cfg.binaryName = filepath.Base(wd)
	}

	if cfg.arcJSON == "" {
		candidate := filepath.Join(wd, "arc.json")
		if _, err := os.Stat(candidate); err == nil {
			cfg.arcJSON = candidate
		}
	}

	cfg.projectDir    = filepath.Join(wd, "arc-project")
	cfg.libarcRepoDir = filepath.Join(cfg.projectDir, "libarc-repo")
	cfg.vcpkgDir      = filepath.Join(cfg.projectDir, "vcpkg")
	cfg.libarcDestDir = filepath.Join(cfg.projectDir, "libarc")
	cfg.moduleLib     = filepath.Join(cfg.projectDir, "libarc-module"+SharedExt())
	cfg.libarcLib     = filepath.Join(cfg.projectDir, LibarcFileName())
	cfg.stubPath      = filepath.Join(wd, "arc_entry_generated.go")

	return cfg, nil
}