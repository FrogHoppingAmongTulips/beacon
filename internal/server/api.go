package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"aqu/internal/awg"
	"aqu/internal/qr"
	"aqu/internal/store"
	"aqu/internal/vpn"
)

// userDTO — представление пользователя для панели (со ссылкой подключения или AmneziaWG-конфигом).
type userDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Online    bool      `json:"online"`
	Enabled   bool      `json:"enabled"`
	BytesUp   uint64    `json:"bytes_up"`
	BytesDown uint64    `json:"bytes_down"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	Protocol  string    `json:"protocol"`
	Link      string    `json:"link"` // vless:// для reality, пусто для amneziawg (см. /conf)
}

func (s *Server) dto(u *store.User) userDTO {
	d := userDTO{
		ID:        u.ID,
		Name:      u.Name,
		Online:    u.Online(),
		Enabled:   u.Enabled,
		BytesUp:   u.BytesUp,
		BytesDown: u.BytesDown,
		CreatedAt: u.CreatedAt,
		LastSeen:  u.LastSeen,
		Protocol:  "reality",
	}
	if u.AWGPublicKey != "" {
		d.Protocol = "amneziawg"
	} else {
		d.Link = vpn.Link(s.cfg, u)
	}
	return d
}

// applyEngine перезапускает движок текущего активного протокола.
func (s *Server) applyEngine() error {
	if s.cfg.Protocol == "amneziawg" && s.awg != nil {
		return s.awg.Apply()
	}
	return s.xray.Apply()
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.getLatest())
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users := s.store.List()
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		out = append(out, s.dto(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAddUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	u, err := s.store.Add(req.Name, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось создать пользователя"})
		return
	}
	if s.cfg.Protocol == "amneziawg" {
		priv, pub, err := awg.GenerateKeypair()
		if err != nil {
			log.Printf("awg keygen для %s: %v", u.ID, err)
		} else {
			ip, err := awg.NextIP(s.cfg.AWGSubnet, s.store.UsedAWGIPs())
			if err != nil {
				log.Printf("awg ip alloc для %s: %v", u.ID, err)
			} else if err := s.store.SetAWG(u.ID, priv, pub, ip); err != nil {
				log.Printf("awg сохранение ключей %s: %v", u.ID, err)
			}
			u, _ = s.store.Get(u.ID)
		}
	}
	// применяем к движку; ошибка reload не должна терять созданного юзера
	if err := s.applyEngine(); err != nil {
		log.Printf("apply после добавления %s: %v", u.ID, err)
	}
	writeJSON(w, http.StatusCreated, s.dto(u))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "пользователь не найден"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.applyEngine(); err != nil {
		log.Printf("apply после удаления %s: %v", id, err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleUpdateUser частично меняет ключ: имя, заметку и/или вкл-выкл.
func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    *string `json:"name"`
		Device  *string `json:"device"`
		Enabled *bool   `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	u, enabledChanged, err := s.store.Update(r.PathValue("id"), req.Name, req.Device, req.Enabled)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "пользователь не найден"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// вкл/выкл меняет список пиров движка — перезапускаем; переименование reload не требует
	if enabledChanged {
		if err := s.applyEngine(); err != nil {
			log.Printf("apply после изменения %s: %v", u.ID, err)
		}
	}
	writeJSON(w, http.StatusOK, s.dto(u))
}

// payload возвращает то, что кодируется в QR/ссылку: vless:// для reality, .conf-текст для amneziawg.
func (s *Server) payload(u *store.User) string {
	if u.AWGPublicKey != "" {
		return awg.ClientConfig(s.cfg, u)
	}
	return vpn.Link(s.cfg, u)
}

func (s *Server) handleUserLink(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "пользователь не найден"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"link": s.payload(u)})
}

func (s *Server) handleUserQR(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "пользователь не найден", http.StatusNotFound)
		return
	}
	png, err := qr.PNG(s.payload(u), 320)
	if err != nil {
		http.Error(w, "не удалось сгенерировать QR", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

// handleUserConf отдаёt .conf для скачивания (AmneziaWG). Для reality — 404.
func (s *Server) handleUserConf(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "пользователь не найден", http.StatusNotFound)
		return
	}
	if u.AWGPublicKey == "" {
		http.Error(w, "у этого ключа нет .conf (протокол reality)", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+u.Name+`.conf"`)
	_, _ = w.Write([]byte(awg.ClientConfig(s.cfg, u)))
}
