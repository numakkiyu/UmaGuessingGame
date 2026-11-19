package httpapi

import (
	"net/http"
	"strings"
	"sync"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// withIngress rejects floods before cookie resolution or database access.
// Health liveness remains independent so existing clients can check reachability.
func (s *Server) withIngress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health/ready" {
			if ok, retry := s.limiter.Allow("request-ip:"+s.clientIP(r), s.cfg.Limiter.RequestPerIP); !ok {
				s.writeError(w, r, problem.RateLimited(retry))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// admitRequest bounds active REST and authentication work without a waiting
// goroutine queue. Established streams use their separate connection capacity.
func (s *Server) admitRequest(w http.ResponseWriter, r *http.Request) (func(), bool) {
	select {
	case s.requestSlots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s.requestSlots }) }, true
	default:
		w.Header().Set("Retry-After", "2")
		s.writeError(w, r, problem.Busy(2))
		return nil, false
	}
}
