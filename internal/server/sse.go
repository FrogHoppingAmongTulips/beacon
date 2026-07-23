package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleStream отдаёт живые метрики через Server-Sent Events (стандартный EventSource в браузере).
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "стриминг не поддерживается", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func() {
		b, _ := json.Marshal(s.getLatest())
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	send() // сразу отдаём текущее состояние
	t := time.NewTicker(1500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			send()
		}
	}
}
