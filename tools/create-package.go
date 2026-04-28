// tools/create-package.go
//
// Assembles a signed, self-contained macOS .app bundle from a local CMake
// renderer build and a compiled Go binary.
//
// Usage:
//
//	go run tools/create-package.go \
//	  --app-path   ./myapp           \
//	  --config     tools/myapp.json
//
// If --config is omitted the tool looks for tools/package.json.
// All generated artefacts land in dist/<AppName>.app.

package main

import (
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

// ── Config schema ─────────────────────────────────────────────────────────────

// PackageConfig is the single source of truth written by the developer.
// Every generated file (Info.plist, entitlements, .storekit) is derived from
// this — nothing is hard-coded anywhere else.
type PackageConfig struct {
	// App identity
	AppName    string `json:"app_name"`    // e.g. "Carbon AI"
	Executable string `json:"executable"`  // binary name inside MacOS/, e.g. "carbonai"
	BundleID   string `json:"bundle_id"`   // e.g. "com.carbon.spreadsheets"
	Version    string `json:"version"`     // CFBundleShortVersionString, e.g. "1.0.0"
	Build      string `json:"build"`       // CFBundleVersion, e.g. "42"
	MinMacOS   string `json:"min_macos"`   // e.g. "13.0"

	// Signing
	TeamID   string `json:"team_id"`   // 10-char Apple team ID, e.g. "G37U93C45G"
	SignCert string `json:"sign_cert"` // Partial name matched by codesign, e.g. "Developer ID Application: Acme"

	// Renderer build
	// Absolute or relative (to repo root) path to the cmake binary output dir.
	// Defaults to "renderer/build/bin" if empty.
	RendererBuildDir string `json:"renderer_build_dir"`

	// In-app purchases  (omit the array entirely if you have no IAP)
	IAP IAPConfig `json:"iap"`
}

type IAPConfig struct {
	// StoreKit sandbox configuration identifier (8-char hex, any value).
	ConfigIdentifier string `json:"config_identifier"`

	SubscriptionGroups []SubscriptionGroupConfig `json:"subscription_groups"`
	NonConsumables     []SimpleProductConfig     `json:"non_consumables"`
	Consumables        []SimpleProductConfig     `json:"consumables"`
}

type SubscriptionGroupConfig struct {
	// 8-char hex group ID, e.g. "7F3D9E2B"
	ID   string                   `json:"id"`
	Name string                   `json:"name"`
	Subs []SubscriptionConfig     `json:"subscriptions"`
}

type SubscriptionConfig struct {
	// 8-char hex internal ID, e.g. "D1A2B3C4"
	InternalID      string `json:"internal_id"`
	ProductID       string `json:"product_id"`        // e.g. "com.acme.app.pro_monthly"
	ReferenceName   string `json:"reference_name"`
	DisplayName     string `json:"display_name"`
	Description     string `json:"description"`
	DisplayPrice    string `json:"display_price"`     // e.g. "9.99"
	Period          string `json:"period"`            // ISO-8601, e.g. "P1M"
	FamilyShareable bool   `json:"family_shareable"`
	GroupNumber     int    `json:"group_number"`
}

type SimpleProductConfig struct {
	InternalID    string `json:"internal_id"`
	ProductID     string `json:"product_id"`
	ReferenceName string `json:"reference_name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	DisplayPrice  string `json:"display_price"`
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	appPath := flag.String("app-path", "", "Path to the compiled Go binary (required)")
	cfgPath := flag.String("config", "tools/package.json", "Path to your package JSON config")
	outDir  := flag.String("out", "dist", "Output directory for the .app bundle")
	skipSign := flag.Bool("skip-sign", false, "Assemble the bundle but skip codesign (useful for CI without certs)")
	flag.Parse()

	if *appPath == "" {
		die("--app-path is required\n\nExample:\n  go run tools/create-package.go --app-path ./myapp --config tools/myapp.json")
	}

	cfg := loadConfig(*cfgPath)
	validate(cfg)

	appBundle := filepath.Join(*outDir, cfg.AppName+".app")
	macosDir  := filepath.Join(appBundle, "Contents", "MacOS")
	resDir    := filepath.Join(appBundle, "Contents", "Resources")

	step("Cleaning previous build")
	must(os.RemoveAll(appBundle))
	must(os.MkdirAll(macosDir, 0o755))
	must(os.MkdirAll(resDir, 0o755))

	step("Copying Go binary  →  %s", filepath.Join(macosDir, cfg.Executable))
	copyExec(*appPath, filepath.Join(macosDir, cfg.Executable))

	rendererSrc := resolveRenderer(cfg)
	step("Copying renderer   →  %s", filepath.Join(macosDir, "renderer"))
	copyExec(rendererSrc, filepath.Join(macosDir, "renderer"))

	step("Generating Info.plist")
	writeFile(filepath.Join(appBundle, "Contents", "Info.plist"), renderInfoPlist(cfg))

	entitlementsPath := filepath.Join(*outDir, cfg.AppName+".entitlements")
	step("Generating entitlements  →  %s", entitlementsPath)
	writeFile(entitlementsPath, renderEntitlements(cfg))

	if hasIAP(cfg) {
		skPath := filepath.Join(*outDir, cfg.AppName+".storekit")
		step("Generating .storekit  →  %s", skPath)
		writeJSON(skPath, buildStoreKit(cfg))
	}

	if *skipSign {
		warn("Skipping codesign (--skip-sign)")
	} else {
		step("Signing bundle")
		sign(appBundle, entitlementsPath, cfg.SignCert)
	}

	fmt.Printf("\n✅  Bundle ready: %s\n", appBundle)
	if !*skipSign {
		fmt.Printf("    Verify with:\n")
		fmt.Printf("      codesign --verify --deep --strict --verbose=2 %q\n", appBundle)
		fmt.Printf("      spctl   --assess --type execute --verbose       %q\n", appBundle)
	}
}

// ── Config loading ────────────────────────────────────────────────────────────

func loadConfig(path string) PackageConfig {
	f, err := os.Open(path)
	if err != nil {
		die("Cannot open config %q: %v\n\nCreate one with:\n  go run tools/create-package.go --config tools/myapp.json\n(see tools/package.example.json for the schema)", path, err)
	}
	defer f.Close()

	var cfg PackageConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		die("Invalid JSON in %q: %v", path, err)
	}

	// Defaults
	if cfg.Executable == "" {
		cfg.Executable = strings.ToLower(strings.ReplaceAll(cfg.AppName, " ", ""))
	}
	if cfg.Version == "" { cfg.Version = "1.0.0" }
	if cfg.Build   == "" { cfg.Build   = "1"     }
	if cfg.MinMacOS == "" { cfg.MinMacOS = "13.0" }
	if cfg.IAP.ConfigIdentifier == "" { cfg.IAP.ConfigIdentifier = "00000000" }

	return cfg
}

func validate(cfg PackageConfig) {
	var errs []string
	if cfg.AppName  == "" { errs = append(errs, "app_name is required") }
	if cfg.BundleID == "" { errs = append(errs, "bundle_id is required") }
	if cfg.TeamID   == "" { errs = append(errs, "team_id is required") }
	if cfg.SignCert  == "" { errs = append(errs, "sign_cert is required (partial match, e.g. \"Developer ID Application: Acme\")") }
	if len(errs) > 0 {
		die("Config validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}
}

// ── Renderer resolution ───────────────────────────────────────────────────────

func resolveRenderer(cfg PackageConfig) string {
	dir := cfg.RendererBuildDir
	if dir == "" {
		dir = "renderer/build/bin"
	}

	candidates := []string{
		filepath.Join(dir, "renderer"),
		filepath.Join(dir, "Debug",   "renderer"),
		filepath.Join(dir, "Release", "renderer"),
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	die(
		"Renderer binary not found. Searched:\n  %s\n\nBuild it first:\n  cmake -B renderer/build -G Ninja -DCMAKE_BUILD_TYPE=Release\n  cmake --build renderer/build",
		strings.Join(candidates, "\n  "),
	)
	return "" // unreachable
}

// ── File generators ───────────────────────────────────────────────────────────

const infoPlistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key>       <string>{{.BundleID}}</string>
    <key>CFBundleName</key>             <string>{{.AppName}}</string>
    <key>CFBundleDisplayName</key>      <string>{{.AppName}}</string>
    <key>CFBundleExecutable</key>       <string>{{.Executable}}</string>
    <key>CFBundlePackageType</key>      <string>APPL</string>
    <key>CFBundleShortVersionString</key><string>{{.Version}}</string>
    <key>CFBundleVersion</key>          <string>{{.Build}}</string>
    <key>LSMinimumSystemVersion</key>   <string>{{.MinMacOS}}</string>
    <key>NSPrincipalClass</key>         <string>NSApplication</string>
    <key>NSHighResolutionCapable</key>  <true/>
</dict>
</plist>
`

const entitlementsTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.application-identifier</key>
    <string>{{.TeamID}}.{{.BundleID}}</string>

    <key>com.apple.developer.team-identifier</key>
    <string>{{.TeamID}}</string>

    <key>com.apple.security.app-sandbox</key>
    <false/>

    <key>com.apple.security.network.client</key>
    <true/>
</dict>
</plist>
`

func renderInfoPlist(cfg PackageConfig) string   { return renderTmpl(infoPlistTmpl, cfg) }
func renderEntitlements(cfg PackageConfig) string { return renderTmpl(entitlementsTmpl, cfg) }

func renderTmpl(tmpl string, data any) string {
	t := template.Must(template.New("").Parse(tmpl))
	var b strings.Builder
	must(t.Execute(&b, data))
	return b.String()
}

// ── StoreKit builder ──────────────────────────────────────────────────────────

// These mirror the exact JSON shape Xcode expects (v2 format).
// All field names and structure match what Xcode 15+ reads.

type skFile struct {
	Identifier         string          `json:"identifier"`
	NonConsumables     []skProduct     `json:"nonConsumableProducts"`
	Consumables        []skProduct     `json:"consumableProducts"`
	SubscriptionGroups []skGroup       `json:"subscriptionGroups"`
	Settings           map[string]any  `json:"settings"`
	Version            skVersion       `json:"version"`
}

type skGroup struct {
	ID            string           `json:"id"`
	Localizations []any            `json:"localizations"`
	Name          string           `json:"name"`
	Subscriptions []skSubscription `json:"subscriptions"`
}

type skSubscription struct {
	AdHocOffers         []any           `json:"adHocOffers"`
	CodeOffers          []any           `json:"codeOffers"`
	DisplayPrice        string          `json:"displayPrice"`
	FamilyShareable     bool            `json:"familyShareable"`
	GroupNumber         int             `json:"groupNumber"`
	InternalID          string          `json:"internalID"`
	IntroductoryOffer   any             `json:"introductoryOffer"`
	Localizations       []skLocale      `json:"localizations"`
	ProductID           string          `json:"productID"`
	RecurringPeriod     string          `json:"recurringSubscriptionPeriod"`
	ReferenceName       string          `json:"referenceName"`
	SubscriptionGroupID string          `json:"subscriptionGroupID"`
	Type                string          `json:"type"`
}

type skProduct struct {
	AdHocOffers   []any      `json:"adHocOffers"`
	CodeOffers    []any      `json:"codeOffers"`
	DisplayPrice  string     `json:"displayPrice"`
	FamilyShareable bool     `json:"familyShareable"`
	InternalID    string     `json:"internalID"`
	Localizations []skLocale `json:"localizations"`
	ProductID     string     `json:"productID"`
	ReferenceName string     `json:"referenceName"`
	Type          string     `json:"type"`
}

type skLocale struct {
	Description string `json:"description"`
	DisplayName string `json:"displayName"`
	Locale      string `json:"locale"`
}

type skVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

func buildStoreKit(cfg PackageConfig) skFile {
	f := skFile{
		Identifier:     cfg.IAP.ConfigIdentifier,
		NonConsumables: []skProduct{},
		Consumables:    []skProduct{},
		Settings:       map[string]any{},
		Version:        skVersion{Major: 2, Minor: 0},
	}

	// Non-consumables
	for _, p := range cfg.IAP.NonConsumables {
		f.NonConsumables = append(f.NonConsumables, skProduct{
			AdHocOffers: []any{}, CodeOffers: []any{},
			DisplayPrice: p.DisplayPrice, FamilyShareable: false,
			InternalID:    p.InternalID,
			ProductID:     p.ProductID,
			ReferenceName: p.ReferenceName,
			Type:          "NonConsumable",
			Localizations: []skLocale{{
				DisplayName: p.DisplayName,
				Description: p.Description,
				Locale:      "en_US",
			}},
		})
	}

	// Consumables
	for _, p := range cfg.IAP.Consumables {
		f.Consumables = append(f.Consumables, skProduct{
			AdHocOffers: []any{}, CodeOffers: []any{},
			DisplayPrice: p.DisplayPrice, FamilyShareable: false,
			InternalID:    p.InternalID,
			ProductID:     p.ProductID,
			ReferenceName: p.ReferenceName,
			Type:          "Consumable",
			Localizations: []skLocale{{
				DisplayName: p.DisplayName,
				Description: p.Description,
				Locale:      "en_US",
			}},
		})
	}

	// Subscription groups
	for _, g := range cfg.IAP.SubscriptionGroups {
		group := skGroup{
			ID:            g.ID,
			Localizations: []any{},
			Name:          g.Name,
		}
		for _, s := range g.Subs {
			group.Subscriptions = append(group.Subscriptions, skSubscription{
				AdHocOffers:         []any{},
				CodeOffers:          []any{},
				DisplayPrice:        s.DisplayPrice,
				FamilyShareable:     s.FamilyShareable,
				GroupNumber:         s.GroupNumber,
				InternalID:          s.InternalID,
				IntroductoryOffer:   nil,
				ProductID:           s.ProductID,
				RecurringPeriod:     s.Period,
				ReferenceName:       s.ReferenceName,
				SubscriptionGroupID: g.ID,
				Type:                "RecurringSubscription",
				Localizations: []skLocale{{
					DisplayName: s.DisplayName,
					Description: s.Description,
					Locale:      "en_US",
				}},
			})
		}
		f.SubscriptionGroups = append(f.SubscriptionGroups, group)
	}

	if f.SubscriptionGroups == nil {
		f.SubscriptionGroups = []skGroup{}
	}

	return f
}

func hasIAP(cfg PackageConfig) bool {
	return len(cfg.IAP.SubscriptionGroups) > 0 ||
		len(cfg.IAP.NonConsumables) > 0 ||
		len(cfg.IAP.Consumables) > 0
}

// ── Signing ───────────────────────────────────────────────────────────────────

func sign(bundle, entitlements, cert string) {
	macosDir := filepath.Join(bundle, "Contents", "MacOS")

	entries, err := os.ReadDir(macosDir)
	must(err)

	// Sign each binary in MacOS/ individually first (inner → outer rule).
	for _, e := range entries {
		if e.IsDir() { continue }
		bin := filepath.Join(macosDir, e.Name())
		step("  codesign %s", e.Name())
		run("codesign", "--force", "--options", "runtime",
			"--sign", cert, bin)
	}

	// Sign the bundle itself with entitlements.
	step("  codesign %s (with entitlements)", filepath.Base(bundle))
	run("codesign",
		"--force",
		"--options", "runtime",
		"--entitlements", entitlements,
		"--sign", cert,
		bundle,
	)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func copyExec(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		die("Cannot read %q: %v", src, err)
	}
	must(os.WriteFile(dst, data, 0o755))
}

func writeFile(path, contents string) {
	must(os.WriteFile(path, []byte(contents), 0o644))
}

func writeJSON(path string, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	must(err)
	must(os.WriteFile(path, data, 0o644))
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		die("Command failed: %s %s\n%v", name, strings.Join(args, " "), err)
	}
}

func step(format string, args ...any) {
	fmt.Printf("\n▶  "+format+"\n", args...)
}

func warn(format string, args ...any) {
	fmt.Printf("⚠️   "+format+"\n", args...)
}

func must(err error) {
	if err != nil { die("%v", err) }
}

// Suppress the unused xml import — xml is used by patch_scheme.go in the
// same tools/ directory. If you keep both files in the same package, remove
// this line and the import above.
var _ = xml.Header

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\n❌  "+format+"\n\n", args...)
	os.Exit(1)
}