#include "impl.h"
#include "wstring.h"
#include "logger.h"

#include <string>

namespace browser {

static constexpr UINT kMsgFlush   = WM_APP + 1;
static constexpr UINT kMsgCommand = WM_APP + 2;

LRESULT CALLBACK wnd_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp)
{
    auto* impl = reinterpret_cast<WebViewImpl*>(
        GetWindowLongPtrW(hwnd, GWLP_USERDATA));

    if (msg == kMsgFlush) {
        MSG extra;
        while (PeekMessageW(&extra, hwnd, kMsgFlush, kMsgFlush, PM_REMOVE))
            ;
        if (impl && impl->owner)
            impl->owner->drain_post_queue();
        return 0;
    }

    if (msg == kMsgCommand) {
        MSG extra;
        while (PeekMessageW(&extra, hwnd, kMsgCommand, kMsgCommand, PM_REMOVE))
            ;
        if (impl && impl->owner)
            impl->owner->drain_cmd_queue();
        return 0;
    }

    if (msg == WM_SIZE && impl && impl->controller) {
        RECT rc;
        GetClientRect(hwnd, &rc);
        impl->controller->put_Bounds(rc);
        logger::Info("window: resized to %ldx%ld", rc.right, rc.bottom);
    }

    if (msg == WM_CLOSE) {
        logger::Info("window: WM_CLOSE received");
        if (impl && impl->on_closed_cb)
            impl->on_closed_cb();
        DestroyWindow(hwnd);
        return 0;
    }

    if (msg == WM_DESTROY) {
        logger::Info("window: WM_DESTROY, posting quit");
        PostQuitMessage(0);
        return 0;
    }

    return DefWindowProcW(hwnd, msg, wp, lp);
}

void WebView::run()
{
    logger::Info("window: entering message loop");
    MSG msg;
    while (GetMessageW(&msg, nullptr, 0, 0)) {
        TranslateMessage(&msg);
        DispatchMessageW(&msg);
    }
    logger::Info("window: message loop exited");
}

void WebView::quit()
{
    logger::Info("window: quit called");
    PostQuitMessage(0);
}

void WebView::set_title(std::string_view title)
{
    logger::Info("window: set_title %.*s", (int)title.size(), title.data());
    SetWindowTextW(impl_->hwnd, win::to_wide(title).c_str());
}

void WebView::set_size(int width, int height)
{
    logger::Info("window: set_size %dx%d", width, height);
    SetWindowPos(impl_->hwnd, nullptr, 0, 0, width, height,
                 SWP_NOMOVE | SWP_NOZORDER);
}

} // namespace browser