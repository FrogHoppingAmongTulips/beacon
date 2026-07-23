package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"beacon/internal/qr"
	"beacon/internal/store"
	"beacon/internal/vpn"
)

// userDTO — представление пользователя для панели (со ссылкой подключения).
type userDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Device    string    `json:"device"`
	Online    bool      `json:"online"`
	Enabled   bool      `json:"enabled"`
	BytesUp   uint64    `json:"bytes_up"`
	BytesDown uint64    `json:"bytes_down"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	Link      string    `json:"link"`
}

func (s *Server) dto(u *store.User) userDTO {
	return userDTO{
		ID:        u.ID,
		Name:      u.Name,
		Device:    u.Device,
		Online:    u.Online(),
		Enabled:   u.Enabled,
		BytesUp:   u.BytesUp,
		BytesDown: u.BytesDown,
		CreatedAt: u.CreatedAt,
		LastSeen:  u.LastSeen,
		Link:      vpn.Link(s.cfg, u),
	}
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
		Name   string `json:"name"`
		Device string `json:"device"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	u, err := s.store.Add(req.Name, req.Device)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "не удалось создать пользователя"})
		return
	}
	// применяем к Xray; ошибка reload не должна терять созданного юзера
	if err := s.xray.Apply(); err != nil {
		log.Printf("xray apply после добавления %s: %v", u.ID, err)
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
	if err := s.xray.Apply(); err != nil {
		log.Printf("xray apply после удаления %s: %v", id, err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUserLink(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "пользователь не найден"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"link": vpn.Link(s.cfg, u)})
}

func (s *Server) handleUserQR(w http.ResponseWriter, r *http.Request) {
	u, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "пользователь не найден", http.StatusNotFound)
		return
	}
	png, err := qr.PNG(vpn.Link(s.cfg, u), 320)
	if err != nil {
		http.Error(w, "не удалось сгенерировать QR", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}
