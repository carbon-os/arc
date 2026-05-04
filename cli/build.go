package cli

import (
	"fmt"
	"runtime"
)

// Run is the entry point called by cmd/arc for the build subcommand.
func Run(args []string) error {
	cfg, err := ParseBuildArgs(args)
	if err != nil {
		return err
	}

	steps := []struct {
		label    string
		fn       func(*buildConfig) error
		skip     bool // set true to skip on this platform
	}{
		{"cloning libarc",         stepCloneLibarc,    false},
		{"building libarc",        stepBuildLibarc,    false},
		{"resolving Go dependencies", stepGoMod,       false},
		{"compiling Go module",    stepCompileGoModule, false},
		{"generating arc-project", stepGenerateProject, false},
		{"configuring cmake",      stepConfigureCmake,  false},
		// StoreKit is macOS-only — the Xcode scheme only exists after the
		// cmake -G Xcode step above, and StoreKit configuration is an
		// Apple-platform concept.
		{"configuring StoreKit",   stepStoreKit,        runtime.GOOS != "darwin"},
	}

	for _, s := range steps {
		if s.skip {
			continue
		}
		fmt.Printf("\n▶  %s\n", s.label)
		if err := s.fn(cfg); err != nil {
			return fmt.Errorf("%s: %w", s.label, err)
		}
	}

	fmt.Printf("\n✅  arc-project/ is ready\n\n")
	fmt.Printf("   To build the final binary:\n")
	fmt.Printf("     cd arc-project && cmake --build build\n\n")
	fmt.Printf("   To debug natively:\n")
	fmt.Printf("     open arc-project/build/*.xcodeproj\n\n")
	return nil
}