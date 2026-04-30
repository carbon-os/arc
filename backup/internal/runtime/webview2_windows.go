//go:build windows

package runtime

import (
	"path/filepath"
)

// EnsureWebView2 returns the path to the extracted WebView2 runtime folder.
// The actual downloading and extraction is now handled entirely by EnsureRenderer
// which unpacks the prebuilt zip containing both the renderer and WebView2.
func EnsureWebView2() (string, error) {
	dir, err := CachedDir()
	if err != nil {
		return "", err
	}
	
	// The prebuild zip extracts the runtime directly into this "webview2" subdirectory
	return filepath.Join(dir, "webview2"), nil
}