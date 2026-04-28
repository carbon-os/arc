package sign

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MacOSBundle signs every binary inside Contents/MacOS/ individually,
// then signs the bundle itself with entitlements (inner → outer rule).
func MacOSBundle(bundle, entitlements, cert string) error {
	macosDir := filepath.Join(bundle, "Contents", "MacOS")
	entries, err := os.ReadDir(macosDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		bin := filepath.Join(macosDir, e.Name())
		if err := run("codesign", "--force", "--options", "runtime", "--sign", cert, bin); err != nil {
			return fmt.Errorf("sign %s: %w", e.Name(), err)
		}
	}

	return run("codesign",
		"--force",
		"--options", "runtime",
		"--entitlements", entitlements,
		"--sign", cert,
		bundle,
	)
}

// WindowsMSIX signs an MSIX package using signtool.
func WindowsMSIX(msixPath, certPath, certPassword string) error {
	args := []string{
		"sign",
		"/fd", "SHA256",
		"/a",
		"/f", certPath,
	}
	if certPassword != "" {
		args = append(args, "/p", certPassword)
	}
	args = append(args, msixPath)
	return run("signtool", args...)
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}