package cli

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ── StoreKit file types (mirrors Apple's .storekit JSON schema) ───────────────

type storeKitFile struct {
	Identifier         string              `json:"identifier"`
	NonConsumable      []any               `json:"nonConsumableProducts"`
	Consumable         []any               `json:"consumableProducts"`
	SubscriptionGroups []skSubscriptionGroup `json:"subscriptionGroups"`
	Settings           map[string]any      `json:"settings"`
	Version            skVersion           `json:"version"`
}

type skSubscriptionGroup struct {
	ID            string           `json:"id"`
	Localizations []any            `json:"localizations"`
	Name          string           `json:"name"`
	Subscriptions []skSubscription `json:"subscriptions"`
}

type skSubscription struct {
	AdHocOffers         []any            `json:"adHocOffers"`
	CodeOffers          []any            `json:"codeOffers"`
	DisplayPrice        string           `json:"displayPrice"`
	FamilyShareable     bool             `json:"familyShareable"`
	GroupNumber         int              `json:"groupNumber"`
	InternalID          string           `json:"internalID"`
	IntroductoryOffer   any              `json:"introductoryOffer"`
	Localizations       []skLocalization `json:"localizations"`
	ProductID           string           `json:"productID"`
	RecurringPeriod     string           `json:"recurringSubscriptionPeriod"`
	ReferenceName       string           `json:"referenceName"`
	SubscriptionGroupID string           `json:"subscriptionGroupID"`
	Type                string           `json:"type"`
}

type skLocalization struct {
	Description string `json:"description"`
	DisplayName string `json:"displayName"`
	Locale      string `json:"locale"`
}

type skVersion struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
}

// GenerateStoreKit converts a BillingConfig from arc.json into a .storekit
// file written to projectDir/<binaryName>.storekit and returns its path.
func GenerateStoreKit(billing *BillingConfig, binaryName, projectDir string) (string, error) {
	sk := storeKitFile{
		Identifier:    billing.Identifier,
		NonConsumable: []any{},
		Consumable:    []any{},
		Settings:      map[string]any{},
		Version:       skVersion{Major: 2, Minor: 0},
	}

	for _, g := range billing.SubscriptionGroups {
		group := skSubscriptionGroup{
			ID:            g.ID,
			Name:          g.Name,
			Localizations: []any{},
		}
		for _, s := range g.Subscriptions {
			sub := skSubscription{
				AdHocOffers:         []any{},
				CodeOffers:          []any{},
				DisplayPrice:        s.DisplayPrice,
				FamilyShareable:     s.FamilyShareable,
				GroupNumber:         s.GroupNumber,
				InternalID:          s.InternalID,
				IntroductoryOffer:   nil,
				ProductID:           s.ProductID,
				RecurringPeriod:     s.RecurringPeriod,
				ReferenceName:       s.ReferenceName,
				SubscriptionGroupID: g.ID,
				Type:                "RecurringSubscription",
			}
			for _, l := range s.Localizations {
				sub.Localizations = append(sub.Localizations, skLocalization{
					Description: l.Description,
					DisplayName: l.DisplayName,
					Locale:      l.Locale,
				})
			}
			group.Subscriptions = append(group.Subscriptions, sub)
		}
		sk.SubscriptionGroups = append(sk.SubscriptionGroups, group)
	}

	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal storekit: %w", err)
	}

	outPath := filepath.Join(projectDir, binaryName+".storekit")
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return "", fmt.Errorf("write storekit: %w", err)
	}
	return outPath, nil
}

// PatchXcodeScheme injects a StoreKitConfigurationFileReference into the
// LaunchAction of the Xcode scheme at schemePath, pointing at storeKitPath
// (expressed as a path relative to the scheme file).
func PatchXcodeScheme(schemePath, storeKitPath string) error {
	data, err := os.ReadFile(schemePath)
	if err != nil {
		return fmt.Errorf("read scheme (did cmake -G Xcode run?): %w", err)
	}

	// Make storeKitPath relative to the directory containing the scheme.
	schemeDir := filepath.Dir(schemePath)
	relPath, err := filepath.Rel(schemeDir, storeKitPath)
	if err != nil {
		return fmt.Errorf("relativize storekit path: %w", err)
	}

	dec := xml.NewDecoder(bytes.NewReader(data))
	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "   ")

	patched := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parse scheme XML: %w", err)
		}

		// Drop the XML declaration — xml.Encoder re-adds it via xml.Header.
		if pi, ok := tok.(xml.ProcInst); ok && pi.Target == "xml" {
			continue
		}

		// If already patched to our path, bail early.
		if el, ok := tok.(xml.StartElement); ok && el.Name.Local == "StoreKitConfigurationFileReference" {
			for _, attr := range el.Attr {
				if attr.Name.Local == "identifier" && attr.Value == relPath {
					fmt.Println("   storekit reference already present — skipping patch")
					return nil
				}
			}
		}

		if el, ok := tok.(xml.StartElement); ok && el.Name.Local == "LaunchAction" {
			if err := enc.EncodeToken(tok); err != nil {
				return fmt.Errorf("encode LaunchAction: %w", err)
			}
			child := xml.StartElement{
				Name: xml.Name{Local: "StoreKitConfigurationFileReference"},
				Attr: []xml.Attr{
					{Name: xml.Name{Local: "identifier"}, Value: relPath},
				},
			}
			if err := enc.EncodeToken(child); err != nil {
				return err
			}
			if err := enc.EncodeToken(child.End()); err != nil {
				return err
			}
			patched = true
			continue
		}

		if err := enc.EncodeToken(tok); err != nil {
			return fmt.Errorf("encode token: %w", err)
		}
	}

	if err := enc.Flush(); err != nil {
		return fmt.Errorf("flush encoder: %w", err)
	}
	if !patched {
		return fmt.Errorf("could not find <LaunchAction> in scheme — is this an Xcode scheme?")
	}

	return os.WriteFile(schemePath, []byte(xml.Header+buf.String()), 0o644)
}