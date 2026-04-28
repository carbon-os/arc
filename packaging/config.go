package packaging

// PackagingConfig is set once in arc.AppConfig.
// Each platform field is optional — nil means that target is skipped.
// Targets are built when --package is passed at runtime.
type PackagingConfig struct {
	MacOS   *MacOSPackage
	Windows *WindowsPackage
	Linux   *LinuxPackage

	// Output directory — defaults to "dist"
	OutDir string

	// Path to the compiled Go binary to embed.
	// Defaults to os.Executable() when empty.
	BinaryPath string

	// Path to the renderer binary (cmake output).
	// Defaults to the value set in arc.RendererConfig.Path.
	RendererBuildDir string
}

// ── macOS ─────────────────────────────────────────────────────────────────────

// MacOSPackage describes a signed .app bundle for macOS.
type MacOSPackage struct {
	BundleID string // e.g. "com.carbon.ai"
	Version  string // CFBundleShortVersionString, e.g. "1.2.0"
	Build    string // CFBundleVersion, e.g. "42"
	MinMacOS string // LSMinimumSystemVersion — defaults to "13.0"

	// Signing — both required for a distributable build.
	// Pass --skip-sign to produce an unsigned bundle for local testing.
	TeamID   string // 10-char Apple team ID, e.g. "G37U93C45G"
	SignCert string // partial name matched by codesign, e.g. "Developer ID Application: Acme"

	// In-app purchases — omit entirely if the app has no IAP.
	IAP *IAPConfig
}

// ── Windows ───────────────────────────────────────────────────────────────────

// WindowsPackage describes an MSIX/APPX bundle for Windows.
type WindowsPackage struct {
	PackageID   string // e.g. "CarbonAI"
	Publisher   string // must match cert subject, e.g. "CN=Carbon Inc, O=Carbon Inc, C=US"
	Version     string // four-part, e.g. "1.2.0.0"
	DisplayName string // shown in Add/Remove Programs

	// Path to your .pfx signing certificate — optional, skip-sign if empty.
	CertPath     string
	CertPassword string
}

// ── Linux ─────────────────────────────────────────────────────────────────────

// LinuxPackage describes Linux distribution targets.
// Both Deb and AppImage can be enabled independently.
type LinuxPackage struct {
	// Common identity
	Name        string // package name, e.g. "carbon-ai"
	Version     string // e.g. "1.2.0"
	Maintainer  string // e.g. "Carbon Inc <hello@carbon.ai>"
	Description string
	Homepage    string

	// Select which Linux formats to produce.
	Deb      bool // produces dist/<name>_<version>_amd64.deb
	AppImage bool // produces dist/<name>-<version>.AppImage
}

// ── IAP (macOS only for now) ──────────────────────────────────────────────────

// IAPConfig describes all in-app purchase products for the app.
type IAPConfig struct {
	// StoreKit sandbox config identifier (8-char hex).
	// Defaults to "00000000" when empty.
	ConfigIdentifier string

	SubscriptionGroups []SubscriptionGroup
	NonConsumables     []SimpleProduct
	Consumables        []SimpleProduct
}

// SubscriptionGroup is an App Store subscription group.
type SubscriptionGroup struct {
	ID            string // 8-char hex, e.g. "7F3D9E2B"
	Name          string
	Subscriptions []Subscription
}

// Subscription is a single auto-renewable subscription within a group.
type Subscription struct {
	InternalID      string // 8-char hex — generated if empty
	ProductID       string // e.g. "com.carbon.ai.plus.monthly"
	ReferenceName   string
	DisplayName     string
	Description     string
	DisplayPrice    string // e.g. "9.99"
	Period          string // ISO-8601, e.g. "P1M"
	FamilyShareable bool
	GroupNumber     int
}

// SimpleProduct is a non-consumable or consumable IAP product.
type SimpleProduct struct {
	InternalID    string // 8-char hex — generated if empty
	ProductID     string
	ReferenceName string
	DisplayName   string
	Description   string
	DisplayPrice  string
}