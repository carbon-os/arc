package packaging

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/carbon-os/arc/packaging/internal/archive"
	"github.com/carbon-os/arc/packaging/internal/plist"
	"github.com/carbon-os/arc/packaging/internal/sign"
	"github.com/carbon-os/arc/packaging/internal/storekit"
)

func buildMacOS(cfg PackagingConfig, appName string, opts BuildOptions) error {
	mac := cfg.MacOS

	if mac.BundleID == "" {
		return fmt.Errorf("MacOSPackage.BundleID is required")
	}
	if mac.Version == "" {
		mac.Version = "1.0.0"
	}
	if mac.Build == "" {
		mac.Build = "1"
	}
	if mac.MinMacOS == "" {
		mac.MinMacOS = "13.0"
	}

	bundleName := appName + ".app"
	bundlePath := filepath.Join(cfg.OutDir, bundleName)
	macosDir   := filepath.Join(bundlePath, "Contents", "MacOS")
	resDir     := filepath.Join(bundlePath, "Contents", "Resources")

	// ── Scaffold ──────────────────────────────────────────────────────────
	step("  cleaning previous build")
	if err := os.RemoveAll(bundlePath); err != nil {
		return err
	}
	for _, d := range []string{macosDir, resDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	// ── Binaries ──────────────────────────────────────────────────────────
	execName := executableName(appName)

	step("  copying Go binary → MacOS/%s", execName)
	if err := archive.CopyExec(cfg.BinaryPath, filepath.Join(macosDir, execName)); err != nil {
		return fmt.Errorf("copy binary: %w", err)
	}

	rendererSrc, err := resolveRenderer(cfg.RendererBuildDir)
	if err != nil {
		return err
	}
	step("  copying renderer → MacOS/renderer")
	if err := archive.CopyExec(rendererSrc, filepath.Join(macosDir, "renderer")); err != nil {
		return fmt.Errorf("copy renderer: %w", err)
	}

	// ── Info.plist ────────────────────────────────────────────────────────
	step("  generating Info.plist")
	infoPlist := plist.RenderInfo(plist.InfoData{
		BundleID:   mac.BundleID,
		AppName:    appName,
		Executable: execName,
		Version:    mac.Version,
		Build:      mac.Build,
		MinMacOS:   mac.MinMacOS,
	})
	if err := os.WriteFile(filepath.Join(bundlePath, "Contents", "Info.plist"), []byte(infoPlist), 0o644); err != nil {
		return err
	}

	// ── Entitlements ──────────────────────────────────────────────────────
	entPath := filepath.Join(cfg.OutDir, appName+".entitlements")
	step("  generating entitlements → %s", filepath.Base(entPath))
	entData := plist.RenderEntitlements(plist.EntitlementsData{
		TeamID:   mac.TeamID,
		BundleID: mac.BundleID,
	})
	if err := os.WriteFile(entPath, []byte(entData), 0o644); err != nil {
		return err
	}

	// ── StoreKit ──────────────────────────────────────────────────────────
	if mac.IAP != nil && storekit.HasProducts(mac.IAP) {
		skPath := filepath.Join(cfg.OutDir, appName+".storekit")
		step("  generating .storekit → %s", filepath.Base(skPath))
		if err := storekit.Write(skPath, toStorekitIAP(mac.IAP)); err != nil {
			return err
		}
	}

	// ── Signing ───────────────────────────────────────────────────────────
	if opts.SkipSign {
		fmt.Println("  ⚠️   skipping codesign (--skip-sign)")
	} else {
		if mac.SignCert == "" {
			return fmt.Errorf("MacOSPackage.SignCert is required for signing (pass --skip-sign to skip)")
		}
		step("  signing bundle")
		if err := sign.MacOSBundle(bundlePath, entPath, mac.SignCert); err != nil {
			return err
		}
	}

	fmt.Printf("  📦  %s\n", bundlePath)
	return nil
}

// toStorekitIAP converts packaging.IAPConfig → storekit.IAPConfig.
func toStorekitIAP(iap *IAPConfig) storekit.IAPConfig {
	out := storekit.IAPConfig{
		ConfigIdentifier: iap.ConfigIdentifier,
	}
	for _, g := range iap.SubscriptionGroups {
		sg := storekit.SubscriptionGroup{ID: g.ID, Name: g.Name}
		for _, s := range g.Subscriptions {
			sg.Subscriptions = append(sg.Subscriptions, storekit.Subscription{
				InternalID:      s.InternalID,
				ProductID:       s.ProductID,
				ReferenceName:   s.ReferenceName,
				DisplayName:     s.DisplayName,
				Description:     s.Description,
				DisplayPrice:    s.DisplayPrice,
				Period:          s.Period,
				FamilyShareable: s.FamilyShareable,
				GroupNumber:     s.GroupNumber,
			})
		}
		out.SubscriptionGroups = append(out.SubscriptionGroups, sg)
	}
	for _, p := range iap.NonConsumables {
		out.NonConsumables = append(out.NonConsumables, storekit.SimpleProduct{
			InternalID: p.InternalID, ProductID: p.ProductID,
			ReferenceName: p.ReferenceName, DisplayName: p.DisplayName,
			Description: p.Description, DisplayPrice: p.DisplayPrice,
		})
	}
	for _, p := range iap.Consumables {
		out.Consumables = append(out.Consumables, storekit.SimpleProduct{
			InternalID: p.InternalID, ProductID: p.ProductID,
			ReferenceName: p.ReferenceName, DisplayName: p.DisplayName,
			Description: p.Description, DisplayPrice: p.DisplayPrice,
		})
	}
	return out
}