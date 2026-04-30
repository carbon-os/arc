#pragma once

#include <string>

namespace browser {

inline std::string mime_for_ext(const std::string& ext)
{
    if (ext == ".html" || ext == ".htm") return "text/html";
    if (ext == ".css")                   return "text/css";
    if (ext == ".js"  || ext == ".mjs")  return "text/javascript";
    if (ext == ".json")                  return "application/json";
    if (ext == ".svg")                   return "image/svg+xml";
    if (ext == ".png")                   return "image/png";
    if (ext == ".jpg" || ext == ".jpeg") return "image/jpeg";
    if (ext == ".gif")                   return "image/gif";
    if (ext == ".ico")                   return "image/x-icon";
    if (ext == ".wasm")                  return "application/wasm";
    if (ext == ".woff")                  return "font/woff";
    if (ext == ".woff2")                 return "font/woff2";
    if (ext == ".ttf")                   return "font/ttf";
    if (ext == ".txt")                   return "text/plain";
    return "application/octet-stream";
}

} // namespace browser