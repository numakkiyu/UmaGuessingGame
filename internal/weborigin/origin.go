// Package weborigin validates browser origins independently of forwarding headers.
package weborigin

import (
	"net/http"
	"net/url"
	"strings"
)

// Parse accepts a serialized HTTP origin, without credentials or URL suffixes.
func Parse(raw string) (string, string, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(raw, " \t\r\n#") {
		return "", "", false
	}
	return strings.ToLower(u.Scheme), strings.ToLower(u.Host), true
}

// Same compares both scheme and authority. TLS-terminating proxies must use an
// explicit public origin allow list; untrusted forwarded headers are not evidence.
func Same(r *http.Request, scheme, host string) bool {
	expected := "http"
	if r.TLS != nil {
		expected = "https"
	}
	return scheme == expected && strings.EqualFold(host, r.Host)
}
