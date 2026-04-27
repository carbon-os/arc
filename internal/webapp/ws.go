package webapp

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// clients holds all active WebSocket connections.
// Access is only from the HTTP server goroutines so a simple map + mutex is fine.
type hub struct {
	conns map[*websocket.Conn]struct{}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("arc: ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	s.hubMu.Lock()
	if s.hub == nil {
		s.hub = make(map[*websocket.Conn]struct{})
	}
	s.hub[conn] = struct{}{}
	s.hubMu.Unlock()

	defer func() {
		s.hubMu.Lock()
		delete(s.hub, conn)
		s.hubMu.Unlock()
	}()

	for {
		mt, msg, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("arc: ws read: %v", err)
			}
			return
		}

		// Broadcast back to all connected clients.
		s.hubMu.Lock()
		for c := range s.hub {
			if writeErr := c.WriteMessage(mt, msg); writeErr != nil {
				log.Printf("arc: ws write: %v", writeErr)
			}
		}
		s.hubMu.Unlock()
	}
}