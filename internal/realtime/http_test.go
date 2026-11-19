package realtime

import (
	"bytes"
	"net/http"
	"net/http/httptest"
)

// newRequestWithOrigin builds a GET request carrying the given Origin and Host.
//
// The Origin tests exercise the real *http.Request path (not a parsed struct), so
// they stay honest about what net/http actually delivers to the handler.
func newRequestWithOrigin(origin, host string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "http://"+host+"/api/v1/rooms/room-x/socket", nil)
	req.Host = host
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return req
}

// recorder is a minimal ResponseWriter that remembers the status and body, so a
// pre-upgrade rejection can be asserted without starting a server.
type recorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newRecorder() *recorder {
	return &recorder{header: make(http.Header), code: http.StatusOK}
}

func (r *recorder) Header() http.Header { return r.header }

func (r *recorder) Write(p []byte) (int, error) { return r.body.Write(p) }

func (r *recorder) WriteHeader(code int) { r.code = code }

// Flush satisfies http.Flusher so the SSE path can be exercised too.
func (r *recorder) Flush() {}
