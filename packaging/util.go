package packaging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func step(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}

// executableName lowercases and strips spaces from the app name.
func executableName(appName string) string {
	return strings.ToLower(strings.ReplaceAll(appName, " ", ""))
}

// resolveRenderer locates the renderer binary from an explicit dir or
// falls back to standard cmake output locations relative to the working dir.
func resolveRenderer(buildDir string) (string, error) {
	if buildDir == "" {
		buildDir = "renderer/build/bin"
	}

	candidates := []string{
		filepath.Join(buildDir, "renderer"),
		filepath.Join(buildDir, "Release", "renderer"),
		filepath.Join(buildDir, "Debug", "renderer"),
		filepath.Join(buildDir, "renderer.exe"),
		filepath.Join(buildDir, "Release", "renderer.exe"),
		filepath.Join(buildDir, "Debug", "renderer.exe"),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf(
		"renderer binary not found — searched:\n  %s\n\nBuild it first:\n  cmake -B renderer/build -G Ninja -DCMAKE_BUILD_TYPE=Release\n  cmake --build renderer/build",
		strings.Join(candidates, "\n  "),
	)
}