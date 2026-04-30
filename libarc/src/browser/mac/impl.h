#pragma once

#include "browser/shared/webview.h"
#include "host_channel.h"

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

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

// ── Embedded web view ─────────────────────────────────────────────────────────
//
// Each EmbeddedWebView is a borderless NSPanel attached as a child window of
// the main NSWindow. It runs in a fully isolated WKWebView context — separate
// data store, no IPC shim injected.

struct EmbeddedWebView {
    NSPanel*   panel   = nullptr;
    WKWebView* webview = nullptr;
    int        zorder  = 0;   // last known zorder; used when re-stacking
};

// ── Main window state ─────────────────────────────────────────────────────────

struct WebViewImpl {
    NSWindow*  window  = nullptr;
    WKWebView* webview = nullptr;
    WebView*   owner   = nullptr;
    id         window_delegate = nil;

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

    // Embedded web views — keyed by the Go-assigned uint32 ID.
    // All access must be from the main thread (consistent with all other
    // NSWindow/WKWebView operations).
    std::unordered_map<uint32_t, EmbeddedWebView> embeds;
};

} // namespace browser