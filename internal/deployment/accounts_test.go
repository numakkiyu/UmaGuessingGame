package deployment

import "testing"

func TestAccountPolicyIndependentAndFailClosed(t *testing.T) {
	base := AccountAccess{Enabled: true, PublicURL: "https://game.example", RequireLogin: map[string]bool{"friend": true}, Providers: []AccountProvider{{ID: "github", Enabled: true, Protocol: "oauth2", ClientID: "example", ClientSecretEnv: "TEST_SECRET"}}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	if !base.Required("friend") || base.Required("event") || base.Required("matchmaking") || base.Required("local") {
		t.Fatal("independent forms conflated")
	}
	for _, mutate := range []func(*AccountAccess){
		func(a *AccountAccess) { a.Enabled = false }, func(a *AccountAccess) { a.PublicURL = "http://game.example" }, func(a *AccountAccess) { a.PublicURL = "https://user:secret@game.example" }, func(a *AccountAccess) { a.PublicURL = "https://game.example/path" }, func(a *AccountAccess) { a.RequireLogin = map[string]bool{"local": true} }, func(a *AccountAccess) { a.Providers = nil }, func(a *AccountAccess) { a.IdleSeconds = 100 }, func(a *AccountAccess) { a.AbsoluteSeconds = 100 }, func(a *AccountAccess) {
			a.Providers = []AccountProvider{{ID: "qq", Enabled: true, Protocol: "oauth2", ClientID: "c", ClientSecretEnv: "S"}}
		},
	} {
		a := base
		mutate(&a)
		if a.Validate() == nil {
			t.Fatalf("insecure or incomplete policy accepted: %+v", a)
		}
	}
	if err := (AccountAccess{}).Validate(); err != nil {
		t.Fatal("default-off policy rejected", err)
	}
}

func TestCustomOIDCTemplateValidation(t *testing.T) {
	p := AccountProvider{ID: "club", Enabled: true, Protocol: "oidc", Issuer: "https://identity.example", ClientID: "club-client", ClientSecretEnv: "CLUB_CLIENT_SECRET", DisplayNames: map[string]string{"zh-Hans": "同好会", "zh-Hant": "同好會", "ja": "サークル"}, IconPath: "/assets/ui/providers/club.svg"}
	config := func(v AccountProvider) AccountAccess {
		return AccountAccess{Enabled: true, PublicURL: "https://game.example", Providers: []AccountProvider{v}}
	}
	if err := config(p).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AccountProvider){
		func(v *AccountProvider) { v.ID = "../google" },
		func(v *AccountProvider) { v.ID = "__proto__" },
		func(v *AccountProvider) { v.Protocol = "oauth2" },
		func(v *AccountProvider) { v.IconPath = "https://evil.example/a.svg" },
		func(v *AccountProvider) { v.IconPath = "/assets/ui/providers/../a.svg" },
		func(v *AccountProvider) { v.IconPath = "data:image/svg+xml,unsafe" },
		func(v *AccountProvider) { v.DisplayNames = map[string]string{"zh-Hans": "同好会"} },
		func(v *AccountProvider) {
			v.DisplayNames = map[string]string{"zh-Hans": "同好会", "zh-Hant": "同好會", "ja": "a\u202eb"}
		},
	} {
		v := p
		mutate(&v)
		if config(v).Validate() == nil {
			t.Fatalf("unsafe provider template accepted: %+v", v)
		}
	}
}
