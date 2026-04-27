#include "impl.h"
#include "browser/shared/str_escape.h"
#include "logger.h"

#import <WebKit/WebKit.h>

#include <string>
#include <vector>

namespace browser {

static void do_eval(WKWebView* wv, const std::string& script)
{
    [wv evaluateJavaScript:[NSString stringWithUTF8String:script.c_str()]
         completionHandler:nil];
}

void WebView::drain_post_queue()
{
    std::unique_lock lock(impl_->post_mutex);
    while (!impl_->post_queue.empty()) {
        OutboundFrame frame = std::move(impl_->post_queue.front());
        impl_->post_queue.pop();
        lock.unlock();

        if (!frame.binary) {
            logger::Info("ipc: posting text to JS channel=%s", frame.channel.c_str());
            std::string script =
                "window._arc_dispatch(\""
                + js_escape(frame.channel) + "\",\""
                + js_escape(frame.text)    + "\")";
            do_eval(impl_->webview, script);

        } else {
            uint64_t    token    = impl_->next_token.fetch_add(1, std::memory_order_relaxed);
            std::string slot_key = frame.channel + ":" + std::to_string(token);

            logger::Info("ipc: posting binary to JS channel=%s bytes=%zu token=%llu",
                         frame.channel.c_str(), frame.data.size(),
                         static_cast<unsigned long long>(token));

            {
                std::lock_guard slock(impl_->slots_mutex);
                impl_->slots[slot_key] = std::move(frame.data);
            }

            std::string script =
                "window._arc_dispatch_binary(\""
                + js_escape(frame.channel) + "\",\""
                + std::to_string(token)     + "\")";
            do_eval(impl_->webview, script);
        }

        lock.lock();
    }
}

void WebView::post_text(std::string_view channel, std::string_view text)
{
    logger::Info("ipc: enqueue post_text channel=%.*s",
                 (int)channel.size(), channel.data());
    {
        std::lock_guard lock(impl_->post_mutex);
        OutboundFrame f;
        f.channel = std::string(channel);
        f.binary  = false;
        f.text    = std::string(text);
        impl_->post_queue.push(std::move(f));
    }
    WebView* self = this;
    dispatch_async(dispatch_get_main_queue(), ^{
        self->drain_post_queue();
    });
}

void WebView::post_binary(std::string_view channel, const std::vector<uint8_t>& data)
{
    logger::Info("ipc: enqueue post_binary channel=%.*s bytes=%zu",
                 (int)channel.size(), channel.data(), data.size());
    {
        std::lock_guard lock(impl_->post_mutex);
        OutboundFrame f;
        f.channel = std::string(channel);
        f.binary  = true;
        f.data    = data;
        impl_->post_queue.push(std::move(f));
    }
    WebView* self = this;
    dispatch_async(dispatch_get_main_queue(), ^{
        self->drain_post_queue();
    });
}

} // namespace browser