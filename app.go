package arc

import (
	"log"
	"os"
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
}

// AppConfig is the top-level application configuration passed to NewApp.
type AppConfig struct {
	Title    string
	WebApp   bool
	Host     string
	Port     int
	Logging  bool
	Renderer RendererConfig

	// ChannelID can be set explicitly if the caller wants to override which
	// renderer channel to connect to. Normally left empty — NewApp detects
	// the --channel flag from os.Args automatically.
	ChannelID string
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
// If --channel <id> is present in os.Args (set by main_process.mm when it
// spawns this process), the channel ID is captured automatically and renderer
// spawning is disabled for all windows.
func NewApp(cfg AppConfig) *App {
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.ChannelID == "" {
		cfg.ChannelID = parseChannelFlag(os.Args)
	}
	return &App{cfg: cfg}
}

// parseChannelFlag scans argv for --channel <id> and returns the value,
// or an empty string if the flag is absent.
func parseChannelFlag(args []string) string {
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--channel" {
			return args[i+1]
		}
	}
	return ""
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

// NewBrowserWindow creates a new BrowserWindow. In self-spawn mode (the
// default) it spawns a dedicated renderer subprocess. In external renderer
// mode (--channel detected) it connects to the pre-spawned renderer instead.
// Safe to call multiple times for multi-window apps. Must be called from
// within the OnReady callback.
func (a *App) NewBrowserWindow(cfg window.Config) *window.BrowserWindow {
	win := window.New(cfg, window.RendererConfig{
		Path:      a.cfg.Renderer.Path,
		Prebuilt:  a.cfg.Renderer.Prebuilt,
		Logging:   a.cfg.Logging,
		ChannelID: a.cfg.ChannelID,
	})

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := win.Run(); err != nil {
			log.Printf("[arc] window run error: %v", err)
		}
	}()

	return win
}

// Run starts the application and blocks until all windows are closed.
//
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

	if readyCb != nil {
		readyCb()
	}

	a.wg.Wait()
	return nil
}