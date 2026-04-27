package main

import (
	"log"
	"runtime"

	"github.com/carbon-os/arc"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/window"
)

func main() {
	app := arc.NewApp(arc.AppConfig{
		Title: "Arc E2E",
		Renderer: arc.RendererConfig{
			Logging: true,
			Path:    rendererPath(),
		},
	})

	app.OnReady(func() {
		win := app.NewBrowserWindow(window.Config{
			Title:  "Arc E2E",
			Width:  900,
			Height: 640,
			Debug:  true,
		})

		ipcMain := win.IPC()

		ipcMain.On("text-ping", func(msg ipc.Message) {
			in := msg.Text()
			log.Printf("[go] text-ping received: %q", in)
			ipcMain.Send("text-pong", "go-echo:"+in)
		})

		ipcMain.On("binary-ping", func(msg ipc.Message) {
			b := msg.Bytes()
			log.Printf("[go] binary-ping received: %d bytes %v", len(b), b)
			out := make([]byte, len(b))
			for i, v := range b {
				out[len(b)-1-i] = v
			}
			log.Printf("[go] sending binary-pong: %v", out)
			ipcMain.SendBytes("binary-pong", out)
		})

		// LoadHTML must be inside win.OnReady — the renderer process needs
		// to connect and signal ready before it can receive any commands.
		win.OnReady(func() {
			win.LoadHTML(testHTML)
		})
	})

	app.OnClose(func() bool { return true })

	if err := app.Run(); err != nil {
		log.Fatalf("arc: %v", err)
	}
}

func rendererPath() string {
	if runtime.GOOS == "windows" {
		return "../renderer/build/bin/Debug/renderer.exe"
	}
	return "../renderer/build/bin/renderer"
}

const testHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Arc E2E</title>
  <style>
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: system-ui, sans-serif;
      background: #0f0f0f;
      color: #f0f0f0;
      padding: 2rem;
    }
    h1 { font-size: 1.2rem; margin-bottom: 1.5rem; color: #aaa; }
    .test {
      background: #1a1a1a;
      border: 1px solid #2a2a2a;
      border-radius: 8px;
      padding: 1rem 1.25rem;
      margin-bottom: 1rem;
    }
    .test h2 { font-size: 0.9rem; color: #888; margin-bottom: 0.75rem; letter-spacing: 0.05em; text-transform: uppercase; }
    .row { display: flex; align-items: center; gap: 0.75rem; margin-bottom: 0.5rem; }
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
    .status {
      font-size: 0.85rem;
      padding: 0.25rem 0.6rem;
      border-radius: 4px;
      background: #111;
      border: 1px solid #333;
      color: #777;
      min-width: 60px;
      text-align: center;
    }
    .status.pass { background: #0d2b0d; border-color: #2a5a2a; color: #4caf50; }
    .status.fail { background: #2b0d0d; border-color: #5a2a2a; color: #f44336; }
    .detail { font-size: 0.8rem; color: #555; font-family: monospace; margin-top: 0.25rem; min-height: 1.2em; }
    #log {
      margin-top: 1.5rem;
      background: #111;
      border: 1px solid #222;
      border-radius: 6px;
      padding: 0.75rem;
      font-family: monospace;
      font-size: 0.8rem;
      color: #666;
      height: 10rem;
      overflow-y: auto;
    }
    .log-send { color: #5b8dd9; }
    .log-recv { color: #4caf50; }
    .log-err  { color: #f44336; }
  </style>
</head>
<body>
  <h1>Arc IPC End-to-End Tests</h1>

  <div class="test">
    <h2>Text round-trip</h2>
    <div class="row">
      <button onclick="runTextTest()">Run</button>
      <span id="text-status" class="status">idle</span>
    </div>
    <div id="text-detail" class="detail"></div>
  </div>

  <div class="test">
    <h2>Binary round-trip</h2>
    <div class="row">
      <button onclick="runBinaryTest()">Run</button>
      <span id="bin-status" class="status">idle</span>
    </div>
    <div id="bin-detail" class="detail"></div>
  </div>

  <div id="log"></div>

  <script>
    // ── Log helper ──────────────────────────────────────────────────────────
    const logEl = document.getElementById('log')
    function appendLog(msg, cls) {
      const line = document.createElement('div')
      line.className = cls || ''
      line.textContent = msg
      logEl.appendChild(line)
      logEl.scrollTop = logEl.scrollHeight
    }

    // ── Status helpers ──────────────────────────────────────────────────────
    function setStatus(id, cls, label) {
      const el = document.getElementById(id)
      el.className = 'status ' + cls
      el.textContent = label
    }
    function setDetail(id, text) {
      document.getElementById(id).textContent = text
    }

    // ── Timeout-aware RPC helper ────────────────────────────────────────────
    function waitForReply(channel, timeoutMs = 3000) {
      return new Promise((resolve, reject) => {
        const t = setTimeout(() => {
          ipc.off(channel)
          reject(new Error('timeout waiting for ' + channel))
        }, timeoutMs)
        ipc.on(channel, (payload) => {
          clearTimeout(t)
          ipc.off(channel)
          resolve(payload)
        })
      })
    }

    // ── Text test ───────────────────────────────────────────────────────────
    async function runTextTest() {
      setStatus('text-status', '', 'running…')
      setDetail('text-detail', '')
      const payload = 'hello-' + Date.now()
      appendLog('→ text-ping: ' + payload, 'log-send')

      try {
        const replyP = waitForReply('text-pong')
        ipc.post('text-ping', payload)
        const reply = await replyP

        const expected = 'go-echo:' + payload
        if (reply === expected) {
          setStatus('text-status', 'pass', 'PASS')
          setDetail('text-detail', 'received: ' + reply)
          appendLog('← text-pong: ' + reply, 'log-recv')
        } else {
          setStatus('text-status', 'fail', 'FAIL')
          setDetail('text-detail', 'got: ' + reply + '  want: ' + expected)
          appendLog('✗ mismatch: ' + reply, 'log-err')
        }
      } catch (e) {
        setStatus('text-status', 'fail', 'FAIL')
        setDetail('text-detail', e.message)
        appendLog('✗ ' + e.message, 'log-err')
      }
    }

    // ── Binary test ─────────────────────────────────────────────────────────
    async function runBinaryTest() {
      setStatus('bin-status', '', 'running…')
      setDetail('bin-detail', '')

      const sent = new Uint8Array([1, 2, 3, 4, 5])
      const expected = new Uint8Array([5, 4, 3, 2, 1]) // Go reverses them
      appendLog('→ binary-ping: [' + Array.from(sent) + ']', 'log-send')

      try {
        const replyP = waitForReply('binary-pong')
        ipc.post('binary-ping', sent.buffer)
        const reply = await replyP  // will be an ArrayBuffer

        const got = new Uint8Array(reply)
        const match = got.length === expected.length &&
          got.every((v, i) => v === expected[i])

        if (match) {
          setStatus('bin-status', 'pass', 'PASS')
          setDetail('bin-detail', 'received: [' + Array.from(got) + ']')
          appendLog('← binary-pong: [' + Array.from(got) + ']', 'log-recv')
        } else {
          setStatus('bin-status', 'fail', 'FAIL')
          setDetail('bin-detail', 'got: [' + Array.from(got) + ']  want: [' + Array.from(expected) + ']')
          appendLog('✗ binary mismatch', 'log-err')
        }
      } catch (e) {
        setStatus('bin-status', 'fail', 'FAIL')
        setDetail('bin-detail', e.message)
        appendLog('✗ ' + e.message, 'log-err')
      }
    }

    // ── Auto-run on load ────────────────────────────────────────────────────
    window.addEventListener('load', async () => {
      await runTextTest()
      await runBinaryTest()
    })
  </script>
</body>
</html>`