package billing

import "time"

// ── Enumerations ──────────────────────────────────────────────────────────────

// AppleProductKind is the StoreKit billing category of a product.
type AppleProductKind string

const (
	AppleKindConsumable    AppleProductKind = "consumable"
	AppleKindNonConsumable AppleProductKind = "non_consumable"
	AppleKindAutoRenewable AppleProductKind = "auto_renewable_subscription"
	AppleKindNonRenewing   AppleProductKind = "non_renewing_subscription"
)

func (k AppleProductKind) String() string { return string(k) }

// ApplePeriodUnit is the time unit of a subscription billing period.
type ApplePeriodUnit string

const (
	ApplePeriodDay   ApplePeriodUnit = "day"
	ApplePeriodWeek  ApplePeriodUnit = "week"
	ApplePeriodMonth ApplePeriodUnit = "month"
	ApplePeriodYear  ApplePeriodUnit = "year"
)

// AppleOfferType identifies what kind of discount offer is attached to a product.
type AppleOfferType string

const (
	AppleOfferIntroductory AppleOfferType = "introductory"
	AppleOfferPromotional  AppleOfferType = "promotional"
	AppleOfferWinBack      AppleOfferType = "win_back"
	AppleOfferCode         AppleOfferType = "offer_code"
)

// ApplePaymentMode is the billing cadence for an offer.
type ApplePaymentMode string

const (
	ApplePaymentFreeTrial  ApplePaymentMode = "free_trial"
	ApplePaymentPayAsYouGo ApplePaymentMode = "pay_as_you_go"
	ApplePaymentPayUpFront ApplePaymentMode = "pay_up_front"
)

// AppleSubscriptionState is the current standing of a subscription.
type AppleSubscriptionState string

const (
	AppleStateActive         AppleSubscriptionState = "active"
	AppleStateExpired        AppleSubscriptionState = "expired"
	AppleStateInBillingRetry AppleSubscriptionState = "in_billing_retry"
	AppleStateInGracePeriod  AppleSubscriptionState = "in_billing_grace_period"
	AppleStateRevoked        AppleSubscriptionState = "revoked"
)

// AppleExpirationReason explains why a subscription lapsed.
type AppleExpirationReason string

const (
	AppleExpiredCancelled          AppleExpirationReason = "cancelled"
	AppleExpiredBillingError       AppleExpirationReason = "billing_error"
	AppleExpiredPriceIncrease      AppleExpirationReason = "price_increase"
	AppleExpiredProductUnavailable AppleExpirationReason = "product_unavailable"
	AppleExpiredUnknown            AppleExpirationReason = "unknown"
)

// ApplePurchaseError is the failure reason when a purchase does not succeed.
type ApplePurchaseError string

const (
	AppleErrCancelled            ApplePurchaseError = "cancelled"
	AppleErrPaymentFailed        ApplePurchaseError = "payment_failed"
	AppleErrProductNotFound      ApplePurchaseError = "product_not_found"
	AppleErrNotEntitled          ApplePurchaseError = "not_entitled"
	AppleErrPendingAuthorization ApplePurchaseError = "pending_authorization"
	AppleErrNetworkError         ApplePurchaseError = "network_error"
	AppleErrUnknown              ApplePurchaseError = "unknown"
)

func (e ApplePurchaseError) Error() string { return string(e) }

// AppleRefundStatus is the outcome of a refund request.
type AppleRefundStatus string

const (
	AppleRefundSuccess       AppleRefundStatus = "success"
	AppleRefundUserCancelled AppleRefundStatus = "user_cancelled"
	AppleRefundError         AppleRefundStatus = "error"
)

// ── Product structs ───────────────────────────────────────────────────────────

// AppleSubscriptionPeriod is the billing frequency of a subscription product.
type AppleSubscriptionPeriod struct {
	Value int
	Unit  ApplePeriodUnit
}

// AppleOffer represents an introductory, promotional, win-back, or offer-code
// discount attached to a subscription product.
type AppleOffer struct {
	ID           string
	Type         AppleOfferType
	PaymentMode  ApplePaymentMode
	DisplayPrice string
	Period       AppleSubscriptionPeriod
}

// AppleProduct is a product returned by the App Store.
type AppleProduct struct {
	ID                 string
	Title              string
	Description        string
	DisplayPrice       string
	Kind               AppleProductKind
	SubscriptionPeriod *AppleSubscriptionPeriod // nil for non-subscription products
	IntroductoryOffer  *AppleOffer
	PromotionalOffers  []AppleOffer
	WinBackOffers      []AppleOffer
}

// ── Transaction / entitlement structs ────────────────────────────────────────

// AppleTransaction is a verified StoreKit transaction.
type AppleTransaction struct {
	ID            string
	OriginalID    string
	ProductID     string
	ProductKind   AppleProductKind
	PurchasedAt   time.Time
	ExpiresAt     *time.Time // nil for non-subscription or non-expiring products
	RevokedAt     *time.Time // non-nil if the transaction was revoked (e.g. refund)
	FamilyShared  bool
	Upgraded      bool
	RedeemedOffer *AppleOffer // non-nil if a promotional or win-back offer was applied
}

// AppleEntitlement is the current subscription standing for a product.
type AppleEntitlement struct {
	ProductID        string
	State            AppleSubscriptionState
	Transaction      AppleTransaction
	ExpiresAt        *time.Time
	WillAutoRenew    bool
	RenewalProductID *string                // non-nil when upgrading/downgrading
	ExpirationReason *AppleExpirationReason // non-nil when State == Expired
}

// ── Result / input structs ────────────────────────────────────────────────────

// ApplePurchaseResult is delivered to OnPurchaseCompleted and OnRestoreCompleted.
// Exactly one of Transaction and Error is non-nil.
type ApplePurchaseResult struct {
	ProductID   string
	Transaction *AppleTransaction
	Error       *ApplePurchaseError
}

// AppleProductSpec identifies a product to register with the store before fetching.
type AppleProductSpec struct {
	ID   string
	Kind AppleProductKind
}

// AppleOfferSignature carries the server-generated signature required when
// applying a promotional or win-back offer at purchase time.
type AppleOfferSignature struct {
	JWS          string `json:"jws,omitempty"`
	KeyID        string `json:"key_id,omitempty"`
	Nonce        string `json:"nonce,omitempty"`
	Timestamp    int64  `json:"timestamp,omitempty"`
	SignatureB64 string `json:"signature_b64,omitempty"`
}

// ApplePurchaseOptions is optional data forwarded with a purchase request.
type ApplePurchaseOptions struct {
	// OfferID is the offer identifier for a promotional or win-back offer.
	OfferID *string
	// OfferSignature is required when OfferID refers to a promotional offer.
	OfferSignature *AppleOfferSignature
}