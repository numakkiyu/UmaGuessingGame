package deployment

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var EntryOperations = []string{"createRoom", "joinRoom", "createMatchmakingTicket", "createChallenge", "createEvent"}

// Turnstile contains no secret value. SecretEnv names an operator-owned variable.
type Turnstile struct {
	Adaptive          *bool    `json:"adaptive,omitempty"`
	RiskThreshold     int      `json:"riskThreshold,omitempty"`
	RiskWindowSeconds int      `json:"riskWindowSeconds,omitempty"`
	BrowserSignals    bool     `json:"browserSignals,omitempty"`
	SignalPerMinute   int      `json:"signalPerMinute,omitempty"`
	SignalBurst       int      `json:"signalBurst,omitempty"`
	Enabled           bool     `json:"enabled"`
	SiteKey           string   `json:"siteKey,omitempty"`
	SecretEnv         string   `json:"secretEnv,omitempty"`
	Hostnames         []string `json:"hostnames,omitempty"`
	GrantSeconds      int      `json:"grantSeconds,omitempty"`
	Operations        []string `json:"operations,omitempty"`
	MaxConcurrent     int      `json:"maxConcurrent,omitempty"`
}

type Identity struct {
	DisambiguateNames *bool `json:"disambiguateNames,omitempty"`
}

func (i Identity) DistinguishNames() bool { return i.DisambiguateNames == nil || *i.DisambiguateNames }
func (t Turnstile) Resolved() Turnstile {
	if t.RiskThreshold == 0 {
		t.RiskThreshold = 6
	}
	if t.RiskWindowSeconds == 0 {
		t.RiskWindowSeconds = 60
	}
	if t.SignalPerMinute == 0 {
		t.SignalPerMinute = 30
	}
	if t.SignalBurst == 0 {
		t.SignalBurst = 10
	}
	if t.SecretEnv == "" {
		t.SecretEnv = "UMA_TURNSTILE_SECRET"
	}
	if t.GrantSeconds == 0 {
		t.GrantSeconds = 900
	}
	if t.MaxConcurrent == 0 {
		t.MaxConcurrent = 8
	}
	if t.Operations == nil {
		t.Operations = append([]string(nil), EntryOperations...)
	}
	return t
}
func (t Turnstile) Validate() error {
	t = t.Resolved()
	if t.RiskThreshold < 3 || t.RiskThreshold > 100 || t.RiskWindowSeconds < 10 || t.RiskWindowSeconds > 600 {
		return fmt.Errorf("turnstile adaptive limits out of range")
	}
	if t.SignalPerMinute < 1 || t.SignalPerMinute > 10000 || t.SignalBurst < 1 || t.SignalBurst > 1000 {
		return fmt.Errorf("turnstile signal limits out of range")
	}
	if t.GrantSeconds < 60 || t.GrantSeconds > 3600 {
		return fmt.Errorf("turnstile.grantSeconds must be 60..3600")
	}
	if t.MaxConcurrent < 1 || t.MaxConcurrent > 64 {
		return fmt.Errorf("turnstile.maxConcurrent must be 1..64")
	}
	if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(t.SecretEnv) {
		return fmt.Errorf("turnstile.secretEnv must name an environment variable")
	}
	seen := map[string]bool{}
	for _, op := range t.Operations {
		if !contains(EntryOperations, op) || seen[op] {
			return fmt.Errorf("turnstile.operations contains invalid or duplicate operation %q", op)
		}
		seen[op] = true
	}
	if t.Enabled && (strings.TrimSpace(t.SiteKey) == "" || len(t.Hostnames) == 0 || len(t.Operations) == 0) {
		return fmt.Errorf("enabled turnstile requires siteKey, hostnames and operations")
	}
	for _, host := range t.Hostnames {
		if len(host) > 253 || host != strings.ToLower(strings.TrimSpace(host)) || strings.ContainsAny(host, "/:* \\@\t\r\n") || host == "" {
			return fmt.Errorf("turnstile.hostnames must contain exact lowercase hostnames without ports")
		}
		if net.ParseIP(host) == nil && !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$`).MatchString(host) {
			return fmt.Errorf("invalid turnstile hostname")
		}
	}
	return nil
}
