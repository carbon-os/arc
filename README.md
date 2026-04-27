# arc

A lightweight framework for building cross-platform desktop applications
using Go and web technologies.

Node.js has Electron. Go has Arc.

Write your UI in HTML, CSS, and JavaScript.
Write your application logic in Go.
Connected by a clean, fast IPC layer — backed by the system's native
webview on every platform.

```bash
go get github.com/carbon-os/arc
```

```go
package main

import (
    "github.com/carbon-os/arc"
    "github.com/carbon-os/arc/ipc"
    "github.com/carbon-os/arc/window"
)

func main() {
    app := arc.NewApp(arc.AppConfig{
        Title: "My App",
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
}
```

```js
// renderer
ipc.post("ping", "hello")
ipc.on("pong", (msg) => console.log(msg))
```

---

## Why Arc

Most Go desktop frameworks embed a webview in the same process as your
application logic. Arc takes a different approach.

Your Go application and each webview renderer run as separate OS processes,
connected by Arc's IPC protocol — the same architectural pattern that
powers Electron, applied to native webviews with none of the weight.

```
Your Go app  ──── Arc IPC ────  Native webview renderer
  (main)                           (child process)
```

Each `BrowserWindow` owns exactly one renderer process. Multiple windows
means multiple independent processes — no shared state, no cross-window
interference.

No bundled browser. No CGo in your application code. No GCC requirement.
Just Go, a thin native renderer, and the web stack you already know.

---

## Platforms

| Platform | Engine         | Minimum Version  |
|----------|----------------|------------------|
| Linux    | WebKit2GTK 4.1 | GTK 3, GLib 2.56 |
| macOS    | WKWebView      | macOS 11         |
| Windows  | WebView2       | Windows 10 1903+ |

---

## Getting Started

```bash
go get github.com/carbon-os/arc
```

```bash
go run .
```

---

## API

### App

```go
app := arc.NewApp(arc.AppConfig{
    Title:  "My App",
    WebApp: false,       // enable web mode (default: false)
    Host:   "localhost", // web mode host (default: localhost)
    Port:   8080,        // web mode port (default: 8080)
})

app.OnReady(func() {
    // fired once the app is ready to create windows
})

app.OnClose(func() bool {
    return true // true → allow exit, false → suppress
})

app.Run()  // blocks until all windows are closed
```

`Port` and `Host` are only active when `WebApp: true` and are silently
ignored otherwise.

### BrowserWindow

```go
win := app.NewBrowserWindow(window.Config{
    Title:  "My App",
    Width:  1280,
    Height: 800,
    Debug:  false,
})

// OnReady fires once the renderer process is connected and ready to
// receive commands. LoadFile, LoadHTML, and LoadURL must be called
// from here or after.
win.OnReady(func() {
    win.LoadFile("frontend/index.html")
})

win.OnClose(func() bool {
    return true // true → allow close, false → suppress
})

win.Quit() // programmatically close this window
```

Each `NewBrowserWindow` call spawns a dedicated renderer process.
All windows are fully independent — separate processes, transports,
and IPC connections.

**Renderer**

Arc needs a renderer binary to display your UI. There are three ways
to provide one.

**Prebuilt — recommended**

Set `Prebuilt: true` in `RendererConfig` and Arc will automatically
fetch the correct renderer for your platform and architecture from
GitHub Releases, verify its checksum, and cache it locally.

```go
app := arc.NewApp(arc.AppConfig{
    Title: "My App",
    Renderer: arc.RendererConfig{
        Prebuilt: true,
    },
})
```

```
arc: fetching prebuilt renderer v0.1.4 (darwin/arm64)...
arc: verified sha256 checksum
arc: renderer ready
```

**Custom path — bring your own build**

```go
app := arc.NewApp(arc.AppConfig{
    Title: "My App",
    Renderer: arc.RendererConfig{
        Path: "/path/to/custom/arc-renderer",
    },
})
```

**Local sidecar — ship it yourself**

Place an `arc-renderer` binary next to your application binary.
Arc will find it automatically with no config required.

```
dist/
├── myapp
└── arc-renderer
```

**Renderer lookup order**

```
1. Renderer.Path     — explicit path, always wins
2. ./arc-renderer    — local sidecar next to the binary
3. Prebuilt: true    — fetch from GitHub releases
4. error             — no renderer found
```

**Compile the renderer yourself**

The full C++ renderer source is included in the repository
under `renderer/`. If you want to audit, modify, or self-host it:

```bash
git clone https://github.com/carbon-os/arc
cd arc/renderer
cmake -B build
cmake --build build
```

Requires CMake 3.22+ and a C++20 compiler.
See [renderer/README.md](renderer/README.md) for platform dependencies.

### Load UI

```go
win.LoadHTML("<html>...</html>")     // inline HTML
win.LoadFile("frontend/index.html")  // local file, sibling assets resolve automatically
win.LoadURL("https://example.com")   // external URL
```

### Window

```go
win.SetTitle("new title")
win.SetSize(1920, 1080)
win.Eval("document.body.style.background = 'red'")
```

### IPC — Go → Renderer

```go
ipcMain := win.IPC()

ipcMain.Send("channel", "hello")
ipcMain.SendBytes("channel", []byte{0x01, 0x02, 0x03})
```

```js
ipc.on("channel", (payload) => {
    if (payload instanceof ArrayBuffer) {
        // binary
    } else {
        // text
    }
})
```

### IPC — Renderer → Go

```js
ipc.post("channel", "hello")

// binary
const buf = new Uint8Array([0x01, 0x02, 0x03]).buffer
ipc.post("channel", buf)
```

```go
ipcMain := win.IPC()

ipcMain.On("channel", func(msg ipc.Message) {
    if msg.IsText()   { s := msg.Text()  }
    if msg.IsBinary() { b := msg.Bytes() }
})

ipcMain.Off("channel") // unregister
```

### Multi-window

Each window gets its own IPC handle. Handlers are scoped to their
window's renderer process — no channel collisions, no ambiguity about
which window sent a message.

```go
app.OnReady(func() {
    main := app.NewBrowserWindow(window.Config{Title: "Main", Width: 1280, Height: 800})
    settings := app.NewBrowserWindow(window.Config{Title: "Settings", Width: 600, Height: 400})

    mainIPC := main.IPC()
    settingsIPC := settings.IPC()

    mainIPC.On("open-settings", func(msg ipc.Message) {
        settings.Show()
    })

    settingsIPC.On("save", func(msg ipc.Message) {
        mainIPC.Send("settings-updated", msg.Text())
    })

    main.OnReady(func() { main.LoadFile("frontend/main.html") })
    settings.OnReady(func() { settings.LoadFile("frontend/settings.html") })
})
```

---

## Web Mode

Arc apps can run as a web server, serving your UI in a browser instead
of a native window. This lets the same binary run on a Linux server with
no display server — a developer or user can simply open their browser and
the app renders there, over HTTP and WebSocket.

Web mode is **opt-in**. An Arc app with no config is purely a native
desktop app — no web surface is exposed, and no surprise behaviour occurs
on headless machines.

### Enabling Web Mode

```go
app := arc.NewApp(arc.AppConfig{
    Title:  "My App",
    WebApp: true,
    Host:   "localhost", // restrict to loopback (default)
    Port:   8080,
})
```

Set `Host` to `"0.0.0.0"` to expose the app on the network. This is an
explicit opt-in — `localhost` is always the default.

### How It Works

When web mode is active, Arc replaces the native webview renderer with an
HTTP + WebSocket server. Your frontend assets are served as static files.
IPC is transparently bridged over WebSocket — your Go and JS code is
unchanged.

```
Native mode:   Go app ──── IPC (pipe) ────  webview process
Web mode:      Go app ──── IPC (WS)   ────  browser tab
```

### Runtime Flags

`AppConfig` provides the defaults. Runtime flags override them:

```bash
./myapp                  # native window
./myapp --webapp         # force web mode
./myapp --port 9000      # override port
./myapp --host 0.0.0.0   # expose to network
```

Arc also auto-detects headless environments. If `WebApp: true` and no
display server is found (`DISPLAY` / `WAYLAND_DISPLAY` unset), Arc falls
back to web mode automatically without requiring an explicit flag.

### Behaviour When WebApp is False

If `WebApp: false` (the default) and `--webapp` is passed at runtime,
Arc exits with a clear message rather than silently doing nothing:

```
arc: web mode is not enabled for this application.
     set WebApp: true in arc.AppConfig to enable it.
     exiting.
```

### Transport Comparison

| | Native mode | Web mode |
|---|---|---|
| Renderer | OS native webview | Browser tab |
| IPC transport | Unix socket / pipe | WebSocket |
| Asset delivery | Local filesystem | HTTP static server |
| `win.Eval()` | Direct to webview | Broadcast over WebSocket |
| Multi-client | Multiple windows | Single session (one active tab) |

---

## Project Structure

```
myapp/
├── main.go
├── frontend/
│   ├── index.html
│   ├── app.js
│   └── style.css
└── arc.toml
```

---

## Building

```bash
go build -o dist/myapp
```

Produces a self-contained binary with your frontend embedded.

```
dist/
├── myapp
└── arc-renderer
```

The renderer ships as a lightweight sidecar alongside your binary.
End users need no compiler, no runtime, and no dependencies installed.

In web mode, `arc-renderer` is not used — the binary serves the UI
directly. It is safe to omit from server deployments.

---

## Repository Structure

```
github.com/carbon-os/arc/
│
├── renderer/                   # C++ renderer source
│
├── window/                     # BrowserWindow
│   └── window.go
│
├── ipc/                        # IPC (On, Off, Send, SendBytes, Message)
│   └── ipc.go
│
├── runtime/                    # Go process management + IPC transport
│   ├── process.go
│   ├── ipc.go
│   ├── bundle.go
│   └── transport.go
│
├── webapp/                     # Web mode HTTP + WebSocket server
│   ├── server.go
│   └── ws.go
├── go.mod
└── README.md
```

---

## Comparison

|                    | Arc     | Electron   |
|--------------------|---------|------------|
| Native webview     | ✅      | ❌         |
| Separate processes | ✅      | ✅         |
| Bundled browser    | ❌      | ✅         |
| Binary size        | ~5mb    | ~200mb     |
| App language       | Go      | JavaScript |
| Web / server mode  | ✅      | ❌         |
| Multi-window       | ✅      | ✅         |

---

## License

MIT