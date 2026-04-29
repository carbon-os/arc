#pragma once

#include "browser/shared/webview.h"
#include "host_channel.h"

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

#include <atomic>
#include <cstdint>
#include <mutex>
#include <queue>
#include <string>
#include <unordered_map>
#include <vector>

namespace browser {

enum class LoadMode { None, Html, File };

struct OutboundFrame {
    std::string          channel;
    bool                 binary = false;
    std::string          text;
    std::vector<uint8_t> data;
};

struct WebViewImpl {
    GtkWidget*     window  = nullptr;
    WebKitWebView* webview = nullptr;
    WebView*       owner   = nullptr;

    // Stored so window.cpp helpers can branch on it if needed later.
    TitleBarStyle titlebar_style = TitleBarStyle::Default;

    WebView::ReadyCallback      on_ready_cb;
    WebView::ClosedCallback     on_closed_cb;
    WebView::IpcTextCallback    on_ipc_text_cb;
    WebView::IpcBinaryCallback  on_ipc_binary_cb;

    std::atomic<uint64_t>                                 next_token { 0 };
    std::mutex                                            slots_mutex;
    std::unordered_map<std::string, std::vector<uint8_t>> slots;

    std::mutex  load_mutex;
    LoadMode    load_mode  = LoadMode::None;
    std::string html_src;
    std::string file_root;
    std::string file_entry;

    std::mutex                post_mutex;
    std::queue<OutboundFrame> post_queue;

    std::mutex                cmd_mutex;
    std::queue<InboundFrame>  cmd_queue;
};

} // namespace browser