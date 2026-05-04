//go:build windows

package billing

import (
	"encoding/json"
	"sync"
)

// MicrosoftStore bridges the arc IPC protocol to the Windows.Services.Store
// API on Windows. Obtain one with NewMicrosoftStore; register callbacks before
// calling app.Run.
type MicrosoftStore struct {
	app App

	mu                  sync.RWMutex
	onProductsFetched   func([]MicrosoftProduct)
	onPurchaseCompleted func(MicrosoftPurchaseResult)
	onOwned             func([]string)
	onEntitlement       func(productID string, owned bool)

	// One-shot response callbacks.
	pendingMu      sync.Mutex
	pendingConsume map[string]func(MicrosoftConsumeResult) // tracking_id → fn
}

// NewMicrosoftStore wires up a MicrosoftStore and registers it with app's
// event stream. Returns nil on non-Windows platforms (see stub file).
func NewMicrosoftStore(app App) *MicrosoftStore {
	s := &MicrosoftStore{
		app:            app,
		pendingConsume: make(map[string]func(MicrosoftConsumeResult)),
	}
	app.OnEventPrefix("microsoft.store.", s.handleEvent)
	return s
}

// ── Commands ──────────────────────────────────────────────────────────────────

// FetchProducts requests product metadata for the given store IDs.
// Results arrive in OnProductsFetched.
func (s *MicrosoftStore) FetchProducts(productIDs []string) {
	s.app.Send(map[string]any{
		"type":        "microsoft.store.fetch_products",
		"product_ids": productIDs,
	})
}

// Purchase initiates a purchase flow for productID.
// The outcome arrives in OnPurchaseCompleted.
func (s *MicrosoftStore) Purchase(productID string) {
	s.app.Send(map[string]any{
		"type":       "microsoft.store.purchase",
		"product_id": productID,
	})
}

// GetOwned requests all owned store product IDs.
// Results arrive in OnOwned.
func (s *MicrosoftStore) GetOwned() {
	s.app.Send(map[string]any{"type": "microsoft.store.get_owned"})
}

// CheckEntitlement checks whether a single product is owned.
// fn is called once with the result.
// Note: the Microsoft Store has no single-product entitlement query;
// this fetches all owned IDs and checks membership internally.
func (s *MicrosoftStore) CheckEntitlement(productID string, fn func(owned bool)) {
	// Wrap in the OnEntitlement callback for this specific call only, then
	// restore. Because we set onEntitlement directly this is simple but means
	// concurrent CheckEntitlement calls for different products will race.
	// For the common single-product case this is fine; callers needing
	// concurrent checks should use GetOwned + OnOwned instead.
	s.mu.Lock()
	prev := s.onEntitlement
	s.onEntitlement = func(pid string, owned bool) {
		if pid == productID {
			s.mu.Lock()
			s.onEntitlement = prev
			s.mu.Unlock()
			fn(owned)
		}
	}
	s.mu.Unlock()
	s.app.Send(map[string]any{
		"type":       "microsoft.store.check_entitlement",
		"product_id": productID,
	})
}

// ReportConsumable tells the Microsoft Store that quantity units of a
// consumable have been granted to the user. trackingID must be a unique
// string (e.g. a UUID) per fulfillment; the Store uses it for idempotency.
// fn is called once with the outcome.
func (s *MicrosoftStore) ReportConsumable(productID string, quantity uint32, trackingID string, fn func(MicrosoftConsumeResult)) {
	s.pendingMu.Lock()
	s.pendingConsume[trackingID] = fn
	s.pendingMu.Unlock()
	s.app.Send(map[string]any{
		"type":        "microsoft.store.report_consumable",
		"product_id":  productID,
		"quantity":    quantity,
		"tracking_id": trackingID,
	})
}

// ── Callback registration ─────────────────────────────────────────────────────

// OnProductsFetched registers fn to be called after FetchProducts completes.
func (s *MicrosoftStore) OnProductsFetched(fn func([]MicrosoftProduct)) {
	s.mu.Lock()
	s.onProductsFetched = fn
	s.mu.Unlock()
}

// OnPurchaseCompleted registers fn to be called when a purchase attempt finishes.
func (s *MicrosoftStore) OnPurchaseCompleted(fn func(MicrosoftPurchaseResult)) {
	s.mu.Lock()
	s.onPurchaseCompleted = fn
	s.mu.Unlock()
}

// OnOwned registers fn to be called with the full list of owned product IDs
// after GetOwned completes.
func (s *MicrosoftStore) OnOwned(fn func(productIDs []string)) {
	s.mu.Lock()
	s.onOwned = fn
	s.mu.Unlock()
}

// OnEntitlement registers fn to be called with the entitlement check result.
// For ad-hoc one-shot checks prefer CheckEntitlement directly.
func (s *MicrosoftStore) OnEntitlement(fn func(productID string, owned bool)) {
	s.mu.Lock()
	s.onEntitlement = fn
	s.mu.Unlock()
}

// ── Event routing ─────────────────────────────────────────────────────────────

func (s *MicrosoftStore) handleEvent(eventType string, payload []byte) {
	switch eventType {
	case "microsoft.store.products_fetched":
		s.handleProductsFetched(payload)
	case "microsoft.store.purchase_completed":
		s.handlePurchaseCompleted(payload)
	case "microsoft.store.owned":
		s.handleOwned(payload)
	case "microsoft.store.entitlement":
		s.handleEntitlement(payload)
	case "microsoft.store.consumable_fulfilled":
		s.handleConsumableFulfilled(payload)
	}
}

func (s *MicrosoftStore) handleProductsFetched(payload []byte) {
	var ev struct {
		Products []struct {
			ID           string `json:"id"`
			Title        string `json:"title"`
			Description  string `json:"description"`
			DisplayPrice string `json:"display_price"`
			Kind         string `json:"kind"`
			IsOwned      bool   `json:"is_owned"`
		} `json:"products"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	products := make([]MicrosoftProduct, len(ev.Products))
	for i, p := range ev.Products {
		products[i] = MicrosoftProduct{
			ID:           p.ID,
			Title:        p.Title,
			Description:  p.Description,
			DisplayPrice: p.DisplayPrice,
			Kind:         MicrosoftProductKind(p.Kind),
			IsOwned:      p.IsOwned,
		}
	}
	s.mu.RLock()
	fn := s.onProductsFetched
	s.mu.RUnlock()
	if fn != nil {
		fn(products)
	}
}

func (s *MicrosoftStore) handlePurchaseCompleted(payload []byte) {
	var ev struct {
		ProductID string  `json:"product_id"`
		Status    string  `json:"status"`
		Error     *string `json:"error"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	r := MicrosoftPurchaseResult{
		ProductID: ev.ProductID,
		Status:    MicrosoftPurchaseStatus(ev.Status),
		Error:     ev.Error,
	}
	s.mu.RLock()
	fn := s.onPurchaseCompleted
	s.mu.RUnlock()
	if fn != nil {
		fn(r)
	}
}

func (s *MicrosoftStore) handleOwned(payload []byte) {
	var ev struct {
		ProductIDs []string `json:"product_ids"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	s.mu.RLock()
	fn := s.onOwned
	s.mu.RUnlock()
	if fn != nil {
		fn(ev.ProductIDs)
	}
}

func (s *MicrosoftStore) handleEntitlement(payload []byte) {
	var ev struct {
		ProductID string `json:"product_id"`
		Owned     bool   `json:"owned"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	s.mu.RLock()
	fn := s.onEntitlement
	s.mu.RUnlock()
	if fn != nil {
		fn(ev.ProductID, ev.Owned)
	}
}

func (s *MicrosoftStore) handleConsumableFulfilled(payload []byte) {
	var ev struct {
		ProductID  string `json:"product_id"`
		TrackingID string `json:"tracking_id"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	s.pendingMu.Lock()
	fn := s.pendingConsume[ev.TrackingID]
	delete(s.pendingConsume, ev.TrackingID)
	s.pendingMu.Unlock()
	if fn != nil {
		fn(MicrosoftConsumeResult{
			ProductID:  ev.ProductID,
			TrackingID: ev.TrackingID,
			Status:     MicrosoftConsumeStatus(ev.Status),
		})
	}
}