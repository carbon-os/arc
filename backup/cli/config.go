package cli

import (
	"os"
	"path/filepath"
	"strings"
)

type buildConfig struct {
	binaryName string   // final host binary name (-o, defaults to dir name)
	goFlags    []string // extra flags forwarded verbatim to go build
	goPackage  string   // package argument, defaults to "."
	arcJSON    string   // path to arc.json (--config, auto-detected if empty)

	wd            string // working directory at invocation time
	projectDir    string // <wd>/arc-project/
	arcRepoDir    string // <wd>/arc-project/arc-repo/
	libarcDir     string // <wd>/arc-project/arc-repo/libarc/
	libarcDestDir string // <wd>/arc-project/libarc/
	moduleLib     string // <wd>/arc-project/libarc-module.{ext}
	libarcLib     string // <wd>/arc-project/{libarc.dylib|libarc.so|arc.dll}
	stubPath      string // <wd>/arc_entry_generated.go
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

	// Auto-detect arc.json if not explicitly provided.
	if cfg.arcJSON == "" {
		candidate := filepath.Join(wd, "arc.json")
		if _, err := os.Stat(candidate); err == nil {
			cfg.arcJSON = candidate
		}
	}

	cfg.projectDir    = filepath.Join(wd, "arc-project")
	cfg.arcRepoDir    = filepath.Join(cfg.projectDir, "arc-repo")
	cfg.libarcDir     = filepath.Join(cfg.arcRepoDir, "libarc")
	cfg.libarcDestDir = filepath.Join(cfg.projectDir, "libarc")
	cfg.moduleLib     = filepath.Join(cfg.projectDir, "libarc-module"+SharedExt())
	cfg.libarcLib     = filepath.Join(cfg.projectDir, LibarcFileName())
	cfg.stubPath      = filepath.Join(wd, "arc_entry_generated.go")

	return cfg, nil
}