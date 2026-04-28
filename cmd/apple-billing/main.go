package main

import (
	"log"
	"runtime"

	"github.com/carbon-os/arc"
	"github.com/carbon-os/arc/billing"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/window"
)

func main() {
	app := arc.NewApp(arc.AppConfig{
		Title:   "Sample App",
		Logging: true,
		Renderer: arc.RendererConfig{
			Path: rendererPath(),
		},
	})

	app.OnReady(func() {
		win := app.NewBrowserWindow(window.Config{
			Title:  "Sample App",
			Width:  800,
			Height: 560,
			Debug:  true,
		})

		win.OnReady(func() {
			b, err := win.NewBilling(billing.Config{
				Products: []billing.Product{
					{
						ID:   "com.sample.app.plus.0001",
						Kind: billing.Subscription,
					},
				},
			})
			if err != nil {
				log.Fatalf("[billing] init failed: %v", err)
			}

			// Fires once the store returns live pricing —
			// forward to JS so the UI can render the real price.
			b.OnProducts(func(products []billing.ProductInfo) {
				for _, p := range products {
					log.Printf("[billing] product ready: %s | %s | %s", p.ID, p.Title, p.FormattedPrice)
					win.IPC().Send("billing:product", p.FormattedPrice)
				}
			})

			b.OnPurchase(func(e billing.PurchaseEvent) {
				switch e.Status {
				case billing.Purchased, billing.Restored:
					log.Printf("[billing] ✅ active: %s", e.ProductID)
					win.IPC().Send("billing:status", "active")
				case billing.Deferred:
					log.Printf("[billing] ⏳ deferred: %s", e.ProductID)
					win.IPC().Send("billing:status", "deferred")
				case billing.Cancelled:
					log.Printf("[billing] cancelled")
					win.IPC().Send("billing:status", "cancelled")
				case billing.Failed:
					log.Printf("[billing] ❌ failed: %v", e.Err)
					win.IPC().Send("billing:status", "failed")
				}
			})

			ipcMain := win.IPC()

			ipcMain.On("billing:buy", func(_ ipc.Message) {
				b.Buy("com.sample.app.plus.0001")
			})

			ipcMain.On("billing:restore", func(_ ipc.Message) {
				b.Restore()
			})

			ipcMain.On("billing:check", func(_ ipc.Message) {
				if b.IsActive("com.sample.app.plus.0001") {
					win.IPC().Send("billing:status", "active")
				} else {
					win.IPC().Send("billing:status", "inactive")
				}
			})

			win.LoadHTML(billingHTML)
		})
	})

	if err := app.Run(); err != nil {
		log.Fatalf("arc: %v", err)
	}
}

func rendererPath() string {
	if runtime.GOOS == "windows" {
		return "../../renderer/build/bin/Debug/renderer.exe"
	}
	return "../../renderer/build/bin/renderer"
}

const billingHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Sample App</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: system-ui, sans-serif;
      background: #0f0f0f;
      color: #f0f0f0;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: center;
      height: 100vh;
      gap: 1.5rem;
    }
    h1 { font-size: 1.4rem; color: #ddd; }
    p  { font-size: 0.9rem; color: #666; }
    #price { font-size: 1rem; color: #888; min-height: 1.2em; }
    .actions { display: flex; gap: 0.75rem; }
    button {
      padding: 0.5rem 1.4rem;
      font-size: 0.9rem;
      cursor: pointer;
      border: 1px solid #444;
      border-radius: 6px;
      background: #1e1e1e;
      color: #f0f0f0;
    }
    button:hover { background: #2a2a2a; }
    #status {
      font-size: 0.85rem;
      padding: 0.3rem 0.8rem;
      border-radius: 4px;
      background: #111;
      border: 1px solid #333;
      color: #666;
    }
    #status.active   { background: #0d2b0d; border-color: #2a5a2a; color: #4caf50; }
    #status.failed   { background: #2b0d0d; border-color: #5a2a2a; color: #f44336; }
    #status.deferred { background: #1a1a0d; border-color: #4a4a2a; color: #bbb84f; }
  </style>
</head>
<body>
  <h1>Sample App Plus</h1>
  <p>Unlock all premium features.</p>
  <div id="price">Loading price…</div>

  <div class="actions">
    <button onclick="ipc.post('billing:buy', '')">Subscribe</button>
    <button onclick="ipc.post('billing:restore', '')">Restore</button>
  </div>

  <div id="status">checking…</div>

  <script>
    const statusEl = document.getElementById('status')

    ipc.on('billing:product', (price) => {
      document.getElementById('price').textContent = price + ' / month'
    })

    ipc.on('billing:status', (s) => {
      statusEl.textContent = s
      statusEl.className = s
    })

    window.addEventListener('load', () => {
      ipc.post('billing:check', '')
    })
  </script>
</body>
</html>`