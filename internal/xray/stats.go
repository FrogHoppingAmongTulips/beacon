package xray

import (
	"context"
	"time"

	"aqu/internal/store"
)

// StatsPoller периодически читает трафик из Xray Stats API и обновляет пользователей.
// Online-статус выводится из активности: если счётчик вырос — юзер помечается активным (last_seen=now).
type StatsPoller struct {
	mgr  *Manager
	prev map[string]Traffic // последнее накопительное значение по каждому email
}

func NewStatsPoller(m *Manager) *StatsPoller {
	return &StatsPoller{mgr: m, prev: make(map[string]Traffic)}
}

// Run опрашивает статистику каждые interval до отмены ctx.
func (p *StatsPoller) Run(ctx context.Context, st *store.Store, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(st)
		}
	}
}

func (p *StatsPoller) tick(st *store.Store) {
	cur, err := p.mgr.QueryTraffic()
	if err != nil {
		return // Xray/API недоступен (например, при разработке) — тихо пропускаем
	}
	now := time.Now()
	for email, t := range cur {
		prev := p.prev[email]
		dUp := delta(t.Up, prev.Up)
		dDown := delta(t.Down, prev.Down)
		p.prev[email] = t
		if dUp > 0 || dDown > 0 {
			st.AddTraffic(email, dUp, dDown, now)
		}
	}
}

// delta учитывает сброс счётчика при рестарте Xray (текущее < предыдущего → берём текущее).
func delta(cur, prev uint64) uint64 {
	if cur >= prev {
		return cur - prev
	}
	return cur
}
