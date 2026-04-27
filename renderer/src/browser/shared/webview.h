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

    void load_html(std::string_view html);
    void load_file(std::string_view path);
    void load_url(std::string_view url);
    void eval(std::string_view js);
    void set_title(std::string_view title);
    void set_size(int width, int height);

    void post_text(std::string_view channel, std::string_view text);
    void post_binary(std::string_view channel, const std::vector<uint8_t>& data);

    void drain_post_queue();
    void drain_cmd_queue();

private:
    void execute_frame(const InboundFrame& f);

    WebViewImpl* impl_;
};

} // namespace browser