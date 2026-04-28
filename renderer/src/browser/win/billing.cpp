#ifdef _WIN32
#include "billing.h"
#include "logger.h"

// Windows billing stub.
// Microsoft Store / Windows.Services.Commerce support will be added here.
// All three entry points immediately report back to the host so the Go
// side does not hang waiting for a response that will never arrive.

struct BillingManager::Impl {
    HostChannel& channel;
    explicit Impl(HostChannel& ch) : channel(ch) {}
};

BillingManager::BillingManager(HostChannel& channel)
    : impl_(std::make_unique<Impl>(channel)) {}

BillingManager::~BillingManager() = default;

void BillingManager::init(const std::vector<BillingProductDecl>&) {
    logger::Warn("billing: Windows billing not yet implemented");
    impl_->channel.send_billing_products({});
}

void BillingManager::buy(const std::string& product_id) {
    logger::Warn("billing: Windows billing not yet implemented");
    impl_->channel.send_billing_purchase(PurchaseStatus::Failed,
                                         product_id,
                                         "Windows billing not yet implemented");
}

void BillingManager::restore() {
    logger::Warn("billing: Windows billing not yet implemented");
}

#endif // _WIN32