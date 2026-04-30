package billing

import (
	"errors"
	"log"
	"sync"

	"github.com/carbon-os/arc/internal/runtime"
)

// ProductKind describes the purchase type.
type ProductKind uint8

const (
	Subscription ProductKind = iota
	OneTime
)

// Product is a product declaration passed to NewBilling.
type Product struct {
	ID   string
	Kind ProductKind
}

// Config holds the product declarations for a Billing handle.
type Config struct {
	Products []Product
}

// ProductInfo is the live metadata returned by the store after fetch.
// Always use FormattedPrice for display — never hardcode prices.
type ProductInfo struct {
	ID             string
	Title          string
	Description    string
	FormattedPrice string
	Kind           ProductKind
}

// PurchaseStatus is the outcome of a purchase or restore attempt.
type PurchaseStatus uint8

const (
	Purchased PurchaseStatus = iota
	Restored
	Deferred  // Apple Ask to Buy — approved by family organiser later. Do not unlock yet.
	Cancelled
	Failed
)

// PurchaseEvent is delivered to the OnPurchase callback for every
// state transition in the purchase lifecycle.
type PurchaseEvent struct {
	ProductID string
	Status    PurchaseStatus
	Err       error // non-nil only when Status == Failed
}

// Billing is the in-app purchase handle for a single BrowserWindow.
// Obtain via win.NewBilling — do not construct directly.
type Billing struct {
	rt  *runtime.Runtime
	log *log.Logger

	mu         sync.RWMutex
	active     map[string]bool
	onProducts func([]ProductInfo)
	onPurchase func(PurchaseEvent)
}

// New creates a Billing handle, registers event callbacks on the runtime,
// and sends CmdBillingInit to configure the renderer's native store.
// Called by win.NewBilling — do not call directly.
func New(rt *runtime.Runtime, cfg Config, logging bool) (*Billing, error) {
	if len(cfg.Products) == 0 {
		return nil, errors.New("billing: Config.Products must not be empty")
	}

	b := &Billing{
		rt:     rt,
		log:    runtime.NewLogger(logging),
		active: make(map[string]bool),
	}

	rt.OnBillingProducts(func(raw []runtime.BillingProduct) {
		b.log.Printf("[go] billing: evtBillingProducts count=%d", len(raw))

		products := make([]ProductInfo, len(raw))
		for i, p := range raw {
			products[i] = ProductInfo{
				ID:             p.ID,
				Title:          p.Title,
				Description:    p.Description,
				FormattedPrice: p.FormattedPrice,
				Kind:           ProductKind(p.Kind),
			}
		}

		b.mu.RLock()
		cb := b.onProducts
		b.mu.RUnlock()
		if cb != nil {
			cb(products)
		}
	})

	rt.OnBillingPurchase(func(raw runtime.BillingPurchaseEvent) {
		b.log.Printf("[go] billing: evtBillingPurchase product=%q status=%d",
			raw.ProductID, raw.Status)

		status := PurchaseStatus(raw.Status)

		// Update local entitlement cache on success.
		if status == Purchased || status == Restored {
			b.mu.Lock()
			b.active[raw.ProductID] = true
			b.mu.Unlock()
		}

		var err error
		if status == Failed && raw.ErrorMsg != "" {
			err = errors.New(raw.ErrorMsg)
		}

		b.mu.RLock()
		cb := b.onPurchase
		b.mu.RUnlock()
		if cb != nil {
			cb(PurchaseEvent{
				ProductID: raw.ProductID,
				Status:    status,
				Err:       err,
			})
		}
	})

	// Declare products to the renderer — triggers SKProductsRequest on Apple,
	// equivalent store fetch on other platforms.
	decls := make([]runtime.BillingProductDecl, len(cfg.Products))
	for i, p := range cfg.Products {
		decls[i] = runtime.BillingProductDecl{
			ID:   p.ID,
			Kind: uint8(p.Kind),
		}
	}
	rt.Send(runtime.CmdBillingInit, runtime.EncodeBillingInit(decls))
	b.log.Printf("[go] billing: CmdBillingInit sent count=%d", len(decls))

	return b, nil
}

// OnProducts registers a callback that fires once the store returns live
// product metadata. Use this to populate your pricing UI.
func (b *Billing) OnProducts(fn func([]ProductInfo)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onProducts = fn
}

// OnPurchase registers a callback for the full purchase lifecycle.
// Replaces any previously registered handler.
func (b *Billing) OnPurchase(fn func(PurchaseEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onPurchase = fn
}

// Buy initiates a purchase for the given product ID.
// The result is delivered asynchronously via OnPurchase.
func (b *Billing) Buy(productID string) {
	b.log.Printf("[go] billing: Buy product=%q", productID)
	b.rt.Send(runtime.CmdBillingBuy, runtime.EncodeStr(productID))
}

// Restore triggers a restore of completed transactions.
// Required by App Store guidelines. Results arrive via OnPurchase.
func (b *Billing) Restore() {
	b.log.Println("[go] billing: Restore")
	b.rt.Send(runtime.CmdBillingRestore, nil)
}

// IsActive reports whether the given product ID is currently active.
// Based on in-memory state — updated when purchases or restores succeed.
// For server-side receipt validation, verify independently.
func (b *Billing) IsActive(productID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.active[productID]
}