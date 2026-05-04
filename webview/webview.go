// Package webview contains the configuration type for overlay WebViews.
package webview

// Config holds the parameters for a view-backed (overlay) WebView.
// Overlay WebViews float on top of a window's primary WebView at a
// fixed position and can be shown, hidden, and repositioned independently.
type Config struct {
	// X, Y are the initial position relative to the parent window's top-left.
	X, Y int

	// Width, Height are the initial size in logical pixels.
	// Defaults to 400×300 if zero.
	Width, Height int

	// ZOrder controls stacking order among overlays on the same window.
	// Reserved for future use — not currently forwarded to the host.
	ZOrder int

	// Debug enables the WebView developer-tools panel.
	Debug bool
}