#include "impl.h"
#include "scheme.h"
#include "shim.h"
#include "logger.h"

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

#include <filesystem>
#include <stdexcept>
#include <string>

// ── Window delegate ───────────────────────────────────────────────────────────

@interface ArcWindowDelegate : NSObject <NSWindowDelegate>
@property (assign) browser::WebViewImpl* impl;
@end

@implementation ArcWindowDelegate

- (void)windowWillClose:(NSNotification*)notification
{
    logger::Info("WebView: windowWillClose");
    if (self.impl->on_closed_cb)
        self.impl->on_closed_cb();
}

@end

// ── Coordinate helper ─────────────────────────────────────────────────────────
//
// Converts Go window-relative coordinates (origin top-left, Y down) to the
// screen-coordinate NSRect expected by NSWindow/NSPanel on macOS
// (origin bottom-left, Y up).

static NSRect embed_screen_rect(const browser::WebViewImpl* impl,
                                int x, int y, int width, int height)
{
    NSRect content = [impl->window contentRectForFrameRect:impl->window.frame];
    CGFloat flipped_y = content.origin.y + content.size.height
                        - static_cast<CGFloat>(y)
                        - static_cast<CGFloat>(height);
    return NSMakeRect(
        content.origin.x + static_cast<CGFloat>(x),
        flipped_y,
        static_cast<CGFloat>(width),
        static_cast<CGFloat>(height));
}

// ── WebView ───────────────────────────────────────────────────────────────────

namespace browser {

WebView::WebView(const WindowConfig& cfg) : impl_(new WebViewImpl())
{
    impl_->owner = this;

    [NSApplication sharedApplication];
    [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];

    // ── WKWebViewConfiguration ────────────────────────────────────────────────

    WKWebViewConfiguration* config = [[WKWebViewConfiguration alloc] init];

    ArcSchemeHandler* handler = [[ArcSchemeHandler alloc] init];
    handler.impl = impl_;
    [config setURLSchemeHandler:handler forURLScheme:@"ui-ipc"];

    NSString*     shimSrc    = [NSString stringWithUTF8String:browser::mac::k_shim];
    WKUserScript* shimScript = [[WKUserScript alloc]
                                    initWithSource:shimSrc
                                     injectionTime:WKUserScriptInjectionTimeAtDocumentStart
                                  forMainFrameOnly:NO];
    [config.userContentController addUserScript:shimScript];
    logger::Info("WebView: IPC shim injected");

    if (cfg.debug) {
        [config.preferences setValue:@YES forKey:@"developerExtrasEnabled"];
        logger::Info("WebView: DevTools enabled");
    }

    // ── WKWebView ─────────────────────────────────────────────────────────────

    NSRect frame   = NSMakeRect(0, 0, cfg.width, cfg.height);
    impl_->webview = [[WKWebView alloc] initWithFrame:frame configuration:config];

    // ── NSWindow ──────────────────────────────────────────────────────────────

    const bool hidden_bar = (cfg.titleBarStyle == TitleBarStyle::Hidden);

    NSWindowStyleMask style = NSWindowStyleMaskTitled
                            | NSWindowStyleMaskClosable
                            | NSWindowStyleMaskResizable
                            | NSWindowStyleMaskMiniaturizable;

    if (hidden_bar)
        style |= NSWindowStyleMaskFullSizeContentView;

    impl_->window = [[NSWindow alloc]
                         initWithContentRect:frame
                                   styleMask:style
                                     backing:NSBackingStoreBuffered
                                       defer:NO];

    if (hidden_bar) {
        impl_->window.titleVisibility           = NSWindowTitleHidden;
        impl_->window.titlebarAppearsTransparent = YES;
        logger::Info("WebView: titleBarStyle=Hidden");
    }

    [impl_->window setTitle:[NSString stringWithUTF8String:cfg.title.c_str()]];
    [impl_->window setContentView:impl_->webview];
    [impl_->window center];

    ArcWindowDelegate* delegate = [[ArcWindowDelegate alloc] init];
    delegate.impl               = impl_;
    impl_->window_delegate      = delegate;
    [impl_->window setDelegate:delegate];

    [impl_->window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];

    logger::Info("WebView: window created %dx%d title=%s titleBarStyle=%d",
                 cfg.width, cfg.height, cfg.title.c_str(),
                 static_cast<int>(cfg.titleBarStyle));

    WebViewImpl* impl = impl_;
    dispatch_async(dispatch_get_main_queue(), ^{
        logger::Info("WebView: firing on_ready");
        if (impl->on_ready_cb)
            impl->on_ready_cb();
    });
}

WebView::~WebView() { delete impl_; }

void WebView::on_ready(ReadyCallback cb)          { impl_->on_ready_cb      = std::move(cb); }
void WebView::on_closed(ClosedCallback cb)        { impl_->on_closed_cb     = std::move(cb); }
void WebView::on_ipc_text(IpcTextCallback cb)     { impl_->on_ipc_text_cb   = std::move(cb); }
void WebView::on_ipc_binary(IpcBinaryCallback cb) { impl_->on_ipc_binary_cb = std::move(cb); }

void WebView::eval(std::string_view js)
{
    logger::Info("WebView: eval %zu chars", js.size());
    NSString* script = [NSString stringWithUTF8String:std::string(js).c_str()];
    [impl_->webview evaluateJavaScript:script completionHandler:nil];
}

void WebView::dispatch(InboundFrame frame)
{
    logger::Info("WebView: dispatch cmd=0x%02X", static_cast<uint8_t>(frame.type));
    {
        std::lock_guard lock(impl_->cmd_mutex);
        impl_->cmd_queue.push(std::move(frame));
    }
    WebView* self = this;
    dispatch_async(dispatch_get_main_queue(), ^{
        self->drain_cmd_queue();
    });
}

void WebView::drain_cmd_queue()
{
    std::unique_lock lock(impl_->cmd_mutex);
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
    case Command::LoadFile:       load_file(f.str);                          break;
    case Command::LoadHTML:       load_html(f.str);                          break;
    case Command::LoadURL:        load_url(f.str);                           break;
    case Command::Eval:           eval(f.str);                               break;
    case Command::SetTitle:       set_title(f.str);                          break;
    case Command::SetSize:        set_size(f.width, f.height);               break;
    case Command::PostText:       post_text(f.channel, f.text);              break;
    case Command::PostBinary:     post_binary(f.channel, f.data);            break;
    case Command::WebViewCreate:  embed_create(f.wv_id, f.wv_x, f.wv_y,
                                               f.wv_width, f.wv_height,
                                               f.wv_zorder);                 break;
    case Command::WebViewLoadURL: embed_load_url(f.wv_id, f.str);            break;
    case Command::WebViewLoadFile:embed_load_file(f.wv_id, f.str);           break;
    case Command::WebViewLoadHTML:embed_load_html(f.wv_id, f.str);           break;
    case Command::WebViewShow:    embed_show(f.wv_id);                       break;
    case Command::WebViewHide:    embed_hide(f.wv_id);                       break;
    case Command::WebViewMove:    embed_move(f.wv_id, f.wv_x, f.wv_y);      break;
    case Command::WebViewResize:  embed_resize(f.wv_id, f.wv_width,
                                               f.wv_height);                 break;
    case Command::WebViewSetBounds: embed_set_bounds(f.wv_id, f.wv_x,
                                                     f.wv_y, f.wv_width,
                                                     f.wv_height);           break;
    case Command::WebViewSetZOrder: embed_set_zorder(f.wv_id, f.wv_zorder);  break;
    case Command::WebViewDestroy: embed_destroy(f.wv_id);                    break;
    default:
        logger::Warn("WebView: execute_frame unknown cmd=0x%02X",
                     static_cast<uint8_t>(f.type));
        break;
    }
}

// ── Embedded web view management ──────────────────────────────────────────────

void WebView::embed_create(uint32_t id, int x, int y, int width, int height, int zorder)
{
    if (impl_->embeds.count(id)) {
        logger::Warn("WebView: embed_create duplicate id=%u", id);
        return;
    }

    NSRect screen = embed_screen_rect(impl_, x, y, width, height);

    NSPanel* panel = [[NSPanel alloc]
                          initWithContentRect:screen
                                    styleMask:NSWindowStyleMaskBorderless
                                      backing:NSBackingStoreBuffered
                                        defer:NO];
    panel.opaque              = YES;
    panel.hasShadow           = NO;
    panel.backgroundColor     = NSColor.clearColor;
    panel.movableByWindowBackground = NO;

    WKWebViewConfiguration* cfg = [[WKWebViewConfiguration alloc] init];
    cfg.websiteDataStore = [WKWebsiteDataStore nonPersistentDataStore];

    WKWebView* wv = [[WKWebView alloc]
                         initWithFrame:NSMakeRect(0, 0, width, height)
                         configuration:cfg];
    [panel setContentView:wv];

    [impl_->window addChildWindow:panel ordered:NSWindowAbove];

    EmbeddedWebView ev;
    ev.panel   = panel;
    ev.webview = wv;
    ev.zorder  = zorder;
    impl_->embeds[id] = ev;

    embed_restack();

    logger::Info("WebView: embed_create id=%u x=%d y=%d w=%d h=%d z=%d",
                 id, x, y, width, height, zorder);
}

void WebView::embed_load_url(uint32_t id, std::string_view url)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_load_url unknown id=%u", id); return; }
    NSURL* nsurl = [NSURL URLWithString:[NSString stringWithUTF8String:std::string(url).c_str()]];
    [it->second.webview loadRequest:[NSURLRequest requestWithURL:nsurl]];
}

void WebView::embed_load_file(uint32_t id, std::string_view path)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_load_file unknown id=%u", id); return; }
    namespace fs = std::filesystem;
    fs::path p   = fs::absolute(fs::path(path).lexically_normal());
    NSURL* base  = [NSURL fileURLWithPath:[NSString stringWithUTF8String:p.parent_path().string().c_str()]
                              isDirectory:YES];
    NSURL* file  = [NSURL fileURLWithPath:[NSString stringWithUTF8String:p.string().c_str()]];
    [it->second.webview loadFileURL:file allowingReadAccessToURL:base];
}

void WebView::embed_load_html(uint32_t id, std::string_view html)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_load_html unknown id=%u", id); return; }
    NSString* src = [NSString stringWithUTF8String:std::string(html).c_str()];
    [it->second.webview loadHTMLString:src baseURL:nil];
}

void WebView::embed_show(uint32_t id)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_show unknown id=%u", id); return; }
    [it->second.panel orderFront:nil];
}

void WebView::embed_hide(uint32_t id)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_hide unknown id=%u", id); return; }
    [it->second.panel orderOut:nil];
}

void WebView::embed_move(uint32_t id, int x, int y)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_move unknown id=%u", id); return; }
    NSRect cur   = it->second.panel.frame;
    NSRect screen = embed_screen_rect(impl_, x, y,
                                      static_cast<int>(cur.size.width),
                                      static_cast<int>(cur.size.height));
    [it->second.panel setFrameOrigin:screen.origin];
}

void WebView::embed_resize(uint32_t id, int width, int height)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_resize unknown id=%u", id); return; }
    [it->second.panel setContentSize:NSMakeSize(width, height)];
}

void WebView::embed_set_bounds(uint32_t id, int x, int y, int width, int height)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_set_bounds unknown id=%u", id); return; }
    NSRect screen = embed_screen_rect(impl_, x, y, width, height);
    [it->second.panel setFrame:screen display:YES];
}

void WebView::embed_set_zorder(uint32_t id, int zorder)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_set_zorder unknown id=%u", id); return; }
    it->second.zorder = zorder;
    embed_restack();
}

void WebView::embed_destroy(uint32_t id)
{
    auto it = impl_->embeds.find(id);
    if (it == impl_->embeds.end()) { logger::Warn("WebView: embed_destroy unknown id=%u", id); return; }
    [impl_->window removeChildWindow:it->second.panel];
    [it->second.panel orderOut:nil];
    impl_->embeds.erase(it);
    logger::Info("WebView: embed_destroy id=%u", id);
}

void WebView::embed_restack()
{
    // Collect and sort by zorder ascending (lower z = further back).
    std::vector<std::pair<int, NSPanel*>> ordered;
    ordered.reserve(impl_->embeds.size());
    for (auto& [id, ev] : impl_->embeds)
        ordered.emplace_back(ev.zorder, ev.panel);
    std::sort(ordered.begin(), ordered.end(),
              [](const auto& a, const auto& b){ return a.first < b.first; });

    for (auto& [z, panel] : ordered)
        [panel orderWindow:NSWindowAbove relativeTo:impl_->window.windowNumber];
}

} // namespace browser