#include "impl.h"
#include "browser/shared/webview.h"
#include "logger.h"

#include <filesystem>
#include <mutex>
#include <stdexcept>
#include <string>

namespace browser {

void WebView::load_html(std::string_view html)
{
    logger::Info("navigation: load_html %zu chars", html.size());
    {
        std::lock_guard lock(impl_->load_mutex);
        impl_->load_mode = LoadMode::Html;
        impl_->html_src  = std::string(html);
        impl_->file_root.clear();
        impl_->file_entry.clear();
    }
    // Navigate to the custom scheme root; scheme.cpp will serve the HTML.
    webkit_web_view_load_uri(impl_->webview, "ui-ipc://app/");
}

void WebView::load_file(std::string_view path)
{
    namespace fs = std::filesystem;
    fs::path p = fs::absolute(fs::path(path).lexically_normal());

    logger::Info("navigation: load_file %s", p.string().c_str());
    {
        std::lock_guard lock(impl_->load_mutex);
        impl_->load_mode  = LoadMode::File;
        impl_->html_src.clear();
        impl_->file_root  = p.parent_path().string();
        impl_->file_entry = p.filename().string();
    }

    std::string url = "ui-ipc://app/" + p.filename().string();
    webkit_web_view_load_uri(impl_->webview, url.c_str());
}

void WebView::load_url(std::string_view url)
{
    if (url.starts_with("ui-ipc://"))
        throw std::invalid_argument("browser::WebView: load_url does not accept ui-ipc:// URLs");

    logger::Info("navigation: load_url %.*s", (int)url.size(), url.data());
    {
        std::lock_guard lock(impl_->load_mutex);
        impl_->load_mode = LoadMode::None;
        impl_->html_src.clear();
        impl_->file_root.clear();
        impl_->file_entry.clear();
    }
    webkit_web_view_load_uri(impl_->webview, std::string(url).c_str());
}

} // namespace browser