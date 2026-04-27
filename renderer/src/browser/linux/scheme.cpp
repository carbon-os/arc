#include "scheme.h"
#include "impl.h"
#include "browser/shared/mime.h"
#include "logger.h"

#include <filesystem>
#include <fstream>
#include <mutex>
#include <string>
#include <vector>

static void finish_ok(WebKitURISchemeRequest* request,
                      const std::vector<uint8_t>& data,
                      const std::string& mime)
{
    gpointer copy = g_memdup2(data.data(), data.size());
    GInputStream* stream = g_memory_input_stream_new_from_data(
        copy, static_cast<gssize>(data.size()), g_free);
    webkit_uri_scheme_request_finish(
        request, stream, static_cast<gint64>(data.size()), mime.c_str());
    g_object_unref(stream);
}

static void finish_no_content(WebKitURISchemeRequest* request)
{
    GInputStream* empty = g_memory_input_stream_new();
    webkit_uri_scheme_request_finish(request, empty, 0, "text/plain");
    g_object_unref(empty);
}

static void finish_error(WebKitURISchemeRequest* request, const char* msg)
{
    GError* err = g_error_new_literal(G_IO_ERROR, G_IO_ERROR_NOT_FOUND, msg);
    webkit_uri_scheme_request_finish_error(request, err);
    g_error_free(err);
}

static std::vector<uint8_t> read_body(WebKitURISchemeRequest* request)
{
    GInputStream* stream = webkit_uri_scheme_request_get_http_body(request);
    if (!stream) return {};
    std::vector<uint8_t> out;
    uint8_t buf[4096];
    while (true) {
        gsize   n   = 0;
        GError* err = nullptr;
        gboolean ok = g_input_stream_read_all(stream, buf, sizeof(buf), &n, nullptr, &err);
        if (err) { g_error_free(err); break; }
        if (n > 0) out.insert(out.end(), buf, buf + n);
        if (!ok || n < sizeof(buf)) break;
    }
    return out;
}

void handle_uri_scheme_request(WebKitURISchemeRequest* request, gpointer user_data)
{
    auto* impl = static_cast<browser::WebViewImpl*>(user_data);

    const char* raw_uri = webkit_uri_scheme_request_get_uri(request);
    std::string uri(raw_uri ? raw_uri : "");

    // Strip "ui-ipc://" (9 chars)
    if (uri.size() < 9) { finish_error(request, "bad uri"); return; }
    std::string path = uri.substr(9);

    auto first_slash = path.find('/');
    if (first_slash == std::string::npos) { finish_error(request, "no path"); return; }

    const std::string authority = path.substr(0, first_slash);
    const std::string rest      = path.substr(first_slash + 1);

    logger::Info("scheme: request authority=%s rest=%s",
                 authority.c_str(), rest.c_str());

    if (authority != "app") {
        logger::Warn("scheme: unhandled authority '%s'", authority.c_str());
        finish_error(request, "unknown authority");
        return;
    }

    // ── /-/js/{text|binary}/{channel}  — JS → C++ ────────────────────────────

    if (rest.rfind("-/js/", 0) == 0) {
        const std::string ipc    = rest.substr(5);
        const auto        vslash = ipc.find('/');
        if (vslash == std::string::npos) { finish_no_content(request); return; }

        const std::string verb    = ipc.substr(0, vslash);
        const std::string channel = ipc.substr(vslash + 1);
        std::vector<uint8_t> bytes = read_body(request);

        logger::Info("scheme: JS→C++ verb=%s channel=%s bytes=%zu",
                     verb.c_str(), channel.c_str(), bytes.size());

        if (verb == "text") {
            std::string text(bytes.begin(), bytes.end());
            if (impl->on_ipc_text_cb)
                impl->on_ipc_text_cb(channel, text);
        } else {
            if (impl->on_ipc_binary_cb)
                impl->on_ipc_binary_cb(channel, bytes);
        }

        finish_no_content(request);
        return;
    }

    // ── /-/host/message/{channel}/{token}  — C++ → JS binary fetch ───────────

    if (rest.rfind("-/host/message/", 0) == 0) {
        const std::string tail        = rest.substr(15);
        const auto        token_slash = tail.find('/');
        if (token_slash == std::string::npos) { finish_no_content(request); return; }

        const std::string channel   = tail.substr(0, token_slash);
        const std::string token_str = tail.substr(token_slash + 1);
        const std::string slot_key  = channel + ":" + token_str;

        std::vector<uint8_t> data;
        {
            std::lock_guard<std::mutex> lock(impl->slots_mutex);
            auto it = impl->slots.find(slot_key);
            if (it != impl->slots.end()) {
                data = std::move(it->second);
                impl->slots.erase(it);
                logger::Info("scheme: binary slot hit channel=%s token=%s bytes=%zu",
                             channel.c_str(), token_str.c_str(), data.size());
            } else {
                logger::Warn("scheme: binary slot miss channel=%s token=%s",
                             channel.c_str(), token_str.c_str());
            }
        }

        finish_ok(request, data, "application/octet-stream");
        return;
    }

    // ── App content (Html / File) ─────────────────────────────────────────────

    std::lock_guard<std::mutex> lock(impl->load_mutex);

    switch (impl->load_mode) {

    case browser::LoadMode::Html: {
        if (rest.empty() || rest == "/" || rest.back() == '/') {
            logger::Info("scheme: serving inline HTML %zu chars", impl->html_src.size());
            finish_ok(request,
                { impl->html_src.begin(), impl->html_src.end() },
                "text/html; charset=utf-8");
        } else {
            finish_error(request, "not found");
        }
        return;
    }

    case browser::LoadMode::File: {
        namespace fs = std::filesystem;
        fs::path requested = (fs::path(impl->file_root) / rest).lexically_normal();
        auto root_abs = fs::absolute(impl->file_root);
        auto req_abs  = fs::absolute(requested);

        if (req_abs.string().rfind(root_abs.string(), 0) != 0) {
            logger::Warn("scheme: path traversal blocked: %s", req_abs.string().c_str());
            GError* err = g_error_new_literal(
                G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED, "forbidden");
            webkit_uri_scheme_request_finish_error(request, err);
            g_error_free(err);
            return;
        }

        std::ifstream f(requested, std::ios::binary);
        if (!f) {
            logger::Warn("scheme: file not found: %s", requested.string().c_str());
            finish_error(request, "not found");
            return;
        }

        std::vector<uint8_t> data(
            (std::istreambuf_iterator<char>(f)),
             std::istreambuf_iterator<char>());

        std::string mime = browser::mime_for_ext(requested.extension().string());
        logger::Info("scheme: serving file %s mime=%s bytes=%zu",
                     requested.filename().string().c_str(), mime.c_str(), data.size());
        finish_ok(request, data, mime);
        return;
    }

    default:
        finish_error(request, "not ready");
        return;
    }
}