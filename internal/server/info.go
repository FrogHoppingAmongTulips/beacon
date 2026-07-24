package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

// handleInfo отдаёт метаданные сервера и протокола для разделов панели (без секретов вроде приватного ключа).
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	m := s.getLatest()
	tlsKind := "self-signed"
	if s.cfg.ACMEDomain != "" {
		tlsKind = "lets-encrypt"
	}
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
		"acme_domain": s.cfg.ACMEDomain,
		"uptime_sec":  m.UptimeSec,
		"tls":         tlsKind,
		"protocol":    s.cfg.Protocol,
		"awg_port":    s.cfg.AWGPort,
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

// handleMasking меняет маскировочный домен Reality (SNI/dest) и перезапускает Xray.
// ВНИМАНИЕ: смена SNI инвалидирует уже выданные ключи — их придётся раздать заново.
func (s *Server) handleMasking(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SNI string `json:"sni"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	sni := strings.TrimSpace(req.SNI)
	if sni == "" || strings.ContainsAny(sni, " /:") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "укажи домен, напр. www.microsoft.com"})
		return
	}
	s.cfg.SNI = sni
	s.cfg.Dest = sni + ":443"
	if err := s.cfg.Save(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить"})
		return
	}
	if err := s.xray.Apply(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "сохранено, но Xray не перезапустился: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sni": sni})
}

// handleProtocol переключает активный VPN-протокол (reality / amneziawg) и поднимает нужный движок.
func (s *Server) handleProtocol(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Protocol string `json:"protocol"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Protocol != "reality" && req.Protocol != "amneziawg" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "protocol должен быть reality или amneziawg"})
		return
	}
	if req.Protocol == s.cfg.Protocol {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "protocol": s.cfg.Protocol})
		return
	}
	prev := s.cfg.Protocol
	s.cfg.Protocol = req.Protocol
	if err := s.cfg.Save(); err != nil {
		s.cfg.Protocol = prev
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось сохранить"})
		return
	}
	if req.Protocol == "amneziawg" && s.awg != nil {
		if err := s.awg.Apply(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "сохранено, но AmneziaWG не поднялся: " + err.Error()})
			return
		}
	} else {
		if s.awg != nil {
			_ = s.awg.Down()
		}
		if err := s.xray.Apply(); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "сохранено, но Xray не перезапустился: " + err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "protocol": s.cfg.Protocol})
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
