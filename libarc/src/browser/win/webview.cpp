#include "impl.h"
#include "scheme.h"
#include "wstring.h"
#include "shim.h"
#include "logger.h"

#include <WebView2EnvironmentOptions.h>
#include <wrl/client.h>
#include <wrl/event.h>
#include <dwmapi.h>

#include <cstdio>
#include <sstream>
#include <stdexcept>
#include <string>

using Microsoft::WRL::Callback;
using Microsoft::WRL::ComPtr;

namespace browser {

LRESULT CALLBACK wnd_proc(HWND, UINT, WPARAM, LPARAM);

static constexpr UINT kMsgFlush   = WM_APP + 1;
static constexpr UINT kMsgCommand = WM_APP + 2;

WebView::WebView(const WindowConfig& cfg) : impl_(new WebViewImpl())
{
    impl_->owner         = this;
    impl_->titlebar_style = cfg.titleBarStyle;

    HRESULT hr_com = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
    if (FAILED(hr_com) && hr_com != S_FALSE && hr_com != RPC_E_CHANGED_MODE)
        throw std::runtime_error("browser::WebView: CoInitializeEx failed");

    static bool registered = false;
    if (!registered) {
        WNDCLASSEXW wc{};
        wc.cbSize        = sizeof(wc);
        wc.lpfnWndProc   = wnd_proc;
        wc.hInstance     = GetModuleHandleW(nullptr);
        wc.lpszClassName = L"arc_renderer";
        wc.hCursor       = LoadCursorW(nullptr, MAKEINTRESOURCEW(32512));
        wc.hbrBackground = reinterpret_cast<HBRUSH>(COLOR_WINDOW + 1);
        RegisterClassExW(&wc);
        registered = true;
        logger::Info("WebView: window class registered");
    }

    // ── Window style ──────────────────────────────────────────────────────────
    //
    // Default: WS_OVERLAPPEDWINDOW — standard title bar + border.
    //
    // Hidden:  WS_THICKFRAME + WS_SYSMENU + WS_MINIMIZEBOX + WS_MAXIMIZEBOX
    //          No WS_CAPTION — removes the title bar chrome.
    //          WS_THICKFRAME keeps the DWM shadow and resize border.
    //          WM_NCCALCSIZE in wnd_proc removes the residual 1px top line.

    const bool hidden_bar = (cfg.titleBarStyle == TitleBarStyle::Hidden);

    DWORD style = hidden_bar
        ? (WS_THICKFRAME | WS_SYSMENU | WS_MINIMIZEBOX | WS_MAXIMIZEBOX)
        : WS_OVERLAPPEDWINDOW;

    impl_->hwnd = CreateWindowExW(
        0, L"arc_renderer",
        win::to_wide(cfg.title).c_str(),
        style,
        CW_USEDEFAULT, CW_USEDEFAULT,
        cfg.width, cfg.height,
        nullptr, nullptr, GetModuleHandleW(nullptr), nullptr);

    if (!impl_->hwnd)
        throw std::runtime_error("browser::WebView: CreateWindowEx failed");

    SetWindowLongPtrW(impl_->hwnd, GWLP_USERDATA,
                      reinterpret_cast<LONG_PTR>(impl_));

    // Extend the DWM frame so the shadow is preserved on the hidden-bar path.
    // A 0-margin call on the default path is a no-op.
    if (hidden_bar) {
        MARGINS margins{ 0, 0, 1, 0 }; // 1px top lets DWM keep its shadow
        DwmExtendFrameIntoClientArea(impl_->hwnd, &margins);
        logger::Info("WebView: titleBarStyle=Hidden — DWM frame extended");
    }

    ShowWindow(impl_->hwnd, SW_SHOW);
    UpdateWindow(impl_->hwnd);

    logger::Info("WebView: window created %dx%d title=%s titleBarStyle=%d",
                 cfg.width, cfg.height, cfg.title.c_str(),
                 static_cast<int>(cfg.titleBarStyle));

    auto opts = Microsoft::WRL::Make<CoreWebView2EnvironmentOptions>();

    ComPtr<ICoreWebView2EnvironmentOptions4> opts4;
    if (SUCCEEDED(opts->QueryInterface(IID_PPV_ARGS(&opts4)))) {
        auto reg = Microsoft::WRL::Make<CoreWebView2CustomSchemeRegistration>(L"ui-ipc");
        reg->put_TreatAsSecure(TRUE);
        reg->put_HasAuthorityComponent(TRUE);
        LPCWSTR origins[] = { L"ui-ipc://app" };
        reg->SetAllowedOrigins(1, origins);
        ICoreWebView2CustomSchemeRegistration* regs[] = { reg.Get() };
        opts4->SetCustomSchemeRegistrations(1, regs);
        logger::Info("WebView: custom scheme ui-ipc registered");
    }

    HRESULT hr = CreateCoreWebView2EnvironmentWithOptions(
        nullptr, nullptr, opts.Get(),
        Callback<ICoreWebView2CreateCoreWebView2EnvironmentCompletedHandler>(
        [this, debug = cfg.debug](HRESULT res, ICoreWebView2Environment* env) -> HRESULT
        {
            if (FAILED(res) || !env) {
                char msg[128];
                std::snprintf(msg, sizeof(msg),
                    "browser::WebView: environment creation failed (0x%08lX)", res);
                logger::Error("WebView: environment creation failed 0x%08lX", res);
                throw std::runtime_error(msg);
            }

            impl_->env = env;
            logger::Info("WebView: environment created");

            return env->CreateCoreWebView2Controller(impl_->hwnd,
                Callback<ICoreWebView2CreateCoreWebView2ControllerCompletedHandler>(
                [this, debug](HRESULT res2, ICoreWebView2Controller* ctrl) -> HRESULT
                {
                    if (FAILED(res2) || !ctrl) {
                        logger::Error("WebView: controller creation failed 0x%08lX", res2);
                        return res2;
                    }

                    impl_->controller = ctrl;
                    ctrl->get_CoreWebView2(&impl_->webview);
                    logger::Info("WebView: controller created");

                    RECT rc;
                    GetClientRect(impl_->hwnd, &rc);
                    ctrl->put_Bounds(rc);

                    if (debug) {
                        ComPtr<ICoreWebView2Settings> s;
                        impl_->webview->get_Settings(&s);
                        s->put_AreDevToolsEnabled(TRUE);
                        logger::Info("WebView: DevTools enabled");
                    }

                    impl_->webview->AddScriptToExecuteOnDocumentCreated(
                        win::to_wide(win::k_shim).c_str(), nullptr);
                    logger::Info("WebView: IPC shim injected");

                    impl_->webview->AddWebResourceRequestedFilter(
                        L"ui-ipc://*/*",
                        COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL);

                    impl_->webview->add_WebResourceRequested(
                        Callback<ICoreWebView2WebResourceRequestedEventHandler>(
                        [this](ICoreWebView2*, ICoreWebView2WebResourceRequestedEventArgs* args) -> HRESULT {
                            return handle_resource_request(impl_, args);
                        }).Get(),
                        &impl_->resource_token);

                    impl_->webview->add_WebMessageReceived(
                        Callback<ICoreWebView2WebMessageReceivedEventHandler>(
                        [this](ICoreWebView2*, ICoreWebView2WebMessageReceivedEventArgs* args) -> HRESULT
                        {
                            LPWSTR raw = nullptr;
                            args->get_WebMessageAsJson(&raw);
                            if (!raw) return S_OK;
                            std::string json = win::to_utf8(raw);
                            CoTaskMemFree(raw);

                            auto get_str = [&](const std::string& key) -> std::string {
                                std::string search = "\"" + key + "\":\"";
                                auto pos = json.find(search);
                                if (pos == std::string::npos) return {};
                                pos += search.size();
                                auto end = json.find('"', pos);
                                if (end == std::string::npos) return {};
                                std::string out;
                                for (auto i = pos; i < end; ++i) {
                                    if (json[i] == '\\' && i + 1 < end && json[i+1] == '"') {
                                        out += '"'; ++i;
                                    } else {
                                        out += json[i];
                                    }
                                }
                                return out;
                            };

                            std::string type    = get_str("type");
                            std::string channel = get_str("channel");

                            if (type == "ipc_text") {
                                std::string text = get_str("text");
                                logger::Info("WebView: ipc_text channel=%s", channel.c_str());
                                if (impl_->on_ipc_text_cb)
                                    impl_->on_ipc_text_cb(channel, text);

                            } else if (type == "ipc_binary") {
                                auto pos = json.find("\"data\":[");
                                std::vector<uint8_t> bytes;
                                if (pos != std::string::npos) {
                                    pos += 8;
                                    auto end = json.find(']', pos);
                                    if (end != std::string::npos) {
                                        std::stringstream ss(json.substr(pos, end - pos));
                                        std::string tok;
                                        while (std::getline(ss, tok, ',')) {
                                            try { bytes.push_back((uint8_t)std::stoi(tok)); }
                                            catch (...) {}
                                        }
                                    }
                                }
                                logger::Info("WebView: ipc_binary channel=%s bytes=%zu",
                                             channel.c_str(), bytes.size());
                                if (impl_->on_ipc_binary_cb)
                                    impl_->on_ipc_binary_cb(channel, bytes);
                            } else {
                                logger::Warn("WebView: unknown postMessage type=%s", type.c_str());
                            }

                            return S_OK;
                        }).Get(),
                        &impl_->message_token);

                    logger::Info("WebView: firing on_ready");
                    if (impl_->on_ready_cb)
                        impl_->on_ready_cb();

                    return S_OK;
                }).Get());
        }).Get());

    if (FAILED(hr))
        throw std::runtime_error("browser::WebView: CreateCoreWebView2EnvironmentWithOptions failed");
}

WebView::~WebView() { delete impl_; }

void WebView::eval(std::string_view js)
{
    logger::Info("WebView: eval %zu chars", js.size());
    impl_->webview->ExecuteScript(win::to_wide(js).c_str(), nullptr);
}

void WebView::on_ready(ReadyCallback cb)          { impl_->on_ready_cb      = std::move(cb); }
void WebView::on_closed(ClosedCallback cb)        { impl_->on_closed_cb     = std::move(cb); }
void WebView::on_ipc_text(IpcTextCallback cb)     { impl_->on_ipc_text_cb   = std::move(cb); }
void WebView::on_ipc_binary(IpcBinaryCallback cb) { impl_->on_ipc_binary_cb = std::move(cb); }

void WebView::dispatch(InboundFrame frame)
{
    logger::Info("WebView: dispatch cmd=0x%02X", static_cast<uint8_t>(frame.type));
    {
        std::lock_guard<std::mutex> lock(impl_->cmd_mutex);
        impl_->cmd_queue.push(std::move(frame));
    }
    PostMessageW(impl_->hwnd, kMsgCommand, 0, 0);
}

void WebView::drain_cmd_queue()
{
    std::unique_lock<std::mutex> lock(impl_->cmd_mutex);
    while (!impl_->cmd_queue.empty()) {
        InboundFrame f = std::move(impl_->cmd_queue.front());
        impl_->cmd_queue.pop();
        lock.unlock();
        execute_frame(f);
        lock.lock();
    }
}

void WebView::execute_frame(const InboundFrame& f)
{
    switch (f.type) {
    case Command::LoadFile:   load_file(f.str);               break;
    case Command::LoadHTML:   load_html(f.str);               break;
    case Command::LoadURL:    load_url(f.str);                break;
    case Command::Eval:       eval(f.str);                    break;
    case Command::SetTitle:   set_title(f.str);               break;
    case Command::SetSize:    set_size(f.width, f.height);    break;
    case Command::PostText:   post_text(f.channel, f.text);   break;
    case Command::PostBinary: post_binary(f.channel, f.data); break;
    default:
        logger::Warn("WebView: execute_frame unknown cmd=0x%02X",
                     static_cast<uint8_t>(f.type));
        break;
    }
}

} // namespace browser