package storekit

import (
	"encoding/json"
	"os"
)

// IAPConfig is the internal storekit representation — decoupled from
// the public packaging.IAPConfig so the two can evolve independently.
type IAPConfig struct {
	ConfigIdentifier   string
	SubscriptionGroups []SubscriptionGroup
	NonConsumables     []SimpleProduct
	Consumables        []SimpleProduct
}

type SubscriptionGroup struct {
	ID            string
	Name          string
	Subscriptions []Subscription
}

type Subscription struct {
	InternalID      string
	ProductID       string
	ReferenceName   string
	DisplayName     string
	Description     string
	DisplayPrice    string
	Period          string
	FamilyShareable bool
	GroupNumber     int
}

type SimpleProduct struct {
	InternalID    string
	ProductID     string
	ReferenceName string
	DisplayName   string
	Description   string
	DisplayPrice  string
}

// HasProducts reports whether any products are declared.
func HasProductsIn(iap IAPConfig) bool {
	return len(iap.SubscriptionGroups) > 0 ||
		len(iap.NonConsumables) > 0 ||
		len(iap.Consumables) > 0
}

// Write serialises the StoreKit v2 JSON file to path.
func Write(path string, iap IAPConfig) error {
	f := buildFile(iap)
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ── Internal JSON shape ───────────────────────────────────────────────────────

type skFile struct {
	Identifier         string         `json:"identifier"`
	NonConsumables     []skProduct    `json:"nonConsumableProducts"`
	Consumables        []skProduct    `json:"consumableProducts"`
	SubscriptionGroups []skGroup      `json:"subscriptionGroups"`
	Settings           map[string]any `json:"settings"`
	Version            skVersion      `json:"version"`
}

type skGroup struct {
	ID            string           `json:"id"`
	Localizations []any            `json:"localizations"`
	Name          string           `json:"name"`
	Subscriptions []skSubscription `json:"subscriptions"`
}

type skSubscription struct {
	AdHocOffers         []any      `json:"adHocOffers"`
	CodeOffers          []any      `json:"codeOffers"`
	DisplayPrice        string     `json:"displayPrice"`
	FamilyShareable     bool       `json:"familyShareable"`
	GroupNumber         int        `json:"groupNumber"`
	InternalID          string     `json:"internalID"`
	IntroductoryOffer   any        `json:"introductoryOffer"`
	Localizations       []skLocale `json:"localizations"`
	ProductID           string     `json:"productID"`
	RecurringPeriod     string     `json:"recurringSubscriptionPeriod"`
	ReferenceName       string     `json:"referenceName"`
	SubscriptionGroupID string     `json:"subscriptionGroupID"`
	Type                string     `json:"type"`
}

type skProduct struct {
	AdHocOffers     []any      `json:"adHocOffers"`
	CodeOffers      []any      `json:"codeOffers"`
	DisplayPrice    string     `json:"displayPrice"`
	FamilyShareable bool       `json:"familyShareable"`
	InternalID      string     `json:"internalID"`
	Localizations   []skLocale `json:"localizations"`
	ProductID       string     `json:"productID"`
	ReferenceName   string     `json:"referenceName"`
	Type            string     `json:"type"`
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

func buildFile(iap IAPConfig) skFile {
	f := skFile{
		Identifier:         iap.ConfigIdentifier,
		NonConsumables:     []skProduct{},
		Consumables:        []skProduct{},
		SubscriptionGroups: []skGroup{},
		Settings:           map[string]any{},
		Version:            skVersion{Major: 2, Minor: 0},
	}

	for _, p := range iap.NonConsumables {
		f.NonConsumables = append(f.NonConsumables, skProduct{
			AdHocOffers: []any{}, CodeOffers: []any{},
			DisplayPrice: p.DisplayPrice, FamilyShareable: false,
			InternalID: p.InternalID, ProductID: p.ProductID,
			ReferenceName: p.ReferenceName, Type: "NonConsumable",
			Localizations: []skLocale{{DisplayName: p.DisplayName, Description: p.Description, Locale: "en_US"}},
		})
	}

	for _, p := range iap.Consumables {
		f.Consumables = append(f.Consumables, skProduct{
			AdHocOffers: []any{}, CodeOffers: []any{},
			DisplayPrice: p.DisplayPrice, FamilyShareable: false,
			InternalID: p.InternalID, ProductID: p.ProductID,
			ReferenceName: p.ReferenceName, Type: "Consumable",
			Localizations: []skLocale{{DisplayName: p.DisplayName, Description: p.Description, Locale: "en_US"}},
		})
	}

	for _, g := range iap.SubscriptionGroups {
		group := skGroup{ID: g.ID, Name: g.Name, Localizations: []any{}}
		for _, s := range g.Subscriptions {
			group.Subscriptions = append(group.Subscriptions, skSubscription{
				AdHocOffers: []any{}, CodeOffers: []any{},
				DisplayPrice: s.DisplayPrice, FamilyShareable: s.FamilyShareable,
				GroupNumber: s.GroupNumber, InternalID: s.InternalID,
				IntroductoryOffer: nil, ProductID: s.ProductID,
				RecurringPeriod: s.Period, ReferenceName: s.ReferenceName,
				SubscriptionGroupID: g.ID, Type: "RecurringSubscription",
				Localizations: []skLocale{{DisplayName: s.DisplayName, Description: s.Description, Locale: "en_US"}},
			})
		}
		f.SubscriptionGroups = append(f.SubscriptionGroups, group)
	}

	return f
}