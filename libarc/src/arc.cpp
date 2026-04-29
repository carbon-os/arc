#include "arc/arc.h"
#include "arc_runner.h"
#include "host_channel.h"
#include "logger.h"

#include <chrono>
#include <random>
#include <stdexcept>
#include <string>
#include <thread>

#ifndef _WIN32
#  include <cstdlib>
#  include <dlfcn.h>
#else
#  include <windows.h>
#endif

#ifdef __APPLE__
#  include <dispatch/dispatch.h>
#endif

namespace arc {

// ── State ─────────────────────────────────────────────────────────────────────

static std::string s_module_path;

// ── LoadModule ────────────────────────────────────────────────────────────────

void LoadModule(const char* path)
{
    s_module_path = path ? path : "";
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// AppMain signature exported by the Go module.
using AppMainFn = int(*)(const char*);

static AppMainFn resolve_app_main(const std::string& path)
{
#ifndef _WIN32
    void* handle = ::dlopen(path.c_str(), RTLD_NOW | RTLD_LOCAL);
    if (!handle)
        throw std::runtime_error(
            std::string("arc::Run: dlopen failed for '") + path + "': " + ::dlerror());

    auto* fn = reinterpret_cast<AppMainFn>(::dlsym(handle, "AppMain"));
    if (!fn)
        throw std::runtime_error(
            "arc::Run: AppMain symbol not found in '" + path + "'");
    return fn;
#else
    HMODULE h = ::LoadLibraryA(path.c_str());
    if (!h)
        throw std::runtime_error(
            "arc::Run: LoadLibrary failed for '" + path + "'");

    auto* fn = reinterpret_cast<AppMainFn>(
        ::GetProcAddress(h, "AppMain"));
    if (!fn)
        throw std::runtime_error(
            "arc::Run: AppMain not found in '" + path + "'");
    return fn;
#endif
}

static std::string gen_socket_path()
{
    std::random_device                      rd;
    std::mt19937                            gen(rd());
    std::uniform_int_distribution<uint32_t> dist;

    char id[17];
    std::snprintf(id, sizeof(id), "%08x%08x", dist(gen), dist(gen));

#ifdef _WIN32
    return std::string("\\\\.\\pipe\\arc-") + id;
#else
    const char* tmp = std::getenv("TMPDIR");
    if (!tmp || !*tmp) tmp = "/tmp";
    return std::string(tmp) + "/arc-" + id + ".sock";
#endif
}

// Retry connect until Go's listener is ready, or time out.
static std::unique_ptr<HostChannel> connect_with_retry(
    const std::string& path,
    int                max_attempts = 100,
    int                interval_ms  = 20)
{
    for (int i = 0; i < max_attempts; ++i) {
        try {
            return std::make_unique<HostChannel>(path);
        } catch (...) {
            std::this_thread::sleep_for(
                std::chrono::milliseconds(interval_ms));
        }
    }
    throw std::runtime_error(
        "arc::Run: timed out waiting for module to start on " + path);
}

// ── Run ───────────────────────────────────────────────────────────────────────

void Run()
{
    if (s_module_path.empty())
        throw std::runtime_error("arc::Run: call LoadModule before Run");

    const std::string sock_path = gen_socket_path();
    logger::Info("arc::Run: socket path %s", sock_path.c_str());

    AppMainFn app_main = resolve_app_main(s_module_path);
    logger::Info("arc::Run: loaded module %s", s_module_path.c_str());

    // Dispatch AppMain on a background thread. It receives the socket path,
    // starts the Go runtime, listens on the socket, and blocks for the
    // lifetime of the application.
    //
    // On Apple we use GCD so the thread has a proper run-loop context.
    // On all other platforms a detached std::thread is sufficient.

    auto* path_copy = new std::string(sock_path);

#ifdef __APPLE__
    dispatch_async(
        dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_DEFAULT, 0), ^{
            app_main(path_copy->c_str());
            delete path_copy;
        });
#else
    std::thread([app_main, path_copy]() {
        app_main(path_copy->c_str());
        delete path_copy;
    }).detach();
#endif

    // Give Go a moment to open its listener, then connect.
    auto channel = connect_with_retry(sock_path);
    logger::Info("arc::Run: connected to module");

    // Hand off to the shared run loop — blocks until the window is closed.
    detail::run_with_channel(*channel);
}

} // namespace arc