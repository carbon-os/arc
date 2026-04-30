#include "scheme.h"
#include "impl.h"
#include "browser/shared/mime.h"
#include "logger.h"

#import <Foundation/Foundation.h>
#import <WebKit/WebKit.h>

#include <filesystem>
#include <fstream>
#include <string>
#include <vector>

// ── Body reader ───────────────────────────────────────────────────────────────

static std::vector<uint8_t> read_request_body(NSURLRequest* req)
{
    if (NSData* body = req.HTTPBody; body && body.length > 0) {
        const auto* p = static_cast<const uint8_t*>(body.bytes);
        return {p, p + body.length};
    }
    if (NSInputStream* stream = req.HTTPBodyStream) {
        [stream open];
        std::vector<uint8_t> out;
        uint8_t  buf[4096];
        NSInteger n;
        while ((n = [stream read:buf maxLength:sizeof(buf)]) > 0)
            out.insert(out.end(), buf, buf + n);
        [stream close];
        return out;
    }
    return {};
}

// ── Response helpers ──────────────────────────────────────────────────────────

static void finish_ok(id<WKURLSchemeTask> task, NSData* data, NSString* mime)
{
    NSHTTPURLResponse* response =
        [[NSHTTPURLResponse alloc] initWithURL:task.request.URL
                                    statusCode:200
                                   HTTPVersion:@"HTTP/1.1"
                                  headerFields:@{ @"Content-Type": mime,
                                                  @"Access-Control-Allow-Origin": @"*" }];
    [task didReceiveResponse:response];
    [task didReceiveData:data];
    [task didFinish];
}

static void finish_no_content(id<WKURLSchemeTask> task)
{
    NSHTTPURLResponse* response =
        [[NSHTTPURLResponse alloc] initWithURL:task.request.URL
                                    statusCode:204
                                   HTTPVersion:@"HTTP/1.1"
                                  headerFields:@{}];
    [task didReceiveResponse:response];
    [task didReceiveData:[NSData data]];
    [task didFinish];
}

static void finish_error(id<WKURLSchemeTask> task, NSInteger code, NSString* msg)
{
    NSError* err = [NSError errorWithDomain:NSURLErrorDomain
                                       code:code
                                   userInfo:@{ NSLocalizedDescriptionKey: msg }];
    [task didFailWithError:err];
}

// ── ArcSchemeHandler ──────────────────────────────────────────────────────────

@implementation ArcSchemeHandler

- (void)webView:(WKWebView*)webView startURLSchemeTask:(id<WKURLSchemeTask>)task
{
    NSURL*    url  = task.request.URL;
    NSString* host = url.host ?: @"";
    NSString* path = url.path ?: @"";

    NSString* rest = [path hasPrefix:@"/"] ? [path substringFromIndex:1] : path;

    logger::Info("scheme: request host=%s rest=%s",
                 host.UTF8String, rest.UTF8String);

    if (![host isEqualToString:@"app"]) {
        logger::Warn("scheme: unhandled host '%s'", host.UTF8String);
        finish_error(task, NSURLErrorFileDoesNotExist, @"unknown host");
        return;
    }

    // ── JS → C++ ─────────────────────────────────────────────────────────────
    // POST ui-ipc://app/-/js/{text|binary}/{channel}

    if ([rest hasPrefix:@"-/js/"]) {
        NSString* ipc   = [rest substringFromIndex:5];
        NSRange   slash = [ipc rangeOfString:@"/"];

        NSString* verb    = slash.location == NSNotFound ? ipc : [ipc substringToIndex:slash.location];
        NSString* channel = slash.location == NSNotFound ? @"" : [ipc substringFromIndex:slash.location + 1];

        std::vector<uint8_t> bytes = read_request_body(task.request);

        logger::Info("scheme: JS→C++ verb=%s channel=%s bytes=%zu",
                     verb.UTF8String, channel.UTF8String, bytes.size());

        if ([verb isEqualToString:@"text"]) {
            if (self.impl->on_ipc_text_cb)
                self.impl->on_ipc_text_cb(
                    channel.UTF8String,
                    std::string_view(reinterpret_cast<const char*>(bytes.data()), bytes.size()));
        } else if ([verb isEqualToString:@"binary"]) {
            if (self.impl->on_ipc_binary_cb)
                self.impl->on_ipc_binary_cb(channel.UTF8String, bytes);
        } else {
            logger::Warn("scheme: unknown JS verb=%s", verb.UTF8String);
        }

        finish_no_content(task);
        return;
    }

    // ── Binary slot fetch ─────────────────────────────────────────────────────
    // GET ui-ipc://app/-/host/message/{channel}/{token}

    if ([rest hasPrefix:@"-/host/message/"]) {
        NSString* tail      = [rest substringFromIndex:15];
        NSRange   tok_slash = [tail rangeOfString:@"/"];
        if (tok_slash.location == NSNotFound) { finish_no_content(task); return; }

        NSString*   channel   = [tail substringToIndex:tok_slash.location];
        NSString*   token_str = [tail substringFromIndex:tok_slash.location + 1];
        std::string slot_key  = std::string(channel.UTF8String)
                              + ":" + std::string(token_str.UTF8String);

        std::vector<uint8_t> data;
        {
            std::lock_guard<std::mutex> lock(self.impl->slots_mutex);
            auto it = self.impl->slots.find(slot_key);
            if (it != self.impl->slots.end()) {
                data = std::move(it->second);
                self.impl->slots.erase(it);
                logger::Info("scheme: binary slot hit channel=%s token=%s bytes=%zu",
                             channel.UTF8String, token_str.UTF8String, data.size());
            } else {
                logger::Warn("scheme: binary slot miss channel=%s token=%s",
                             channel.UTF8String, token_str.UTF8String);
            }
        }

        NSData* nsdata = [NSData dataWithBytes:data.data() length:data.size()];
        finish_ok(task, nsdata, @"application/octet-stream");
        return;
    }

    // ── App content (Html / File) ─────────────────────────────────────────────

    std::lock_guard<std::mutex> lock(self.impl->load_mutex);

    switch (self.impl->load_mode) {

    case browser::LoadMode::Html: {
        if (rest.length > 0 && [rest characterAtIndex:rest.length - 1] != '/') {
            finish_error(task, NSURLErrorFileDoesNotExist, @"not found");
            return;
        }
        logger::Info("scheme: serving inline HTML %zu chars", self.impl->html_src.size());
        NSData* data = [NSData dataWithBytes:self.impl->html_src.data()
                                      length:self.impl->html_src.size()];
        finish_ok(task, data, @"text/html; charset=utf-8");
        return;
    }

    case browser::LoadMode::File: {
        namespace fs = std::filesystem;
        fs::path requested = (fs::path(self.impl->file_root) /
                              std::string(rest.UTF8String)).lexically_normal();
        auto root_abs = fs::absolute(self.impl->file_root);
        auto req_abs  = fs::absolute(requested);

        if (req_abs.string().rfind(root_abs.string(), 0) != 0) {
            logger::Warn("scheme: path traversal blocked: %s", req_abs.string().c_str());
            finish_error(task, NSURLErrorNoPermissionsToReadFile, @"forbidden");
            return;
        }

        std::ifstream f(requested, std::ios::binary);
        if (!f) {
            logger::Warn("scheme: file not found: %s", requested.string().c_str());
            finish_error(task, NSURLErrorFileDoesNotExist, @"not found");
            return;
        }

        std::vector<uint8_t> bytes(
            (std::istreambuf_iterator<char>(f)),
             std::istreambuf_iterator<char>());

        std::string mime_str = browser::mime_for_ext(requested.extension().string());
        NSString*   mime     = [NSString stringWithUTF8String:mime_str.c_str()];
        NSData*     data     = [NSData dataWithBytes:bytes.data() length:bytes.size()];

        logger::Info("scheme: serving file %s mime=%s bytes=%zu",
                     requested.filename().string().c_str(), mime_str.c_str(), bytes.size());
        finish_ok(task, data, mime);
        return;
    }

    default:
        finish_error(task, NSURLErrorFileDoesNotExist, @"not ready");
        return;
    }
}

- (void)webView:(WKWebView*)webView stopURLSchemeTask:(id<WKURLSchemeTask>)task
{
    // Synchronous handler — nothing to cancel.
}

@end