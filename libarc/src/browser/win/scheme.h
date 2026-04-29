#pragma once

#ifndef WIN32_LEAN_AND_MEAN
#  define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#include <ole2.h>
#include <WebView2.h>

namespace browser {
    struct WebViewImpl;
}

HRESULT handle_resource_request(
    browser::WebViewImpl* impl,
    ICoreWebView2WebResourceRequestedEventArgs* args);