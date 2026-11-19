package account

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type provider struct {
	config          deployment.AccountProvider
	oauth           oauth2.Config
	verifier        *oidc.IDTokenVerifier
	issuer, userURL string
}

func loadProvider(ctx context.Context, c deployment.AccountProvider, callback string, client *http.Client) (*provider, error) {
	secret := os.Getenv(c.ClientSecretEnv)
	if secret == "" {
		return nil, ErrLogin
	}
	p := &provider{config: c, oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: secret, RedirectURL: callback}}
	ctx = oidc.ClientContext(ctx, client)
	if c.Protocol == "oidc" {
		op, err := oidc.NewProvider(ctx, c.Issuer)
		if err != nil {
			return nil, ErrLogin
		}
		p.issuer = c.Issuer
		p.oauth.Endpoint = op.Endpoint()
		p.oauth.Scopes = []string{oidc.ScopeOpenID}
		p.verifier = op.VerifierContext(ctx, &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{"RS256", "ES256"}})
	} else if c.ID == "github" {
		p.issuer = "https://github.com"
		p.oauth.Endpoint = oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}
		p.userURL = "https://api.github.com/user"
	} else if c.ID == "x" {
		p.issuer = "https://x.com"
		p.oauth.Endpoint = oauth2.Endpoint{AuthURL: "https://x.com/i/oauth2/authorize", TokenURL: "https://api.x.com/2/oauth2/token", AuthStyle: oauth2.AuthStyleInHeader}
		p.oauth.Scopes = []string{"users.read", "tweet.read"}
		p.userURL = "https://api.x.com/2/users/me"
	} else {
		return nil, ErrLogin
	}
	for _, raw := range []string{p.oauth.Endpoint.AuthURL, p.oauth.Endpoint.TokenURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, ErrLogin
		}
	}
	return p, nil
}
func (p *provider) subject(ctx context.Context, code, verifier, nonce string, client *http.Client) (string, error) {
	ctx = oidc.ClientContext(ctx, client)
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", ErrLogin
	}
	if !strings.EqualFold(token.TokenType, "Bearer") || token.AccessToken == "" {
		return "", ErrLogin
	}
	if p.verifier != nil {
		raw, ok := token.Extra("id_token").(string)
		if !ok || len(raw) > 32768 {
			return "", ErrLogin
		}
		id, err := p.verifier.Verify(ctx, raw)
		if err != nil || id.Subject == "" || id.IssuedAt.IsZero() || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(nonce)) != 1 || id.IssuedAt.After(time.Now().Add(time.Minute)) {
			return "", ErrLogin
		}
		var claims struct {
			AZP string `json:"azp"`
		}
		if id.Claims(&claims) != nil || (len(id.Audience) > 1 && claims.AZP == "") || (claims.AZP != "" && claims.AZP != p.config.ClientID) {
			return "", ErrLogin
		}
		if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
			return "", ErrLogin
		}
		return id.Subject, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userURL, nil)
	if err != nil {
		return "", ErrLogin
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", ErrLogin
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", ErrLogin
	}
	var body struct {
		ID   json.Number `json:"id"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if decoder.Decode(&body) != nil {
		return "", ErrLogin
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", ErrLogin
	}
	subject := body.Data.ID
	if p.config.ID == "github" {
		subject = string(body.ID)
	}
	// GitHub and X use canonical positive decimal IDs, not mutable usernames.
	if len(subject) == 0 || len(subject) > 20 || subject[0] < '1' || subject[0] > '9' {
		return "", ErrLogin
	}
	for _, digit := range subject {
		if digit < '0' || digit > '9' {
			return "", ErrLogin
		}
	}
	if _, err := strconv.ParseUint(subject, 10, 64); err != nil {
		return "", ErrLogin
	}
	return subject, nil
}

// All outbound identity requests use fixed operator configuration, public HTTPS,
// bounded bodies, a connection budget and no redirects (including JWKS fetches).
func externalClient() *http.Client {
	transport := &http.Transport{Proxy: nil, MaxConnsPerHost: 4, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 32768, ForceAttemptHTTP2: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || port != "443" {
				return nil, ErrLogin
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, ErrLogin
			}
			for _, ip := range ips {
				if !publicIP(ip) {
					return nil, ErrLogin
				}
			}
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		}}
	return &http.Client{Timeout: 10 * time.Second, Transport: boundedTransport{base: transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/32", "2002::/16", "64:ff9b::/96"} {
		if netip.MustParsePrefix(raw).Contains(ip) {
			return false
		}
	}
	return true
}

type boundedTransport struct{ base http.RoundTripper }

func (b boundedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.User != nil {
		return nil, ErrLogin
	}
	resp, err := b.base.RoundTrip(r)
	if err != nil {
		return nil, ErrLogin
	}
	if resp.ContentLength > 1<<20 {
		resp.Body.Close()
		return nil, ErrLogin
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, left: 1 << 20}
	return resp, nil
}

type limitedBody struct {
	io.ReadCloser
	left int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, fmt.Errorf("account: provider response too large")
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.ReadCloser.Read(p)
	b.left -= int64(n)
	return n, err
}
