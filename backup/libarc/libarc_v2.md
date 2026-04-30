# libarc v2

A cross-platform native webview framework for desktop applications.
Language agnostic — controlled entirely over IPC.

---

## Overview

libarc owns the native webview, the window, the run loop, and the IPC
socket. It does not know or care what language is on the other side of
the socket. Any process that speaks the protocol can drive it.

```
your app (any language)
        │
        │  connects to
        ▼
   libarc (listening)
        │
        ├── native window
        ├── views (webview / raw)
        └── run loop (main thread)
```

libarc **listens**. Your application **connects**. libarc is the service
provider — your app is the client.

In development, `arc-host` is a standalone binary. It binds the socket,
writes the path to stdout, then your app connects. In production, libarc
binds the socket before dispatching your app's entry point on a
background thread.

---

## Webview Engines

| Platform | Engine         | Minimum OS          |
|----------|----------------|---------------------|
| macOS    | WKWebView      | macOS 11            |
| Windows  | WebView2       | Windows 10 1903+    |
| Linux    | WebKit2GTK 4.1 | GTK 3, GLib 2.56    |

No browser is bundled. libarc uses the engine the OS already provides.

---

## IPC

All communication between your application and libarc happens over a
Unix socket (macOS / Linux) or named pipe (Windows).

libarc binds and listens. Your application connects.

### Frame Format

Every message is a frame. There are only two frame types:

```
┌────────────┬────────────────┬───────────────┐
│ type (1B)  │ length (4B LE) │ payload       │
└────────────┴────────────────┴───────────────┘

0x01 = JSON
0x02 = Binary
```

**JSON frames** carry all commands and events. Self-describing and
forward-compatible. Unknown fields are ignored — this is how the
protocol evolves without breaking changes.

**Binary frames** carry raw bulk data — audio, video frames, file bytes,
anything that does not belong in JSON. A binary frame is always preceded
by a JSON frame that describes it.

```
→ JSON:   { "cmd": "webview_post_binary", "view_id": "v1", "channel": "video", "size": 16384 }
→ Binary: <raw bytes — 16384>
```

### Session Sequence

```
client                              libarc
  │                                    │
  │  connects to socket                │
  │ ─────────────────────────────────> │
  │ <───────────────────────────────── │  { "event": "ready" }
  │                                    │
  │  { "cmd": "window_create", ... }   │
  │ ─────────────────────────────────> │
  │ <───────────────────────────────── │  { "event": "window_ready", "window_id": "w1" }
  │                                    │
  │  { "cmd": "view_create", ... }     │
  │ ─────────────────────────────────> │
  │ <───────────────────────────────── │  { "event": "view_ready", "view_id": "v1", "view_type": "webview" }
  │                                    │
  │  { "cmd": "webview_load_url", ... }│
  │ ─────────────────────────────────> │
  │                                    │  navigates webview
  │ <───────────────────────────────── │  { "event": "load_finish", ... }
  │                                    │
  │  { "cmd": "quit" }                 │
  │ ─────────────────────────────────> │
  │ <───────────────────────────────── │  { "event": "closed" }
```

---

## Windows

A window is the native OS window. It owns the chrome, title bar, sizing
constraints, and platform decorations. Views live inside it.

### window_create

```json
{
    "cmd": "window_create",
    "title": "My App",
    "width": 1280,
    "height": 800,
    "min_width": 800,
    "min_height": 600,
    "max_width": null,
    "max_height": null,
    "resizable": true,
    "center": true,
    "frameless": false,
    "transparent": false,
    "always_on_top": false,
    "platform": {
        "mac": {
            "vibrancy": "sidebar",
            "title_bar_style": "hidden"
        },
        "win": {
            "mica": true
        }
    }
}
```

**Response**
```json
{ "event": "window_ready", "window_id": "w1" }
```

### window_set_title
```json
{ "cmd": "window_set_title", "window_id": "w1", "title": "New Title" }
```

### window_set_size
```json
{ "cmd": "window_set_size", "window_id": "w1", "width": 1440, "height": 900 }
```

### window_set_min_size
```json
{ "cmd": "window_set_min_size", "window_id": "w1", "width": 800, "height": 600 }
```

### window_set_max_size
```json
{ "cmd": "window_set_max_size", "window_id": "w1", "width": 2560, "height": 1440 }
```

### window_center
```json
{ "cmd": "window_center", "window_id": "w1" }
```

### window_fullscreen
```json
{ "cmd": "window_fullscreen", "window_id": "w1", "enabled": true }
```

### window_minimize
```json
{ "cmd": "window_minimize", "window_id": "w1" }
```

### window_maximize
```json
{ "cmd": "window_maximize", "window_id": "w1" }
```

### window_close
```json
{ "cmd": "window_close", "window_id": "w1" }
```

**Window Events**
```json
{ "event": "window_ready",      "window_id": "w1" }
{ "event": "window_closed",     "window_id": "w1" }
{ "event": "window_resized",    "window_id": "w1", "width": 1440, "height": 900 }
{ "event": "window_moved",      "window_id": "w1", "x": 200, "y": 100 }
{ "event": "window_focused",    "window_id": "w1" }
{ "event": "window_unfocused",  "window_id": "w1" }
{ "event": "window_fullscreen", "window_id": "w1", "enabled": true }
```

---

## Views

A view is a surface that lives inside a window. All views share the same
geometry, visibility, and lifecycle API. The `view_type` field at
creation time determines what the surface renders.

| view_type | Description                                      |
|-----------|--------------------------------------------------|
| `webview` | A native browser engine instance                 |
| `raw`     | A shared memory buffer rendered directly         |

The only distinction between a `root` and `overlay` view is `layout` —
how the view positions itself within its window.

| layout    | Behaviour                                                |
|-----------|----------------------------------------------------------|
| `root`    | Fills the window. Resizes automatically with it.         |
| `overlay` | Positioned explicitly. x, y, width, height, z required.  |

Layout is a creation-time config flag, not a type. The full view API
surface is available on every view regardless of layout or view_type.

---

### view_create

**Webview — Root**
```json
{
    "cmd": "view_create",
    "window_id": "w1",
    "view_type": "webview",
    "layout": "root"
}
```

**Webview — Overlay**
```json
{
    "cmd": "view_create",
    "window_id": "w1",
    "view_type": "webview",
    "layout": "overlay",
    "x": 100,
    "y": 100,
    "width": 600,
    "height": 400,
    "z": 1
}
```

**Raw — Overlay**
```json
{
    "cmd": "view_create",
    "window_id": "w1",
    "view_type": "raw",
    "layout": "overlay",
    "x": 0,
    "y": 0,
    "width": 1280,
    "height": 720,
    "z": 2,
    "shm_channel": "id234234",
    "pixel_format": "bgra8"
}
```

| pixel_format | Description                  |
|--------------|------------------------------|
| `bgra8`      | 8-bit BGRA, packed           |
| `rgba8`      | 8-bit RGBA, packed           |
| `nv12`       | YUV 4:2:0, Y plane + UV plane|

**Response**
```json
{ "event": "view_ready", "view_id": "v1", "view_type": "webview" }
{ "event": "view_ready", "view_id": "v2", "view_type": "raw" }
```

---

### Position and Size

These commands apply to all view types and layouts. On a root view,
move and resize override the fill behaviour for that view.

```json
{ "cmd": "view_move",       "view_id": "v1", "x": 200, "y": 200 }
{ "cmd": "view_resize",     "view_id": "v1", "width": 800, "height": 500 }
{ "cmd": "view_set_bounds", "view_id": "v1", "x": 200, "y": 200, "width": 800, "height": 500 }
{ "cmd": "view_set_z",      "view_id": "v1", "z": 2 }
```

Prefer `view_set_bounds` when repositioning and showing simultaneously
to avoid a visible jump from separate move and show calls.

**View Events**
```json
{ "event": "view_resized", "view_id": "v1", "view_type": "raw", "width": 800, "height": 500 }
```

`view_resized` is fired for `raw` views so the producer knows to update
the shm buffer dimensions. The producer should drain in-flight frames,
update the shm header, and begin writing at the new dimensions before
the next poll cycle.

---

### Visibility

```json
{ "cmd": "view_show", "view_id": "v1" }
{ "cmd": "view_hide", "view_id": "v1" }
```

---

### Destruction

```json
{ "cmd": "view_destroy", "view_id": "v1" }
```

After `view_destroy` the view id is invalid and must not be used.
For raw views, libarc stops polling the shm segment on destroy. The
producer is responsible for unlinking the segment.

---

## Raw View — Shared Memory

A raw view reads pixel data directly from a shared memory segment. The
producer process writes frames into the segment. libarc polls the
segment internally — no per-frame IPC traffic is required.

### Platform Shm

| Platform | Mechanism                                    | Channel name convention |
|----------|----------------------------------------------|-------------------------|
| macOS    | `shm_open`                                   | Prepend `/`             |
| Linux    | `shm_open` (prefer over `/dev/shm` directly) | Prepend `/`             |
| Windows  | `CreateFileMapping(INVALID_HANDLE_VALUE, ...)` | No slash               |

The `shm_channel` string in `view_create` is used verbatim as the
segment name on each platform. Both libarc and the producer open the
same name independently — no path coordination required.

### Shm Layout

A fixed header lives at offset 0. Pixel data follows at a 4096-byte
(one page) aligned offset. Double buffering is supported via
`write_buf`.

```c
struct ShmHeader {
    uint32_t magic;         // 0x41524356 "ARCV"
    uint32_t frame_id;      // monotonically increasing, producer writes
    uint32_t width;         // current frame width in pixels
    uint32_t height;        // current frame height in pixels
    uint32_t pixel_format;  // 0=BGRA8, 1=RGBA8, 2=NV12
    uint32_t write_buf;     // double-buffer index: 0 or 1
    uint32_t _pad[2];
};

// buf[0] at offset 4096
// buf[1] at offset 4096 + stride * height
```

The producer increments `frame_id` and updates `write_buf` **after**
finishing a write. libarc reads `write_buf` to select which buffer to
upload. libarc always trusts `width` and `height` from the header —
not the creation-time or resize-command dimensions — so resize races
are handled naturally.

### Polling Techniques

libarc's internal poll loop can be implemented with any of the
following. The technique is an internal implementation detail and does
not affect the protocol.

| Technique | Notes |
|-----------|-------|
| Atomic frame counter spin | Reader loops until `frame_id != last_seen`. Simple. |
| Dirty flag | Producer sets a byte to `1` after write, reader clears after consume. One cache line. |
| Futex / WaitOnAddress / `ulock_wait` | OS-assisted wait on the counter. Zero CPU when idle, wakes immediately on write. |
| vsync-locked poll | Raw view wakes on display vsync, reads whatever is current. Naturally rate-limited. |

---

## Webview-Specific Commands

The following commands apply only to `webview` views.

### Loading Content

```json
{ "cmd": "webview_load_url",  "view_id": "v1", "url": "https://example.com" }
{ "cmd": "webview_load_file", "view_id": "v1", "path": "frontend/index.html" }
{ "cmd": "webview_load_html", "view_id": "v1", "html": "<h1>Hello</h1>" }
```

---

### Navigation Control

```json
{ "cmd": "webview_go_back",    "view_id": "v1" }
{ "cmd": "webview_go_forward", "view_id": "v1" }
{ "cmd": "webview_reload",     "view_id": "v1" }
{ "cmd": "webview_stop",       "view_id": "v1" }
{ "cmd": "webview_get_url",    "view_id": "v1" }
```

**Responses**
```json
{ "event": "webview_can_navigate", "view_id": "v1", "can_go_back": true, "can_go_forward": false }
{ "event": "webview_url",          "view_id": "v1", "url": "https://example.com" }
```

---

### Navigation Events

```json
{ "event": "load_start",      "view_id": "v1", "url": "https://example.com" }
{ "event": "load_finish",     "view_id": "v1", "url": "https://example.com" }
{ "event": "load_error",      "view_id": "v1", "url": "https://example.com", "code": 404, "description": "Not Found" }
{ "event": "url_changed",     "view_id": "v1", "url": "https://example.com/page" }
{ "event": "title_changed",   "view_id": "v1", "title": "Page Title" }
{ "event": "favicon_changed", "view_id": "v1", "url": "https://example.com/favicon.ico" }
```

---

### JavaScript

**Fire and forget**
```json
{ "cmd": "webview_eval", "view_id": "v1", "js": "document.title" }
```

**With result**
```json
{ "cmd": "webview_eval", "view_id": "v1", "req_id": "r1", "js": "document.title" }
```
```json
{ "event": "eval_result", "view_id": "v1", "req_id": "r1", "result": "Page Title" }
```

`req_id` is caller-supplied. Include it to get a response. Omit it for
fire-and-forget. Use it to correlate responses when multiple evals are
in flight.

---

### renderer_ipc

The channel between JavaScript running inside a webview and your
application. libarc sits in the middle — it routes messages in both
directions.

**App → JS**
```json
{ "cmd": "webview_post_text", "view_id": "v1", "channel": "ping", "payload": "hello" }
```

**JS → App**
```json
{ "event": "ipc_text", "view_id": "v1", "channel": "pong", "payload": "hello back" }
```

In JavaScript:
```js
arc.on("ping", (payload) => {
    arc.post("pong", "hello back")
})
```

**Binary — App → JS**

A JSON frame describes the message, the binary frame carries the bytes.
```json
{ "cmd": "webview_post_binary", "view_id": "v1", "channel": "video", "size": 16384 }
```
```
<binary frame — 16384 bytes>
```

**Binary — JS → App**
```json
{ "event": "ipc_binary", "view_id": "v1", "channel": "video", "size": 16384 }
```
```
<binary frame — 16384 bytes>
```

---

### Zoom

```json
{ "cmd": "webview_set_zoom", "view_id": "v1", "factor": 1.5 }
{ "cmd": "webview_get_zoom", "view_id": "v1" }
```
```json
{ "event": "webview_zoom", "view_id": "v1", "factor": 1.5 }
```

---

### Find in Page

```json
{ "cmd": "webview_find",      "view_id": "v1", "query": "hello", "case_sensitive": false, "wrap": true }
{ "cmd": "webview_find_next", "view_id": "v1" }
{ "cmd": "webview_find_prev", "view_id": "v1" }
{ "cmd": "webview_find_stop", "view_id": "v1" }
```
```json
{ "event": "find_result", "view_id": "v1", "match_index": 2, "total_matches": 7 }
```

---

### New Window / Popup Policy

Fires when the page calls `window.open()` or uses `target=_blank`.
Your app must respond with a policy.

```json
{ "event": "new_window_requested", "view_id": "v1", "url": "https://example.com" }
```
```json
{ "cmd": "webview_new_window_policy", "view_id": "v1", "url": "https://example.com", "policy": "deny" }
```

| policy     | Behaviour                           |
|------------|-------------------------------------|
| `allow`    | Open in a new webview               |
| `deny`     | Block the navigation                |
| `redirect` | Load the URL in the current webview |

---

### Download Handling

```json
{ "event": "download_requested", "view_id": "v1", "url": "https://example.com/file.zip", "filename": "file.zip", "mime": "application/zip" }
```
```json
{ "cmd": "webview_download_policy", "view_id": "v1", "url": "https://example.com/file.zip", "policy": "allow", "save_path": "/Users/user/Downloads/file.zip" }
```

---

### Permission Requests

```json
{ "event": "permission_requested",  "view_id": "v1", "type": "camera" }
{ "event": "geolocation_requested", "view_id": "v1", "origin": "https://example.com" }
```
```json
{ "cmd": "webview_permission_response",  "view_id": "v1", "type": "camera", "granted": true }
{ "cmd": "webview_geolocation_response", "view_id": "v1", "origin": "https://example.com", "granted": false }
```

Permission types: `camera`, `microphone`, `notifications`, `clipboard`

---

### Context Menu

```json
{
    "event": "context_menu",
    "view_id": "v1",
    "x": 240,
    "y": 180,
    "media_type": "none",
    "link_url": "https://example.com",
    "selected_text": "hello world"
}
```

libarc fires the event and waits. Your app draws a native context menu
and responds with the chosen action or dismisses it.

---

### Auth and Certificates

```json
{ "event": "auth_challenge", "view_id": "v1", "host": "example.com", "realm": "Restricted" }
```
```json
{ "cmd": "webview_auth_response", "view_id": "v1", "username": "user", "password": "pass" }
```

```json
{ "event": "certificate_error", "view_id": "v1", "url": "https://example.com", "error": "CERT_AUTHORITY_INVALID" }
```
```json
{ "cmd": "webview_certificate_response", "view_id": "v1", "proceed": false }
```

---

## Billing

In-app purchase support implemented natively per platform. libarc owns
the platform billing layer — your application just sends commands and
receives events. `billing_init` must be sent before `billing_buy` or
`billing_restore`.

| `store_type`      | Backend                        |
|-------------------|--------------------------------|
| `apple_store`     | StoreKit                       |
| `microsoft_store` | Windows.Services.Store / AppX  |
| `none`            | Linux — stub, returns `failed` |

### billing_init

```json
{
    "cmd": "billing_init",
    "products": [
        { "id": "carbon.ai.plus.0002", "kind": "subscription" },
        { "id": "carbon.ai.pro.0001",  "kind": "consumable" }
    ]
}
```

**Response**
```json
{
    "event": "billing_products",
    "store_type": "apple_store",
    "products": [
        {
            "id": "carbon.ai.plus.0002",
            "title": "Carbon AI Plus",
            "description": "Unlock all Carbon AI premium features.",
            "price": "$9.99",
            "kind": "subscription"
        }
    ]
}
```

### billing_buy

```json
{ "cmd": "billing_buy", "product_id": "carbon.ai.plus.0002" }
```

### billing_restore

```json
{ "cmd": "billing_restore" }
```

**Purchase Events**

```json
{ "event": "billing_purchase", "store_type": "apple_store", "product_id": "carbon.ai.plus.0002", "status": "purchased", "error": null }
{ "event": "billing_purchase", "store_type": "apple_store", "product_id": "carbon.ai.plus.0002", "status": "cancelled", "error": null }
{ "event": "billing_purchase", "store_type": "apple_store", "product_id": "carbon.ai.plus.0002", "status": "restored",  "error": null }
{ "event": "billing_purchase", "store_type": "apple_store", "product_id": "carbon.ai.plus.0002", "status": "deferred",  "error": null }
{ "event": "billing_purchase", "store_type": "apple_store", "product_id": "carbon.ai.plus.0002", "status": "failed",    "error": "payment declined" }
```

| status      | Meaning                                              |
|-------------|------------------------------------------------------|
| `purchased` | Transaction complete, unlock the content             |
| `cancelled` | User cancelled                                       |
| `restored`  | Previously purchased, unlock the content             |
| `deferred`  | Ask to Buy — parent must approve, do not unlock yet  |
| `failed`    | Transaction failed, see `error`                      |

---

## Quit

```json
{ "cmd": "quit" }
```
```json
{ "event": "closed" }
```

---

## Source Layout

```
libarc/
├── include/
│   └── arc/
│       └── arc.h                            ← public API (LoadModule / Run)
├── src/
│   ├── arc.cpp                              ← arc::LoadModule / arc::Run
│   ├── arc_host_main.cpp                    ← arc-host entry point
│   ├── arc_runner.h                         ← shared run loop logic
│   ├── ipc/
│   │   ├── ipc.h                            ← IPC interface, frame types
│   │   ├── ipc_unix.cpp                     ← Unix socket (macOS / Linux)
│   │   └── ipc_win.cpp                      ← named pipe (Windows)
│   ├── webview_ipc/
│   │   └── webview_ipc.h                    ← JS ↔ app message routing
│   ├── billing/
│   │   ├── billing.h                        ← BillingManager interface
│   │   ├── billing_mac.mm                   ← StoreKit
│   │   ├── billing_win.cpp                  ← Windows.Services.Store
│   │   └── billing_linux.cpp                ← stub (empty)
│   ├── view/
│   │   └── view.h                           ← shared view interface (geometry, lifecycle, z-order)
│   ├── webview/
│   │   ├── webview.h                        ← webview interface
│   │   ├── mac/                             ← WKWebView (Objective-C++)
│   │   ├── win/                             ← WebView2 (C++)
│   │   └── linux/                           ← WebKit2GTK (C++)
│   ├── rawview/
│   │   ├── rawview.h                        ← rawview interface
│   │   ├── mac/
│   │   │   ├── rawview_mac_calayer.mm        ← gpu  (CALayer + IOSurface)
│   │   │   └── rawview_mac_bitmap.mm         ← nogpu (NSBitmapImageRep blit)
│   │   ├── win/
│   │   │   ├── rawview_win_d3d11.cpp         ← gpu  (D3D11 texture upload)
│   │   │   └── rawview_win_gdi.cpp           ← nogpu (StretchDIBits / DIBSection)
│   │   └── linux/
│   │       ├── rawview_linux_gl.cpp          ← gpu  (GtkGLArea texture upload)
│   │       └── rawview_linux_cairo.cpp       ← nogpu (GtkDrawingArea + cairo_image_surface_t)
│   ├── logger.h                             ← stderr logger
│   ├── logger.cpp
│   └── utils/
│       ├── mime.h                           ← MIME type lookup
│       ├── str_escape.h                     ← JS / JSON string escaping
│       └── types.h                          ← shared types
├── CMakeLists.txt
└── vcpkg.json
```

---

## Platform Implementation

| Platform | Container | View primitives | Coord system | Clipping |
|----------|-----------|-----------------|--------------|----------|
| macOS | `ArcFlippedView` (`NSView isFlipped=YES`) | `WKWebView` subview, `CALayer` for raw | `isFlipped` on container | `layer.masksToBounds = YES` |
| Windows | `HWND` `WS_CHILD` + `WS_CLIPCHILDREN` | `WebView2` child `HWND`, D3D11 swap chain for raw | Y-down, no flip needed | `WS_CLIPSIBLINGS` on children |
| Linux | `GtkFixed` inside `GtkOverlay` | `WebKitWebView` via `gtk_fixed_put`, Cairo/GL surface for raw | Y-down natively | `GtkOverlay` allocation clipping |

---

## Threading Model

The native run loop always stays on the main thread. `arc::Run` must be
called from `main` and never returns until the session ends.

Your application logic runs on a background thread. The IPC layer is
thread-safe. Sends are non-blocking — frames are enqueued and written by
a dedicated sender thread. Reads are blocking and must be called from a
single dedicated reader thread.

Raw view poll loops run on a dedicated background thread per raw view.
They never touch the main thread directly — uploads are dispatched to
the main thread via the platform's native dispatch mechanism.

---

## Logging

Logging is off by default.

**arc-host** (development):
```bash
arc-host --ipc-channel <id> --logging
```

**libarc** (production): call `logger::init(true)` before `arc::Run()`.

Log lines are prefixed with severity:
```
[INFO]  libarc: socket path /tmp/arc-3f2a1b4c.sock
[INFO]  arc-host: client connected on channel 3f2a1b4c
[WARN]  billing: buy called before init
[ERROR] ipc: frame read failed
```

---

## Building

### Prerequisites

| Platform | Requirements |
|----------|--------------|
| macOS    | Xcode Command Line Tools, CMake ≥ 3.22 |
| Windows  | Visual Studio 2022, CMake ≥ 3.22, vcpkg |
| Linux    | GCC or Clang, CMake ≥ 3.22, `webkit2gtk-4.1`, `gtk+-3.0` |

```bash
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev
```

### Configure and Build

```bash
cd libarc
cmake -B build -DCMAKE_BUILD_TYPE=Release
cmake --build build
```

**Windows**
```bat
cmake -B build ^
  -DCMAKE_BUILD_TYPE=Release ^
  -DCMAKE_TOOLCHAIN_FILE="%VCPKG_ROOT%\scripts\buildsystems\vcpkg.cmake"
cmake --build build --config Release
```

Outputs:
```
build/lib/libarc.dylib     # (or .so / .dll)
build/bin/arc-host
```

### Install

```bash
cmake --install build --prefix /usr/local
```

Installs `libarc` to `lib/`, `arc-host` to `bin/`, and the public
header to `include/arc/arc.h`.

---

## Design Principles

**libarc listens.** The native side is the service. Whatever language
drives it is the client. The connection model reflects that.

**Two frame types only.** JSON for everything semantic. Binary for bulk
data. The framing layer is dumb on purpose — it knows length and type,
nothing else.

**Flat view model.** All views are the same thing at the geometry and
lifecycle layer. `view_type` determines what the surface renders.
`layout` is a placement instruction. The full position, visibility, and
z-order API is available on every view regardless of type or layout.

**JSON is the protocol.** Adding a new command or event is adding a
field. The framing layer never changes. Unknown fields are ignored —
forward compatibility is free.

**No bundled browser.** The webview is the OS's own. Binary size stays
small and rendering is consistent with the platform.

**No per-frame IPC.** Raw views poll shared memory directly. The IPC
socket carries lifecycle and geometry only — never frame traffic.

**Language agnostic.** libarc has no opinion about what connects to it.