//go:build !darwin

package billing

// NewAppleStore returns nil on non-Apple platforms. Callers should nil-guard
// before using the returned value.
func NewAppleStore(_ App) *AppleStore { return nil }

// AppleStore is declared on all platforms so callers can hold a *AppleStore
// field without build tags. All methods are unreachable via the nil guard.
type AppleStore struct{}