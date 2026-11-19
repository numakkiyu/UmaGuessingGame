package account

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/session"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func testService(t *testing.T) *Service {
	t.Helper()
	s, err := newService(context.Background(), testDB(t), deployment.AccountAccess{Enabled: true, RequireLogin: map[string]bool{"friend": true}}, http.DefaultClient)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func identity(t *testing.T, s *Service, subject string) string {
	t.Helper()
	id, err := s.ResolveIdentity(context.Background(), "https://issuer.example", "client", subject)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func loggedRequest(t *testing.T, s *Service, id string) *http.Request {
	t.Helper()
	w := httptest.NewRecorder()
	if err := s.issue(context.Background(), w, id); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://game.example/api/v1/account", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}
func gameID(t *testing.T, s *Service) string {
	t.Helper()
	result, err := session.New(s.db, session.Config{}).Create(context.Background(), "同名训练员")
	if err != nil {
		t.Fatal(err)
	}
	return result.Session.ID
}
func TestIdentityMappingConcurrentAndSeparate(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan string, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := s.ResolveIdentity(ctx, "https://issuer.example", "client", "subject")
			if err != nil {
				t.Error(err)
			}
			results <- id
		}()
	}
	wg.Wait()
	close(results)
	first := ""
	for id := range results {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("duplicate UUID for one identity")
		}
	}
	for _, scope := range [][3]string{{"https://other.example", "client", "subject"}, {"https://issuer.example", "other-client", "subject"}, {"https://issuer.example", "client", "other-subject"}} {
		id, err := s.ResolveIdentity(ctx, scope[0], scope[1], scope[2])
		if err != nil || id == first {
			t.Fatal("merged independent identity", err)
		}
	}
	if len(first) != 36 || strings.HasPrefix(first, "ses_") {
		t.Fatal("not a distinct account UUID")
	}
}
func TestAccountGateDoesNotTrustAnonymousCookieOrOtherAccount(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	sid := gameID(t, s)
	request := httptest.NewRequest("GET", "https://game.example", nil)
	if _, err := s.Require(ctx, request, "friend", sid); err == nil {
		t.Fatal("anonymous bypass")
	}
	if _, err := s.Require(ctx, request, "event", sid); err != nil {
		t.Fatal("independent anonymous form blocked")
	}
	user := identity(t, s, "one")
	r := loggedRequest(t, s, user)
	st, err := s.Require(ctx, r, "friend", sid)
	if err != nil {
		t.Fatal(err)
	}
	if st.UserID == sid {
		t.Fatal("account and game identities conflated")
	}
	active, err := s.GameActive(ctx, sid)
	if err != nil || !active {
		t.Fatal("stream grant missing", err)
	}
	other := loggedRequest(t, s, identity(t, s, "two"))
	if _, err := s.Require(ctx, other, "friend", sid); err == nil {
		t.Fatal("another account inherited protected seat")
	}
	if _, err := s.Require(ctx, request, "friend", sid); err == nil {
		t.Fatal("bound anonymous cookie treated as login")
	}
	if err := s.Logout(ctx, httptest.NewRecorder(), st); err != nil {
		t.Fatal(err)
	}
	active, _ = s.GameActive(ctx, sid)
	if active {
		t.Fatal("revoked account retained streaming access")
	}
}
func TestCookieLifetimeRotationAndRevocation(t *testing.T) {
	s := testService(t)
	now := time.Now().UTC().Truncate(time.Second)
	s.now = func() time.Time { return now }
	s.cfg.IdleSeconds = 3600
	s.cfg.AbsoluteSeconds = 7200
	r := loggedRequest(t, s, identity(t, s, "lifetime"))
	st, err := s.Current(r.Context(), r)
	if err != nil || !st.Authenticated {
		t.Fatal(err)
	}
	r.Header.Set("X-Account-CSRF", st.CSRFToken)
	if !s.VerifyCSRF(r, st) {
		t.Fatal("valid CSRF rejected")
	}
	r.Header.Set("X-Account-CSRF", "wrong")
	if s.VerifyCSRF(r, st) {
		t.Fatal("forged CSRF accepted")
	}
	now = now.Add(31 * time.Minute)
	w := httptest.NewRecorder()
	if err := s.Renew(r.Context(), w, r, st); err != nil {
		t.Fatal(err)
	}
	c := w.Result().Cookies()[0]
	old := cookie(r, cookieName)
	if c.Value == old || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Domain != "" || c.Path != "/" {
		t.Fatal("invalid rotated cookie policy")
	}
	if st2, _ := s.Current(r.Context(), r); !st2.Authenticated {
		t.Fatal("in-flight grace missing")
	}
	// A stale concurrent request must never put the old cookie back in the browser.
	w2 := httptest.NewRecorder()
	if err := s.Renew(r.Context(), w2, r, st); err != nil {
		t.Fatal(err)
	}
	if len(w2.Result().Cookies()) != 0 {
		t.Fatal("stale rotation overwrote new cookie")
	}
	now = now.Add(31 * time.Second)
	if st2, _ := s.Current(r.Context(), r); st2.Authenticated {
		t.Fatal("old cookie outlived grace")
	}
	fresh := httptest.NewRequest("GET", "https://game.example", nil)
	fresh.AddCookie(c)
	st, _ = s.Current(r.Context(), fresh)
	if !st.Authenticated {
		t.Fatal("new cookie unusable")
	}
	now = now.Add(31 * time.Minute)
	st, _ = s.Current(r.Context(), fresh)
	if err := s.Renew(r.Context(), httptest.NewRecorder(), fresh, st); err != nil {
		t.Fatal(err)
	}
	now = st.absolute
	if active, _ := s.Active(r.Context(), st.ID); active {
		t.Fatal("renewal extended absolute lifetime")
	}
	var raw string
	if err := s.db.Read().QueryRow(`SELECT token_hash FROM account_sessions WHERE id=?`, st.ID).Scan(&raw); err != nil || raw == old || raw == c.Value {
		t.Fatal("cookie persisted without hashing")
	}
}
func TestIdleExpiryCannotBeRenewed(t *testing.T) {
	s := testService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	s.cfg.IdleSeconds = 300
	r := loggedRequest(t, s, identity(t, s, "idle"))
	st, _ := s.Current(r.Context(), r)
	now = now.Add(301 * time.Second)
	if current, _ := s.Current(r.Context(), r); current.Authenticated {
		t.Fatal("idle timeout ignored")
	}
	w := httptest.NewRecorder()
	_ = s.Renew(r.Context(), w, r, st)
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("expired session revived")
	}
}

func TestOIDCCodeFlowAndHostileCallbacks(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	var issuer, nonce, challenge, mode string
	var exchanges int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			exchanges++
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			v := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(v[:]) != challenge {
				t.Error("missing PKCE binding")
			}
			if r.Form.Get("redirect_uri") != "https://game.example/api/v1/account/callback/google" {
				t.Error("callback changed")
			}
			aud := "client"
			n := nonce
			iss := issuer
			expiry := time.Now().Add(time.Hour)
			switch mode {
			case "aud":
				aud = "attacker"
			case "nonce":
				n = "wrong"
			case "issuer":
				iss = "https://evil.example"
			case "expired":
				expiry = time.Now().Add(-time.Hour)
			}
			issuedAt := jwt.NewNumericDate(time.Now())
			if mode == "missing-iat" {
				issuedAt = nil
			}
			raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: iss, Subject: "stable-subject", Audience: jwt.Audience{aud}, IssuedAt: issuedAt, Expiry: jwt.NewNumericDate(expiry)}).Claims(map[string]any{"nonce": n}).Serialize()
			if err != nil {
				t.Error(err)
			}
			if mode == "signature" {
				raw = raw[:len(raw)-10] + "AAAAAAAAAA"
			}
			if mode == "missing-id" {
				raw = ""
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "provider-secret", "token_type": "Bearer", "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	issuer = server.URL
	t.Setenv("TEST_OIDC_SECRET", "local-test-secret")
	s, err := newService(context.Background(), testDB(t), deployment.AccountAccess{Enabled: true, PublicURL: "https://game.example", Providers: []deployment.AccountProvider{{ID: "google", Enabled: true, Protocol: "oidc", Issuer: issuer, ClientID: "client", ClientSecretEnv: "TEST_OIDC_SECRET"}}}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, testMode := range []string{"success", "aud", "nonce", "issuer", "expired", "signature", "missing-id", "missing-iat", "cleanup-failed", "cancelled", "wrong-browser", "wrong-provider", "expired-state"} {
		t.Run(testMode, func(t *testing.T) {
			mode = testMode
			w := httptest.NewRecorder()
			startReq := httptest.NewRequest("POST", "https://game.example", nil)
			target, err := s.Start(w, startReq, "google", "/friends")
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(target)
			q := u.Query()
			nonce = q.Get("nonce")
			challenge = q.Get("code_challenge")
			if q.Get("code_challenge_method") != "S256" {
				t.Fatal("weak PKCE")
			}
			callback := httptest.NewRequest("GET", "https://game.example/api/v1/account/callback/google?"+url.Values{"state": {q.Get("state")}, "code": {"test-code"}}.Encode(), nil)
			callback.AddCookie(w.Result().Cookies()[0])
			id := "google"
			switch mode {
			case "cancelled":
				callback.URL.RawQuery += "&error=access_denied"
			case "wrong-browser":
				callback.Header.Del("Cookie")
			case "wrong-provider":
				id = "github"
			case "expired-state":
				s.now = func() time.Time { return time.Now().Add(6 * time.Minute) }
				defer func() { s.now = time.Now }()
			}
			out := httptest.NewRecorder()
			before := exchanges
			cleaned := false
			returnTo, err := s.Callback(callback.Context(), out, callback, id, func() error {
				cleaned = true
				if mode == "cleanup-failed" {
					return fmt.Errorf("test: cleanup unavailable")
				}
				return nil
			})
			if mode == "success" {
				if err != nil || returnTo != "/friends" {
					t.Fatal("login failed", err)
				}
				var accountCookie *http.Cookie
				for _, c := range out.Result().Cookies() {
					if c.Name == cookieName {
						accountCookie = c
					}
				}
				if accountCookie == nil {
					t.Fatal("no account cookie")
				}
				signed := httptest.NewRequest("GET", "https://game.example", nil)
				signed.AddCookie(accountCookie)
				st, err := s.Current(signed.Context(), signed)
				if err != nil || !st.Authenticated {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(st)
				if strings.Contains(string(raw), "provider-secret") || strings.Contains(string(raw), "stable-subject") {
					t.Fatal("credential leaked")
				}
				if _, err = s.Callback(callback.Context(), httptest.NewRecorder(), callback, id); err == nil {
					t.Fatal("callback replay succeeded")
				}
				if exchanges != before+1 {
					t.Fatal("replay contacted provider")
				}
			} else if err == nil {
				t.Fatal("invalid login accepted")
			}
			if cleaned != (mode == "success" || mode == "cleanup-failed") {
				t.Fatal("identity cleanup ran before verification")
			}
			if mode == "cleanup-failed" {
				for _, c := range out.Result().Cookies() {
					if c.Name == cookieName {
						t.Fatal("new account issued despite failed cleanup")
					}
				}
			}
			if mode == "wrong-browser" || mode == "wrong-provider" || mode == "expired-state" || mode == "cancelled" {
				if exchanges != before {
					t.Fatal("invalid callback reached token endpoint")
				}
			}
		})
	}
}
func TestReturnAndOutboundRestrictions(t *testing.T) {
	for _, raw := range []string{"https://evil.example", "//evil.example", "/\\evil", "/%2fevil", "/friends?next=x", "/friends#x", "/unknown"} {
		if SafeReturn(raw) != "/" {
			t.Errorf("unsafe return %s", raw)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.1.1", "::1", "::ffff:127.0.0.1", "fc00::1", "64:ff9b::a00:1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Errorf("private outbound address allowed: %s", ip)
		}
	}
	client := externalClient()
	for _, raw := range []string{"http://example.com", "https://127.0.0.1", "https://user:secret@example.com"} {
		r, _ := http.NewRequest("GET", raw, nil)
		resp, err := client.Do(r)
		if resp != nil {
			resp.Body.Close()
		}
		if err == nil {
			t.Error(fmt.Sprintf("unsafe outbound accepted: %s", raw))
		}
	}
}

func TestPublicProvidersOnlyExposeConfiguredActiveTemplates(t *testing.T) {
	s := testService(t)
	s.cfg.Providers = []deployment.AccountProvider{
		{ID: "microsoft", Enabled: true},
		{ID: "google", Enabled: true},
		{ID: "github", Enabled: false},
		{ID: "club", Enabled: true, DisplayNames: map[string]string{"zh-Hans": "同好会", "zh-Hant": "同好會", "ja": "サークル"}, IconPath: "/assets/ui/providers/club.svg", ClientSecretEnv: "PRIVATE_SECRET_NAME", Issuer: "https://private-config.example"},
	}
	s.providers = map[string]*provider{"microsoft": {}, "google": {}, "github": {}, "club": {}}
	public := s.Public()
	if len(public.Providers) != 3 || public.Providers[0].ID != "microsoft" || public.Providers[1].ID != "google" || public.Providers[2].ID != "club" {
		t.Fatalf("inactive or reordered provider: %+v", public.Providers)
	}
	if public.Providers[2].DisplayNames["ja"] != "サークル" || public.Providers[2].IconPath != "/assets/ui/providers/club.svg" {
		t.Fatal("custom template metadata missing")
	}
	raw, err := json.Marshal(public)
	if err != nil || strings.Contains(string(raw), "PRIVATE_SECRET_NAME") || strings.Contains(string(raw), "private-config.example") {
		t.Fatal("private provider configuration exposed")
	}
}
