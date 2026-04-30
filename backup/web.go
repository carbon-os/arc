package arc

import (
	"fmt"
	"os"

	"github.com/carbon-os/arc/internal/webapp"
)

func (a *App) runWeb(readyCb func(), closeCb func() bool) error {
	// Honour --webapp flag even when WebApp is false; exit clearly when not.
	for _, arg := range os.Args[1:] {
		if arg == "--webapp" && !a.cfg.WebApp {
			fmt.Fprintln(os.Stderr,
				"arc: web mode is not enabled for this application.\n"+
					"     set WebApp: true in arc.AppConfig to enable it.\n"+
					"     exiting.")
			os.Exit(1)
		}
	}

	srv := webapp.NewServer(webapp.Config{
		Host:    a.cfg.Host,
		Port:    a.cfg.Port,
		OnReady: readyCb,
	})
	return srv.Run()
}