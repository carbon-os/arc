# arc

**Build desktop apps with Go and web technologies.**

arc lets you write your application logic in Go and your UI in HTML, CSS, and JavaScript — all shipped as a single, native binary. No runtime to install. No separate process to manage in production.

---

## Why arc?

- **Go is your backend.** Business logic, file system, networking, databases — all in Go. No context switching.
- **The web is your UI.** Use any frontend framework you like. React, Vue, Svelte, vanilla JS — arc doesn't care.
- **Truly native.** Your app uses the OS's built-in WebView. No bundled browser engine bloating your distributable.
- **First-class IPC.** A clean, typed message channel connects Go and JavaScript bidirectionally, with no boilerplate.
- **App Store ready.** Built-in StoreKit and Microsoft Store billing, sandbox-safe production builds, and a one-command build pipeline.

---

## Installation

```bash
GOPROXY=direct go install github.com/carbon-os/arc/cmd/arc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

---

## Quick start

```bash
mkdir myapp && cd myapp
go mod init myapp
```

**main.go**

```go
package main

import (
    "github.com/carbon-os/arc"
    wcfg "github.com/carbon-os/arc/window"
)

func main() {
    app := arc.NewApp(arc.AppConfig{
        Title: "My App",
        Renderer: arc.RendererConfig{Path: "./arc-host"},
    })

    app.OnReady(func() {
        win := app.NewWindow(wcfg.Config{
            Title:  "My App",
            Width:  1200,
            Height: 800,
        })

        win.OnReady(func() {
            win.LoadURL("http://localhost:3000")

            // Listen for messages from JavaScript
            win.IPC().On("greet", func(msg ipc.Message) {
                name := msg.Text()
                win.IPC().Send("reply", "Hello, "+name+"!")
            })
        })
    })

    app.Run()
}
```

**In your JavaScript:**

```js
// Send a message to Go
ipc.post("greet", "world")

// Receive a reply
ipc.on("reply", (msg) => {
    console.log(msg) // "Hello, world!"
})
```

Run it in development — no build step needed:

```bash
go run .
```

---

## How it works

arc has two modes:

| | Development | Production |
|---|---|---|
| Command | `go run .` | `arc build` → `cmake --build` |
| Process model | Go spawns renderer as subprocess | Single unified process |
| Build requirements | Go toolchain only | CMake + C++ compiler |
| App Store ready | No | Yes |

In development, arc spawns the native WebView host as a child process and communicates with it over a local IPC channel (Unix socket on macOS/Linux, named pipe on Windows). In production, your Go logic is compiled to a shared library and linked directly into the native host — one binary, no moving parts.

---

## Windows

### Creating windows

```go
app.OnReady(func() {
    win := app.NewWindow(wcfg.Config{
        Title:    "Dashboard",
        Width:    1400,
        Height:   900,
        NoResize: false,
        Debug:    true, // enables DevTools
    })
})
```

### Window management

```go
win.SetTitle("Updated Title")
win.SetSize(1600, 1000)
win.SetPosition(100, 100)
win.Center()
win.Minimize()
win.Maximize()
win.Restore()
win.SetFullscreen(true)
win.SetAlwaysOnTop(true)
win.SetMinSize(800, 600)
win.SetMaxSize(2560, 1440)
```

### Platform effects

Apply native backdrop effects to give your app a premium feel:

```go
win.SetEffect("vibrancy")  // macOS — frosted glass
win.SetEffect("mica")      // Windows 11 — translucent material
win.SetEffect("acrylic")   // Windows — acrylic blur
win.ClearEffect()
```

### Lifecycle events

```go
win.OnResize(func(w, h int) { fmt.Println("resized:", w, h) })
win.OnMove(func(x, y int)   { fmt.Println("moved:", x, y) })
win.OnFocus(func()          { fmt.Println("focused") })
win.OnBlur(func()           { fmt.Println("blurred") })
win.OnClose(func()          { fmt.Println("closed") })
win.OnStateChange(func(state string) {
    // "normal" | "minimized" | "maximized" | "fullscreen"
})
```

---

## IPC — Go ↔ JavaScript

The IPC bridge is the backbone of every arc app. Every window and overlay WebView has its own bridge.

### Go → JavaScript

```go
// Send a string
win.IPC().Send("status", "connected")

// Send binary data (received as Uint8Array in JS)
win.IPC().SendBytes("image", pngBytes)
```

### JavaScript → Go

```go
win.IPC().On("save", func(msg ipc.Message) {
    content := msg.Text()
    os.WriteFile("output.txt", []byte(content), 0644)
})

win.IPC().Off("save") // unregister when done
```

### Working with structured data

```go
win.IPC().On("submit", func(msg ipc.Message) {
    var form struct {
        Name  string `json:"name"`
        Email string `json:"email"`
    }
    json.Unmarshal(msg.Raw(), &form)
    fmt.Println(form.Name, form.Email)
})
```

---

## Loading content

```go
win.LoadURL("https://example.com")       // remote URL
win.LoadURL("http://localhost:5173")     // local dev server
win.LoadFile("/path/to/index.html")      // local file
win.LoadHTML("<h1>Hello</h1>")           // inline HTML

win.Reload()
win.GoBack()
win.GoForward()
win.SetZoom(1.5) // 150%
```

### Navigation events

```go
win.OnLoadStart(func(url string)        { fmt.Println("loading:", url) })
win.OnLoadFinish(func(url string)       { fmt.Println("loaded:", url) })
win.OnLoadFailed(func(url, err string)  { fmt.Println("failed:", err) })
win.OnNavigate(func(url string)         { fmt.Println("navigated:", url) })
win.OnTitleChange(func(title string)    { fmt.Println("title:", title) })
```

---

## Overlay WebViews

Overlay WebViews float on top of a window's primary WebView at a fixed position. Use them for sidebars, drawers, popups, or picture-in-picture panels — each independently positioned, sized, shown, and hidden.

```go
win.OnReady(func() {
    sidebar := win.NewWebView(webview.Config{
        X: 0, Y: 0,
        Width: 280, Height: 900,
        Debug: false,
    })

    sidebar.LoadURL("http://localhost:3000/sidebar")
    sidebar.Show()

    // Independent IPC per overlay
    sidebar.IPC().On("navigate", func(msg ipc.Message) {
        win.LoadURL(msg.Text())
    })

    // Z-ordering
    sidebar.BringToFront()
    sidebar.SendToBack()
    sidebar.SetZOrder(10)

    // Reposition and resize at runtime
    sidebar.SetPosition(0, 60)
    sidebar.SetSize(320, 900)
    sidebar.SetBounds(0, 60, 320, 900)

    sidebar.Hide()
    sidebar.Destroy()
})
```

---

## JavaScript evaluation

Execute arbitrary JavaScript in any WebView from Go:

```go
win.Eval(`document.body.style.background = "red"`)
win.Eval(`window.dispatchEvent(new CustomEvent("data-ready"))`)
```

---

## App lifecycle

```go
app := arc.NewApp(arc.AppConfig{
    Title:   "My App",
    Logging: true, // structured IPC log to stdout
    Renderer: arc.RendererConfig{
        Path: "./arc-host",
    },
})

app.OnReady(func() {
    // Create windows here
})

app.OnClose(func() bool {
    // Return false to prevent shutdown; true to allow it
    return true
})

app.Ping()     // health-check the host
app.Shutdown() // graceful shutdown
app.Run()      // blocks until the app exits
```

---

## Billing

arc ships first-class in-app purchase support for both the Apple App Store and the Microsoft Store. Both integrations follow the same callback-oriented pattern and are wired up before calling `app.Run`.

### Apple App Store (macOS)

```go
store := billing.NewAppleStore(app) // nil on non-Apple platforms
if store != nil {
    store.OnProductsFetched(func(products []billing.AppleProduct) {
        for _, p := range products {
            fmt.Println(p.ID, p.DisplayPrice)
        }
    })

    store.OnPurchaseCompleted(func(result billing.ApplePurchaseResult) {
        if result.Error != nil {
            fmt.Println("purchase failed:", result.Error)
            return
        }
        fmt.Println("purchased:", result.Transaction.ID)
    })

    store.OnEntitlementsChanged(func() {
        store.CurrentEntitlements()
    })

    store.FetchProducts([]billing.AppleProductSpec{
        {ID: "com.example.app.pro.monthly", Kind: billing.AppleKindAutoRenewable},
    })
}
```

#### One-shot entitlement check

```go
store.CheckEntitlement("com.example.app.pro.monthly", func(ent *billing.AppleEntitlement) {
    if ent == nil {
        fmt.Println("not subscribed")
        return
    }
    fmt.Println("subscribed, expires:", ent.ExpiresAt)
})
```

#### Refunds

```go
store.RequestRefund(transactionID, func(status billing.AppleRefundStatus) {
    fmt.Println("refund status:", status)
})
```

### Microsoft Store (Windows)

```go
store := billing.NewMicrosoftStore(app) // nil on non-Windows platforms
if store != nil {
    store.OnProductsFetched(func(products []billing.MicrosoftProduct) {
        for _, p := range products {
            fmt.Println(p.ID, p.IsOwned)
        }
    })

    store.OnPurchaseCompleted(func(result billing.MicrosoftPurchaseResult) {
        fmt.Println("status:", result.Status)
    })

    store.FetchProducts([]string{"9NBLGGH4NNS1"})
    store.Purchase("9NBLGGH4NNS1")

    // Consumables
    store.ReportConsumable("coin_pack_100", 100, uuid.NewString(), func(r billing.MicrosoftConsumeResult) {
        fmt.Println("fulfilled:", r.Status)
    })
}
```

---

## Production builds

### arc.json (optional)

Place alongside your `main.go` to configure app metadata and billing:

```json
{
  "app": {
    "name": "My App",
    "bundle_id": "com.example.myapp"
  },
  "billing": {
    "identifier": "a1b2c3d4",
    "subscription_groups": [
      {
        "id": "e5f6a7b8",
        "name": "Pro",
        "subscriptions": [
          {
            "product_id": "com.example.myapp.pro.monthly",
            "reference_name": "Pro Monthly",
            "display_price": "4.99",
            "recurring_period": "P1M",
            "localizations": [
              {
                "locale": "en_US",
                "display_name": "Pro Monthly",
                "description": "Full access, billed monthly."
              }
            ]
          }
        ]
      }
    ]
  }
}
```

### Build

```bash
# Standard
arc build .

# Custom binary name
arc build -o myapp .

# With explicit config
arc build -o myapp --config path/to/arc.json .

# Pass extra go build flags
arc build -race -o myapp .
```

`arc build` handles everything:

1. Clones or updates libarc (no `git` binary required — uses pure Go)
2. Configures and builds the native WebView host with CMake
3. Runs `go mod tidy`
4. Compiles your Go module as a shared library
5. Generates `arc-project/` with a ready-to-build CMake tree
6. Pre-configures the build tree (Xcode project on macOS)
7. Generates the `.storekit` config if billing is configured

Then build the final binary:

```bash
cd arc-project && cmake --build build
```

Or open `arc-project/build/*.xcodeproj` in Xcode for a full native debug session with breakpoints and Instruments.

### Output layout

```
myapp/
├── main.go
├── arc.json
├── go.mod
└── arc-project/
    ├── CMakeLists.txt
    ├── main.cpp
    ├── libarc-module.dylib     ← your compiled Go logic
    ├── libarc.dylib            ← native WebView host
    ├── myapp.storekit          ← generated (macOS, if billing configured)
    ├── libarc/
    │   └── include/
    └── build/                  ← cmake build tree, ready to go
```

---

## Platform requirements

| Platform | Requirements |
|---|---|
| macOS | Xcode Command Line Tools, CMake ≥ 3.22 |
| Windows | Visual Studio 2022, CMake ≥ 3.22, vcpkg (`VCPKG_ROOT` set) |
| Linux | GCC or Clang, CMake ≥ 3.22, `libwebkit2gtk-4.1-dev`, `libgtk-3-dev` |

**Linux dependencies:**

```bash
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev

# Fedora
sudo dnf install webkit2gtk4.1-devel gtk3-devel
```

---

## License

MIT