package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStreamOriginPolicyCoversBrowserSSE pins the fix for a real defect.
//
// EventSource does not send Origin for same-origin requests, and Origin is a
// forbidden header name so page script cannot add one. The original policy
// required Origin unconditionally, which made every browser SSE stream fail with
// 403 while curl succeeded — invisible to tests that hand-built a request with an
// Origin header.
func TestStreamOriginPolicyCoversBrowserSSE(t *testing.T) {
	t.Parallel()

	newSvc := func(allowMissing bool) *Service {
		return &Service{cfg: Config{AllowMissingOrigin: allowMissing}}
	}
	req := func(origin, referer string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/rooms/rom_1/stream", nil)
		r.Host = "uma.example:8080"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if referer != "" {
			r.Header.Set("Referer", referer)
		}
		return r
	}

	cases := []struct {
		name         string
		allowMissing bool
		origin       string
		referer      string
		wantErr      bool
		why          string
	}{
		{
			name:    "browser EventSource: same-origin Referer, no Origin",
			referer: "http://uma.example:8080/rooms/rom_1",
			wantErr: false,
			why:     "this is the exact shape a browser EventSource sends; rejecting it breaks SSE entirely",
		},
		{
			name:    "browser EventSource: cross-origin Referer",
			referer: "http://evil.example/rooms/rom_1",
			wantErr: true,
			why:     "the fallback must still enforce same-origin",
		},
		{
			name:    "malformed Referer is rejected, not ignored",
			referer: "://not-a-url",
			wantErr: true,
			why:     "garbage must not become a bypass",
		},
		{
			name:    "explicit Origin still wins and is checked",
			origin:  "http://evil.example",
			referer: "http://uma.example:8080/rooms/rom_1",
			wantErr: true,
			why:     "a mismatched Origin must fail even if Referer looks fine",
		},
		{
			name:    "explicit same-origin Origin passes",
			origin:  "http://uma.example:8080",
			wantErr: false,
		},
		{
			name:    "no Origin and no Referer without opt-in",
			wantErr: true,
			why:     "non-browser clients need an explicit opt-in",
		},
		{
			name:         "no Origin and no Referer with opt-in",
			allowMissing: true,
			wantErr:      false,
			why:          "the documented private-network case",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSvc(tc.allowMissing)
			err := svc.OriginAllowedForStream(req(tc.origin, tc.referer))
			if tc.wantErr && err == nil {
				t.Fatalf("expected rejection: %s", tc.why)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected acceptance but got %v: %s", err, tc.why)
			}
		})
	}
}
