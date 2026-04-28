package window

import (
	"sync"

	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/internal/runtime"
)

// RendererConfig is forwarded from AppConfig to each window's renderer process.
// Logging is carried here from AppConfig.Logging — not set directly by callers.
type RendererConfig struct {
	Path     string
	Prebuilt bool
	Logging  bool
}

// Config holds the options for a new BrowserWindow.
type Config struct {
	Title  string
	Width  int
	Height int
	Debug  bool
}

// BrowserWindow is a handle to a native window and its dedicated renderer
// process. Each BrowserWindow spawns and owns exactly one renderer process.
type BrowserWindow struct {
	cfg     Config
	rt      *runtime.Runtime
	logging bool
	ipcObj  *ipc.IPC
	ipcOnce sync.Once
	mu      sync.Mutex
	onReady func()
	onClose func() bool
}

// New creates a BrowserWindow and prepares its runtime. The renderer process
// is not spawned until Run is called — which App.NewBrowserWindow does
// automatically in a goroutine.
func New(cfg Config, rendererCfg RendererConfig) *BrowserWindow {
	if cfg.Width == 0 {
		cfg.Width = 1280
	}
	if cfg.Height == 0 {
		cfg.Height = 800
	}

	w := &BrowserWindow{cfg: cfg, logging: rendererCfg.Logging}

	rt, _ := runtime.New(runtime.Config{
		Title:        cfg.Title,
		Width:        cfg.Width,
		Height:       cfg.Height,
		Debug:        cfg.Debug,
		RendererPath: rendererCfg.Path,
		Prebuilt:     rendererCfg.Prebuilt,
		Logging:      rendererCfg.Logging,
		OnReady: func() {
			w.mu.Lock()
			cb := w.onReady
			w.mu.Unlock()
			if cb != nil {
				cb()
			}
		},
		OnClose: func() bool {
			w.mu.Lock()
			cb := w.onClose
			w.mu.Unlock()
			if cb != nil {
				return cb()
			}
			return true
		},
	})

	w.rt = rt
	return w
}

// Run spawns the renderer process and blocks until the window is closed.
// Called automatically by App.NewBrowserWindow — do not call directly.
func (w *BrowserWindow) Run() error {
	return w.rt.Run()
}

// IPC returns the IPC handle for this window. Lazily initialised on first
// call; the same instance is returned on every subsequent call.
func (w *BrowserWindow) IPC() *ipc.IPC {
	w.ipcOnce.Do(func() {
		w.ipcObj = ipc.New(w.rt, w.logging)
	})
	return w.ipcObj
}

// OnReady registers a callback that fires once this window's renderer is
// initialised and ready to receive commands.
func (w *BrowserWindow) OnReady(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onReady = fn
}

// OnClose registers a callback that fires when the user tries to close this
// window. Return true to allow the close, false to suppress it.
func (w *BrowserWindow) OnClose(fn func() bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.onClose = fn
}

// LoadFile navigates the window to a local HTML file.
func (w *BrowserWindow) LoadFile(path string) {
	w.rt.Send(runtime.CmdLoadFile, runtime.EncodeStr(path))
}

// LoadHTML loads a raw HTML string directly into the window.
func (w *BrowserWindow) LoadHTML(html string) {
	w.rt.Send(runtime.CmdLoadHTML, runtime.EncodeStr(html))
}

// LoadURL navigates the window to an external URL.
func (w *BrowserWindow) LoadURL(url string) {
	w.rt.Send(runtime.CmdLoadURL, runtime.EncodeStr(url))
}

// SetTitle updates the window title bar.
func (w *BrowserWindow) SetTitle(title string) {
	w.rt.Send(runtime.CmdSetTitle, runtime.EncodeStr(title))
}

// SetSize resizes the window to the given pixel dimensions.
func (w *BrowserWindow) SetSize(width, height int) {
	w.rt.Send(runtime.CmdSetSize, runtime.EncodeU32U32(uint32(width), uint32(height)))
}

// Eval executes a JavaScript expression in the window's current page.
func (w *BrowserWindow) Eval(js string) {
	w.rt.Send(runtime.CmdEval, runtime.EncodeStr(js))
}

// Quit programmatically closes this window.
func (w *BrowserWindow) Quit() {
	w.rt.Quit()
}