package main

import (
	"log"

	"github.com/carbon-os/arc"
	"github.com/carbon-os/arc/ipc"
	"github.com/carbon-os/arc/webview"
	"github.com/carbon-os/arc/window"
)

const arcHostPath = "/Users/galaxy/Desktop/libarc/build/bin/arc-host"

func main() {
	app := arc.NewApp(arc.AppConfig{
		Title:   "IPC Demo",
		Logging: true,
		Host:    arc.HostConfig{Path: arcHostPath},
	})

	app.OnReady(func() {
		win := app.NewWindow(window.Config{
			Title:  "IPC Demo",
			Width:  480,
			Height: 320,
			Center: true,
		})

		win.OnReady(func() {
			view := win.NewWebView(webview.Config{Layout: "root"})
			h := view.IPC()

			h.On("get-time", func(msg ipc.Message) {
				log.Printf("[go] got get-time from JS: %q", msg.Text())
				h.Send("time", msg.Text())
			})

			h.On("log", func(msg ipc.Message) {
				log.Printf("[js] %s", msg.Text())
			})

			view.OnReady(func() {
				view.LoadHTML(ui)
			})
		})
	})

	app.OnClose(func() {
		log.Println("app closed")
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

const ui = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>IPC Demo</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: system-ui, sans-serif;
    display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    height: 100vh; gap: 16px; background: #f5f5f5;
  }
  button {
    padding: 10px 28px; font-size: 15px;
    border: none; border-radius: 8px;
    background: #0066ff; color: #fff; cursor: pointer;
  }
  button:active { opacity: .8; }
  #out { font-size: 13px; color: #444; min-height: 20px; }
</style>
</head>
<body>

<button id="btn">Ask Go for the time</button>
<p id="out">—</p>

<script>
  let seq = 0;

  document.getElementById('btn').addEventListener('click', () => {
    const id = String(++seq);

    ipc.on('time', (payload) => {
      ipc.off('time');
      document.getElementById('out').textContent = 'Go replied: ' + payload;
      ipc.post('log', 'received reply for request ' + payload);
    });

    ipc.post('get-time', id);
    ipc.post('log', 'sent get-time request ' + id);
  });
</script>

</body>
</html>
`