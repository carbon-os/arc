#pragma once

#ifdef _WIN32
#  ifndef WIN32_LEAN_AND_MEAN
#    define WIN32_LEAN_AND_MEAN
#  endif
#  include <windows.h>
#endif

#include <cstdint>
#include <condition_variable>
#include <mutex>
#include <queue>
#include <string>
#include <string_view>
#include <thread>
#include <vector>

// ── Window config (sent as first frame from Go) ───────────────────────────────

struct WindowConfig {
    int         width  = 1280;
    int         height = 800;
    bool        debug  = false;
    std::string title;
};

// ── Command bytes (Go → renderer) ────────────────────────────────────────────

enum class Command : uint8_t {
    WindowCreate = 0x01,
    LoadFile     = 0x02,
    LoadHTML     = 0x03,
    LoadURL      = 0x04,
    Eval         = 0x05,
    SetTitle     = 0x06,
    SetSize      = 0x07,
    PostText     = 0x08,
    PostBinary   = 0x09,
    Quit         = 0x0A,
};

// ── Event bytes (renderer → Go) ──────────────────────────────────────────────

enum class Event : uint8_t {
    Ready     = 0x81,
    Closed    = 0x82,
    IpcText   = 0x83,
    IpcBinary = 0x84,
};

// ── Decoded inbound frame ─────────────────────────────────────────────────────

struct InboundFrame {
    Command      type {};
    WindowConfig wc;
    std::string  str;
    std::string  channel;
    std::string  text;
    std::vector<uint8_t> data;
    int width  = 0;
    int height = 0;
};

// ── HostChannel ───────────────────────────────────────────────────────────────

class HostChannel {
public:
    explicit HostChannel(const std::string& path);
    ~HostChannel();

    bool connected() const;

    // Blocking read — call from a dedicated reader thread only.
    bool read_frame(InboundFrame& out);

    // Non-blocking sends — enqueue and return immediately.
    void send_event(Event type);
    void send_ipc_text(std::string_view channel, std::string_view text);
    void send_ipc_binary(std::string_view channel, const std::vector<uint8_t>& data);

private:
    bool read_exact(void* buf, uint32_t n);
    void write_raw(const std::vector<uint8_t>& frame);
    void sender_loop();
    void enqueue(std::vector<uint8_t> frame);

    void* pipe_ = nullptr;

    std::mutex              queue_mutex_;
    std::condition_variable queue_cv_;
    std::queue<std::vector<uint8_t>> send_queue_;
    bool                    stopping_ = false;
    std::thread             sender_thread_;
};