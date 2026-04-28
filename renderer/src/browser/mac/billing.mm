#include "billing.h"
#include "logger.h"

#import <StoreKit/StoreKit.h>
#include <unordered_map>
#include <string>

// ── ObjC IAP manager ─────────────────────────────────────────────────────────

@interface ArcIAPManager : NSObject <SKProductsRequestDelegate,
                                      SKPaymentTransactionObserver>

- (instancetype)initWithChannel:(HostChannel*)channel;
- (void)fetchProducts:(const std::vector<BillingProductDecl>&)decls;
- (void)buyProductID:(NSString*)productID;
- (void)restorePurchases;

@end

@implementation ArcIAPManager {
    HostChannel*                       _channel;
    NSArray<SKProduct*>*               _products;
    std::unordered_map<std::string, uint8_t> _kindMap;
}

- (instancetype)initWithChannel:(HostChannel*)channel {
    if (self = [super init]) {
        _channel  = channel;
        _products = @[];
        [[SKPaymentQueue defaultQueue] addTransactionObserver:self];
    }
    return self;
}

- (void)dealloc {
    [[SKPaymentQueue defaultQueue] removeTransactionObserver:self];
}

- (void)fetchProducts:(const std::vector<BillingProductDecl>&)decls {
    if (![SKPaymentQueue canMakePayments]) {
        logger::Warn("billing: payments disabled on this device");
        _channel->send_billing_products({});
        return;
    }

    NSMutableSet<NSString*>* ids = [NSMutableSet set];
    for (const auto& d : decls) {
        [ids addObject:[NSString stringWithUTF8String:d.id.c_str()]];
        _kindMap[d.id] = d.kind;
    }

    logger::Info("billing: fetching %zu product(s)", decls.size());
    SKProductsRequest* req = [[SKProductsRequest alloc]
                               initWithProductIdentifiers:ids];
    req.delegate = self;
    [req start];
}

- (void)buyProductID:(NSString*)productID {
    for (SKProduct* p in _products) {
        if ([p.productIdentifier isEqualToString:productID]) {
            logger::Info("billing: initiating purchase %s",
                         productID.UTF8String);
            [[SKPaymentQueue defaultQueue]
                addPayment:[SKPayment paymentWithProduct:p]];
            return;
        }
    }
    logger::Warn("billing: buy called for unknown product %s",
                 productID.UTF8String);
    _channel->send_billing_purchase(PurchaseStatus::Failed,
                                    productID.UTF8String,
                                    "product not found — call OnProducts first");
}

- (void)restorePurchases {
    logger::Info("billing: restoring purchases");
    [[SKPaymentQueue defaultQueue] restoreCompletedTransactions];
}

// ── SKProductsRequestDelegate ─────────────────────────────────────────────────

- (void)productsRequest:(SKProductsRequest*)request
     didReceiveResponse:(SKProductsResponse*)response {

    _products = response.products;

    if (response.invalidProductIdentifiers.count > 0) {
        for (NSString* inv in response.invalidProductIdentifiers)
            logger::Warn("billing: invalid product ID: %s", inv.UTF8String);
    }

    std::vector<BillingProductInfo> infos;
    infos.reserve(_products.count);

    for (SKProduct* p in _products) {
        NSNumberFormatter* fmt = [[NSNumberFormatter alloc] init];
        fmt.numberStyle = NSNumberFormatterCurrencyStyle;
        fmt.locale      = p.priceLocale;
        NSString* price = [fmt stringFromNumber:p.price] ?: @"";

        BillingProductInfo info;
        info.id             = p.productIdentifier.UTF8String;
        info.title          = p.localizedTitle.UTF8String;
        info.description    = p.localizedDescription.UTF8String;
        info.formatted_price = price.UTF8String;
        info.kind = _kindMap.count(info.id) ? _kindMap.at(info.id) : 0;

        logger::Info("billing: product ready %s | %s | %s",
                     info.id.c_str(), info.title.c_str(),
                     info.formatted_price.c_str());
        infos.push_back(std::move(info));
    }

    _channel->send_billing_products(infos);
}

- (void)request:(SKRequest*)request didFailWithError:(NSError*)error {
    logger::Error("billing: product request failed: %s",
                  error.localizedDescription.UTF8String);
    _channel->send_billing_products({});
}

// ── SKPaymentTransactionObserver ──────────────────────────────────────────────

- (void)paymentQueue:(SKPaymentQueue*)queue
 updatedTransactions:(NSArray<SKPaymentTransaction*>*)transactions {

    for (SKPaymentTransaction* t in transactions) {
        switch (t.transactionState) {

        case SKPaymentTransactionStatePurchasing:
            // In-flight — no action needed.
            break;

        case SKPaymentTransactionStatePurchased:
            logger::Info("billing: purchased %s",
                         t.payment.productIdentifier.UTF8String);
            _channel->send_billing_purchase(
                PurchaseStatus::Purchased,
                t.payment.productIdentifier.UTF8String, {});
            [[SKPaymentQueue defaultQueue] finishTransaction:t];
            break;

        case SKPaymentTransactionStateFailed:
            if (t.error.code == SKErrorPaymentCancelled) {
                logger::Info("billing: cancelled by user");
                _channel->send_billing_purchase(
                    PurchaseStatus::Cancelled,
                    t.payment.productIdentifier.UTF8String, {});
            } else {
                logger::Error("billing: purchase failed: %s",
                              t.error.localizedDescription.UTF8String);
                _channel->send_billing_purchase(
                    PurchaseStatus::Failed,
                    t.payment.productIdentifier.UTF8String,
                    t.error.localizedDescription.UTF8String);
            }
            [[SKPaymentQueue defaultQueue] finishTransaction:t];
            break;

        case SKPaymentTransactionStateRestored:
            logger::Info("billing: restored %s",
                t.originalTransaction.payment.productIdentifier.UTF8String);
            _channel->send_billing_purchase(
                PurchaseStatus::Restored,
                t.originalTransaction.payment.productIdentifier.UTF8String,
                {});
            [[SKPaymentQueue defaultQueue] finishTransaction:t];
            break;

        case SKPaymentTransactionStateDeferred:
            // Ask to Buy — parent must approve. Do not unlock yet.
            logger::Info("billing: deferred (Ask to Buy) %s",
                         t.payment.productIdentifier.UTF8String);
            _channel->send_billing_purchase(
                PurchaseStatus::Deferred,
                t.payment.productIdentifier.UTF8String, {});
            break;
        }
    }
}

- (void)paymentQueueRestoreCompletedTransactionsFinished:(SKPaymentQueue*)queue {
    logger::Info("billing: restore finished");
}

- (void)paymentQueue:(SKPaymentQueue*)queue
restoreCompletedTransactionsFailedWithError:(NSError*)error {
    logger::Error("billing: restore failed: %s",
                  error.localizedDescription.UTF8String);
    _channel->send_billing_purchase(PurchaseStatus::Failed, {},
                                    error.localizedDescription.UTF8String);
}

@end

// ── BillingManager (C++ pimpl) ────────────────────────────────────────────────

struct BillingManager::Impl {
    HostChannel&   channel;
    ArcIAPManager* manager{nil};

    explicit Impl(HostChannel& ch) : channel(ch) {}
};

BillingManager::BillingManager(HostChannel& channel)
    : impl_(std::make_unique<Impl>(channel)) {}

BillingManager::~BillingManager() {
    impl_->manager = nil;
}

void BillingManager::init(const std::vector<BillingProductDecl>& products) {
    // StoreKit must be touched on the main thread.
    auto products_copy = products;
    Impl* impl = impl_.get();
    dispatch_async(dispatch_get_main_queue(), ^{
        impl->manager = [[ArcIAPManager alloc]
                          initWithChannel:&impl->channel];
        [impl->manager fetchProducts:products_copy];
    });
}

void BillingManager::buy(const std::string& product_id) {
    NSString* nsid = [NSString stringWithUTF8String:product_id.c_str()];
    Impl* impl = impl_.get();
    dispatch_async(dispatch_get_main_queue(), ^{
        [impl->manager buyProductID:nsid];
    });
}

void BillingManager::restore() {
    Impl* impl = impl_.get();
    dispatch_async(dispatch_get_main_queue(), ^{
        [impl->manager restorePurchases];
    });
}