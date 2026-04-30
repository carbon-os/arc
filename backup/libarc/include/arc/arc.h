#pragma once

// ── Public API ────────────────────────────────────────────────────────────────
//
// Production (single-process) usage:
//
//   #include <arc/arc.h>
//
//   int main() {
//       arc::LoadModule("@executable_path/libarc-module.dylib"); // macOS
//       arc::Run();
//       return 0;
//   }
//
// LoadModule must be called before Run. Run() blocks for the lifetime of the
// application on the calling (main) thread.
//
// Development mode (multi-process) does not use this API — arc-host is driven
// directly by the Go binary over IPC.

#ifdef __cplusplus

namespace arc {

// Store the path to the Go module shared library. Called once before Run().
// The path is platform-specific:
//
//   macOS   "@executable_path/libarc-module.dylib"
//   Linux   "libarc-module.so"
//   Windows "libarc-module.dll"
//
// For local testing any absolute path is accepted.
void LoadModule(const char* path);

// Start the application. Loads the module set by LoadModule, opens an IPC
// socket, dispatches AppMain on a background thread, then starts the native
// run loop on the calling thread. Blocks until the window is closed.
void Run();

} // namespace arc

#endif // __cplusplus