# libarc

The native host library for the Arc framework. Owns the webview, the native
run loop, the IPC socket, and — in production — module loading for the Go
runtime.

---

## What it is

libarc is the C++ side of Arc. It wraps the platform's native webview engine
and exposes a minimal IPC protocol that the Go layer speaks to create windows,
load content, exchange messages with JavaScript, and handle in-app purchases.

It ships in two forms:

| Form | Used in | Description |
|---|---|---|
| `libarc.dylib` / `libarc.so` / `libarc.dll` | Production | Loaded into the host process. Calls into your Go module via `AppMain`. |
| `arc-host` | Development | Standalone binary. Spawned by `go run .` as a subprocess and driven over IPC. |

Your Go code is identical in both cases. The form in use is an infrastructure
detail that Arc manages for you.

---

## Webview engines

| Platform | Engine | Minimum OS |
|---|---|---|
| macOS | WKWebView | macOS 11 |
| Windows | WebView2 | Windows 10 1903+ |
| Linux | WebKit2GTK 4.1 | GTK 3, GLib 2.56 |

No browser is bundled. libarc uses the engine the OS already provides, which
keeps binary size small and rendering consistent with the platform.

---

## The `arc` CLI

The `arc` binary is a thin build tool. It is not a compiler — it proxies
arguments to the local `go build` toolchain and handles everything around it:
cloning libarc, compiling your Go code as a shared library, and wiring up a
CMake project so the final single-process binary can be built and debugged
natively.

### `arc build`

```bash
cd your-app/
arc build -o your-binary-name .
```

This is the only command you need for a production build. Under the hood it
does the following, in order:

**1. Clone libarc**

Uses [go-git v5](https://github.com/go-git/go-git) to clone (or update)
`https://github.com/carbon-os/arc` into `arc-project/libarc/`. No `git`
binary is required on the host — go-git is a pure Go implementation bundled
into the `arc` binary itself.

```
downloading libarc from github.com/carbon-os/arc
```

**2. Compile your Go module**

Proxies to the local `go build` toolchain with `-buildmode=c-shared` to
produce the Go module shared library:

```
go build -buildmode=c-shared -o arc-project/libarc-module.dylib .
```

The `AppMain` stub (`_arc_entry.go`) is injected automatically before the
build and removed on completion. Your `main.go` is never modified.

**3. Set up `arc-project/`**

Generates a self-contained `arc-project/` directory in your app folder,
ready to build the final single-process binary with CMake or open directly
in Xcode for debugging.

---

### What `arc-project/` looks like after `arc build`

```
your-app/
├── main.go                        ← your app, untouched
├── go.mod
│
└── arc-project/
    ├── CMakeLists.txt             ← auto-generated, wired to libarc + your module
    ├── main.cpp                   ← auto-generated host entry point
    ├── libarc-module.dylib        ← your compiled Go logic
    ├── libarc.dylib               ← native webview + run loop
    └── build/                     ← cmake build dir, ready but not yet built
```

The `build/` directory is pre-configured. To produce your final binary:

```bash
cd arc-project/
cmake --build build
```

Or open `arc-project/` directly in Xcode for a full native debug session with
breakpoints, Instruments, and the memory graph.

---

### The generated `main.cpp`

`arc build` stamps out a `main.cpp` that is identical on every platform:

```cpp
#include <arc/arc.h>

int main() {
    arc::LoadModule("@executable_path/libarc-module.dylib"); // macOS
    arc::Run();
    return 0;
}
```

Platform-correct paths per target:

```cpp
arc::LoadModule("@executable_path/libarc-module.dylib"); // macOS
arc::LoadModule("libarc-module.so");                      // Linux
arc::LoadModule("libarc-module.dll");                     // Windows
```

You never write or edit this file. `arc build` regenerates it on every run.

---

### Development mode — no `arc build` needed

During day-to-day development you do not use `arc build` at all:

```bash
go run .
```

The Go arc package spawns `arc-host` as a subprocess and drives it over IPC.
No CMake, no C compiler, no Xcode. `arc build` is only for producing a
distribution-ready binary.

---

## Public API

The public C++ API is two functions, declared in `include/arc/arc.h`:

```cpp
namespace arc {
    void LoadModule(const char* path);
    void Run();
}
```

`LoadModule` must be called before `Run`. `Run` blocks on the calling thread
for the lifetime of the application and must be called from `main`.

---

## IPC protocol

libarc and the Go module communicate over a length-prefixed binary protocol on
a Unix socket (macOS, Linux) or named pipe (Windows). The protocol is the same
in development and production — the only difference is whether the socket
crosses a process boundary.

### Frame format

```
┌─────────────────────────────────────┐
│  length   (4 bytes, little-endian)  │
├─────────────────────────────────────┤
│  payload  (length bytes)            │
│    [0]  command / event byte        │
│    [1…] fields                      │
└─────────────────────────────────────┘
```

String fields are length-prefixed:

```
┌──────────────────┬───────────────────────┐
│  len (4 bytes LE)│  UTF-8 bytes          │
└──────────────────┴───────────────────────┘
```

### Commands — Go → libarc

| Byte | Command | Payload |
|---|---|---|
| `0x01` | `WindowCreate` | `width u32, height u32, debug u8, title str` |
| `0x02` | `LoadFile` | `path str` |
| `0x03` | `LoadHTML` | `html str` |
| `0x04` | `LoadURL` | `url str` |
| `0x05` | `Eval` | `js str` |
| `0x06` | `SetTitle` | `title str` |
| `0x07` | `SetSize` | `width u32, height u32` |
| `0x08` | `PostText` | `channel str, payload str` |
| `0x09` | `PostBinary` | `channel str, payload bytes` |
| `0x0A` | `Quit` | — |
| `0x0B` | `BillingInit` | `count u32, [id str, kind u8]…` |
| `0x0C` | `BillingBuy` | `product_id str` |
| `0x0D` | `BillingRestore` | — |

`WindowCreate` must be the first command sent. All others are invalid before
the `Ready` event is received.

### Events — libarc → Go

| Byte | Event | Payload |
|---|---|---|
| `0x81` | `Ready` | — |
| `0x82` | `Closed` | — |
| `0x83` | `IpcText` | `channel str, payload str` |
| `0x84` | `IpcBinary` | `channel str, payload bytes` |
| `0x85` | `BillingProducts` | `count u32, [id str, title str, desc str, price str, kind u8]…` |
| `0x86` | `BillingPurchase` | `status u8, product_id str, error str` |

### Session sequence

```
Go                              libarc
 │                                │
 │── WindowCreate ───────────────>│  must be first
 │<── Ready ──────────────────────│  window and webview are live
 │                                │
 │── LoadFile / LoadHTML / …  ───>│
 │── PostText / PostBinary ──────>│─── ipc.on(ch) → JS
 │                                │
 │             JS posts           │
 │<── IpcText / IpcBinary ────────│
 │                                │
 │── Quit ───────────────────────>│
 │<── Closed ─────────────────────│
```

---

## Building libarc directly

The `arc` CLI handles this automatically. The steps below are for contributors
or anyone building the library outside of `arc build`.

### Prerequisites

| Platform | Requirements |
|---|---|
| macOS | Xcode Command Line Tools, CMake ≥ 3.22 |
| Windows | Visual Studio 2022, CMake ≥ 3.22, vcpkg |
| Linux | GCC or Clang, CMake ≥ 3.22, `webkit2gtk-4.1`, `gtk+-3.0` |

On Linux, install the webview dependencies with your package manager:

```bash
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev

# Fedora
sudo dnf install webkit2gtk4.1-devel gtk3-devel
```

### Configure and build

```bash
cd libarc

cmake -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build
```

This produces two outputs:

```
build/lib/libarc.dylib     # (or .so / .dll)
build/bin/arc-host
```

On Windows, pass the vcpkg toolchain file:

```bat
cmake -B build ^
  -DCMAKE_BUILD_TYPE=Release ^
  -DCMAKE_TOOLCHAIN_FILE="%VCPKG_ROOT%\scripts\buildsystems\vcpkg.cmake"
cmake --build build --config Release
```

### Install

```bash
cmake --install build --prefix /usr/local
```

Installs `libarc` to `lib/`, `arc-host` to `bin/`, and the public header to
`include/arc/arc.h`.

---

## Source layout

```
libarc/
├── include/
│   └── arc/
│       └── arc.h                 ← public API (LoadModule / Run)
├── src/
│   ├── arc.cpp                   ← arc::LoadModule / arc::Run implementation
│   ├── arc_host_main.cpp         ← arc-host entry point (development binary)
│   ├── arc_runner.h              ← shared run-loop logic (internal)
│   ├── host_channel.h            ← IPC types and HostChannel interface
│   ├── host_channel_unix.cpp     ← Unix socket implementation
│   ├── host_channel_win.cpp      ← named pipe implementation
│   ├── billing.h                 ← BillingManager interface
│   ├── logger.h / logger.cpp     ← stderr logger (enabled with --logging)
│   ├── mime.h                    ← MIME type lookup for scheme handler
│   ├── str_escape.h              ← JS / JSON string escaping
│   ├── types.h                   ← shared browser types
│   └── browser/
│       ├── shared/
│       │   └── webview.h         ← WebView interface
│       ├── mac/                  ← WKWebView (Objective-C++)
│       ├── win/                  ← WebView2 (C++)
│       └── linux/                ← WebKit2GTK (C++)
├── CMakeLists.txt
└── vcpkg.json
```

---

## Logging

Both `libarc` and `arc-host` write diagnostic output to stderr. Logging is
off by default and must be explicitly enabled.

**arc-host** (development):

```bash
arc-host --channel <id> --logging
```

**libarc** (production): call `logger::init(true)` before `arc::Run()`. In
practice the `arc` CLI controls this through a build flag — it is not enabled
in release builds.

Log lines are prefixed with severity:

```
[INFO]  arc::Run: socket path /tmp/arc-3f2a1b4c.sock
[INFO]  arc-host: connecting on channel 3f2a1b4c
[WARN]  arc: BillingBuy before BillingInit
[ERROR] arc-host: missing --channel <id>
```

---

## Billing

In-app purchase support is implemented per-platform in `src/browser/*/billing.*`
and exposed to Go through three commands (`BillingInit`, `BillingBuy`,
`BillingRestore`) and two events (`BillingProducts`, `BillingPurchase`).

`BillingInit` must be sent before `BillingBuy` or `BillingRestore`. The Go arc
library enforces this ordering — sending the commands out of order produces a
warning log and a no-op.

| Platform | Backend |
|---|---|
| macOS / iOS | StoreKit |
| Windows | Windows.Services.Store |
| Linux | Not supported (stub returns `Failed`) |

---

## Threading model

The native run loop always stays on the main thread. libarc enforces this:
`arc::Run` must be called from `main`, and it never returns control until the
session ends.

Go runs on a background thread in both modes. In development it runs in a
separate process entirely. In production, `AppMain` is dispatched by libarc
onto a background thread before the run loop starts.

`HostChannel` is thread-safe. Sends are non-blocking — frames are enqueued and
written by a dedicated sender thread. Reads are blocking and must be called
from a single dedicated reader thread.

---

## License

See `LICENSE` in the repository root.