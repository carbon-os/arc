package main

import (
	"encoding/json"
	"log"

	"github.com/carbon-os/arc"
	"github.com/carbon-os/arc/integrations/billing"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/window"
)

func main() {
	app := arc.NewApp(arc.AppConfig{
		Title:   "Microsoft Store Demo",
		Logging: true,
		Renderer: arc.RendererConfig{
			Path: `C:\libarc\build\arc-host\arc-host.exe`,
		},
	})

	store := billing.NewMicrosoftStore(app)
	if store == nil {
		log.Fatal("Microsoft Store is not available on this platform")
	}

	store.OnProductsFetched(func(products []billing.MicrosoftProduct) {
		log.Printf("[store] fetched %d products", len(products))
		for _, p := range products {
			log.Printf("  %s  %s  kind=%s  owned=%v", p.ID, p.DisplayPrice, p.Kind, p.IsOwned)
		}
	})

	store.OnPurchaseCompleted(func(r billing.MicrosoftPurchaseResult) {
		if r.Error != nil {
			log.Printf("[store] purchase failed: %s  error=%s", r.ProductID, *r.Error)
			return
		}
		log.Printf("[store] purchase: %s  status=%s", r.ProductID, r.Status)
	})

	store.OnOwned(func(productIDs []string) {
		log.Printf("[store] owned products: %v", productIDs)
	})

	store.OnEntitlement(func(productID string, owned bool) {
		log.Printf("[store] entitlement: %s  owned=%v", productID, owned)
	})

	app.OnReady(func() {
		win := app.NewWindow(window.Config{
			Title:  "Microsoft Store Demo",
			Width:  900,
			Height: 640,
			Debug:  true,
		})

		win.SetAlwaysOnTop(true)

		ipcMain := win.IPC()

		// JS → Go: fetch products by ID list
		ipcMain.On("store:fetch", func(msg ipc.Message) {
			var ids []string
			if err := json.Unmarshal([]byte(msg.Text()), &ids); err != nil {
				log.Printf("[store] bad product ID list: %v", err)
				return
			}
			store.FetchProducts(ids)
		})

		// JS → Go: purchase a product
		ipcMain.On("store:purchase", func(msg ipc.Message) {
			store.Purchase(msg.Text())
		})

		// JS → Go: get all owned product IDs
		ipcMain.On("store:get_owned", func(_ ipc.Message) {
			store.GetOwned()
		})

		// JS → Go: check a single product entitlement
		ipcMain.On("store:check", func(msg ipc.Message) {
			productID := msg.Text()
			store.CheckEntitlement(productID, func(owned bool) {
				data, _ := json.Marshal(map[string]any{
					"product_id": productID,
					"owned":      owned,
				})
				ipcMain.Send("store:entitlement", string(data))
			})
		})

		// JS → Go: report a consumable fulfilled
		// expects JSON: {"product_id":"...","quantity":1,"tracking_id":"..."}
		ipcMain.On("store:consume", func(msg ipc.Message) {
			var req struct {
				ProductID  string `json:"product_id"`
				Quantity   uint32 `json:"quantity"`
				TrackingID string `json:"tracking_id"`
			}
			if err := json.Unmarshal([]byte(msg.Text()), &req); err != nil {
				log.Printf("[store] bad consume request: %v", err)
				return
			}
			store.ReportConsumable(req.ProductID, req.Quantity, req.TrackingID,
				func(r billing.MicrosoftConsumeResult) {
					data, _ := json.Marshal(map[string]any{
						"product_id":  r.ProductID,
						"tracking_id": r.TrackingID,
						"status":      r.Status,
					})
					ipcMain.Send("store:consumed", string(data))
				})
		})

		win.OnReady(func() {
			// Fetch known products and owned list on launch.
			store.FetchProducts([]string{
				"com.example.pro_lifetime",
				"com.example.credits_100",
			})
			store.GetOwned()

			win.LoadHTML(microsoftStoreHTML)
		})
	})

	app.OnClose(func() bool { return true })

	if err := app.Run(); err != nil {
		log.Fatalf("arc: %v", err)
	}
}

const microsoftStoreHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Microsoft Store Demo</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: system-ui, sans-serif;
      background: #0f0f0f;
      color: #f0f0f0;
      padding: 2rem;
    }
    h1 { font-size: 1.2rem; margin-bottom: 1.5rem; color: #aaa; }
    .section {
      background: #1a1a1a;
      border: 1px solid #2a2a2a;
      border-radius: 8px;
      padding: 1rem 1.25rem;
      margin-bottom: 1rem;
    }
    .section h2 { font-size: 0.85rem; color: #888; margin-bottom: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
    .row { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.5rem; flex-wrap: wrap; }
    button {
      padding: 0.4rem 1rem;
      font-size: 0.85rem;
      cursor: pointer;
      border: 1px solid #444;
      border-radius: 5px;
      background: #222;
      color: #f0f0f0;
    }
    button:hover { background: #2e2e2e; }
    input {
      padding: 0.35rem 0.6rem;
      font-size: 0.85rem;
      background: #111;
      border: 1px solid #333;
      border-radius: 5px;
      color: #f0f0f0;
      width: 220px;
    }
    #log {
      margin-top: 1.5rem;
      background: #111;
      border: 1px solid #222;
      border-radius: 6px;
      padding: 0.75rem;
      font-family: monospace;
      font-size: 0.8rem;
      color: #666;
      height: 12rem;
      overflow-y: auto;
    }
    .log-send { color: #5b8dd9; }
    .log-recv { color: #4caf50; }
    .log-err  { color: #f44336; }
  </style>
</head>
<body>
  <h1>Microsoft Store Demo</h1>

  <div class="section">
    <h2>Products</h2>
    <div class="row">
      <button onclick="fetchProducts()">Fetch Products</button>
      <button onclick="getOwned()">Get Owned</button>
    </div>
  </div>

  <div class="section">
    <h2>Purchase</h2>
    <div class="row">
      <input id="purchase-id" placeholder="com.example.pro_lifetime" value="com.example.pro_lifetime">
      <button onclick="purchase()">Purchase</button>
    </div>
  </div>

  <div class="section">
    <h2>Entitlement Check</h2>
    <div class="row">
      <input id="check-id" placeholder="com.example.pro_lifetime" value="com.example.pro_lifetime">
      <button onclick="checkEntitlement()">Check</button>
    </div>
  </div>

  <div class="section">
    <h2>Report Consumable</h2>
    <div class="row">
      <input id="consume-product" placeholder="com.example.credits_100" value="com.example.credits_100">
      <input id="consume-qty"     placeholder="quantity" value="1" style="width:80px">
      <button onclick="reportConsumable()">Report Fulfilled</button>
    </div>
  </div>

  <div id="log"></div>

  <script>
    const logEl = document.getElementById('log')
    function appendLog(msg, cls) {
      const line = document.createElement('div')
      line.className = cls || ''
      line.textContent = msg
      logEl.appendChild(line)
      logEl.scrollTop = logEl.scrollHeight
    }

    ipc.on('store:entitlement', payload => {
      appendLog('← entitlement: ' + (typeof payload === 'string' ? payload : JSON.stringify(payload)), 'log-recv')
    })

    ipc.on('store:consumed', payload => {
      appendLog('← consumed: ' + (typeof payload === 'string' ? payload : JSON.stringify(payload)), 'log-recv')
    })

    function fetchProducts() {
      const ids = JSON.stringify(['com.example.pro_lifetime', 'com.example.credits_100'])
      appendLog('→ store:fetch  ' + ids, 'log-send')
      ipc.send('store:fetch', ids)
    }

    function getOwned() {
      appendLog('→ store:get_owned', 'log-send')
      ipc.send('store:get_owned', '')
    }

    function purchase() {
      const id = document.getElementById('purchase-id').value.trim()
      if (!id) return
      appendLog('→ store:purchase  ' + id, 'log-send')
      ipc.send('store:purchase', id)
    }

    function checkEntitlement() {
      const id = document.getElementById('check-id').value.trim()
      if (!id) return
      appendLog('→ store:check  ' + id, 'log-send')
      ipc.send('store:check', id)
    }

    function reportConsumable() {
      const productID  = document.getElementById('consume-product').value.trim()
      const quantity   = parseInt(document.getElementById('consume-qty').value, 10) || 1
      const trackingID = crypto.randomUUID()
      const payload    = JSON.stringify({ product_id: productID, quantity, tracking_id: trackingID })
      appendLog('→ store:consume  ' + payload, 'log-send')
      ipc.send('store:consume', payload)
    }
  </script>
</body>
</html>`