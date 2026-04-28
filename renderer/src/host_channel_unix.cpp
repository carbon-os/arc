#ifndef _WIN32
#include "host_channel.h"
#include "logger.h"

#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>
#include <cerrno>
#include <cstring>
#include <stdexcept>

// ── Little-endian helpers ─────────────────────────────────────────────────────

static uint32_t le32_dec(const uint8_t* p)
{
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8)
         | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
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
    le32_enc(frame, len);
    frame.insert(frame.end(), payload.begin(), payload.end());
    return frame;
}

static void* fd_pack(int fd)    { return reinterpret_cast<void*>(static_cast<intptr_t>(fd + 1)); }
static int   fd_unpack(void* p) { return static_cast<int>(reinterpret_cast<intptr_t>(p)) - 1; }

// ── HostChannel ───────────────────────────────────────────────────────────────

HostChannel::HostChannel(const std::string& path)
{
    int fd = ::socket(AF_UNIX, SOCK_STREAM, 0);
    if (fd < 0)
        throw std::runtime_error(
            std::string("HostChannel: socket() failed: ") + std::strerror(errno));

    struct sockaddr_un addr{};
    addr.sun_family = AF_UNIX;
    std::strncpy(addr.sun_path, path.c_str(), sizeof(addr.sun_path) - 1);

    if (::connect(fd, reinterpret_cast<sockaddr*>(&addr), sizeof(addr)) < 0) {
        ::close(fd);
        throw std::runtime_error(
            "HostChannel: connect() failed on " + path + ": " + std::strerror(errno));
    }

    pipe_ = fd_pack(fd);
    logger::Info("HostChannel: connected to unix socket %s", path.c_str());
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
    if (pipe_) {
        ::close(fd_unpack(pipe_));
        pipe_ = nullptr;
    }
    logger::Info("HostChannel: destroyed");
}

bool HostChannel::connected() const { return pipe_ != nullptr; }

void HostChannel::sender_loop()
{
    logger::Info("HostChannel: sender thread started");
    for (;;) {
        std::vector<uint8_t> frame;
        {
            std::unique_lock<std::mutex> lock(queue_mutex_);
            queue_cv_.wait(lock, [this] { return stopping_ || !send_queue_.empty(); });
            if (stopping_ && send_queue_.empty()) return;
            frame = std::move(send_queue_.front());
            send_queue_.pop();
        }
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
    int    fd      = fd_unpack(pipe_);
    size_t written = 0;
    while (written < frame.size()) {
        ssize_t n = ::write(fd, frame.data() + written, frame.size() - written);
        if (n < 0) {
            if (errno == EINTR) continue;
            logger::Error("HostChannel: write() failed: %s", std::strerror(errno));
            return;
        }
        written += static_cast<size_t>(n);
    }
}

bool HostChannel::read_exact(void* buf, uint32_t n)
{
    int      fd  = fd_unpack(pipe_);
    auto*    p   = static_cast<uint8_t*>(buf);
    uint32_t got = 0;
    while (got < n) {
        ssize_t r = ::read(fd, p + got, n - got);
        if (r < 0) { if (errno == EINTR) continue; return false; }
        if (r == 0) return false;
        got += static_cast<uint32_t>(r);
    }
    return true;
}

bool HostChannel::read_frame(InboundFrame& out)
{
    uint8_t hdr[4];
    if (!read_exact(hdr, 4)) return false;

    uint32_t len = le32_dec(hdr);
    if (len == 0) { logger::Warn("HostChannel: zero-length frame"); return false; }

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
        out.wc.width  = static_cast<int>(read_u32());
        out.wc.height = static_cast<int>(read_u32());
        out.wc.debug  = (*p++ != 0);
        out.wc.title  = read_str();
        break;
    case Command::LoadFile:
    case Command::LoadHTML:
    case Command::LoadURL:
    case Command::Eval:
    case Command::SetTitle:
    case Command::BillingBuy:   // payload is just the product ID string
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
    case Command::BillingInit: {
        uint32_t count = read_u32();
        out.billing_products.reserve(count);
        for (uint32_t i = 0; i < count; ++i) {
            BillingProductDecl d;
            d.id = read_str();
            if (!need(1)) break;
            d.kind = *p++;
            out.billing_products.push_back(std::move(d));
        }
        break;
    }
    case Command::BillingRestore:
    case Command::Quit:
        break;
    default:
        logger::Warn("HostChannel: unknown command 0x%02X",
                     static_cast<uint8_t>(out.type));
        break;
    }

    return true;
}

// ── Send helpers ──────────────────────────────────────────────────────────────

void HostChannel::send_event(Event type)
{
    std::vector<uint8_t> payload{ static_cast<uint8_t>(type) };
    enqueue(make_frame(payload));
}

void HostChannel::send_ipc_text(std::string_view channel, std::string_view text)
{
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
    std::vector<uint8_t> payload;
    payload.reserve(1 + 4 + channel.size() + data.size());
    payload.push_back(static_cast<uint8_t>(Event::IpcBinary));
    append_str(payload, channel);
    payload.insert(payload.end(), data.begin(), data.end());
    enqueue(make_frame(payload));
}

void HostChannel::send_billing_products(const std::vector<BillingProductInfo>& products)
{
    logger::Info("HostChannel: send_billing_products count=%zu", products.size());
    std::vector<uint8_t> payload;
    payload.push_back(static_cast<uint8_t>(Event::BillingProducts));
    le32_enc(payload, static_cast<uint32_t>(products.size()));
    for (const auto& p : products) {
        append_str(payload, p.id);
        append_str(payload, p.title);
        append_str(payload, p.description);
        append_str(payload, p.formatted_price);
        payload.push_back(p.kind);
    }
    enqueue(make_frame(payload));
}

void HostChannel::send_billing_purchase(PurchaseStatus status,
                                         std::string_view product_id,
                                         std::string_view error_msg)
{
    logger::Info("HostChannel: send_billing_purchase product=%.*s status=%d",
                 (int)product_id.size(), product_id.data(), (int)status);
    std::vector<uint8_t> payload;
    payload.push_back(static_cast<uint8_t>(Event::BillingPurchase));
    payload.push_back(static_cast<uint8_t>(status));
    append_str(payload, product_id);
    append_str(payload, error_msg);
    enqueue(make_frame(payload));
}

#endif // !_WIN32