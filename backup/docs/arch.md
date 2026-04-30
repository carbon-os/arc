# Arc — Architecture

## Overview

Arc has two operating modes that share the same Go API and the same IPC
protocol. The mode in use is an infrastructure detail — your application
code is identical in both.

```
Development (multi-process)

  Go binary  ──── IPC (socket / pipe) ────  arc-host
  (your app)                                (subprocess)


Production (single-process)

  ┌────────────────────────────────────────────────────┐
  │                    host process                    │
  │                                                    │
  │   libarc  ──── IPC (socket) ────  libarc-module   │
  │  (native webview, run loop)      (your Go logic)   │
  └────────────────────────────────────────────────────┘
```

---

## Development Mode — Multi-Process

The Go application runs as a standalone binary and is the controller.
`arc-host` is spawned as a subprocess. IPC runs over a Unix socket
(macOS / Linux) or named pipe (Windows) between the two processes.

This mode requires only the Go toolchain and a prebuilt `arc-host` binary
for your platform. No C compiler, no CMake, no Xcode.

```bash
go run .
```

`arc-host` is the prebuilt `libarc` binary. In this mode Go drives it
over IPC — `LoadModule` is not called, Go is the controller.

```
your app (Go binary)
  │
  ├── spawns arc-host subprocess
  ├── opens IPC socket at /tmp/arc-<id>.sock
  └── drives arc-host over IPC for the lifetime of the app
```

**Sandbox compatibility:** Multi-process mode is not sandbox safe.
Spawning a subprocess from a sandboxed application on macOS or Windows
requires the child to be independently signed and entitled. This is
fragile enough in development and not viable for App Store or MSIX
distribution. Multi-process mode is therefore not intended for shipping.

---

## Production Mode — Single-Process

Both `libarc` and `libarc-module` are loaded into a single host process.
`libarc` is the controller — it owns the run loop, the IPC socket, and
the webview. It loads `libarc-module` and invokes `AppMain` to start the
Go runtime.

This mode is produced by `arc build`. The developer never configures it
manually.

```
host process
  │
  ├── arc::LoadModule("libarc-module.dylib")
  │     ├── opens IPC socket at /tmp/arc-<id>.sock
  │     ├── dlopen libarc-module
  │     └── dispatches AppMain(sockPath) on background thread
  │           └── Go runtime starts
  │                 ├── listens on sockPath
  │                 ├── accepts libarc connection
  │                 └── begins command loop
  │
  └── arc::Run()
        └── starts native run loop (main thread)
```

The native run loop stays on the main thread where the OS requires it.
`AppMain` blocks on the background thread for the lifetime of the
application. When `app.Run()` returns, `AppMain` returns, and the host
tears down cleanly.

**Sandbox compatibility:** Both libraries are loaded in-process. No
subprocess spawning occurs. This satisfies macOS App Sandbox, Mac App
Store, and Windows AppX / MSIX packaging requirements.

---

## The `arc` CLI

The `arc` binary is a thin build tool that sits in front of your existing
Go toolchain. It does not replace `go build` — it calls it. All standard
Go flags and arguments are proxied through unchanged.

### `arc build`

```bash
cd your-app/
arc build -o your-binary-name .
```

Running `arc build` performs the following steps in order:

**1. Clone libarc**

Uses [go-git v5](https://github.com/go-git/go-git) — a pure Go git
implementation bundled into the `arc` binary — to clone or update
`https://github.com/carbon-os/arc` into `arc-project/libarc/`. No `git`
binary is required on the host machine.

```
downloading libarc from github.com/carbon-os/arc
creating arc-project/
```

**2. Compile your Go module**

Injects a temporary `_arc_entry.go` into your package (the `AppMain`
stub), then proxies to the local `go build` toolchain with
`-buildmode=c-shared`:

```bash
go build -buildmode=c-shared -o arc-project/libarc-module.dylib .
```

The stub is removed on completion. Your source files are never modified.

**3. Generate `arc-project/`**

Writes a self-contained `arc-project/` directory alongside your Go code
with a generated `CMakeLists.txt` and `main.cpp`, the compiled Go module,
and the libarc shared library. The CMake build tree is pre-configured but
not yet compiled — you decide when to build the final binary.

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
    └── build/                     ← cmake build dir, configured and ready
```

To produce the final host binary:

```bash
cd arc-project/
cmake --build build
```

Or open `arc-project/` in Xcode for a full native debug session with
breakpoints, Instruments, and the memory graph.

### The generated `main.cpp`

`arc build` stamps out a `main.cpp` that is identical on every platform
and never changes:

```cpp
#include <arc/arc.h>

int main() {
    arc::LoadModule("@executable_path/libarc-module.dylib");
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

You never write or edit this file. `arc build` regenerates it on every
run.

---

## libarc

The core native library. Owns everything on the C++ side:

- Native webview (WKWebView / WebView2 / WebKit2GTK)
- IPC socket / named pipe
- Native run loop
- Module loading (`dlopen` / `LoadLibrary`)
- Background thread dispatch

In development mode, `libarc` is shipped as `arc-host` — a prebuilt
standalone binary that Go spawns as a subprocess and drives over IPC.

In production mode, `libarc` is loaded into the host process directly
and becomes the controller.

| Platform | Webview Engine | Minimum Version  |
|----------|----------------|------------------|
| macOS    | WKWebView      | macOS 11         |
| Windows  | WebView2       | Windows 10 1903+ |
| Linux    | WebKit2GTK 4.1 | GTK 3, GLib 2.56 |

---

## libarc-module

Your Go application compiled as a shared library with
`-buildmode=c-shared`. Exports one symbol: `AppMain`. Everything else —
window management, IPC handlers, business logic — is internal to the Go
runtime.

---

## AppMain — The Entry Point

In production mode Go's `func main()` is not called — it is not an entry
point in a shared library. `arc build` automatically injects a stub at
build time:

```go
// auto-generated by arc build — do not edit
package main

import "C"

//export AppMain
func AppMain(sockPath *C.char) C.int {
    main()
    return 1
}
```

This file is written into the package directory during the build and
removed on completion. The developer's `main.go` is never modified.

`AppMain` calls `main()` directly and blocks. `libarc` is responsible
for dispatching it on the correct background thread for the platform.

**macOS**
```objc
dispatch_async(dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_DEFAULT, 0), ^{
    AppMain(sockPathC);
});
```

**Windows / Linux**
```cpp
std::thread([sockPathC]{
    AppMain(sockPathC);
}).detach();
```

---

## Host Entry Point

The generated `main.cpp` is identical on every platform. `arc build`
stamps it out — it never changes:

```cpp
#include <arc/arc.h>

int main() {
    arc::LoadModule("libarc-module.dylib");
    arc::Run();
    return 0;
}
```

`arc build` generates the correct platform path:

```cpp
arc::LoadModule("@executable_path/libarc-module.dylib"); // macOS
arc::LoadModule("libarc-module.so");                      // Linux
arc::LoadModule("libarc-module.dll");                     // Windows
```

For local testing during development you can point `LoadModule` at any
path, or skip the host binary entirely and run `arc-host` directly:

```bash
# build your Go module
go build -buildmode=c-shared -o libarc-module.dylib .

# run it with the prebuilt arc-host
arc-host libarc-module.dylib
```

---

## IPC

The IPC contract is the same in both modes. A length-prefixed binary
framing protocol over a Unix socket or named pipe. In development the
socket crosses a process boundary. In production it is loopback within
the same process.

```
libarc-module (Go — background thread)      libarc (native UI thread)
        │                                         │
        │──── WindowCreate ───────────────────── >│
        │< ───────────────────────── Ready ────── │
        │                                         │
        │──── LoadFile ───────────────────────── >│
        │                                         │── navigate webview
        │                                         │
        │──── PostText(ch, payload) ────────────> │
        │                                         │── ipc.on(ch) → JS
        │                                         │
        │                              JS posts   │
        │< ──────────────── IpcText(ch, payload) ─│
        │                                         │
        │──── Quit ───────────────────────────── >│
        │< ───────────────────────── Closed ────── │
```

### Commands (Go → libarc)

| Command        | Description                               |
|----------------|-------------------------------------------|
| `WindowCreate` | Initialise the native window and webview  |
| `LoadFile`     | Navigate to a local file                  |
| `LoadHTML`     | Load inline HTML                          |
| `LoadURL`      | Navigate to an external URL               |
| `Eval`         | Execute JavaScript in the current page    |
| `SetTitle`     | Update the window title                   |
| `SetSize`      | Resize the window                         |
| `PostText`     | Send a text message to a JS IPC channel   |
| `PostBinary`   | Send a binary message to a JS IPC channel |
| `Quit`         | Tear down the webview                     |

### Events (libarc → Go)

| Event       | Description                              |
|-------------|------------------------------------------|
| `Ready`     | Window and webview are ready             |
| `Closed`    | User closed the window                   |
| `IpcText`   | JS posted a text message                 |
| `IpcBinary` | JS posted a binary message               |

---

## Go API

The public API is the same in both modes. The operating mode is
invisible to the developer.

```go
app := arc.NewApp(arc.AppConfig{
    Title: "My App",
    Renderer: arc.RendererConfig{
        Prebuilt: true,
    },
})

app.OnReady(func() {
    win := app.NewBrowserWindow(window.Config{
        Width:  1280,
        Height: 800,
    })

    ipcMain := win.IPC()

    ipcMain.On("ping", func(msg ipc.Message) {
        ipcMain.Send("pong", "hello back!")
    })

    win.OnReady(func() {
        win.LoadFile("frontend/index.html")
    })
})

app.Run()
```

In development, `go run .` starts the app directly. In production,
`arc build` compiles it to a signed, sandboxed, distributable bundle.
The source code is the same in both cases.

---

## Mode Comparison

|                          | Development          | Production              |
|--------------------------|----------------------|-------------------------|
| Process count            | 2 (app + arc-host)   | 1                       |
| Controller               | Go binary            | libarc                  |
| arc-host / libarc        | Subprocess           | In-process lib          |
| IPC transport            | Cross-process        | In-process loopback     |
| Go entry point           | `func main()`        | `AppMain` (generated)   |
| `LoadModule`             | Not called           | Called by libarc        |
| Threading                | Go runtime owned     | libarc dispatches       |
| Build requirement        | Go only              | `arc build`             |
| Sandbox safe             | No                   | Yes                     |
| App Store / MSIX ready   | No                   | Yes                     |
| Developer code changes   | None                 | None                    |

---

## Design Principles

**Go is the controller in development.** In production, `libarc` takes
over as controller and Go becomes the module it loads. The developer
sees neither transition.

**`arc build` is a proxy, not a build system.** It calls your local `go
build` toolchain with the right flags, clones libarc via go-git so no
`git` binary is needed, and generates an `arc-project/` directory you
can build with CMake or open in Xcode. It adds no new concepts on top of
the tools you already know.

**IPC is the boundary.** Go and `libarc` communicate only through the
IPC protocol in both modes. The Go runtime stays off the native UI
thread. The boundary is clean and testable.

**Threading is a platform decision.** `libarc` owns the run loop and
owns the thread that `AppMain` runs on. Go makes no threading
assumptions about its environment.

**No CGo in your code.** The C ABI surface is one generated function:
`AppMain`. Everything above that is plain Go.

**No bundled browser.** The webview is the OS's own. Binary size stays
small.

**One codebase, two modes.** The developer writes Go. Arc decides how
to run it.