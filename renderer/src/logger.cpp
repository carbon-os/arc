#include "logger.h"

#include <cstdio>
#include <cstdarg>

namespace logger {

static bool g_enabled = false;

void init(bool enabled) { g_enabled = enabled; }

static void emit(const char* level, const char* fmt, va_list ap)
{
    if (!g_enabled) return;
    std::fprintf(stderr, "[%s] ", level);
    std::vfprintf(stderr, fmt, ap);
    std::fprintf(stderr, "\n");
    std::fflush(stderr);
}

void Info(const char* fmt, ...)
{
    va_list ap; va_start(ap, fmt); emit("INFO",  fmt, ap); va_end(ap);
}

void Warn(const char* fmt, ...)
{
    va_list ap; va_start(ap, fmt); emit("WARN",  fmt, ap); va_end(ap);
}

void Error(const char* fmt, ...)
{
    va_list ap; va_start(ap, fmt); emit("ERROR", fmt, ap); va_end(ap);
}

} // namespace logger