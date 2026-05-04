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
		Title:   "Apple Store Demo",
		Logging: true,
		Renderer: arc.RendererConfig{
			Path: "/Users/galaxy/Desktop/libarc/build/arc-host/arc-host",
		},
	})

	store := billing.NewAppleStore(app)
	if store == nil {
		log.Fatal("Apple Store is not available on this platform")
	}

	store.OnProductsFetched(func(products []billing.AppleProduct) {
		log.Printf("[store] fetched %d products", len(products))
		for _, p := range products {
			log.Printf("  %s  %s  kind=%s", p.ID, p.DisplayPrice, p.Kind)
			if p.IntroductoryOffer != nil {
				log.Printf("    intro: %s  mode=%s", p.IntroductoryOffer.DisplayPrice, p.IntroductoryOffer.PaymentMode)
			}
		}
	})

	store.OnPurchaseCompleted(func(r billing.ApplePurchaseResult) {
		if r.Error != nil {
			log.Printf("[store] purchase failed: %s  reason=%s", r.ProductID, *r.Error)
			return
		}
		log.Printf("[store] purchased: %s  txn=%s", r.ProductID, r.Transaction.ID)
	})

	store.OnRestoreCompleted(func(results []billing.ApplePurchaseResult) {
		log.Printf("[store] restore: %d results", len(results))
		for _, r := range results {
			if r.Error != nil {
				log.Printf("  %s  error=%s", r.ProductID, *r.Error)
			} else {
				log.Printf("  %s  txn=%s", r.ProductID, r.Transaction.ID)
			}
		}
	})

	store.OnEntitlementsChanged(func() {
		log.Printf("[store] entitlements changed — refreshing")
		store.CurrentEntitlements()
	})

	store.OnEntitlements(func(ents []billing.AppleEntitlement) {
		log.Printf("[store] entitlements: %d active", len(ents))
		for _, e := range ents {
			log.Printf("  %s  state=%s  renews=%v", e.ProductID, e.State, e.WillAutoRenew)
		}
	})

	store.OnPromotedIAP(func(productID string) {
		log.Printf("[store] promoted IAP tapped: %s — deferring to UI", productID)
		// We don't call store.Purchase here; the UI will trigger it.
	})

	app.OnReady(func() {
		win := app.NewWindow(window.Config{
			Title:  "Apple Store Demo",
			Width:  900,
			Height: 640,
			Debug:  true,
		})

		win.SetAlwaysOnTop(true)

		ipcMain := win.IPC()

		// JS → Go: fetch products
		ipcMain.On("store:fetch", func(_ ipc.Message) {
			store.FetchProducts([]billing.AppleProductSpec{
				{ID: "com.example.pro_lifetime", Kind: billing.AppleKindNonConsumable},
				{ID: "com.example.monthly",      Kind: billing.AppleKindAutoRenewable},
				{ID: "com.example.credits_100",  Kind: billing.AppleKindConsumable},
			})
		})

		// JS → Go: purchase a product
		ipcMain.On("store:purchase", func(msg ipc.Message) {
			store.Purchase(msg.Text(), billing.ApplePurchaseOptions{})
		})

		// JS → Go: restore purchases
		ipcMain.On("store:restore", func(_ ipc.Message) {
			store.RestorePurchases()
		})

		// JS → Go: check a single entitlement
		ipcMain.On("store:check", func(msg ipc.Message) {
			productID := msg.Text()
			store.CheckEntitlement(productID, func(e *billing.AppleEntitlement) {
				var body any
				if e == nil {
					body = map[string]any{"product_id": productID, "owned": false}
				} else {
					body = map[string]any{
						"product_id": productID,
						"owned":      true,
						"state":      e.State,
						"renews":     e.WillAutoRenew,
					}
				}
				data, _ := json.Marshal(body)
				ipcMain.Send("store:entitlement", string(data))
			})
		})

		// JS → Go: request a refund
		ipcMain.On("store:refund", func(msg ipc.Message) {
			store.RequestRefund(msg.Text(), func(status billing.AppleRefundStatus) {
				ipcMain.Send("store:refund_status", string(status))
			})
		})

		win.OnReady(func() {
			// Fetch products and current entitlements on launch.
			store.FetchProducts([]billing.AppleProductSpec{
				{ID: "com.example.pro_lifetime", Kind: billing.AppleKindNonConsumable},
				{ID: "com.example.monthly",      Kind: billing.AppleKindAutoRenewable},
				{ID: "com.example.credits_100",  Kind: billing.AppleKindConsumable},
			})
			store.CurrentEntitlements()

			win.LoadHTML(appleStoreHTML)
		})
	})

	app.OnClose(func() bool { return true })

	if err := app.Run(); err != nil {
		log.Fatalf("arc: %v", err)
	}
}

const appleStoreHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Apple Store Demo</title>
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
      width: 260px;
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
  <h1>Apple Store Demo</h1>

  <div class="section">
    <h2>Products</h2>
    <div class="row">
      <button onclick="fetchProducts()">Fetch Products</button>
    </div>
  </div>

  <div class="section">
    <h2>Purchase</h2>
    <div class="row">
      <input id="purchase-id" placeholder="com.example.monthly" value="com.example.monthly">
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
    <h2>Refund</h2>
    <div class="row">
      <input id="refund-id" placeholder="transaction ID">
      <button onclick="requestRefund()">Request Refund</button>
    </div>
  </div>

  <div class="section">
    <h2>Restore</h2>
    <div class="row">
      <button onclick="restore()">Restore Purchases</button>
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

    ipc.on('store:refund_status', payload => {
      appendLog('← refund status: ' + payload, 'log-recv')
    })

    function fetchProducts() {
      appendLog('→ store:fetch', 'log-send')
      ipc.send('store:fetch', '')
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

    function requestRefund() {
      const id = document.getElementById('refund-id').value.trim()
      if (!id) return
      appendLog('→ store:refund  ' + id, 'log-send')
      ipc.send('store:refund', id)
    }

    function restore() {
      appendLog('→ store:restore', 'log-send')
      ipc.send('store:restore', '')
    }
  </script>
</body>
</html>`