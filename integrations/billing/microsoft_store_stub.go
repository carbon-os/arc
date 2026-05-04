//go:build !windows

package billing

// NewMicrosoftStore returns nil on non-Windows platforms. Callers should
// nil-guard before using the returned value.
func NewMicrosoftStore(_ App) *MicrosoftStore { return nil }

// MicrosoftStore is declared on all platforms so callers can hold a
// *MicrosoftStore field without build tags.
type MicrosoftStore struct{}