package window

import (
	"sync"

	"github.com/carbon-os/arc/billing"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/internal/runtime"
)

// TitleBarStyle controls the appearance of the native window title bar.
// Re-exported here so callers only need to import the window package.
type TitleBarStyle = runtime.TitleBarStyle

const (
	// TitleBarDefault shows the standard OS title bar.
	TitleBarDefault TitleBarStyle = runtime.TitleBarDefault

	// TitleBarHidden hides the title bar while keeping the window border,
	// shadow, resize handles, and traffic lights (macOS).
	TitleBarHidden TitleBarStyle = runtime.TitleBarHidden
)

// RendererConfig is forwarded from AppConfig to each window's runtime.
type RendererConfig struct {
	Path      string
	Prebuilt  bool
	Logging   bool
	ChannelID string
}

// Config holds the options for a new BrowserWindow.
type Config struct {
	Title         string
	Width         int
	Height        int
	Debug         bool
	TitleBarStyle TitleBarStyle
}

// BrowserWindow is a handle to a native window and its dedicated renderer
// process. Each BrowserWindow spawns and owns exactly one renderer process,
// or connects to one pre-spawned by an external parent (e.g. main_process.mm).
type BrowserWindow struct {
	cfg     Config
	rt      *runtime.Runtime
	logging bool

	ipcObj  *ipc.IPC
	ipcOnce sync.Once

	billingMu  sync.Mutex
	billingObj *billing.Billing

	mu      sync.Mutex
	onReady func()
	onClose func() bool
}

// New creates a BrowserWindow and prepares its runtime. The renderer process
// is not spawned (or connected to) until Run is called — which App.NewBrowserWindow
// does automatically in a goroutine.
func New(cfg Config, rendererCfg RendererConfig) *BrowserWindow {
	if cfg.Width == 0 {
		cfg.Width = 1280
	}
	if cfg.Height == 0 {
		cfg.Height = 800
	}

	w := &BrowserWindow{cfg: cfg, logging: rendererCfg.Logging}

	rt, _ := runtime.New(runtime.Config{
		Title:         cfg.Title,
		Width:         cfg.Width,
		Height:        cfg.Height,
		Debug:         cfg.Debug,
		TitleBarStyle: cfg.TitleBarStyle,
		RendererPath:  rendererCfg.Path,
		Prebuilt:      rendererCfg.Prebuilt,
		Logging:       rendererCfg.Logging,
		ChannelID:     rendererCfg.ChannelID,
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

// Run connects to or spawns the renderer and blocks until the window is closed.
// Called automatically by App.NewBrowserWindow — do not call directly.
func (w *BrowserWindow) Run() error {
	return w.rt.Run()
}

// IPC returns the IPC handle for this window. Lazily initialised on first call.
func (w *BrowserWindow) IPC() *ipc.IPC {
	w.ipcOnce.Do(func() {
		w.ipcObj = ipc.New(w.rt, w.logging)
	})
	return w.ipcObj
}

// NewBilling creates and initialises the Billing handle for this window.
// Must be called from within OnReady. Subsequent calls return the existing
// handle unchanged.
func (w *BrowserWindow) NewBilling(cfg billing.Config) (*billing.Billing, error) {
	w.billingMu.Lock()
	defer w.billingMu.Unlock()

	if w.billingObj != nil {
		return w.billingObj, nil
	}

	b, err := billing.New(w.rt, cfg, w.logging)
	if err != nil {
		return nil, err
	}

	w.billingObj = b
	return b, nil
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