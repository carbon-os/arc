#pragma once

#include "host_channel.h"
#include <functional>
#include <string>
#include <string_view>
#include <vector>

namespace browser {

struct WebViewImpl;

class WebView {
public:
    using ReadyCallback     = std::function<void()>;
    using ClosedCallback    = std::function<void()>;
    using IpcTextCallback   = std::function<void(std::string_view, std::string_view)>;
    using IpcBinaryCallback = std::function<void(std::string_view, const std::vector<uint8_t>&)>;

    explicit WebView(const WindowConfig& cfg);
    ~WebView();

    WebView(const WebView&)            = delete;
    WebView& operator=(const WebView&) = delete;

    void on_ready(ReadyCallback cb);
    void on_closed(ClosedCallback cb);
    void on_ipc_text(IpcTextCallback cb);
    void on_ipc_binary(IpcBinaryCallback cb);

    void run();
    void quit();

    void dispatch(InboundFrame frame);

    // Main window navigation
    void load_html(std::string_view html);
    void load_file(std::string_view path);
    void load_url(std::string_view url);
    void eval(std::string_view js);
    void set_title(std::string_view title);
    void set_size(int width, int height);

    // Main window IPC
    void post_text(std::string_view channel, std::string_view text);
    void post_binary(std::string_view channel, const std::vector<uint8_t>& data);

    void drain_post_queue();
    void drain_cmd_queue();

    // Embedded web view management — all must be called on the main thread.
    void embed_create(uint32_t id, int x, int y, int width, int height, int zorder);
    void embed_load_url(uint32_t id, std::string_view url);
    void embed_load_file(uint32_t id, std::string_view path);
    void embed_load_html(uint32_t id, std::string_view html);
    void embed_show(uint32_t id);
    void embed_hide(uint32_t id);
    void embed_move(uint32_t id, int x, int y);
    void embed_resize(uint32_t id, int width, int height);
    void embed_set_bounds(uint32_t id, int x, int y, int width, int height);
    void embed_set_zorder(uint32_t id, int zorder);
    void embed_destroy(uint32_t id);

private:
    void execute_frame(const InboundFrame& f);

    // Re-stack all child panels in ascending zorder using addChildWindow /
    // orderWindow. Call after any zorder change.
    void embed_restack();

    WebViewImpl* impl_;
};

} // namespace browser