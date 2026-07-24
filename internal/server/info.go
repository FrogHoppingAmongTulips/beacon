package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
)

// handleInfo отдаёт метаданные сервера и протокола для разделов панели (без секретов вроде приватного ключа).
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	m := s.getLatest()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     s.version,
		"host":        s.cfg.PublicHost,
		"vpn_port":    s.cfg.VPNPort,
		"listen":      s.cfg.ListenAddr,
		"sni":         s.cfg.SNI,
		"dest":        s.cfg.Dest,
		"fingerprint": s.cfg.Fingerprint,
		"public_key":  s.cfg.PublicKey,
		"short_ids":   s.cfg.ShortIDs,
		"uptime_sec":  m.UptimeSec,
		"tls":         "self-signed",
	})
}

// handlePassword меняет пароль панели «на лету» (без перезапуска — сервер держит cfg по указателю).
func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if len(req.Password) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "пароль минимум 6 символов"})
		return
	}
	s.cfg.SetPassword(req.Password)
	if err := s.cfg.Save(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить пароль"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleLogs стримит журнал beacon+xray через SSE (journalctl -f). Только чтение.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "стриминг не поддерживается", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}

	cmd := exec.CommandContext(r.Context(), "journalctl", "-u", "beacon", "-u", "xray", "-n", "80", "-f", "-o", "short-iso", "--no-pager")
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		send("журнал недоступен на этой системе")
		return
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		send(sc.Text())
	}
	_ = cmd.Wait()
}
