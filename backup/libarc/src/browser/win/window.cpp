#include "impl.h"
#include "wstring.h"
#include "logger.h"

#include <dwmapi.h>
#include <string>

namespace browser {

static constexpr UINT kMsgFlush   = WM_APP + 1;
static constexpr UINT kMsgCommand = WM_APP + 2;

LRESULT CALLBACK wnd_proc(HWND hwnd, UINT msg, WPARAM wp, LPARAM lp)
{
    auto* impl = reinterpret_cast<WebViewImpl*>(
        GetWindowLongPtrW(hwnd, GWLP_USERDATA));

    // ── Hidden title bar: remove the 1px top remnant left by WS_THICKFRAME ───
    //
    // When WS_CAPTION is absent, Windows still leaves a 1-pixel non-client
    // region at the top. Intercepting WM_NCCALCSIZE and returning 0 makes the
    // client area fill the entire window rect. WS_THICKFRAME continues to
    // provide resize hit-testing and DWM shadow independently of this.

    if (msg == WM_NCCALCSIZE && wp == TRUE) {
        if (impl && impl->titlebar_style == TitleBarStyle::Hidden) {
            // Returning 0 tells Windows: client area == proposed window rect.
            // No caption bar, no top border line.
            return 0;
        }
    }

    // ── Hidden title bar: restore resize hit-testing on all four edges ───────
    //
    // Because WM_NCCALCSIZE returns 0, the OS believes the non-client area is
    // zero-sized and won't fire resize hit-tests from the default handler.
    // We replicate them manually for the four resize edges and four corners.

    if (msg == WM_NCHITTEST) {
        if (impl && impl->titlebar_style == TitleBarStyle::Hidden) {
            // Let DWM handle its own shadow / snap zones first.
            LRESULT hit = DefWindowProcW(hwnd, msg, wp, lp);
            if (hit != HTCLIENT)
                return hit;

            // Map the cursor to window-local coords.
            POINT pt{ GET_X_LPARAM(lp), GET_Y_LPARAM(lp) };
            ScreenToClient(hwnd, &pt);

            RECT rc;
            GetClientRect(hwnd, &rc);

            // Use the system resize border thickness so it scales with DPI.
            const int border = GetSystemMetrics(SM_CXSIZEFRAME)
                             + GetSystemMetrics(SM_CXPADDEDBORDER);

            const bool on_left   = pt.x <  border;
            const bool on_right  = pt.x >= rc.right  - border;
            const bool on_top    = pt.y <  border;
            const bool on_bottom = pt.y >= rc.bottom - border;

            if (on_top    && on_left)  return HTTOPLEFT;
            if (on_top    && on_right) return HTTOPRIGHT;
            if (on_bottom && on_left)  return HTBOTTOMLEFT;
            if (on_bottom && on_right) return HTBOTTOMRIGHT;
            if (on_top)                return HTTOP;
            if (on_bottom)             return HTBOTTOM;
            if (on_left)               return HTLEFT;
            if (on_right)              return HTRIGHT;

            return HTCLIENT;
        }
    }

    // ── Standard message handling ─────────────────────────────────────────────

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