// Package deployment defines immutable, validated per-instance game capabilities.
package deployment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

var Forms = []string{"local", "verified", "solo", "friend", "matchmaking", "event"}
var Modes = []string{"classic", "aptitude-grid", "draft", "race-rebuild", "co-race-path", "sort", "memory", "bluff", "clue-puzzle", "strategy-race", "rapport", "creative", "trade", "pedigree", "audio"}

type Limits struct {
	APIRequests              int `json:"apiRequests,omitempty"`
	RequestPerIPMinute       int `json:"requestPerIPMinute,omitempty"`
	RequestPerIPBurst        int `json:"requestPerIPBurst,omitempty"`
	SessionCreatePerIPMinute int `json:"sessionCreatePerIPMinute,omitempty"`
	SessionCreatePerIPBurst  int `json:"sessionCreatePerIPBurst,omitempty"`
	QueuedCommands           int `json:"queuedCommands,omitempty"`
	RateLimitBuckets         int `json:"rateLimitBuckets,omitempty"`
	VerifiedChallenges       int `json:"verifiedChallenges,omitempty"`
	Connections              int `json:"connections,omitempty"`
	ConnectionsPerRoom       int `json:"connectionsPerRoom,omitempty"`
	ConnectionsPerSession    int `json:"connectionsPerSession,omitempty"`
	PlayersPerRoom           int `json:"playersPerRoom,omitempty"`
	SpectatorsPerRoom        int `json:"spectatorsPerRoom,omitempty"`
}

type Policy struct {
	Accounts       AccountAccess              `json:"accounts,omitempty"`
	Matchmaking    Matchmaking                `json:"matchmaking,omitempty"`
	Network        Network                    `json:"network,omitempty"`
	Turnstile      Turnstile                  `json:"turnstile,omitempty"`
	Identity       Identity                   `json:"identity,omitempty"`
	DeploymentMode string                     `json:"deploymentMode,omitempty"`
	Forms          map[string]bool            `json:"forms,omitempty"`
	Modes          map[string]map[string]bool `json:"modes,omitempty"`
	Limits         Limits                     `json:"limits,omitempty"`
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (p Policy) Validate() error {
	if err := p.Accounts.Validate(); err != nil {
		return err
	}
	if err := p.Matchmaking.Validate(); err != nil {
		return err
	}
	if _, err := ParseTrustedProxyCIDRs(p.Network.TrustedProxyCIDRs); err != nil {
		return err
	}
	if err := p.Turnstile.Validate(); err != nil {
		return err
	}
	if p.DeploymentMode != "" && p.DeploymentMode != "internet" && p.DeploymentMode != "lan" {
		return fmt.Errorf("deploymentMode must be internet or lan")
	}
	for form := range p.Forms {
		if !contains(Forms, form) {
			return fmt.Errorf("unknown form %q", form)
		}
	}
	for mode, forms := range p.Modes {
		if !contains(Modes, mode) {
			return fmt.Errorf("unknown mode %q", mode)
		}
		for form := range forms {
			if !contains(Forms, form) {
				return fmt.Errorf("unknown form %q for %s", form, mode)
			}
		}
	}
	for name, item := range map[string]struct{ value, max int }{
		"apiRequests":              {p.Limits.APIRequests, 4096},
		"requestPerIPMinute":       {p.Limits.RequestPerIPMinute, 1000000},
		"requestPerIPBurst":        {p.Limits.RequestPerIPBurst, 100000},
		"sessionCreatePerIPMinute": {p.Limits.SessionCreatePerIPMinute, 10000},
		"sessionCreatePerIPBurst":  {p.Limits.SessionCreatePerIPBurst, 2000},
		"queuedCommands":           {p.Limits.QueuedCommands, 100000},
		"rateLimitBuckets":         {p.Limits.RateLimitBuckets, 200000},
		"connections":              {p.Limits.Connections, 20000}, "verifiedChallenges": {p.Limits.VerifiedChallenges, 20000}, "connectionsPerRoom": {p.Limits.ConnectionsPerRoom, 1000},
		"connectionsPerSession": {p.Limits.ConnectionsPerSession, 10}, "playersPerRoom": {p.Limits.PlayersPerRoom, 200},
		"spectatorsPerRoom": {p.Limits.SpectatorsPerRoom, 300},
	} {
		if item.value < 0 || item.value > item.max {
			return fmt.Errorf("limit %s must be 0 (default)..%d", name, item.max)
		}
	}
	return nil
}

// Load fails closed on typos, oversized files and concatenated JSON documents.
func Load(path string) (Policy, error) {
	var p Policy
	file, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return p, err
	}
	if len(data) > 65536 {
		return p, fmt.Errorf("deployment config exceeds 64 KiB")
	}
	if trimmed := bytes.TrimSpace(data); len(trimmed) == 0 || trimmed[0] != '{' {
		return p, fmt.Errorf("deployment config must be an object")
	}
	var structure any
	if err := json.Unmarshal(data, &structure); err != nil {
		return p, err
	}
	if hasNull(structure) {
		return p, fmt.Errorf("deployment config does not accept null values")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&p); err != nil {
		return p, err
	}
	var extra any
	if err = dec.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("deployment config must contain one JSON object")
	}
	if err = p.Validate(); err != nil {
		return p, err
	}
	return p, nil
}

// Allowed intersects global and per-mode controls; absent values preserve compatibility.
func (p Policy) Allowed(mode, form string) bool {
	if !contains(Forms, form) || mode == "" {
		return false
	}
	if enabled, exists := p.Forms[form]; exists && !enabled {
		return false
	}
	if enabled, exists := p.Modes[mode][form]; exists && !enabled {
		return false
	}
	return true
}
func (p Policy) Any(mode string) bool {
	for _, form := range Forms {
		if p.Allowed(mode, form) {
			return true
		}
	}
	return false
}
func (p Policy) Capabilities(mode string) map[string]bool {
	out := map[string]bool{}
	for _, form := range Forms {
		out[form] = p.Allowed(mode, form)
	}
	return out
}
func (p Policy) ResolvedLimits() Limits {
	l := p.Limits
	if l.APIRequests == 0 {
		l.APIRequests = 128
	}
	if l.RequestPerIPMinute == 0 {
		l.RequestPerIPMinute = 6000
	}
	if l.RequestPerIPBurst == 0 {
		l.RequestPerIPBurst = 1200
	}
	if l.SessionCreatePerIPMinute == 0 {
		l.SessionCreatePerIPMinute = 300
	}
	if l.SessionCreatePerIPBurst == 0 {
		l.SessionCreatePerIPBurst = 100
	}
	if l.QueuedCommands == 0 {
		l.QueuedCommands = 4096
	}
	if l.RateLimitBuckets == 0 {
		l.RateLimitBuckets = 20000
	}
	if l.VerifiedChallenges == 0 {
		l.VerifiedChallenges = 512
	}
	if l.Connections == 0 {
		l.Connections = 2000
	}
	if l.ConnectionsPerRoom == 0 {
		l.ConnectionsPerRoom = 220
	}
	if l.ConnectionsPerSession == 0 {
		l.ConnectionsPerSession = 2
	}
	if l.PlayersPerRoom == 0 {
		l.PlayersPerRoom = 100
	}
	if l.SpectatorsPerRoom == 0 {
		l.SpectatorsPerRoom = 100
	}
	return l
}

func hasNull(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case map[string]any:
		for _, child := range v {
			if hasNull(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if hasNull(child) {
				return true
			}
		}
	}
	return false
}
