//go:build darwin

package billing

import (
	"encoding/json"
	"sync"
	"time"
)

// AppleStore bridges the arc IPC protocol to StoreKit on Apple platforms.
// Obtain one with NewAppleStore; register callbacks before calling app.Run.
type AppleStore struct {
	app App

	mu                   sync.RWMutex
	onProductsFetched    func([]AppleProduct)
	onPurchaseCompleted  func(ApplePurchaseResult)
	onRestoreCompleted   func([]ApplePurchaseResult)
	onEntitlementsChanged func()
	onEntitlements       func([]AppleEntitlement)
	onPromotedIAP        func(string)

	// One-shot response callbacks keyed by their natural correlation ID.
	pendingMu          sync.Mutex
	pendingEntitlement map[string]func(*AppleEntitlement) // product_id → fn
	pendingRefunds     map[string]func(AppleRefundStatus)  // transaction_id → fn
}

// NewAppleStore wires up an AppleStore and registers it with app's event
// stream. Returns nil on non-Apple platforms (see apple_store_stub.go).
func NewAppleStore(app App) *AppleStore {
	s := &AppleStore{
		app:                app,
		pendingEntitlement: make(map[string]func(*AppleEntitlement)),
		pendingRefunds:     make(map[string]func(AppleRefundStatus)),
	}
	app.OnEventPrefix("apple.store.", s.handleEvent)
	return s
}

// ── Commands ──────────────────────────────────────────────────────────────────

// FetchProducts registers specs with the store and triggers a fetch.
// Results arrive in the OnProductsFetched callback.
func (s *AppleStore) FetchProducts(specs []AppleProductSpec) {
	type item struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	items := make([]item, len(specs))
	for i, sp := range specs {
		items[i] = item{ID: sp.ID, Kind: sp.Kind.String()}
	}
	s.app.Send(map[string]any{
		"type":     "apple.store.fetch_products",
		"products": items,
	})
}

// Purchase initiates a purchase for productID with optional offer details.
// The outcome arrives in OnPurchaseCompleted.
func (s *AppleStore) Purchase(productID string, opts ApplePurchaseOptions) {
	msg := map[string]any{
		"type":       "apple.store.purchase",
		"product_id": productID,
	}
	if opts.OfferID != nil {
		msg["offer_id"] = *opts.OfferID
	}
	if opts.OfferSignature != nil {
		msg["offer_signature"] = opts.OfferSignature
	}
	s.app.Send(msg)
}

// RestorePurchases asks StoreKit to restore previous transactions.
// Each result arrives in OnRestoreCompleted.
func (s *AppleStore) RestorePurchases() {
	s.app.Send(map[string]any{"type": "apple.store.restore_purchases"})
}

// CurrentEntitlements requests all current subscription entitlements.
// Results arrive in OnEntitlements.
func (s *AppleStore) CurrentEntitlements() {
	s.app.Send(map[string]any{"type": "apple.store.current_entitlements"})
}

// CheckEntitlement queries the entitlement for a single product.
// fn is called once with the entitlement, or nil if the product is not owned.
func (s *AppleStore) CheckEntitlement(productID string, fn func(*AppleEntitlement)) {
	s.pendingMu.Lock()
	s.pendingEntitlement[productID] = fn
	s.pendingMu.Unlock()
	s.app.Send(map[string]any{
		"type":       "apple.store.check_entitlement",
		"product_id": productID,
	})
}

// RequestRefund surfaces the system refund UI for transactionID.
// fn is called once with the outcome.
func (s *AppleStore) RequestRefund(transactionID string, fn func(AppleRefundStatus)) {
	s.pendingMu.Lock()
	s.pendingRefunds[transactionID] = fn
	s.pendingMu.Unlock()
	s.app.Send(map[string]any{
		"type":           "apple.store.request_refund",
		"transaction_id": transactionID,
	})
}

// ── Callback registration ─────────────────────────────────────────────────────

// OnProductsFetched registers fn to be called after FetchProducts completes.
func (s *AppleStore) OnProductsFetched(fn func([]AppleProduct)) {
	s.mu.Lock()
	s.onProductsFetched = fn
	s.mu.Unlock()
}

// OnPurchaseCompleted registers fn to be called when a purchase attempt finishes.
func (s *AppleStore) OnPurchaseCompleted(fn func(ApplePurchaseResult)) {
	s.mu.Lock()
	s.onPurchaseCompleted = fn
	s.mu.Unlock()
}

// OnRestoreCompleted registers fn to be called when RestorePurchases finishes.
func (s *AppleStore) OnRestoreCompleted(fn func([]ApplePurchaseResult)) {
	s.mu.Lock()
	s.onRestoreCompleted = fn
	s.mu.Unlock()
}

// OnEntitlementsChanged registers fn to be called whenever the entitlement set
// changes (e.g. renewal, cancellation). Call CurrentEntitlements inside fn to
// get the updated state.
func (s *AppleStore) OnEntitlementsChanged(fn func()) {
	s.mu.Lock()
	s.onEntitlementsChanged = fn
	s.mu.Unlock()
}

// OnEntitlements registers fn to be called with results from CurrentEntitlements.
func (s *AppleStore) OnEntitlements(fn func([]AppleEntitlement)) {
	s.mu.Lock()
	s.onEntitlements = fn
	s.mu.Unlock()
}

// OnPromotedIAP registers fn to be called when the user taps Buy directly on
// the App Store product page. The host always defers the purchase; call
// Purchase yourself when you are ready to proceed.
func (s *AppleStore) OnPromotedIAP(fn func(productID string)) {
	s.mu.Lock()
	s.onPromotedIAP = fn
	s.mu.Unlock()
}

// ── Event routing ─────────────────────────────────────────────────────────────

func (s *AppleStore) handleEvent(eventType string, payload []byte) {
	switch eventType {
	case "apple.store.products_fetched":
		s.handleProductsFetched(payload)
	case "apple.store.purchase_completed":
		s.handlePurchaseCompleted(payload)
	case "apple.store.restore_completed":
		s.handleRestoreCompleted(payload)
	case "apple.store.entitlements_changed":
		s.mu.RLock()
		fn := s.onEntitlementsChanged
		s.mu.RUnlock()
		if fn != nil {
			fn()
		}
	case "apple.store.entitlements":
		s.handleEntitlements(payload)
	case "apple.store.entitlement":
		s.handleEntitlement(payload)
	case "apple.store.refund_status":
		s.handleRefundStatus(payload)
	case "apple.store.promoted_iap":
		s.handlePromotedIAP(payload)
	}
}

func (s *AppleStore) handleProductsFetched(payload []byte) {
	var ev struct {
		Products []rawProduct `json:"products"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	products := make([]AppleProduct, len(ev.Products))
	for i, r := range ev.Products {
		products[i] = parseProduct(r)
	}
	s.mu.RLock()
	fn := s.onProductsFetched
	s.mu.RUnlock()
	if fn != nil {
		fn(products)
	}
}

func (s *AppleStore) handlePurchaseCompleted(payload []byte) {
	var ev struct {
		ProductID   string          `json:"product_id"`
		Transaction *rawTransaction `json:"transaction"`
		Error       *string         `json:"error"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	r := ApplePurchaseResult{ProductID: ev.ProductID}
	if ev.Transaction != nil {
		t := parseTransaction(*ev.Transaction)
		r.Transaction = &t
	}
	if ev.Error != nil {
		e := ApplePurchaseError(*ev.Error)
		r.Error = &e
	}
	s.mu.RLock()
	fn := s.onPurchaseCompleted
	s.mu.RUnlock()
	if fn != nil {
		fn(r)
	}
}

func (s *AppleStore) handleRestoreCompleted(payload []byte) {
	var ev struct {
		Results []struct {
			ProductID   string          `json:"product_id"`
			Transaction *rawTransaction `json:"transaction"`
			Error       *string         `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	results := make([]ApplePurchaseResult, len(ev.Results))
	for i, item := range ev.Results {
		r := ApplePurchaseResult{ProductID: item.ProductID}
		if item.Transaction != nil {
			t := parseTransaction(*item.Transaction)
			r.Transaction = &t
		}
		if item.Error != nil {
			e := ApplePurchaseError(*item.Error)
			r.Error = &e
		}
		results[i] = r
	}
	s.mu.RLock()
	fn := s.onRestoreCompleted
	s.mu.RUnlock()
	if fn != nil {
		fn(results)
	}
}

func (s *AppleStore) handleEntitlements(payload []byte) {
	var ev struct {
		Entitlements []rawEntitlement `json:"entitlements"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	ents := make([]AppleEntitlement, len(ev.Entitlements))
	for i, r := range ev.Entitlements {
		ents[i] = parseEntitlement(r)
	}
	s.mu.RLock()
	fn := s.onEntitlements
	s.mu.RUnlock()
	if fn != nil {
		fn(ents)
	}
}

func (s *AppleStore) handleEntitlement(payload []byte) {
	var ev struct {
		ProductID   string           `json:"product_id"`
		Entitlement *rawEntitlement  `json:"entitlement"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	s.pendingMu.Lock()
	fn := s.pendingEntitlement[ev.ProductID]
	delete(s.pendingEntitlement, ev.ProductID)
	s.pendingMu.Unlock()
	if fn == nil {
		return
	}
	if ev.Entitlement != nil {
		e := parseEntitlement(*ev.Entitlement)
		fn(&e)
	} else {
		fn(nil)
	}
}

func (s *AppleStore) handleRefundStatus(payload []byte) {
	var ev struct {
		TransactionID string `json:"transaction_id"`
		Status        string `json:"status"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	s.pendingMu.Lock()
	fn := s.pendingRefunds[ev.TransactionID]
	delete(s.pendingRefunds, ev.TransactionID)
	s.pendingMu.Unlock()
	if fn != nil {
		fn(AppleRefundStatus(ev.Status))
	}
}

func (s *AppleStore) handlePromotedIAP(payload []byte) {
	var ev struct {
		ProductID string `json:"product_id"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	s.mu.RLock()
	fn := s.onPromotedIAP
	s.mu.RUnlock()
	if fn != nil {
		fn(ev.ProductID)
	}
}

// ── JSON parsing helpers ──────────────────────────────────────────────────────

type rawPeriod struct {
	Value int    `json:"value"`
	Unit  string `json:"unit"`
}

type rawOffer struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	PaymentMode  string    `json:"payment_mode"`
	DisplayPrice string    `json:"display_price"`
	Period       rawPeriod `json:"period"`
}

type rawTransaction struct {
	ID            string    `json:"id"`
	OriginalID    string    `json:"original_id"`
	ProductID     string    `json:"product_id"`
	ProductKind   string    `json:"product_kind"`
	PurchasedAt   int64     `json:"purchased_at"`
	ExpiresAt     *int64    `json:"expires_at"`
	RevokedAt     *int64    `json:"revoked_at"`
	FamilyShared  bool      `json:"family_shared"`
	Upgraded      bool      `json:"upgraded"`
	RedeemedOffer *rawOffer `json:"redeemed_offer"`
}

type rawEntitlement struct {
	ProductID        string         `json:"product_id"`
	State            string         `json:"state"`
	Transaction      rawTransaction `json:"transaction"`
	ExpiresAt        *int64         `json:"expires_at"`
	WillAutoRenew    bool           `json:"will_auto_renew"`
	RenewalProductID *string        `json:"renewal_product_id"`
	ExpirationReason *string        `json:"expiration_reason"`
}

type rawProduct struct {
	ID                 string     `json:"id"`
	Title              string     `json:"title"`
	Description        string     `json:"description"`
	DisplayPrice       string     `json:"display_price"`
	Kind               string     `json:"kind"`
	SubscriptionPeriod *rawPeriod `json:"subscription_period"`
	IntroductoryOffer  *rawOffer  `json:"introductory_offer"`
	PromotionalOffers  []rawOffer `json:"promotional_offers"`
	WinBackOffers      []rawOffer `json:"win_back_offers"`
}

func unixMsToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}

func parsePeriod(r rawPeriod) AppleSubscriptionPeriod {
	return AppleSubscriptionPeriod{Value: r.Value, Unit: ApplePeriodUnit(r.Unit)}
}

func parseOffer(r rawOffer) AppleOffer {
	return AppleOffer{
		ID:           r.ID,
		Type:         AppleOfferType(r.Type),
		PaymentMode:  ApplePaymentMode(r.PaymentMode),
		DisplayPrice: r.DisplayPrice,
		Period:       parsePeriod(r.Period),
	}
}

func parseTransaction(r rawTransaction) AppleTransaction {
	t := AppleTransaction{
		ID:           r.ID,
		OriginalID:   r.OriginalID,
		ProductID:    r.ProductID,
		ProductKind:  AppleProductKind(r.ProductKind),
		PurchasedAt:  unixMsToTime(r.PurchasedAt),
		FamilyShared: r.FamilyShared,
		Upgraded:     r.Upgraded,
	}
	if r.ExpiresAt != nil {
		ts := unixMsToTime(*r.ExpiresAt)
		t.ExpiresAt = &ts
	}
	if r.RevokedAt != nil {
		ts := unixMsToTime(*r.RevokedAt)
		t.RevokedAt = &ts
	}
	if r.RedeemedOffer != nil {
		o := parseOffer(*r.RedeemedOffer)
		t.RedeemedOffer = &o
	}
	return t
}

func parseEntitlement(r rawEntitlement) AppleEntitlement {
	e := AppleEntitlement{
		ProductID:        r.ProductID,
		State:            AppleSubscriptionState(r.State),
		Transaction:      parseTransaction(r.Transaction),
		WillAutoRenew:    r.WillAutoRenew,
		RenewalProductID: r.RenewalProductID,
	}
	if r.ExpiresAt != nil {
		ts := unixMsToTime(*r.ExpiresAt)
		e.ExpiresAt = &ts
	}
	if r.ExpirationReason != nil {
		reason := AppleExpirationReason(*r.ExpirationReason)
		e.ExpirationReason = &reason
	}
	return e
}

func parseProduct(r rawProduct) AppleProduct {
	p := AppleProduct{
		ID:           r.ID,
		Title:        r.Title,
		Description:  r.Description,
		DisplayPrice: r.DisplayPrice,
		Kind:         AppleProductKind(r.Kind),
	}
	if r.SubscriptionPeriod != nil {
		period := parsePeriod(*r.SubscriptionPeriod)
		p.SubscriptionPeriod = &period
	}
	if r.IntroductoryOffer != nil {
		offer := parseOffer(*r.IntroductoryOffer)
		p.IntroductoryOffer = &offer
	}
	for _, o := range r.PromotionalOffers {
		p.PromotionalOffers = append(p.PromotionalOffers, parseOffer(o))
	}
	for _, o := range r.WinBackOffers {
		p.WinBackOffers = append(p.WinBackOffers, parseOffer(o))
	}
	return p
}