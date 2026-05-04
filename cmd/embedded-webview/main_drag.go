// main_drag.go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/carbon-os/arc"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/webview"
	"github.com/carbon-os/arc/window"
)

const (
	winW   = 1440
	winH   = 900
	barH   = 50
	shellW = 800
	shellH = 500
)

func main() {
	app := arc.NewApp(arc.AppConfig{
		Title:   "Arc – Draggable WebView",
		Logging: true,
		Renderer: arc.RendererConfig{
			Path: "/Users/galaxy/Desktop/libarc/build/arc-host/arc-host",
		},
	})

	app.OnReady(func() {
		win := app.NewBrowserWindow(window.Config{
			Title:  "Arc – Draggable WebView",
			Width:  winW,
			Height: winH,
			Debug:  true,
		})

		ipcMain := win.IPC()

		win.OnReady(func() {
			overlay := win.NewWebView(webview.Config{
				X:      0,
				Y:      0,
				Width:  shellW,
				Height: shellH - barH,
			})
			overlay.LoadURL("https://www.wikipedia.org")

			visible      := false
			overlayReady := false

			// contentH tracks the JS viewport height (window.innerHeight).
			// Initialized to a safe guess; corrected on first wv-init from JS.
			// NOTE: do NOT update this from win.OnResize — the C++ resize event
			// reports the frame height (includes native title bar on macOS).
			// JS is the authoritative source via wv-init and wv-resize.
			contentH := winH

			// overlayY converts a CSS top-origin Y into the native NSView Y.
			// On macOS the View coordinate origin is bottom-left, so we flip.
			overlayY := func(cssY, viewH int) int {
				return contentH - cssY - viewH
			}

			overlay.OnReady(func() {
				log.Println("[drag] overlay webview ready")
				overlayReady = true
			})

			overlay.OnLoadStart(func(url string) {
				log.Printf("[drag] overlay load start → %s", url)
			})
			overlay.OnLoadFinish(func(url string) {
				log.Printf("[drag] overlay load finish → %s", url)
			})
			overlay.OnLoadFailed(func(url, errMsg string) {
				log.Printf("[drag] overlay load FAILED → %s: %s", url, errMsg)
			})

			// JS sends initial shell position and viewport height once it knows them.
			ipcMain.On("wv-init", func(msg ipc.Message) {
				var p struct {
					X  int `json:"x"`
					Y  int `json:"y"`
					VH int `json:"vh"`
				}
				if err := json.Unmarshal([]byte(msg.Text()), &p); err != nil {
					log.Printf("[drag] wv-init parse error: %v", err)
					return
				}
				contentH = p.VH
				log.Printf("[drag] init: shell=(%d,%d) viewport_h=%d", p.X, p.Y, p.VH)

				viewH := shellH - barH
				overlay.SetBounds(p.X, overlayY(p.Y+barH, viewH), shellW, viewH)

				show := func() {
					overlay.Show()
					visible = true
					ipcMain.Send("wv-state", fmt.Sprintf("%v", visible))
				}

				if overlayReady {
					show()
				} else {
					overlay.OnReady(func() { show() })
				}
			})

			// JS sends updated viewport dimensions when the window is resized.
			ipcMain.On("wv-resize", func(msg ipc.Message) {
				var p struct {
					VH int `json:"vh"`
					VW int `json:"vw"`
				}
				if err := json.Unmarshal([]byte(msg.Text()), &p); err != nil {
					log.Printf("[drag] wv-resize parse error: %v", err)
					return
				}
				contentH = p.VH
				log.Printf("[drag] viewport resize → %d×%d", p.VW, p.VH)
			})

			// JS sends updated shell position on every drag tick.
			ipcMain.On("wv-move", func(msg ipc.Message) {
				var p struct {
					X int `json:"x"`
					Y int `json:"y"`
				}
				if err := json.Unmarshal([]byte(msg.Text()), &p); err != nil {
					return
				}
				if !visible {
					return
				}
				viewH := shellH - barH
				overlay.SetBounds(p.X, overlayY(p.Y+barH, viewH), shellW, viewH)
			})

			ipcMain.On("wv-toggle", func(_ ipc.Message) {
				if visible {
					overlay.Hide()
					visible = false
				} else {
					overlay.Show()
					visible = true
				}
				ipcMain.Send("wv-state", fmt.Sprintf("%v", visible))
			})

			// Sprintf order: CSS titlebar height, BAR_H const, SHELL_W const, SHELL_H const
			win.LoadHTML(fmt.Sprintf(shellHTML, barH, barH, shellW, shellH))
		})
	})

	app.OnClose(func() bool { return true })
	if err := app.Run(); err != nil {
		log.Fatalf("arc: %v", err)
	}
}

// Sprintf args: %d[1]=CSS titlebar height, %d[2]=BAR_H, %d[3]=SHELL_W, %d[4]=SHELL_H
const shellHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }

  html, body {
    width: 100vw; height: 100vh;
    overflow: hidden;
    background: #e8e8e8;
    font-family: system-ui, sans-serif;
    user-select: none;
  }

  #debug-bar {
    position: fixed;
    top: 0; left: 0; right: 0;
    height: 48px;
    background: #f5f5f5;
    border-bottom: 1px solid #ccc;
    display: flex;
    align-items: center;
    padding: 0 16px;
    gap: 10px;
    z-index: 9999;
    font-size: 12px;
    color: #555;
  }
  #debug-bar code {
    font-size: 11px;
    background: #e0e0e0;
    padding: 2px 7px;
    border-radius: 4px;
    color: #222;
    font-family: monospace;
  }
  .sep { color: #ccc; }
  .dbg-btn {
    padding: 5px 14px;
    font-size: 12px;
    font-weight: 500;
    border-radius: 6px;
    border: 1px solid #bbb;
    background: #fff;
    color: #333;
    cursor: pointer;
  }
  .dbg-btn:hover  { background: #ebebeb; }
  .dbg-btn.active { background: #333; color: #fff; border-color: #333; }

  #shell {
    position: fixed;
    border-radius: 10px;
    border: 1px solid #bbb;
    box-shadow: 0 4px 24px rgba(0,0,0,.15);
    pointer-events: none;
  }

  #titlebar {
    height: %dpx;
    display: flex;
    align-items: center;
    padding: 0 12px;
    gap: 10px;
    background: #3a3a3a;
    border-radius: 10px 10px 0 0;
    cursor: grab;
    pointer-events: all;
  }
  #titlebar.dragging { cursor: grabbing; }

  .tl { display: flex; gap: 6px; }
  .tl span { width: 12px; height: 12px; border-radius: 50%; display: block; }
  .tl .c { background: #ff5f57; }
  .tl .m { background: #ffbd2e; }
  .tl .x { background: #28c840; }

  #title-text {
    flex: 1;
    text-align: center;
    font-size: 12px;
    color: rgba(255,255,255,.5);
  }

  #placeholder {
    border-radius: 0 0 9px 9px;
    background: #d8d8d8;
    border: 2px dashed #bbb;
    display: none;
    align-items: center;
    justify-content: center;
    color: #999;
    font-size: 13px;
  }
</style>
</head>
<body>

<div id="debug-bar">
  <span>viewport</span><code id="lbl-vp">…</code>
  <span class="sep">|</span>
  <span>dpr</span><code id="lbl-dpr">…</code>
  <span class="sep">|</span>
  <span>shell</span><code id="lbl-pos">…</code>
  <span class="sep">|</span>
  <button class="dbg-btn" id="toggle-btn" onclick="toggleOverlay()">Show overlay</button>
</div>

<div id="shell">
  <div id="titlebar">
    <div class="tl">
      <span class="c"></span><span class="m"></span><span class="x"></span>
    </div>
    <span id="title-text">wikipedia.org</span>
  </div>
  <div id="placeholder"></div>
</div>

<script>
const BAR_H       = %d;
const SHELL_W     = %d;
const SHELL_H     = %d;
const DEBUG_BAR_H = 48;

const shell       = document.getElementById('shell');
const bar         = document.getElementById('titlebar');
const placeholder = document.getElementById('placeholder');
const toggleBtn   = document.getElementById('toggle-btn');

// ── Diagnostics ───────────────────────────────────────────────────────────────
function updateDiagnostics() {
  document.getElementById('lbl-vp').textContent  = window.innerWidth + '×' + window.innerHeight;
  document.getElementById('lbl-dpr').textContent = window.devicePixelRatio;
}
updateDiagnostics();

// ── Initial shell placement ───────────────────────────────────────────────────
const initX = Math.round((window.innerWidth - SHELL_W) / 2);
const initY = DEBUG_BAR_H + 20;

shell.style.width   = SHELL_W + 'px';
shell.style.height  = SHELL_H + 'px';
shell.style.left    = initX   + 'px';
shell.style.top     = initY   + 'px';
placeholder.style.height = (SHELL_H - BAR_H) + 'px';

updatePosLabel(initX, initY);

// Send Go the initial position AND the viewport height so it can flip Y.
ipc.send('wv-init', JSON.stringify({
  x:  initX,
  y:  initY,
  vh: window.innerHeight,
}));

// ── Viewport resize → Go ──────────────────────────────────────────────────────
window.addEventListener('resize', () => {
  updateDiagnostics();
  ipc.send('wv-resize', JSON.stringify({
    vw: window.innerWidth,
    vh: window.innerHeight,
  }));
});

// ── Drag ─────────────────────────────────────────────────────────────────────
let drag = null;

bar.addEventListener('mousedown', e => {
  e.preventDefault();
  const r = shell.getBoundingClientRect();
  drag = { offX: e.clientX - r.left, offY: e.clientY - r.top };
  bar.classList.add('dragging');
});

document.addEventListener('mousemove', e => {
  if (!drag) return;

  let x = e.clientX - drag.offX;
  let y = e.clientY - drag.offY;
  x = Math.max(0, Math.min(window.innerWidth  - SHELL_W, x));
  y = Math.max(0, Math.min(window.innerHeight - SHELL_H, y));

  shell.style.left = x + 'px';
  shell.style.top  = y + 'px';
  updatePosLabel(x, y);

  ipc.send('wv-move', JSON.stringify({ x, y }));
});

document.addEventListener('mouseup', () => {
  if (!drag) return;
  drag = null;
  bar.classList.remove('dragging');
});

window.addEventListener('blur', () => {
  if (!drag) return;
  drag = null;
  bar.classList.remove('dragging');
});

// ── Toggle ────────────────────────────────────────────────────────────────────
function toggleOverlay() { ipc.send('wv-toggle', null); }

ipc.on('wv-state', state => {
  const on = state === 'true';
  toggleBtn.textContent = on ? 'Hide overlay' : 'Show overlay';
  toggleBtn.classList.toggle('active', on);
  placeholder.style.display = on ? 'none' : 'flex';
});

function updatePosLabel(x, y) {
  document.getElementById('lbl-pos').textContent =
    'x=' + x + ' y=' + y + ' w=' + SHELL_W + ' h=' + SHELL_H;
}
</script>
</body>
</html>`