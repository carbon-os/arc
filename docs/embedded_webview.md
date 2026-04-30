# Embedded Web Views

## Concept

Arc supports embedding isolated web views inside your application window
using a native overlay model. Each web view is a real top-level OS window
sitting on top of your main window — borderless, attached, and driven
entirely from Go.

```
┌─────────────────────────────────┐
│         Main App Window         │
│                                 │
│   ┌─────────────────────────┐   │
│   │      Web View           │   │  ← real native window on top
│   │   (isolated context)    │   │
│   └─────────────────────────┘   │
│                                 │
└─────────────────────────────────┘
```

The web view is a sibling window attached to the main window — it moves
with it, minimizes with it, and stays on top of it. It runs in a fully
isolated context with its own session and storage.

---

## API

### Creating a Web View

```go
overlay := win.NewWebView(webview.Config{
    X:      100,
    Y:      100,
    Width:  600,
    Height: 400,
    ZOrder: 0,
})
```

`X` and `Y` are relative to the main window's top-left corner.
`ZOrder` sets the initial stacking order among web views on this window.
Lower values are further back, higher values are closer to the front.

### Loading Content

```go
overlay.LoadURL("https://example.com")       // external URL
overlay.LoadFile("frontend/panel.html")      // local file
overlay.LoadHTML("<h1>Hello</h1>")           // inline HTML
```

### Visibility

```go
overlay.Show()
overlay.Hide()
```

### Position and Size

```go
overlay.Move(x, y)         // reposition
overlay.Resize(w, h)       // resize
overlay.SetBounds(x, y, w, h) // reposition and resize atomically
```

Prefer `SetBounds` when showing a web view at a new position — it avoids
a visible jump that can occur when `Move` and `Show` are called
separately.

```go
overlay.SetBounds(x, y, w, h)
overlay.Show()
```

### Stacking

```go
overlay.SetZOrder(1)
```

Re-stacks this web view relative to other web views on the same window.
Only affects ordering among web views — the main window is always behind
all of them.

### Destruction

```go
overlay.Destroy()
```

Tears down the native window and releases all associated resources.
After `Destroy`, the web view handle is invalid and must not be used.

---

## Coordinate System

Coordinates are relative to the main window's top-left corner.

```
(0, 0)                       (windowWidth, 0)
  ┌──────────────────────────────┐
  │                              │
  │                              │
  └──────────────────────────────┘
(0, windowHeight)
```

```go
// Centered
x := (windowWidth - overlayWidth) / 2
y := (windowHeight - overlayHeight) / 2
overlay.SetBounds(x, y, overlayWidth, overlayHeight)
```

---

## Isolation

Each web view runs in a fully isolated context:

- Separate session and storage
- No access to the main window's JS, DOM, or cookies
- No access to other web views

---

## Platform Implementation

| Platform | Window primitive | Move call          |
|----------|------------------|--------------------|
| macOS    | `NSPanel`        | `setFrame:display:`|
| Windows  | `HWND`           | `SetWindowPos`     |
| Linux    | `GtkWindow`      | `gtk_window_move`  |

On all platforms the web view window is borderless, non-activating by
default, and attached to the main window so it moves and minimizes
together with it.

---

## Full Example

```go
app.OnReady(func() {
    win := app.NewBrowserWindow(window.Config{
        Width:  1280,
        Height: 800,
    })

    overlay := win.NewWebView(webview.Config{
        X:      100,
        Y:      100,
        Width:  600,
        Height: 400,
        ZOrder: 0,
    })

    overlay.LoadURL("https://example.com")
    overlay.Show()

    win.OnReady(func() {
        win.LoadFile("frontend/index.html")
    })
})

app.Run()
```