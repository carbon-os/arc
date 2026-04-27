#include "impl.h"
#include "scheme.h"
#include "shim.h"
#include "logger.h"

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <JavaScriptCore/JavaScript.h>

#include <sstream>
#include <stdexcept>
#include <string>

namespace browser {

// ── Signal callbacks ──────────────────────────────────────────────────────────

static gboolean on_delete_event(GtkWidget*, GdkEvent*, gpointer user_data)
{
    auto* impl = static_cast<WebViewImpl*>(user_data);
    logger::Info("WebView: delete-event");
    if (impl->on_closed_cb)
        impl->on_closed_cb();
    return FALSE;
}

// Deferred on_ready — fires on the first main-loop iteration after construction
// so that the caller has a chance to set callbacks before they trigger.
static gboolean fire_ready(gpointer user_data)
{
    auto* impl = static_cast<WebViewImpl*>(user_data);
    logger::Info("WebView: firing on_ready");
    if (impl->on_ready_cb)
        impl->on_ready_cb();
    return G_SOURCE_REMOVE;
}

// ── Constructor ───────────────────────────────────────────────────────────────

WebView::WebView(const WindowConfig& cfg) : impl_(new WebViewImpl())
{
    impl_->owner = this;

    static bool gtk_inited = false;
    if (!gtk_inited) {
        int argc = 0;
        gtk_init(&argc, nullptr);
        gtk_inited = true;
        logger::Info("WebView: GTK initialised");
    }

    // ── WebKit context ────────────────────────────────────────────────────────

    WebKitWebContext* context = webkit_web_context_new();

    // Register ui-ipc:// scheme; user_data is impl_ (owned by this WebView).
    webkit_web_context_register_uri_scheme(
        context, "ui-ipc",
        handle_uri_scheme_request,
        impl_, nullptr);

    // Mark the scheme as secure so fetch() works inside it.
    // CORS registration is no longer needed — everything is same-origin
    // under ui-ipc://app/ now that the binary-slot route moved there too.
    WebKitSecurityManager* sm = webkit_web_context_get_security_manager(context);
    webkit_security_manager_register_uri_scheme_as_secure(sm, "ui-ipc");

    logger::Info("WebView: custom scheme ui-ipc registered");

    // ── User content manager ──────────────────────────────────────────────────

    WebKitUserContentManager* manager = webkit_user_content_manager_new();

    // Inject the IPC shim before any page script runs.
    // JS→C++ messages now arrive via POST to ui-ipc://app/-/js/{verb}/{channel}
    // so we no longer need the "ipc" webkit messageHandler or its signal.
    WebKitUserScript* shim_script = webkit_user_script_new(
        browser::gtk::k_shim,
        WEBKIT_USER_CONTENT_INJECT_ALL_FRAMES,
        WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START,
        nullptr, nullptr);
    webkit_user_content_manager_add_script(manager, shim_script);
    webkit_user_script_unref(shim_script);
    logger::Info("WebView: IPC shim injected");

    // ── WebKitWebView ─────────────────────────────────────────────────────────

    impl_->webview = WEBKIT_WEB_VIEW(g_object_new(
        WEBKIT_TYPE_WEB_VIEW,
        "web-context",          context,
        "user-content-manager", manager,
        nullptr));

    g_object_unref(context);
    g_object_unref(manager);

    if (cfg.debug) {
        WebKitSettings* settings = webkit_web_view_get_settings(impl_->webview);
        webkit_settings_set_enable_developer_extras(settings, TRUE);
        logger::Info("WebView: DevTools enabled");
    }

    // ── GTK window ────────────────────────────────────────────────────────────

    impl_->window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(impl_->window), cfg.title.c_str());
    gtk_window_set_default_size(GTK_WINDOW(impl_->window), cfg.width, cfg.height);
    gtk_container_add(GTK_CONTAINER(impl_->window), GTK_WIDGET(impl_->webview));

    g_signal_connect(impl_->window, "delete-event",
                     G_CALLBACK(on_delete_event), impl_);
    g_signal_connect(impl_->window, "destroy",
                     G_CALLBACK(+[](GtkWidget*, gpointer) { gtk_main_quit(); }),
                     nullptr);

    gtk_widget_show_all(impl_->window);
    logger::Info("WebView: window created %dx%d title=%s",
                 cfg.width, cfg.height, cfg.title.c_str());

    g_idle_add(fire_ready, impl_);
}

WebView::~WebView() { delete impl_; }

// ── Public interface ──────────────────────────────────────────────────────────

void WebView::eval(std::string_view js)
{
    logger::Info("WebView: eval %zu chars", js.size());
    webkit_web_view_evaluate_javascript(
        impl_->webview,
        std::string(js).c_str(), -1,
        nullptr, nullptr,
        nullptr,
        nullptr, nullptr);
}

void WebView::on_ready(ReadyCallback cb)          { impl_->on_ready_cb      = std::move(cb); }
void WebView::on_closed(ClosedCallback cb)        { impl_->on_closed_cb     = std::move(cb); }
void WebView::on_ipc_text(IpcTextCallback cb)     { impl_->on_ipc_text_cb   = std::move(cb); }
void WebView::on_ipc_binary(IpcBinaryCallback cb) { impl_->on_ipc_binary_cb = std::move(cb); }

void WebView::dispatch(InboundFrame frame)
{
    logger::Info("WebView: dispatch cmd=0x%02X", static_cast<uint8_t>(frame.type));
    {
        std::lock_guard lock(impl_->cmd_mutex);
        impl_->cmd_queue.push(std::move(frame));
    }
    g_idle_add([](gpointer p) -> gboolean {
        static_cast<WebView*>(p)->drain_cmd_queue();
        return G_SOURCE_REMOVE;
    }, this);
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