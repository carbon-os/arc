#pragma once

#include "host_channel.h"
#include <memory>
#include <string>
#include <vector>

// BillingManager is the platform-native in-app purchase layer.
// One instance per window, created when CmdBillingInit is received.
// All public methods are safe to call from any thread.
class BillingManager {
public:
    explicit BillingManager(HostChannel& channel);
    ~BillingManager();

    // Register product IDs with the platform store and fetch live metadata.
    // Fires send_billing_products on the channel when the store responds.
    void init(const std::vector<BillingProductDecl>& products);

    // Initiate a purchase. Result arrives via send_billing_purchase.
    void buy(const std::string& product_id);

    // Restore completed transactions. Results arrive via send_billing_purchase.
    void restore();

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};