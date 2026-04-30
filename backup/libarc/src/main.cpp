#include "host_channel.h"
#include "billing.h"
#include "logger.h"
#include "browser/shared/webview.h"

#include <cstring>
#include <memory>
#include <string>
#include <thread>

#ifndef _WIN32
#  include <cstdlib>
#endif

static std::string channel_path(const std::string& id)
{
#ifdef _WIN32
    return "\\\\.\\pipe\\arc-" + id;
#else
    const char* tmp = std::getenv("TMPDIR");
    if (!tmp || !*tmp) tmp = "/tmp";
    return std::string(tmp) + "/arc-" + id + ".sock";
#endif
}

static std::string parse_flag(int argc, char** argv, const char* flag)
{
    for (int i = 1; i < argc - 1; ++i)
        if (std::strcmp(argv[i], flag) == 0)
            return argv[i + 1];
    return {};
}

static bool parse_bool_flag(int argc, char** argv, const char* flag)
{
    for (int i = 1; i < argc; ++i)
        if (std::strcmp(argv[i], flag) == 0)
            return true;
    return false;
}

int main(int argc, char** argv)
{
    logger::init(parse_bool_flag(argc, argv, "--logging"));

    const std::string id = parse_flag(argc, argv, "--channel");
    if (id.empty()) {
        logger::Error("renderer: missing --channel <id>");
        return 1;
    }

    logger::Info("renderer: connecting on channel %s", id.c_str());

    HostChannel channel(channel_path(id));
    if (!channel.connected()) {
        logger::Error("renderer: failed to connect to host");
        return 1;
    }

    InboundFrame first;
    if (!channel.read_frame(first) || first.type != Command::WindowCreate) {
        logger::Error("renderer: expected WindowCreate as first command");
        return 1;
    }

    logger::Info("renderer: WindowCreate %dx%d title=%s",
                 first.wc.width, first.wc.height, first.wc.title.c_str());

    browser::WebView wv(first.wc);

    // Billing manager is created lazily when CmdBillingInit arrives.
    std::unique_ptr<BillingManager> billing;

    wv.on_ready([&]() {
        logger::Info("renderer: ready");
        channel.send_event(Event::Ready);

        std::thread([&]() {
            InboundFrame f;
            while (channel.read_frame(f)) {
                switch (f.type) {

                case Command::Quit:
                    logger::Info("renderer: Quit received");
                    wv.quit();
                    return;

                case Command::BillingInit:
                    logger::Info("renderer: BillingInit — %zu product(s)",
                                 f.billing_products.size());
                    billing = std::make_unique<BillingManager>(channel);
                    billing->init(f.billing_products);
                    break;

                case Command::BillingBuy:
                    if (billing) {
                        billing->buy(f.str);
                    } else {
                        logger::Warn("renderer: BillingBuy before BillingInit");
                    }
                    break;

                case Command::BillingRestore:
                    if (billing) {
                        billing->restore();
                    } else {
                        logger::Warn("renderer: BillingRestore before BillingInit");
                    }
                    break;

                default:
                    wv.dispatch(std::move(f));
                    break;
                }
            }
            logger::Warn("renderer: channel closed, quitting");
            wv.quit();
        }).detach();
    });

    wv.on_closed([&]() {
        channel.send_event(Event::Closed);
    });

    wv.on_resize([&](int w, int h) {
        channel.send_resized(w, h);
    });

    wv.on_ipc_text([&](std::string_view ch, std::string_view text) {
        channel.send_ipc_text(ch, text);
    });

    wv.on_ipc_binary([&](std::string_view ch, const std::vector<uint8_t>& data) {
        channel.send_ipc_binary(ch, data);
    });

    wv.run();
    return 0;
}