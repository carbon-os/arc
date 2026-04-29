#include "impl.h"
#include "scheme.h"
#include "shim.h"
#include "logger.h"

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

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

    NSWindowStyleMask style = NSWindowStyleMaskTitled
                            | NSWindowStyleMaskClosable
                            | NSWindowStyleMaskResizable
                            | NSWindowStyleMaskMiniaturizable;

    impl_->window = [[NSWindow alloc]
                         initWithContentRect:frame
                                   styleMask:style
                                     backing:NSBackingStoreBuffered
                                       defer:NO];

    [impl_->window setTitle:[NSString stringWithUTF8String:cfg.title.c_str()]];
    [impl_->window setContentView:impl_->webview];
    [impl_->window center];

    ArcWindowDelegate* delegate = [[ArcWindowDelegate alloc] init];
    delegate.impl               = impl_;
    impl_->window_delegate      = delegate;
    [impl_->window setDelegate:delegate];

    [impl_->window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];

    logger::Info("WebView: window created %dx%d title=%s",
                 cfg.width, cfg.height, cfg.title.c_str());

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