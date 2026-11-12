// Package observability provides structured logs, metrics and health probes.
// Request bodies, credentials and private answers are excluded from logs.
package observability

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Level names accepted from configuration.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// NewLogger builds a structured JSON logger on stderr.
//
// JSON is chosen so that requestId can be filtered by log tooling without
// parsing prose. Values that could identify a person (display names) are never
// logged; only opaque ids and numeric measurements are.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case LevelDebug:
		lvl = slog.LevelDebug
	case LevelWarn:
		lvl = slog.LevelWarn
	case LevelError:
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}

// Metrics exports a fixed set of counters, gauges and timers in Prometheus text format.
type Metrics struct {
	mu       sync.RWMutex
	counters map[string]*counter
	gauges   map[string]*gauge
	timers   map[string]*timer
	started  time.Time
}

type counter struct{ v float64 }

type gauge struct{ v float64 }

// timer accumulates duration observations. It exposes a sum and a count, which
// is enough to compute a mean and to feed an external histogram if needed.
type timer struct {
	sum   float64
	count float64
	max   float64
}

// NewMetrics builds an empty registry.
func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]*counter),
		gauges:   make(map[string]*gauge),
		timers:   make(map[string]*timer),
		started:  time.Now(),
	}
}

// Inc adds one to the named counter.
func (m *Metrics) Inc(name string) { m.Add(name, 1) }

// Add adds delta to the named counter.
func (m *Metrics) Add(name string, delta float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.counters[name]
	if !ok {
		c = &counter{}
		m.counters[name] = c
	}
	c.v += delta
}

// Set records a gauge value.
func (m *Metrics) Set(name string, v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.gauges[name]
	if !ok {
		g = &gauge{}
		m.gauges[name] = g
	}
	g.v = v
}

// Observe records a duration observation for the named timer.
func (m *Metrics) Observe(name string, d time.Duration) {
	ms := float64(d.Microseconds()) / 1000
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.timers[name]
	if !ok {
		t = &timer{}
		m.timers[name] = t
	}
	t.sum += ms
	t.count++
	if ms > t.max {
		t.max = ms
	}
}

// CounterValue reads a counter (used by tests and diagnostics).
func (m *Metrics) CounterValue(name string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if c, ok := m.counters[name]; ok {
		return c.v
	}
	return 0
}

// GaugeValue reads a gauge.
func (m *Metrics) GaugeValue(name string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if g, ok := m.gauges[name]; ok {
		return g.v
	}
	return 0
}

// WriteText renders the Prometheus text exposition format.
func (m *Metrics) WriteText(w *strings.Builder) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w.WriteString("# HELP uma_uptime_seconds Process uptime in seconds.\n")
	w.WriteString("# TYPE uma_uptime_seconds gauge\n")
	w.WriteString("uma_uptime_seconds ")
	w.WriteString(formatFloat(time.Since(m.started).Seconds()))
	w.WriteString("\n")
	for name, c := range m.counters {
		w.WriteString("# TYPE uma_" + name + " counter\n")
		w.WriteString("uma_" + name + " ")
		w.WriteString(formatFloat(c.v))
		w.WriteString("\n")
	}
	for name, g := range m.gauges {
		w.WriteString("# TYPE uma_" + name + " gauge\n")
		w.WriteString("uma_" + name + " ")
		w.WriteString(formatFloat(g.v))
		w.WriteString("\n")
	}
	for name, t := range m.timers {
		w.WriteString("# TYPE uma_" + name + "_ms summary\n")
		w.WriteString("uma_" + name + "_ms_sum ")
		w.WriteString(formatFloat(t.sum))
		w.WriteString("\n")
		w.WriteString("uma_" + name + "_ms_count ")
		w.WriteString(formatFloat(t.count))
		w.WriteString("\n")
		w.WriteString("uma_" + name + "_ms_max ")
		w.WriteString(formatFloat(t.max))
		w.WriteString("\n")
	}
}

// formatFloat renders a float without exponent for small magnitudes.
func formatFloat(v float64) string {
	if v == float64(int64(v)) {
		return itoa(int64(v))
	}
	return ftoa(v)
}

// itoa is a tiny int formatter to avoid pulling strconv into every call path.
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [24]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ftoa renders a float with three decimals, which is the resolution the
// millisecond timers actually measure.
func ftoa(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	whole := int64(v)
	frac := int64((v - float64(whole)) * 1000)
	s := itoa(whole) + "."
	fs := itoa(frac)
	for len(fs) < 3 {
		fs = "0" + fs
	}
	s += fs
	if neg {
		return "-" + s
	}
	return s
}

// Health aggregates the probes readiness needs.
//
// Liveness must only prove the process runs : it never touches the
// database, so a busy or migrating database cannot cause a restart loop.
type Health struct {
	BuildVersion string
	// Ready holds the readiness probes, in report order.
	Ready []Probe
}

// Probe is one named readiness check.
type Probe struct {
	Name string
	// Check returns nil when healthy.
	Check func(ctx context.Context) error
}

// Status is the readiness outcome.
type Status struct {
	Ready  bool
	Failed []string
}

// Check runs every probe with a bounded context.
func (h *Health) Check(ctx context.Context) Status {
	var failed []string
	for _, p := range h.Ready {
		if p.Check == nil {
			continue
		}
		if err := p.Check(ctx); err != nil {
			failed = append(failed, p.Name)
		}
	}
	return Status{Ready: len(failed) == 0, Failed: failed}
}
