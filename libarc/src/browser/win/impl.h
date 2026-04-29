#pragma once

#include "browser/shared/webview.h"
#include "host_channel.h"

#ifndef WIN32_LEAN_AND_MEAN
#  define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#include <dwmapi.h>
#include <wrl/client.h>
#include <WebView2.h>

#include <atomic>
#include <cstdint>
#include <mutex>
#include <queue>
#include <string>
#include <unordered_map>
#include <vector>

using Microsoft::WRL::ComPtr;

namespace browser {

enum class LoadMode { None, Html, File };

struct OutboundFrame {
    std::string          channel;
    bool                 binary = false;
    std::string          text;
    std::vector<uint8_t> data;
};

struct WebViewImpl {
    HWND     hwnd  = nullptr;
    WebView* owner = nullptr;

    // Stored so wnd_proc can branch on it without capturing cfg.
    TitleBarStyle titlebar_style = TitleBarStyle::Default;

    ComPtr<ICoreWebView2Environment> env;
    ComPtr<ICoreWebView2Controller>  controller;
    ComPtr<ICoreWebView2>            webview;

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

    std::mutex                  post_mutex;
    std::queue<OutboundFrame>   post_queue;

    std::mutex                  cmd_mutex;
    std::queue<InboundFrame>    cmd_queue;

    EventRegistrationToken resource_token {};
    EventRegistrationToken message_token  {};
};

} // namespace browser