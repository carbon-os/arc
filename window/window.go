// Package window contains the configuration type for native windows.
package window

// Config holds the parameters for creating a native window.
type Config struct {
	// Title is the text shown in the window's title bar.
	Title string

	// Width and Height are the initial client-area dimensions in logical pixels.
	// Defaults to 800×600 if zero.
	Width, Height int

	// NoResize prevents the user from resizing the window.
	// The zero value (false) means the window is resizable — the typical default.
	NoResize bool

	// Debug enables the WebView developer-tools panel.
	// Useful during development; disable for production builds.
	Debug bool
}