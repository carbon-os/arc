package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

// ArcConfig is the schema for arc.json placed alongside the user's app.
type ArcConfig struct {
	App     *AppConfig     `json:"app,omitempty"`
	Billing *BillingConfig `json:"billing,omitempty"`
}

type AppConfig struct {
	Name     string `json:"name"`
	BundleID string `json:"bundle_id"`
}

// BillingConfig maps directly to a StoreKit configuration file.
type BillingConfig struct {
	// Identifier is the top-level 8-char hex identifier for the .storekit file.
	Identifier         string                   `json:"identifier"`
	SubscriptionGroups []SubscriptionGroupConfig `json:"subscription_groups"`
}

type SubscriptionGroupConfig struct {
	ID            string               `json:"id"`   // 8-char hex
	Name          string               `json:"name"`
	Subscriptions []SubscriptionConfig `json:"subscriptions"`
}

type SubscriptionConfig struct {
	ProductID       string                 `json:"product_id"`
	ReferenceName   string                 `json:"reference_name"`
	DisplayPrice    string                 `json:"display_price"`
	RecurringPeriod string                 `json:"recurring_period"` // e.g. "P1M", "P1Y"
	FamilyShareable bool                   `json:"family_shareable"`
	GroupNumber     int                    `json:"group_number"`
	InternalID      string                 `json:"internal_id"` // 8-char hex
	Localizations   []LocalizationConfig   `json:"localizations"`
}

type LocalizationConfig struct {
	Locale      string `json:"locale"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// LoadArcConfig reads and parses an arc.json file.
func LoadArcConfig(path string) (*ArcConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read arc.json: %w", err)
	}
	var cfg ArcConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse arc.json: %w", err)
	}
	return &cfg, nil
}