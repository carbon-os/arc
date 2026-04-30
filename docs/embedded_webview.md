# Embedded Web Views

## Concept

Arc supports embedding isolated web views inside your application window
using a native overlay model. Each web view is a native view added to a
container that fills the main window — it moves with the window, is
clipped by the window's bounds, and is driven entirely from Go.

```
┌─────────────────────────────────┐
│         Main App Window         │
│                                 │
│   ┌─────────────────────────┐   │
│   │      Web View           │   │  ← native view inside the window
│   │   (isolated context)    │   │
│   └─────────────────────────┘   │
│                                 │
└─────────────────────────────────┘
```

The web view lives inside the window's view hierarchy — not as a separate
OS window. This means it moves and resizes with the main window for free,
can never drift outside the window's bounds, and z-ordering among
overlapping web views is stable across resize and focus changes. It runs
in a fully isolated context with its own session and storage.

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
overlay.Move(x, y)            // reposition
overlay.Resize(w, h)          // resize
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
Only affects ordering among web views — the main window content is always
behind all of them.

### Destruction

```go
overlay.Destroy()
```

Removes the web view from the window and releases all associated
resources. After `Destroy`, the web view handle is invalid and must not
be used.

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

Web views are clipped to the window's bounds — a web view positioned
partially or fully outside the window area will be clipped at the edge
and will never appear on the desktop outside the main window.

---

## Isolation

Each web view runs in a fully isolated context:

- Separate session and storage
- No access to the main window's JS, DOM, or cookies
- No access to other web views

---

## Platform Implementation

On all platforms the implementation uses a flipped container view that
fills the main window's content area. The main WKWebView/WebView2/WebKitWebView
and all embedded web views are children of this container. This gives
correct move, resize, clip, and z-order behaviour without any manual
screen-coordinate tracking.

| Platform | Container        | Embed primitive  | Flipped coords       | Clipping                   |
|----------|------------------|------------------|----------------------|----------------------------|
| macOS    | `ArcFlippedView` (`NSView`, `isFlipped=YES`) | `WKWebView` subview | `isFlipped` override on container | `layer.masksToBounds = YES` |
| Windows  | `HWND` with `WS_CHILD` + `WS_CLIPCHILDREN` | `WebView2` as child `HWND` | Y-axis flipped manually in `WM_SIZE` / `SetWindowPos` | `WS_CLIPSIBLINGS` on each child; container clips naturally |
| Linux    | `GtkFixed` inside a `GtkOverlay` | `WebKitWebView` added via `gtk_fixed_put` | `gtk_fixed_move` uses top-left Y-down natively | `gtk_widget_set_clip` on the container |

### macOS detail

A single `ArcFlippedView` (`NSView` subclass with `isFlipped = YES`,
`wantsLayer = YES`, `layer.masksToBounds = YES`) is set as the window's
content view. The main `WKWebView` fills it via `autoresizingMask`. Each
embedded web view is a `WKWebView` added as a subview with `addSubview:positioned:relativeTo:`.
Because everything lives in the same view hierarchy, move and resize are
handled entirely by AppKit — no delegate hooks, no screen-coordinate
arithmetic, no manual re-clamping.

### Windows detail

The main window is created with `WS_CLIPCHILDREN`. A child `HWND`
container (also `WS_CHILD`) fills the client area and is kept in sync via
`WM_SIZE`. Each embedded `WebView2` controller is initialised into its
own child `HWND` inside the container using
`CreateCoreWebView2Controller`. Position and size are set with
`SetWindowPos` using client-area coordinates (top-left, Y-down — no
flipping needed on Windows). `WS_CLIPSIBLINGS` on child windows prevents
them from painting over each other outside their bounds.

### Linux detail

A `GtkOverlay` is set as the top-level container. The main
`WebKitWebView` is the base child. A `GtkFixed` is added as the overlay
widget; each embedded `WebKitWebView` is placed into it with
`gtk_fixed_put(fixed, widget, x, y)` and repositioned with
`gtk_fixed_move`. `GTK_WIDGET_SET_VISUAL` and an `rgba` colormap give
the container a transparent background so only the web views paint.
Clipping to the window is enforced by the `GtkOverlay`'s own allocation
logic.

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