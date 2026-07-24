// Package metrics собирает нагрузку сервера из /proc (Linux).
// На системах без /proc (например macOS при разработке) возвращает нули без паники.
package metrics

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sample — снимок состояния сервера в момент времени.
type Sample struct {
	CPUPercent float64 `json:"cpu"`         // загрузка CPU, 0..100
	MemUsed    uint64  `json:"mem_used"`    // байт
	MemTotal   uint64  `json:"mem_total"`   // байт
	NetRxBps   float64 `json:"net_rx_bps"`  // байт/сек скачивание
	NetTxBps   float64 `json:"net_tx_bps"`  // байт/сек отдача
	Load1      float64 `json:"load1"`       // load average 1m
	Load5      float64 `json:"load5"`       // load average 5m
	Load15     float64 `json:"load15"`      // load average 15m
	UptimeSec  int64   `json:"uptime_sec"`  // секунд с запуска ОС
	Timestamp  int64   `json:"ts"`          // unix ms
}

// Collector считает дельты между последовательными замерами.
type Collector struct {
	mu       sync.Mutex
	prevCPU  cpuTimes
	prevNet  netTotals
	prevTime time.Time
	primed   bool
}

func New() *Collector { return &Collector{} }

// Sample делает замер. Первый вызов не имеет базы для скорости/CPU — вернёт нули по ним.
func (c *Collector) Sample() Sample {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	s := Sample{Timestamp: now.UnixMilli()}

	// память
	total, avail := readMem()
	s.MemTotal = total
	if total > avail {
		s.MemUsed = total - avail
	}

	// load average + uptime
	s.Load1, s.Load5, s.Load15 = readLoadAvg()
	s.UptimeSec = readUptime()

	// CPU (дельта)
	cpu := readCPU()
	if c.primed {
		dTotal := float64(cpu.total() - c.prevCPU.total())
		dIdle := float64(cpu.idleAll() - c.prevCPU.idleAll())
		if dTotal > 0 {
			s.CPUPercent = clamp((1-dIdle/dTotal)*100, 0, 100)
		}
	}
	c.prevCPU = cpu

	// сеть (дельта)
	net := readNet()
	if c.primed {
		dt := now.Sub(c.prevTime).Seconds()
		if dt > 0 {
			s.NetRxBps = deltaRate(net.rx, c.prevNet.rx, dt)
			s.NetTxBps = deltaRate(net.tx, c.prevNet.tx, dt)
		}
	}
	c.prevNet = net
	c.prevTime = now
	c.primed = true

	return s
}

// ---- /proc парсинг ----

type cpuTimes struct{ user, nice, system, idle, iowait, irq, softirq, steal uint64 }

func (t cpuTimes) total() uint64 {
	return t.user + t.nice + t.system + t.idle + t.iowait + t.irq + t.softirq + t.steal
}
func (t cpuTimes) idleAll() uint64 { return t.idle + t.iowait }

func readCPU() cpuTimes {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		f := strings.Fields(line)[1:]
		n := make([]uint64, 8)
		for i := 0; i < len(n) && i < len(f); i++ {
			n[i], _ = strconv.ParseUint(f[i], 10, 64)
		}
		return cpuTimes{n[0], n[1], n[2], n[3], n[4], n[5], n[6], n[7]}
	}
	return cpuTimes{}
}

func readMem() (total, avail uint64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		kb, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = kb * 1024
		case "MemAvailable:":
			avail = kb * 1024
		}
	}
	return total, avail
}

type netTotals struct{ rx, tx uint64 }

func readNet() netTotals {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return netTotals{}
	}
	var t netTotals
	for _, line := range strings.Split(string(b), "\n") {
		i := strings.IndexByte(line, ':')
		if i < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:i])
		if iface == "lo" || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "veth") {
			continue
		}
		f := strings.Fields(line[i+1:])
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		t.rx += rx
		t.tx += tx
	}
	return t
}

func readLoadAvg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(f[0], 64)
	l5, _ = strconv.ParseFloat(f[1], 64)
	l15, _ = strconv.ParseFloat(f[2], 64)
	return
}

func readUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	sec, _ := strconv.ParseFloat(f[0], 64)
	return int64(sec)
}

func deltaRate(cur, prev uint64, dt float64) float64 {
	if cur < prev { // счётчик сбросился (перезапуск интерфейса)
		return 0
	}
	return float64(cur-prev) / dt
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
