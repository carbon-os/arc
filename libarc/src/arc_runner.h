#pragma once

// Internal — not part of the public API.

#include "host_channel.h"
#include "billing.h"
#include "logger.h"
#include "browser/shared/webview.h"

#include <memory>
#include <thread>

namespace arc {
namespace detail {

// Connect a fully-open HostChannel to a WebView and run the native event loop.
// Blocks on the calling thread until the window is closed or the channel drops.
inline void run_with_channel(HostChannel& channel)
{
    InboundFrame first;
    if (!channel.read_frame(first) || first.type != Command::WindowCreate) {
        logger::Error("arc: expected WindowCreate as first command");
        return;
    }

    logger::Info("arc: WindowCreate %dx%d title=%s",
                 first.wc.width, first.wc.height, first.wc.title.c_str());

    browser::WebView wv(first.wc);
    std::unique_ptr<BillingManager> billing;

    wv.on_ready([&]() {
        logger::Info("arc: ready");
        channel.send_event(Event::Ready);

        std::thread([&]() {
            InboundFrame f;
            while (channel.read_frame(f)) {
                switch (f.type) {

                case Command::Quit:
                    logger::Info("arc: Quit");
                    wv.quit();
                    return;

                case Command::BillingInit:
                    logger::Info("arc: BillingInit — %zu product(s)",
                                 f.billing_products.size());
                    billing = std::make_unique<BillingManager>(channel);
                    billing->init(f.billing_products);
                    break;

                case Command::BillingBuy:
                    if (billing)
                        billing->buy(f.str);
                    else
                        logger::Warn("arc: BillingBuy before BillingInit");
                    break;

                case Command::BillingRestore:
                    if (billing)
                        billing->restore();
                    else
                        logger::Warn("arc: BillingRestore before BillingInit");
                    break;

                default:
                    wv.dispatch(std::move(f));
                    break;
                }
            }
            logger::Warn("arc: channel closed — quitting");
            wv.quit();
        }).detach();
    });

    wv.on_closed([&]() {
        channel.send_event(Event::Closed);
    });

    wv.on_ipc_text([&](std::string_view ch, std::string_view text) {
        channel.send_ipc_text(ch, text);
    });

    wv.on_ipc_binary([&](std::string_view ch, const std::vector<uint8_t>& data) {
        channel.send_ipc_binary(ch, data);
    });

    wv.run();
}

} // namespace detail
} // namespace arc