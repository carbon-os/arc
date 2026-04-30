#pragma once

#include <webkit2/webkit2.h>
#include <glib.h>

namespace browser { struct WebViewImpl; }

// Registered with webkit_web_context_register_uri_scheme; user_data is WebViewImpl*.
void handle_uri_scheme_request(WebKitURISchemeRequest* request, gpointer user_data);