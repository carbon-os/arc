// cmd/embedded-webview/main.go
//
// Demonstrates Arc's embedded WebView API.
//
// Layout (1 200 × 800 window):
//
//   ┌──────────────────────────────────────────────────────┐
//   │  Control Panel (400 px)  │  Google WebView (800 px)  │
//   │  base window — white bg  │  isolated overlay         │
//   └──────────────────────────────────────────────────────┘
//
// The control panel lives in the base window's HTML and talks to Go
// over IPC. Go drives the WebView (show/hide, navigate, resize).

package main

import (
	"log"
	"runtime"

	"github.com/carbon-os/arc"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/webview"
	"github.com/carbon-os/arc/window"
)

const (
	winW = 1200
	winH = 800

	panelW = 400 // left control-panel width (base window HTML)

	overlayX = panelW
	overlayY = 0
	overlayW = winW - panelW
	overlayH = winH
)

func main() {
	app := arc.NewApp(arc.AppConfig{
		Title:   "Arc – Embedded WebView",
		Logging: true,
		Renderer: arc.RendererConfig{
			Path: rendererPath(),
		},
	})

	app.OnReady(func() {
		win := app.NewBrowserWindow(window.Config{
			Title:  "Arc – Embedded WebView",
			Width:  winW,
			Height: winH,
			Debug:  true,
		})

		// ── IPC ────────────────────────────────────────────────────────────
		ipcMain := win.IPC()

		// Track visibility so the panel button label stays in sync.
		// Track current window dimensions so we can correctly size the overlay
		// when it is shown after having been hidden during a resize.
		visible := false
		currentW, currentH := winW, winH

		// ── WebView and IPC handlers are wired up once the renderer is ready ──

		win.OnReady(func() {
			// Safe to create the WebView here — the renderer is connected.
			overlay := win.NewWebView(webview.Config{
				X:      overlayX,
				Y:      overlayY,
				Width:  overlayW,
				Height: overlayH,
				ZOrder: 0,
			})

			overlay.LoadURL("https://www.google.com")
			overlay.Show()
			visible = true

			// ── Resize — anchor overlay to the right of the fixed panel ──────

			win.OnResize(func(w, h int) {
				currentW, currentH = w, h
				if visible {
					overlay.SetBounds(panelW, 0, w-panelW, h)
				}
        log.Printf("[go] window resized → %d × %d", w, h)
			})

			// ── IPC handlers ──────────────────────────────────────────────

			// "navigate"  payload: URL string
			ipcMain.On("navigate", func(msg ipc.Message) {
				url := msg.Text()
				if url == "" {
					return
				}
				log.Printf("[go] navigate → %s", url)
				overlay.LoadURL(url)
			})

			// "toggle-visibility"  no payload
			ipcMain.On("toggle-visibility", func(_ ipc.Message) {
				if visible {
					overlay.Hide()
					log.Println("[go] overlay hidden")
				} else {
					// Snap to current window size before showing in case a
					// resize occurred while the overlay was hidden.
					overlay.SetBounds(panelW, 0, currentW-panelW, currentH)
					overlay.Show()
					log.Println("[go] overlay shown")
				}
				visible = !visible

				// Tell the panel which label to show next.
				label := "Hide panel"
				if !visible {
					label = "Show panel"
				}
				ipcMain.Send("visibility-changed", label)
			})

			// "resize-panel"  payload: "small" | "large"
			ipcMain.On("resize-panel", func(msg ipc.Message) {
				switch msg.Text() {
				case "small":
					// Shrink overlay to a narrow side strip (320 px).
					overlay.SetBounds(winW-320, 0, 320, winH)
					log.Println("[go] overlay → narrow (320 px)")
				case "large":
					// Expand overlay back to default.
					overlay.SetBounds(overlayX, overlayY, overlayW, overlayH)
					log.Println("[go] overlay → default (800 px)")
				}
			})

			// Load the control panel HTML last so the JS-side ipc.on handlers
			// are registered before the first visibility-changed message could
			// arrive.
			win.LoadHTML(controlPanelHTML)
		})
	})

	app.OnClose(func() bool { return true })

	if err := app.Run(); err != nil {
		log.Fatalf("arc: %v", err)
	}
}

func rendererPath() string {
	if runtime.GOOS == "windows" {
		return "../../libarc/build/bin/Debug/arc-host.exe"
	}
	return "../../libarc/build/bin/arc-host"
}

// ── Control-panel HTML ──────────────────────────────────────────────────────
//
// Occupies the left 400 px of the window. Sends IPC messages to Go; receives
// "visibility-changed" back to keep the toggle button label accurate.

const controlPanelHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Control Panel</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

    body {
      font-family: system-ui, -apple-system, sans-serif;
      background: #ffffff;
      color: #1a1a1a;
      height: 100vh;
      display: flex;
      flex-direction: column;
      padding: 2rem 1.75rem;
      gap: 2rem;

      /* Hard right boundary — the WebView overlay sits to the right of here. */
      max-width: 400px;
      overflow: hidden;
    }

    /* ── Header ── */
    header h1 {
      font-size: 1.1rem;
      font-weight: 600;
      color: #111;
    }
    header p {
      margin-top: 0.35rem;
      font-size: 0.8rem;
      color: #888;
      line-height: 1.5;
    }

    /* ── Sections ── */
    section {
      display: flex;
      flex-direction: column;
      gap: 0.6rem;
    }
    section label {
      font-size: 0.72rem;
      font-weight: 600;
      text-transform: uppercase;
      letter-spacing: 0.07em;
      color: #aaa;
    }

    /* ── Inputs ── */
    input[type="url"] {
      width: 100%;
      padding: 0.55rem 0.75rem;
      font-size: 0.85rem;
      border: 1px solid #ddd;
      border-radius: 7px;
      outline: none;
      color: #111;
      background: #fafafa;
      transition: border-color 0.15s;
    }
    input[type="url"]:focus {
      border-color: #4285f4;
      background: #fff;
    }

    /* ── Buttons ── */
    .btn {
      width: 100%;
      padding: 0.6rem 1rem;
      font-size: 0.85rem;
      font-weight: 500;
      border-radius: 7px;
      border: 1px solid transparent;
      cursor: pointer;
      transition: background 0.12s, border-color 0.12s;
    }
    .btn-primary {
      background: #4285f4;
      color: #fff;
    }
    .btn-primary:hover { background: #2b6ee0; }

    .btn-outline {
      background: #fff;
      color: #333;
      border-color: #ddd;
    }
    .btn-outline:hover { background: #f5f5f5; }

    .btn-danger {
      background: #fff;
      color: #d93025;
      border-color: #fad2cf;
    }
    .btn-danger:hover { background: #fef2f1; }

    /* ── Quick-nav chips ── */
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: 0.5rem;
    }
    .chip {
      padding: 0.35rem 0.8rem;
      font-size: 0.78rem;
      border-radius: 999px;
      border: 1px solid #e0e0e0;
      background: #fafafa;
      color: #333;
      cursor: pointer;
      transition: background 0.12s, border-color 0.12s;
    }
    .chip:hover { background: #f0f0f0; border-color: #ccc; }

    /* ── Divider ── */
    hr { border: none; border-top: 1px solid #f0f0f0; }

    /* ── Footer ── */
    footer {
      margin-top: auto;
      font-size: 0.75rem;
      color: #ccc;
      line-height: 1.6;
    }
  </style>
</head>
<body>

  <header>
    <h1>WebView Panel</h1>
    <p>The right-hand overlay is an isolated Arc WebView.<br>
       Use the controls below to drive it from Go.</p>
  </header>

  <hr>

  <!-- Navigate to URL -->
  <section>
    <label>Navigate to URL</label>
    <input type="url" id="url-input" value="https://www.google.com"
           placeholder="https://…"
           onkeydown="if(event.key==='Enter') navigate()">
    <button class="btn btn-primary" onclick="navigate()">Go</button>
  </section>

  <!-- Quick-nav chips -->
  <section>
    <label>Quick navigation</label>
    <div class="chips">
      <span class="chip" onclick="goTo('https://www.google.com')">Google</span>
      <span class="chip" onclick="goTo('https://www.wikipedia.org')">Wikipedia</span>
      <span class="chip" onclick="goTo('https://github.com')">GitHub</span>
      <span class="chip" onclick="goTo('https://news.ycombinator.com')">HN</span>
      <span class="chip" onclick="goTo('https://www.youtube.com')">YouTube</span>
    </div>
  </section>

  <hr>

  <!-- Visibility -->
  <section>
    <label>Visibility</label>
    <button id="toggle-btn" class="btn btn-outline"
            onclick="toggleVisibility()">Hide panel</button>
  </section>

  <!-- Width presets -->
  <section>
    <label>Panel width</label>
    <div style="display:flex; gap:0.5rem;">
      <button class="btn btn-outline" style="flex:1"
              onclick="resizePanel('large')">Default (800 px)</button>
      <button class="btn btn-outline" style="flex:1"
              onclick="resizePanel('small')">Narrow (320 px)</button>
    </div>
  </section>

  <hr>

  <footer>
    WebView runs in an isolated context — separate session, storage, and JS
    environment. No access to this window or other views.
  </footer>

  <script>
    // ── Navigate ────────────────────────────────────────────────────────────
    function navigate() {
      const url = document.getElementById('url-input').value.trim()
      if (!url) return
      ipc.post('navigate', url)
    }

    function goTo(url) {
      document.getElementById('url-input').value = url
      ipc.post('navigate', url)
    }

    // ── Visibility toggle ───────────────────────────────────────────────────
    function toggleVisibility() {
      ipc.post('toggle-visibility', '')
    }

    // Go sends back the *next* button label after each toggle.
    ipc.on('visibility-changed', (label) => {
      document.getElementById('toggle-btn').textContent = label
    })

    // ── Width presets ───────────────────────────────────────────────────────
    function resizePanel(size) {
      ipc.post('resize-panel', size)
    }
  </script>
</body>
</html>`