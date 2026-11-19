package deployment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidConfigFailsRatherThanOpeningAllGames(t *testing.T) {
	for _, raw := range []string{`null`, `{"forms":{"event":null}}`, `{"modes":null}`, `{"deplymentMode":"lan"}`, `{"forms":{"evnet":false}}`, `{"modes":{"stratgey-race":{"event":false}}}`, `{"forms":{"event":"false"}}`, `{"limits":{"connections":-1}}`, `{"deploymentMode":"cloud"}`, `{} {}`} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			os.WriteFile(path, []byte(raw), 0600)
			if _, err := Load(path); err == nil {
				t.Fatal("invalid policy silently accepted")
			}
		})
	}
}
func TestGlobalDenialCannotBeOverriddenByMode(t *testing.T) {
	p := Policy{Forms: map[string]bool{"event": false}, Modes: map[string]map[string]bool{"classic": {"event": true, "friend": false}}}
	if p.Allowed("classic", "event") || p.Allowed("classic", "friend") {
		t.Fatal("disabled game form bypassed")
	}
	if !p.Allowed("classic", "local") || !p.Allowed("sort", "friend") {
		t.Fatal("independent form was disabled")
	}
}

func TestSessionArrivalLimitConfiguration(t *testing.T) {
	for _, raw := range []string{
		`{"limits":{"sessionCreatePerIPMinute":-1}}`,
		`{"limits":{"sessionCreatePerIPMinute":10001}}`,
		`{"limits":{"sessionCreatePerIPBurst":-1}}`,
		`{"limits":{"sessionCreatePerIPBurst":2001}}`,
	} {
		path := filepath.Join(t.TempDir(), "policy.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("unbounded session arrival config accepted: %s", raw)
		}
	}
	defaults := (Policy{}).ResolvedLimits()
	if defaults.SessionCreatePerIPMinute != 300 || defaults.SessionCreatePerIPBurst != 100 {
		t.Fatal("missing bounded arrival defaults")
	}
	for _, file := range []string{"../../config/cloud.json", "../../config/lan.json"} {
		p, err := Load(file)
		if err != nil {
			t.Fatal(err)
		}
		resolved := p.ResolvedLimits()
		if resolved.SessionCreatePerIPMinute != p.Limits.SessionCreatePerIPMinute || resolved.SessionCreatePerIPBurst != p.Limits.SessionCreatePerIPBurst {
			t.Fatal("explicit venue arrival limits overwritten")
		}
	}
}

func TestTurnstileConfigurationFailsClosed(t *testing.T) {
	for _, raw := range []string{
		`{"turnstile":{"enabled":true}}`,
		`{"turnstile":{"enabled":true,"siteKey":"site","hostnames":["https://uma.example"]}}`,
		`{"turnstile":{"enabled":true,"siteKey":"site","hostnames":["uma.example"],"operations":["createSubmission"]}}`,
		`{"turnstile":{"grantSeconds":3601}}`, `{"turnstile":{"maxConcurrent":-1}}`,
		`{"turnstile":{"secretEnv":"BAD-NAME"}}`, `{"turnstile":{"signalBurst":-1}}`,
		`{"identity":{"disambiguateNames":null}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(p); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	for _, file := range []string{"../../config/lan.json", "../../config/cloud.json"} {
		p, err := Load(file)
		if err != nil {
			t.Fatal(err)
		}
		if p.Turnstile.Enabled || !p.Identity.DistinguishNames() {
			t.Fatal("unexpected example defaults")
		}
	}
}
