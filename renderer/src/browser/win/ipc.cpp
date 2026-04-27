#include "impl.h"
#include "wstring.h"
#include "browser/shared/str_escape.h"
#include "logger.h"

#include <cstdint>
#include <mutex>
#include <string>
#include <vector>

namespace browser {

static constexpr UINT kMsgFlush = WM_APP + 1;

void WebView::drain_post_queue()
{
    std::unique_lock<std::mutex> lock(impl_->post_mutex);
    while (!impl_->post_queue.empty()) {
        OutboundFrame frame = std::move(impl_->post_queue.front());
        impl_->post_queue.pop();
        lock.unlock();

        if (!frame.binary) {
            logger::Info("ipc: posting text to JS channel=%s", frame.channel.c_str());
            std::string json =
                "{\"type\":\"host_ipc_message\","
                "\"channel\":\"" + js_escape(frame.channel) + "\","
                "\"text\":\""    + js_escape(frame.text)    + "\"}";
            impl_->webview->PostWebMessageAsJson(win::to_wide(json).c_str());
        } else {
            uint64_t    token    = impl_->next_token.fetch_add(1, std::memory_order_relaxed);
            std::string slot_key = frame.channel + ":" + std::to_string(token);

            logger::Info("ipc: posting binary to JS channel=%s bytes=%zu token=%llu",
                         frame.channel.c_str(), frame.data.size(),
                         (unsigned long long)token);

            {
                std::lock_guard<std::mutex> slock(impl_->slots_mutex);
                impl_->slots[slot_key] = std::move(frame.data);
            }

            std::string json =
                "{\"type\":\"host_ipc_message\","
                "\"channel\":\"" + js_escape(frame.channel) + "\","
                "\"token\":\""   + std::to_string(token)      + "\"}";
            impl_->webview->PostWebMessageAsJson(win::to_wide(json).c_str());
        }

        lock.lock();
    }
}

void WebView::post_text(std::string_view channel, std::string_view text)
{
    logger::Info("ipc: enqueue post_text channel=%.*s",
                 (int)channel.size(), channel.data());
    {
        std::lock_guard<std::mutex> lock(impl_->post_mutex);
        OutboundFrame f;
        f.channel = std::string(channel);
        f.binary  = false;
        f.text    = std::string(text);
        impl_->post_queue.push(std::move(f));
    }
    PostMessageW(impl_->hwnd, kMsgFlush, 0, 0);
}

void WebView::post_binary(std::string_view channel, const std::vector<uint8_t>& data)
{
    logger::Info("ipc: enqueue post_binary channel=%.*s bytes=%zu",
                 (int)channel.size(), channel.data(), data.size());
    {
        std::lock_guard<std::mutex> lock(impl_->post_mutex);
        OutboundFrame f;
        f.channel = std::string(channel);
        f.binary  = true;
        f.data    = data;
        impl_->post_queue.push(std::move(f));
    }
    PostMessageW(impl_->hwnd, kMsgFlush, 0, 0);
}

} // namespace browser