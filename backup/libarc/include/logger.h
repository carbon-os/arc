#pragma once
#include <cstdarg>

namespace logger {
    void init(bool enabled);
    void Info (const char* fmt, ...);
    void Warn (const char* fmt, ...);
    void Error(const char* fmt, ...);
}