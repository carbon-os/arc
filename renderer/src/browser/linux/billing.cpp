#if !defined(_WIN32) && !defined(__APPLE__)
#include "billing.h"
#include "logger.h"

// Linux billing stub.
// There is no system-native IAP on Linux. Wire in a payment processor
// (Stripe, Paddle, etc.) here if needed for direct distribution.

struct BillingManager::Impl {
    HostChannel& channel;
    explicit Impl(HostChannel& ch) : channel(ch) {}
};

BillingManager::BillingManager(HostChannel& channel)
    : impl_(std::make_unique<Impl>(channel)) {}

BillingManager::~BillingManager() = default;

void BillingManager::init(const std::vector<BillingProductDecl>&) {
    logger::Warn("billing: no native billing on Linux");
    impl_->channel.send_billing_products({});
}

void BillingManager::buy(const std::string& product_id) {
    logger::Warn("billing: no native billing on Linux");
    impl_->channel.send_billing_purchase(PurchaseStatus::Failed,
                                         product_id,
                                         "no native billing on Linux");
}

void BillingManager::restore() {
    logger::Warn("billing: no native billing on Linux");
}

#endif