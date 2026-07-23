// Package store хранит пользователей VPN в JSON-файле (users.json).
// Под масштаб ~20 ключей файла достаточно; интерфейс позволяет позже заменить на SQLite.
package store

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// ErrNotFound — пользователь с таким id не найден.
var ErrNotFound = errors.New("пользователь не найден")

// User — один ключ доступа (клиент Xray).
type User struct {
	ID        string    `json:"id"`         // UUID клиента Xray (он же email в inbound)
	Name      string    `json:"name"`       // отображаемое имя
	Device    string    `json:"device"`     // устройство/заметка
	Enabled   bool      `json:"enabled"`    // выдаётся ли доступ
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	BytesUp   uint64    `json:"bytes_up"`
	BytesDown uint64    `json:"bytes_down"`
}

// Online — считаем онлайн, если хендшейк был недавно.
func (u *User) Online() bool {
	return !u.LastSeen.IsZero() && time.Since(u.LastSeen) < 2*time.Minute
}

// Store — потокобезопасное файловое хранилище.
type Store struct {
	mu    sync.RWMutex
	path  string
	users map[string]*User
}

// Open загружает существующий файл или создаёт пустое хранилище.
func Open(path string) (*Store, error) {
	s := &Store{path: path, users: map[string]*User{}}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var list []*User
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	for _, u := range list {
		s.users[u.ID] = u
	}
	return s, nil
}

// List возвращает пользователей, отсортированных по дате создания.
func (s *Store) List() []*User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		cp := *u
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// Get возвращает копию пользователя по id.
func (s *Store) Get(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *u
	return &cp, nil
}

// Add создаёт нового пользователя с новым UUID и сохраняет файл.
func (s *Store) Add(name, device string) (*User, error) {
	if name == "" {
		name = "Без имени"
	}
	u := &User{
		ID:        newUUID(),
		Name:      name,
		Device:    device,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	s.users[u.ID] = u
	err := s.persistLocked()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	cp := *u
	return &cp, nil
}

// Delete удаляет пользователя по id.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[id]; !ok {
		return ErrNotFound
	}
	delete(s.users, id)
	return s.persistLocked()
}

// Touch обновляет статистику/last_seen (вызывается синхронизацией с Xray).
func (s *Store) Touch(id string, up, down uint64, seen time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[id]; ok {
		u.BytesUp, u.BytesDown = up, down
		if seen.After(u.LastSeen) {
			u.LastSeen = seen
		}
	}
}

func (s *Store) persistLocked() error {
	list := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		list = append(list, u)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// newUUID генерирует UUID v4 (формат 8-4-4-4-12).
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // версия 4
	b[8] = (b[8] & 0x3f) | 0x80 // вариант
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
