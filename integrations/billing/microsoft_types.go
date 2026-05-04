package billing

// ── Enumerations ──────────────────────────────────────────────────────────────

// MicrosoftProductKind is the Microsoft Store product category.
type MicrosoftProductKind string

const (
	MicrosoftKindApp                 MicrosoftProductKind = "app"
	MicrosoftKindGame                MicrosoftProductKind = "game"
	MicrosoftKindConsumable          MicrosoftProductKind = "consumable"
	MicrosoftKindUnmanagedConsumable MicrosoftProductKind = "unmanaged_consumable"
	MicrosoftKindDurable             MicrosoftProductKind = "durable"
)

// MicrosoftPurchaseStatus is the outcome of a Microsoft Store purchase attempt.
type MicrosoftPurchaseStatus string

const (
	MicrosoftPurchaseSucceeded        MicrosoftPurchaseStatus = "succeeded"
	MicrosoftPurchaseAlreadyPurchased MicrosoftPurchaseStatus = "already_purchased"
	MicrosoftPurchaseNotPurchased     MicrosoftPurchaseStatus = "not_purchased"
	MicrosoftPurchaseNetworkError     MicrosoftPurchaseStatus = "network_error"
	MicrosoftPurchaseServerError      MicrosoftPurchaseStatus = "server_error"
	MicrosoftPurchaseUnknown          MicrosoftPurchaseStatus = "unknown"
)

// MicrosoftConsumeStatus is the outcome of a consumable fulfillment report.
type MicrosoftConsumeStatus string

const (
	MicrosoftConsumeSucceeded           MicrosoftConsumeStatus = "succeeded"
	MicrosoftConsumeInsufficientQuantity MicrosoftConsumeStatus = "insufficient_quantity"
	MicrosoftConsumeNetworkError        MicrosoftConsumeStatus = "network_error"
	MicrosoftConsumeServerError         MicrosoftConsumeStatus = "server_error"
	MicrosoftConsumeUnknown             MicrosoftConsumeStatus = "unknown"
)

// ── Structs ───────────────────────────────────────────────────────────────────

// MicrosoftProduct is a product returned by the Microsoft Store.
type MicrosoftProduct struct {
	ID           string
	Title        string
	Description  string
	DisplayPrice string
	Kind         MicrosoftProductKind
	IsOwned      bool
}

// MicrosoftPurchaseResult is delivered to OnPurchaseCompleted.
type MicrosoftPurchaseResult struct {
	ProductID string
	Status    MicrosoftPurchaseStatus
	// Error contains extended error detail when Status indicates failure.
	// Nil on success.
	Error *string
}

// MicrosoftConsumeResult is delivered to the callback passed to ReportConsumable.
type MicrosoftConsumeResult struct {
	ProductID  string
	TrackingID string
	Status     MicrosoftConsumeStatus
}