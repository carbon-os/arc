#include "impl.h"
#include "browser/shared/webview.h"
#include "logger.h"

#import <WebKit/WebKit.h>
#import <Foundation/Foundation.h>

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
    NSURL*        url = [NSURL URLWithString:@"ui-ipc://app/"];
    NSURLRequest* req = [NSURLRequest requestWithURL:url];
    [impl_->webview loadRequest:req];
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

    std::string url_str = "ui-ipc://app/" + p.filename().string();
    NSURL*        url   = [NSURL URLWithString:[NSString stringWithUTF8String:url_str.c_str()]];
    NSURLRequest* req   = [NSURLRequest requestWithURL:url];
    [impl_->webview loadRequest:req];
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

    NSURL*        nsurl = [NSURL URLWithString:[NSString stringWithUTF8String:std::string(url).c_str()]];
    NSURLRequest* req   = [NSURLRequest requestWithURL:nsurl];
    [impl_->webview loadRequest:req];
}

} // namespace browser