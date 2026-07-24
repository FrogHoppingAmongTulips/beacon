package server

import (
	"io"
	"math/rand"
	"net/http"
	"strconv"
)

// maxSpeedBytes ограничивает размер тестовой передачи, чтобы клиент не мог запросить произвольный объём.
const maxSpeedBytes = 32 * 1024 * 1024

// handleSpeedDown отдаёт N случайных байт для замера скорости скачивания.
// Клиент сам засекает время между началом и концом чтения тела ответа.
func (s *Server) handleSpeedDown(w http.ResponseWriter, r *http.Request) {
	n := 8 * 1024 * 1024
	if v := r.URL.Query().Get("bytes"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= maxSpeedBytes {
			n = parsed
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	buf := make([]byte, 64*1024)
	rnd := rand.New(rand.NewSource(1))
	for n > 0 {
		chunk := len(buf)
		if n < chunk {
			chunk = n
		}
		rnd.Read(buf[:chunk])
		if _, err := w.Write(buf[:chunk]); err != nil {
			return
		}
		n -= chunk
	}
}

// handleSpeedUp читает и отбрасывает тело запроса — клиент замеряет время отправки.
func (s *Server) handleSpeedUp(w http.ResponseWriter, r *http.Request) {
	n, _ := io.Copy(io.Discard, io.LimitReader(r.Body, maxSpeedBytes))
	writeJSON(w, http.StatusOK, map[string]int64{"received": n})
}

