//go:build !windows

package runtime

// EnsureWebView2 is a no-op on non-Windows platforms; WebView2 is
// Windows-only. Returns ("", nil) so call-sites need no platform guards.
func EnsureWebView2() (string, error) { return "", nil }