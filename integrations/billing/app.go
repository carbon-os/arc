// Package billing provides Apple App Store and Microsoft Store integrations
// for arc applications.
//
// Use billing.NewAppleStore on Apple platforms and billing.NewMicrosoftStore
// on Windows. Both constructors return nil on non-matching platforms so call
// sites can nil-guard rather than using build tags.
package billing

// App is the subset of arc.App required by the billing stores.
// Declaring it here avoids a circular dependency: billing imports this
// interface; arc is not imported at all.
type App interface {
	// Send enqueues a JSON-serialisable command to arc-host.
	Send(v any)

	// OnEventPrefix registers fn to receive every inbound event whose type
	// starts with prefix. A second call with the same prefix replaces the
	// first handler.
	OnEventPrefix(prefix string, fn func(eventType string, payload []byte))
}