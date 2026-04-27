package arc

import (
	"log"
	"sync"

	"github.com/carbon-os/arc/window"
)

// RendererConfig controls how Arc locates the native renderer binary.
type RendererConfig struct {
	// Path points at a locally built renderer binary (cmake output).
	Path string

	// Prebuilt enables automatic download of the renderer binary from
	// GitHub Releases when no cached binary exists. Ignored if Path is set.
	Prebuilt bool

	// Logging enables verbose renderer logging by passing --logging to the
	// renderer process. All renderer log lines are prefixed with [INFO],
	// [WARN], or [ERROR] and written to stderr.
	Logging bool
}

// AppConfig is the top-level application configuration passed to NewApp.
type AppConfig struct {
	Title    string
	WebApp   bool
	Host     string
	Port     int
	Renderer RendererConfig
}

// App is the top-level handle for an Arc application.
// Create one with NewApp, register callbacks, then call Run.
type App struct {
	cfg     AppConfig
	onReady func()
	onClose func() bool
	mu      sync.Mutex
	wg      sync.WaitGroup
}

// NewApp creates a new App with the given configuration.
// It does not start any renderers — call Run to do that.
func NewApp(cfg AppConfig) *App {
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	return &App{cfg: cfg}
}

// OnReady registers a callback that fires once Run is called and the app
// is ready to create windows.
func (a *App) OnReady(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onReady = fn
}

// OnClose registers a callback that fires when the last window is closed.
// Return true to allow the app to exit, false to suppress it.
func (a *App) OnClose(fn func() bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onClose = fn
}

// NewBrowserWindow creates a new BrowserWindow and spawns its own dedicated
// renderer process. Each window is fully independent — its own process,
// transport, and IPC connection. Safe to call multiple times for multi-window
// apps. Must be called from within the OnReady callback.
func (a *App) NewBrowserWindow(cfg window.Config) *window.BrowserWindow {
	win := window.New(cfg, window.RendererConfig{
		Path:     a.cfg.Renderer.Path,
		Prebuilt: a.cfg.Renderer.Prebuilt,
		Logging:  a.cfg.Renderer.Logging,
	})

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := win.Run(); err != nil {
			// Actually log the error so we aren't flying blind!
			log.Printf("[arc] window run error: %v", err)
		}
	}()

	return win
}

// Run starts the application and blocks until all windows are closed.
// OnReady is called synchronously so all NewBrowserWindow calls and their
// wg.Add(1) registrations complete before wg.Wait() is reached.
func (a *App) Run() error {
	if a.cfg.WebApp {
		a.mu.Lock()
		readyCb := a.onReady
		closeCb := a.onClose
		a.mu.Unlock()
		return a.runWeb(readyCb, closeCb)
	}

	a.mu.Lock()
	readyCb := a.onReady
	a.mu.Unlock()

	// Call synchronously so all NewBrowserWindow calls complete
	// and wg.Add(1) is registered before wg.Wait() below.
	if readyCb != nil {
		readyCb()
	}

	// Block until every spawned window has exited.
	a.wg.Wait()
	return nil
}