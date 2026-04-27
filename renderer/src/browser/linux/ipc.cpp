#include "impl.h"
#include "browser/shared/str_escape.h"
#include "logger.h"

#include <cstdint>
#include <mutex>
#include <string>
#include <vector>

namespace browser {

static void do_eval(WebKitWebView* wv, const std::string& script)
{
    webkit_web_view_evaluate_javascript(
        wv, script.c_str(), -1,
        nullptr, nullptr,
        nullptr, nullptr, nullptr);
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
                + std::to_string(token)         + "\")";
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
    g_idle_add([](gpointer p) -> gboolean {
        static_cast<WebView*>(p)->drain_post_queue();
        return G_SOURCE_REMOVE;
    }, this);
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
    g_idle_add([](gpointer p) -> gboolean {
        static_cast<WebView*>(p)->drain_post_queue();
        return G_SOURCE_REMOVE;
    }, this);
}

} // namespace browser