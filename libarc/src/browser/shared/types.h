#pragma once

#include <cstdint>
#include <string>
#include <vector>

namespace browser {

enum class LoadMode { None, Html, File };

struct OutboundFrame {
    std::string          channel;
    bool                 binary = false;
    std::string          text;
    std::vector<uint8_t> data;
};

} // namespace browser