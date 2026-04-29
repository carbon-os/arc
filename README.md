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

Arc uses the OS's own webview engine — WKWebView on macOS, WebView2 on
Windows, WebKit2GTK on Linux. No bundled browser, no CGo in your
application code, no GCC requirement at runtime.

The Go and native layers communicate exclusively through Arc's IPC
protocol. In development this runs across two processes; in production
both are loaded into a single sandboxed host process by `arc build`. Your
application code is identical in both cases — the mode is an
infrastructure detail Arc manages for you.

```
Development    Go binary ──── IPC (socket / pipe) ────  arc-host (subprocess)

Production     ┌─────────────────────────────────────────────────────┐
               │  host process                                       │
               │  libarc  ──── IPC ────  libarc-module (your Go)    │
               └─────────────────────────────────────────────────────┘
```

See [Architecture](arch.md) for a full breakdown.

---

## Platforms

| Platform | Engine         | Minimum Version  |
|----------|----------------|------------------|
| macOS    | WKWebView      | macOS 11         |
| Windows  | WebView2       | Windows 10 1903+ |
| Linux    | WebKit2GTK 4.1 | GTK 3, GLib 2.56 |

---

## Getting Started

```bash
go get github.com/carbon-os/arc
go run .
```

During development `go run .` is all you need. Arc spawns `arc-host` as
a subprocess automatically — no C compiler, no CMake, no Xcode.

For a distribution-ready binary, use the `arc` CLI:

```bash
arc build -o dist/myapp .
```

See [Building](#building) below.

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

// OnReady fires once the native window and webview are live.
// LoadFile, LoadHTML, and LoadURL must be called from here or after.
win.OnReady(func() {
    win.LoadFile("frontend/index.html")
})

win.OnClose(func() bool {
    return true // true → allow close, false → suppress
})

win.Quit() // programmatically close this window
```

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
window — no channel collisions, no ambiguity about which window sent a
message.

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

Set `Host` to `"0.0.0.0"` to expose the app on the network.

### How It Works

When web mode is active, Arc replaces the native webview with an HTTP +
WebSocket server. Your frontend assets are served as static files. IPC
is transparently bridged over WebSocket — your Go and JS code is
unchanged.

```
Native mode:   Go app ──── IPC (socket / pipe) ────  arc-host / libarc
Web mode:      Go app ──── IPC (WebSocket)     ────  browser tab
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

---

## Building

### Development

```bash
go run .
```

Arc spawns `arc-host` as a subprocess. No C compiler, no CMake, no
Xcode required.

### Production

```bash
arc build -o dist/myapp .
```

`arc build` is a thin wrapper around your local `go build` toolchain. It:

1. Clones `libarc` via go-git (no `git` binary required)
2. Compiles your Go code as a shared library with `-buildmode=c-shared`
3. Generates a self-contained `arc-project/` directory with a CMake build
   wired to `libarc`

To produce the final binary:

```bash
cd arc-project/
cmake --build build
```

Or open `arc-project/` in Xcode for a full native debug session. See
[libarc/README.md](renderer/README.md) for platform build prerequisites
and the full `arc-project/` layout.

Production builds run as a single process, satisfy macOS App Sandbox,
and are ready for Mac App Store and Windows MSIX distribution.

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

## Repository Structure

```
github.com/carbon-os/arc/
│
├── renderer/                   # C++ renderer source (libarc / arc-host)
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
| Bundled browser    | ❌      | ✅         |
| Binary size        | ~5 MB   | ~200 MB    |
| App language       | Go      | JavaScript |
| Single-process prod| ✅      | ❌         |
| Sandbox / App Store| ✅      | ❌         |
| Web / server mode  | ✅      | ❌         |
| Multi-window       | ✅      | ✅         |

---

## License

MIT