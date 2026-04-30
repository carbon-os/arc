// arc-host — development-mode standalone renderer.
//
// Spawned by the Go arc library as a subprocess. Receives --channel <id> on
// the command line, connects to the IPC socket/pipe that Go has already opened,
// and runs the webview for the lifetime of the session.
//
// This binary is not used in production. Production builds use libarc directly
// via arc::LoadModule / arc::Run.

#include "arc_runner.h"
#include "host_channel.h"
#include "logger.h"

#include <cstring>
#include <string>

#ifndef _WIN32
#  include <cstdlib>
#endif

// ── Helpers ───────────────────────────────────────────────────────────────────

static std::string channel_path(const std::string& id)
{
#ifdef _WIN32
    return "\\\\.\\pipe\\arc-" + id;
#else
    const char* tmp = std::getenv("TMPDIR");
    if (!tmp || !*tmp) tmp = "/tmp";
    return std::string(tmp) + "/arc-" + id + ".sock";
#endif
}

static std::string parse_flag(int argc, char** argv, const char* flag)
{
    for (int i = 1; i < argc - 1; ++i)
        if (std::strcmp(argv[i], flag) == 0)
            return argv[i + 1];
    return {};
}

static bool parse_bool_flag(int argc, char** argv, const char* flag)
{
    for (int i = 1; i < argc; ++i)
        if (std::strcmp(argv[i], flag) == 0)
            return true;
    return false;
}

// ── Entry point ───────────────────────────────────────────────────────────────

int main(int argc, char** argv)
{
    logger::init(parse_bool_flag(argc, argv, "--logging"));

    const std::string id = parse_flag(argc, argv, "--channel");
    if (id.empty()) {
        logger::Error("arc-host: missing --channel <id>");
        return 1;
    }

    logger::Info("arc-host: connecting on channel %s", id.c_str());

    HostChannel channel(channel_path(id));
    if (!channel.connected()) {
        logger::Error("arc-host: failed to connect to host");
        return 1;
    }

    arc::detail::run_with_channel(channel);
    return 0;
}