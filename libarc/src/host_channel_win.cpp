#include "host_channel.h"
#include "logger.h"

#include <windows.h>

#include <cstring>
#include <stdexcept>

// ── Little-endian helpers ─────────────────────────────────────────────────────

static uint32_t le32_dec(const uint8_t* p)
{
    return (uint32_t)p[0]
         | ((uint32_t)p[1] <<  8)
         | ((uint32_t)p[2] << 16)
         | ((uint32_t)p[3] << 24);
}

static void le32_enc(std::vector<uint8_t>& v, uint32_t n)
{
    v.push_back((uint8_t)(n));
    v.push_back((uint8_t)(n >>  8));
    v.push_back((uint8_t)(n >> 16));
    v.push_back((uint8_t)(n >> 24));
}

static void append_str(std::vector<uint8_t>& v, std::string_view s)
{
    le32_enc(v, (uint32_t)s.size());
    v.insert(v.end(), s.begin(), s.end());
}

static std::vector<uint8_t> make_frame(const std::vector<uint8_t>& payload)
{
    uint32_t len = static_cast<uint32_t>(payload.size());
    std::vector<uint8_t> frame;
    frame.reserve(4 + len);
    frame.push_back((uint8_t)(len));
    frame.push_back((uint8_t)(len >>  8));
    frame.push_back((uint8_t)(len >> 16));
    frame.push_back((uint8_t)(len >> 24));
    frame.insert(frame.end(), payload.begin(), payload.end());
    return frame;
}

// ── HostChannel ───────────────────────────────────────────────────────────────

HostChannel::HostChannel(const std::string& path)
{
    HANDLE h = CreateFileA(
        path.c_str(),
        GENERIC_READ | GENERIC_WRITE,
        0, nullptr,
        OPEN_EXISTING,
        FILE_FLAG_OVERLAPPED,
        nullptr);

    if (h == INVALID_HANDLE_VALUE)
        throw std::runtime_error("HostChannel: could not connect to named pipe: " + path);

    pipe_ = h;
    logger::Info("HostChannel: connected to named pipe %s", path.c_str());

    sender_thread_ = std::thread(&HostChannel::sender_loop, this);
}

HostChannel::~HostChannel()
{
    {
        std::lock_guard<std::mutex> lock(queue_mutex_);
        stopping_ = true;
    }
    queue_cv_.notify_all();
    if (sender_thread_.joinable())
        sender_thread_.join();

    if (pipe_) CloseHandle(static_cast<HANDLE>(pipe_));
    logger::Info("HostChannel: destroyed");
}

bool HostChannel::connected() const { return pipe_ != nullptr; }

// ── Sender thread ─────────────────────────────────────────────────────────────

void HostChannel::sender_loop()
{
    logger::Info("HostChannel: sender thread started");
    for (;;) {
        std::vector<uint8_t> frame;
        {
            std::unique_lock<std::mutex> lock(queue_mutex_);
            queue_cv_.wait(lock, [this] {
                return stopping_ || !send_queue_.empty();
            });
            if (stopping_ && send_queue_.empty()) {
                logger::Info("HostChannel: sender thread stopping");
                return;
            }
            frame = std::move(send_queue_.front());
            send_queue_.pop();
        }
        logger::Info("HostChannel: sending frame %zu bytes", frame.size());
        write_raw(frame);
    }
}

void HostChannel::enqueue(std::vector<uint8_t> frame)
{
    {
        std::lock_guard<std::mutex> lock(queue_mutex_);
        send_queue_.push(std::move(frame));
    }
    queue_cv_.notify_one();
}

void HostChannel::write_raw(const std::vector<uint8_t>& frame)
{
    OVERLAPPED ov{};
    ov.hEvent = CreateEvent(nullptr, TRUE, FALSE, nullptr);

    DWORD w  = 0;
    BOOL  ok = WriteFile(static_cast<HANDLE>(pipe_), frame.data(),
                         static_cast<DWORD>(frame.size()), nullptr, &ov);

    if (!ok && GetLastError() == ERROR_IO_PENDING)
        ok = GetOverlappedResult(static_cast<HANDLE>(pipe_), &ov, &w, TRUE);
    else if (ok)
        ok = GetOverlappedResult(static_cast<HANDLE>(pipe_), &ov, &w, FALSE);

    if (ov.hEvent) CloseHandle(ov.hEvent);

    if (!ok || w != static_cast<DWORD>(frame.size()))
        logger::Error("HostChannel: WriteFile failed: wrote %lu of %zu err=%lu",
                      (unsigned long)w, frame.size(), GetLastError());
    else
        logger::Info("HostChannel: WriteFile ok %lu bytes", w);
}

// ── Blocking read ─────────────────────────────────────────────────────────────

bool HostChannel::read_exact(void* buf, uint32_t n)
{
    auto*    p   = static_cast<uint8_t*>(buf);
    uint32_t got = 0;
    while (got < n) {
        OVERLAPPED ov{};
        ov.hEvent = CreateEvent(nullptr, TRUE, FALSE, nullptr);

        DWORD r  = 0;
        BOOL  ok = ReadFile(static_cast<HANDLE>(pipe_), p + got, n - got, nullptr, &ov);

        if (!ok && GetLastError() == ERROR_IO_PENDING)
            ok = GetOverlappedResult(static_cast<HANDLE>(pipe_), &ov, &r, TRUE);
        else if (ok)
            ok = GetOverlappedResult(static_cast<HANDLE>(pipe_), &ov, &r, FALSE);

        if (ov.hEvent) CloseHandle(ov.hEvent);
        if (!ok || r == 0) return false;
        got += r;
    }
    return true;
}

bool HostChannel::read_frame(InboundFrame& out)
{
    uint8_t hdr[4];
    if (!read_exact(hdr, 4)) return false;

    uint32_t len = le32_dec(hdr);
    if (len == 0) {
        logger::Warn("HostChannel: received zero-length frame");
        return false;
    }

    std::vector<uint8_t> payload(len);
    if (!read_exact(payload.data(), len)) return false;

    logger::Info("HostChannel: read_frame %u bytes type=0x%02X", len, payload[0]);

    const uint8_t* p   = payload.data();
    const uint8_t* end = p + len;

    out      = {};
    out.type = static_cast<Command>(*p++);

    auto need     = [&](uint32_t n) { return (p + n) <= end; };
    auto read_u32 = [&]() -> uint32_t {
        if (!need(4)) return 0;
        uint32_t v = le32_dec(p); p += 4; return v;
    };
    auto read_str = [&]() -> std::string {
        uint32_t n = read_u32();
        if (!need(n)) return {};
        std::string s(reinterpret_cast<const char*>(p), n);
        p += n;
        return s;
    };

    switch (out.type) {
    case Command::WindowCreate:
        out.wc.width         = static_cast<int>(read_u32());
        out.wc.height        = static_cast<int>(read_u32());
        out.wc.debug         = (*p++ != 0);
        out.wc.title         = read_str();
        out.wc.titleBarStyle = need(1)
            ? static_cast<TitleBarStyle>(*p++)
            : TitleBarStyle::Default;
        break;
    case Command::LoadFile:
    case Command::LoadHTML:
    case Command::LoadURL:
    case Command::Eval:
    case Command::SetTitle:
        out.str = read_str();
        break;
    case Command::SetSize:
        out.width  = static_cast<int>(read_u32());
        out.height = static_cast<int>(read_u32());
        break;
    case Command::PostText:
        out.channel = read_str();
        out.text    = read_str();
        break;
    case Command::PostBinary:
        out.channel = read_str();
        out.data.assign(p, end);
        break;
    case Command::Quit:
        break;
    default:
        logger::Warn("HostChannel: unknown command byte 0x%02X",
                     static_cast<uint8_t>(out.type));
        break;
    }

    return true;
}

// ── Public sends ──────────────────────────────────────────────────────────────

void HostChannel::send_event(Event type)
{
    logger::Info("HostChannel: send_event 0x%02X", static_cast<uint8_t>(type));
    std::vector<uint8_t> payload { static_cast<uint8_t>(type) };
    enqueue(make_frame(payload));
}

void HostChannel::send_ipc_text(std::string_view channel, std::string_view text)
{
    logger::Info("HostChannel: send_ipc_text channel=%.*s",
                 (int)channel.size(), channel.data());
    std::vector<uint8_t> payload;
    payload.reserve(1 + 4 + channel.size() + 4 + text.size());
    payload.push_back(static_cast<uint8_t>(Event::IpcText));
    append_str(payload, channel);
    append_str(payload, text);
    enqueue(make_frame(payload));
}

void HostChannel::send_ipc_binary(std::string_view channel,
                                   const std::vector<uint8_t>& data)
{
    logger::Info("HostChannel: send_ipc_binary channel=%.*s bytes=%zu",
                 (int)channel.size(), channel.data(), data.size());
    std::vector<uint8_t> payload;
    payload.reserve(1 + 4 + channel.size() + data.size());
    payload.push_back(static_cast<uint8_t>(Event::IpcBinary));
    append_str(payload, channel);
    payload.insert(payload.end(), data.begin(), data.end());
    enqueue(make_frame(payload));
}