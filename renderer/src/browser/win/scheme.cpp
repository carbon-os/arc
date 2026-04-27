#include "scheme.h"
#include "impl.h"
#include "wstring.h"
#include "browser/shared/mime.h"
#include "logger.h"

#include <ole2.h>
#include <wrl/client.h>

#include <filesystem>
#include <fstream>
#include <string>
#include <vector>

using Microsoft::WRL::ComPtr;

static std::vector<uint8_t> read_stream(IStream* stream)
{
    if (!stream) return {};
    std::vector<uint8_t> out;
    uint8_t buf[4096];
    ULONG   read = 0;
    while (SUCCEEDED(stream->Read(buf, sizeof(buf), &read)) && read > 0)
        out.insert(out.end(), buf, buf + read);
    return out;
}

static ComPtr<IStream> make_stream(const std::vector<uint8_t>& data)
{
    HGLOBAL hg = GlobalAlloc(GMEM_MOVEABLE, data.size());
    if (!hg) return nullptr;
    void* ptr = GlobalLock(hg);
    if (ptr) { memcpy(ptr, data.data(), data.size()); GlobalUnlock(hg); }
    IStream* stream = nullptr;
    if (FAILED(CreateStreamOnHGlobal(hg, TRUE, &stream))) { GlobalFree(hg); return nullptr; }
    return ComPtr<IStream>(stream);
}

HRESULT handle_resource_request(
    browser::WebViewImpl* impl,
    ICoreWebView2WebResourceRequestedEventArgs* args)
{
    ComPtr<ICoreWebView2WebResourceRequest> req;
    args->get_Request(&req);

    LPWSTR raw_uri = nullptr;
    req->get_Uri(&raw_uri);
    std::string uri = browser::win::to_utf8(raw_uri);
    CoTaskMemFree(raw_uri);

    std::string path = uri.substr(9); // strip "ui-ipc://"

    auto first_slash = path.find('/');
    if (first_slash == std::string::npos) return S_OK;

    const std::string authority    = path.substr(0, first_slash);
    const std::string rest         = path.substr(first_slash + 1);
    const auto        second_slash = rest.find('/');

    logger::Info("scheme: request authority=%s rest=%s",
                 authority.c_str(), rest.c_str());

    // ── ui-ipc://app ─────────────────────────────────────────────────────────

    if (authority == "app") {
        std::lock_guard<std::mutex> lock(impl->load_mutex);

        switch (impl->load_mode) {

        case browser::LoadMode::Html: {
            if (rest.empty() || rest == "/" || rest.back() == '/') {
                logger::Info("scheme: serving inline HTML %zu chars",
                             impl->html_src.size());
                auto stream = make_stream({
                    impl->html_src.begin(),
                    impl->html_src.end()});
                ComPtr<ICoreWebView2WebResourceResponse> response;
                impl->env->CreateWebResourceResponse(
                    stream.Get(), 200, L"OK",
                    L"Content-Type: text/html; charset=utf-8",
                    &response);
                args->put_Response(response.Get());
            }
            return S_OK;
        }

        case browser::LoadMode::File: {
            namespace fs = std::filesystem;
            fs::path requested = (fs::path(impl->file_root) / rest).lexically_normal();
            auto     root_abs  = fs::absolute(impl->file_root);
            auto     req_abs   = fs::absolute(requested);

            if (req_abs.string().rfind(root_abs.string(), 0) != 0) {
                logger::Warn("scheme: path traversal blocked: %s",
                             req_abs.string().c_str());
                return S_OK;
            }

            std::ifstream f(requested, std::ios::binary);
            if (!f) {
                logger::Warn("scheme: file not found: %s",
                             requested.string().c_str());
                return S_OK;
            }

            std::vector<uint8_t> data(
                (std::istreambuf_iterator<char>(f)),
                 std::istreambuf_iterator<char>());

            std::string  ext  = requested.extension().string();
            std::string  mime = browser::mime_for_ext(ext);

            logger::Info("scheme: serving file %s mime=%s bytes=%zu",
                         requested.filename().string().c_str(),
                         mime.c_str(), data.size());

            std::wstring content_type_header =
                browser::win::to_wide("Content-Type: " + mime);

            auto stream = make_stream(data);
            ComPtr<ICoreWebView2WebResourceResponse> response;
            impl->env->CreateWebResourceResponse(
                stream.Get(), 200, L"OK",
                content_type_header.c_str(),
                &response);
            args->put_Response(response.Get());
            return S_OK;
        }

        default:
            return S_OK;
        }
    }

    // ── ui-ipc://js ──────────────────────────────────────────────────────────

    if (authority == "js") {
        if (second_slash == std::string::npos) return S_OK;

        const std::string verb    = rest.substr(0, second_slash);
        const std::string channel = rest.substr(second_slash + 1);

        ComPtr<IStream> body_stream;
        req->get_Content(&body_stream);
        std::vector<uint8_t> bytes = read_stream(body_stream.Get());

        logger::Info("scheme: js/%s channel=%s bytes=%zu",
                     verb.c_str(), channel.c_str(), bytes.size());

        if (verb == "text") {
            if (impl->on_ipc_text_cb)
                impl->on_ipc_text_cb(
                    channel,
                    std::string_view(reinterpret_cast<const char*>(bytes.data()), bytes.size()));
        } else if (verb == "binary") {
            if (impl->on_ipc_binary_cb)
                impl->on_ipc_binary_cb(channel, bytes);
        } else {
            logger::Warn("scheme: unknown js verb=%s", verb.c_str());
        }

        ComPtr<ICoreWebView2WebResourceResponse> response;
        impl->env->CreateWebResourceResponse(
            nullptr, 204, L"No Content",
            L"Access-Control-Allow-Origin: *",
            &response);
        args->put_Response(response.Get());
        return S_OK;
    }

    // ── ui-ipc://host ────────────────────────────────────────────────────────

    if (authority == "host") {
        if (second_slash == std::string::npos) return S_OK;

        const std::string verb = rest.substr(0, second_slash);
        const std::string tail = rest.substr(second_slash + 1);

        if (verb == "message") {
            auto token_slash = tail.find('/');
            if (token_slash == std::string::npos) return S_OK;

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
                    logger::Info("scheme: host/message slot hit channel=%s token=%s bytes=%zu",
                                 channel.c_str(), token_str.c_str(), data.size());
                } else {
                    logger::Warn("scheme: host/message slot miss channel=%s token=%s",
                                 channel.c_str(), token_str.c_str());
                }
            }

            ComPtr<IStream> stream = make_stream(data);
            ComPtr<ICoreWebView2WebResourceResponse> response;
            impl->env->CreateWebResourceResponse(
                stream.Get(), 200, L"OK",
                L"Content-Type: application/octet-stream\r\n"
                L"Access-Control-Allow-Origin: *",
                &response);
            args->put_Response(response.Get());
            return S_OK;
        }
    }

    logger::Warn("scheme: unhandled request authority=%s", authority.c_str());
    return S_OK;
}