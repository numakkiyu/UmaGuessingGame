package account

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
)

type accountTransport func(*http.Request) (*http.Response, error)

func (f accountTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeOAuthLogin(t *testing.T) {
	for _, id := range []string{"github", "x"} {
		t.Run(id, func(t *testing.T) {
			t.Setenv("TEST_NATIVE_SECRET", "test-client-secret")
			var mode, challenge string
			exchanges := 0
			client := &http.Client{Transport: accountTransport(func(r *http.Request) (*http.Response, error) {
				response := func(status int, body string) (*http.Response, error) {
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				}
				tokenURL, userURL := "https://github.com/login/oauth/access_token", "https://api.github.com/user"
				if id == "x" {
					tokenURL, userURL = "https://api.x.com/2/oauth2/token", "https://api.x.com/2/users/me"
				}
				switch r.URL.String() {
				case tokenURL:
					exchanges++
					if r.Method != http.MethodPost || r.ParseForm() != nil {
						t.Fatal("invalid token exchange")
					}
					v := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					if base64.RawURLEncoding.EncodeToString(v[:]) != challenge || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "test-code" || r.Form.Get("redirect_uri") != "https://game.example/api/v1/account/callback/"+id {
						t.Fatal("missing authorization code or PKCE binding")
					}
					if id == "github" {
						if r.Form.Get("client_id") != "test-client" || r.Form.Get("client_secret") != "test-client-secret" {
							t.Fatal("missing GitHub client credentials")
						}
					} else if name, secret, ok := r.BasicAuth(); !ok || name != "test-client" || secret != "test-client-secret" {
						t.Fatal("missing X client authentication")
					}
					if mode == "token-error" {
						return response(400, `{"error":"invalid_grant"}`)
					}
					kind := "Bearer"
					if mode == "wrong-token-type" {
						kind = "MAC"
					}
					return response(200, fmt.Sprintf(`{"access_token":"test-provider-token","token_type":%q}`, kind))
				case userURL:
					if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer test-provider-token" {
						t.Fatal("missing authorized current-user lookup")
					}
					if mode == "user-error" {
						return response(401, `{"error":"unauthorized"}`)
					}
					if mode == "missing-id" {
						return response(200, `{"login":"mutable-name"}`)
					}
					if mode == "malformed" {
						return response(200, `{"id":`)
					}
					sub := "12345678"
					switch mode {
					case "zero":
						sub = "0"
					case "negative":
						sub = "-1"
					case "overflow":
						sub = "18446744073709551616"
					case "fraction":
						sub = "1.5"
					case "exponent":
						sub = "1e2"
					}
					body := `{"id":` + sub + `,"login":"mutable-name"}`
					if id == "x" {
						body = fmt.Sprintf(`{"data":{"id":%q,"username":"mutable-name"}}`, sub)
					}
					if mode == "trailing" {
						body += ` {"another":"object"}`
					}
					return response(200, body)
				default:
					t.Fatalf("unexpected outgoing request: %s", r.URL)
					return nil, fmt.Errorf("unexpected URL")
				}
			})}
			s, err := newService(context.Background(), testDB(t), deployment.AccountAccess{Enabled: true, PublicURL: "https://game.example", Providers: []deployment.AccountProvider{{ID: id, Enabled: true, Protocol: "oauth2", ClientID: "test-client", ClientSecretEnv: "TEST_NATIVE_SECRET"}}}, client)
			if err != nil {
				t.Fatal(err)
			}
			for _, testMode := range []string{"success", "token-error", "wrong-token-type", "user-error", "missing-id", "malformed", "zero", "negative", "overflow", "fraction", "exponent", "trailing"} {
				t.Run(testMode, func(t *testing.T) {
					mode = testMode
					start := httptest.NewRecorder()
					target, err := s.Start(start, httptest.NewRequest("POST", "https://game.example", nil), id, "/friends")
					if err != nil {
						t.Fatal(err)
					}
					u, _ := url.Parse(target)
					challenge = u.Query().Get("code_challenge")
					if u.Query().Get("code_challenge_method") != "S256" || challenge == "" {
						t.Fatal("missing PKCE")
					}
					callback := httptest.NewRequest("GET", "https://game.example/api/v1/account/callback/"+id+"?"+url.Values{"state": {u.Query().Get("state")}, "code": {"test-code"}}.Encode(), nil)
					callback.AddCookie(start.Result().Cookies()[0])
					out := httptest.NewRecorder()
					path, err := s.Callback(callback.Context(), out, callback, id)
					var issued *http.Cookie
					for _, c := range out.Result().Cookies() {
						if c.Name == cookieName {
							issued = c
						}
					}
					if mode != "success" {
						if err == nil || issued != nil {
							t.Fatal("invalid provider response issued a session")
						}
						return
					}
					if err != nil || path != "/friends" || issued == nil {
						t.Fatal("login failed", err)
					}
					current := httptest.NewRequest("GET", "https://game.example/api/v1/account", nil)
					current.AddCookie(issued)
					status, err := s.Current(current.Context(), current)
					if err != nil || !status.Authenticated || status.UserID == "" {
						t.Fatal("verified identity did not produce a usable session", err)
					}
					body, _ := json.Marshal(status)
					if strings.Contains(string(body), "test-provider-token") || strings.Contains(string(body), "12345678") {
						t.Fatal("provider data leaked to session response")
					}
					before := exchanges
					if _, err := s.Callback(callback.Context(), httptest.NewRecorder(), callback, id); err == nil || exchanges != before {
						t.Fatal("callback replay reached provider")
					}
				})
			}
		})
	}
}
