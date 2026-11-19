package deployment

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AccountAccess controls authentication independently of anonymous game sessions.
type AccountAccess struct {
	Enabled         bool              `json:"enabled"`
	PublicURL       string            `json:"publicUrl,omitempty"`
	RequireLogin    map[string]bool   `json:"requireLogin,omitempty"`
	IdleSeconds     int               `json:"idleSeconds,omitempty"`
	AbsoluteSeconds int               `json:"absoluteSeconds,omitempty"`
	Providers       []AccountProvider `json:"providers,omitempty"`
}

type AccountProvider struct {
	ID              string            `json:"id"`
	Enabled         bool              `json:"enabled"`
	Protocol        string            `json:"protocol,omitempty"`
	Issuer          string            `json:"issuer,omitempty"`
	ClientID        string            `json:"clientId,omitempty"`
	ClientSecretEnv string            `json:"clientSecretEnv,omitempty"`
	DisplayNames    map[string]string `json:"displayNames,omitempty"`
	IconPath        string            `json:"iconPath,omitempty"`
}

var AccountPresets = []string{"qq", "google", "x", "microsoft", "github", "facebook", "oidc"}

func (a AccountAccess) Resolved() AccountAccess {
	if a.IdleSeconds == 0 {
		a.IdleSeconds = 86400
	}
	if a.AbsoluteSeconds == 0 {
		a.AbsoluteSeconds = 604800
	}
	return a
}

func (a AccountAccess) Required(form string) bool { return a.Enabled && a.RequireLogin[form] }

func (a AccountAccess) Validate() error {
	a = a.Resolved()
	if a.IdleSeconds < 300 || a.IdleSeconds > 604800 || a.AbsoluteSeconds < a.IdleSeconds || a.AbsoluteSeconds > 2592000 {
		return fmt.Errorf("accounts: invalid idle or absolute lifetime")
	}
	for form, required := range a.RequireLogin {
		if !contains([]string{"friend", "event", "matchmaking"}, form) {
			return fmt.Errorf("accounts: invalid play form %q", form)
		}
		if required && !a.Enabled {
			return fmt.Errorf("accounts: requireLogin needs enabled accounts")
		}
	}
	if a.Enabled {
		u, err := url.Parse(a.PublicURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("accounts.publicUrl must be an HTTPS origin")
		}
	}
	seen, active := map[string]bool{}, 0
	if len(a.Providers) > 12 {
		return fmt.Errorf("accounts: at most 12 providers may be configured")
	}
	for _, p := range a.Providers {
		if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`).MatchString(p.ID) || seen[p.ID] {
			return fmt.Errorf("accounts: unknown or duplicate provider %q", p.ID)
		}
		seen[p.ID] = true
		if !p.Enabled {
			continue
		}
		active++
		if len(p.DisplayNames) != 0 || !contains(AccountPresets, p.ID) {
			if len(p.DisplayNames) != 3 {
				return fmt.Errorf("accounts: %s needs names for zh-Hans, zh-Hant and ja", p.ID)
			}
			for _, locale := range []string{"zh-Hans", "zh-Hant", "ja"} {
				name := p.DisplayNames[locale]
				if strings.TrimSpace(name) != name || name == "" || utf8.RuneCountInString(name) > 48 || strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) {
					return fmt.Errorf("accounts: invalid display name for %s (%s)", p.ID, locale)
				}
			}
		}
		if p.IconPath != "" && (strings.Contains(p.IconPath, "..") || !regexp.MustCompile(`^/assets/ui/providers/[a-z0-9][a-z0-9._-]{0,63}\.(svg|png|webp)$`).MatchString(p.IconPath)) {
			return fmt.Errorf("accounts: %s iconPath must be a local provider asset", p.ID)
		}
		if p.ClientID == "" || len(p.ClientID) > 512 || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(p.ClientSecretEnv) {
			return fmt.Errorf("accounts: %s needs clientId and clientSecretEnv", p.ID)
		}
		if p.Protocol == "oidc" {
			u, err := url.Parse(p.Issuer)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return fmt.Errorf("accounts: invalid issuer for %s", p.ID)
			}
		} else if p.Protocol != "oauth2" || (p.ID != "github" && p.ID != "x") {
			return fmt.Errorf("accounts: %s needs OIDC (native OAuth adapters: github, x)", p.ID)
		}
	}
	if a.Enabled && active == 0 {
		return fmt.Errorf("accounts: enable at least one configured provider")
	}
	return nil
}
