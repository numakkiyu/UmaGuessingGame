package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestIngressLimitsInvalidCredentialsBeforeLookup(t *testing.T) {
	s := New(Deps{Config: Config{Limiter: LimiterConfig{RequestPerIP: Rate{PerMinute: 1, BurstTokens: 1}}}})
	for i, want := range []int{501, 429} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/sessions/current", nil)
		r.AddCookie(&http.Cookie{Name: "uma_session", Value: "invalid"})
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("request %d: %d, want %d", i, w.Code, want)
		}
		if want == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing backoff")
		}
	}
}

func TestRESTCapacityReleasesAndLivenessRemainsAvailable(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s := New(Deps{Config: Config{MaxConcurrentRequests: 1}, Ready: func(ctx context.Context) (bool, []string) {
		once.Do(func() { close(entered) })
		select {
		case <-finish:
			return true, nil
		case <-ctx.Done():
			return false, nil
		}
	}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/health/ready", nil))
	}()
	<-entered
	for path, want := range map[string]int{"/api/v1/config": 503, "/health/live": 200} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s = %d, want %d", path, w.Code, want)
		}
		if want == 503 && w.Header().Get("Retry-After") == "" {
			t.Error("no retry hint")
		}
	}
	close(finish)
	<-done
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/health/ready", nil))
	if w.Code != 200 {
		t.Fatalf("slot leaked: %d", w.Code)
	}
}

func TestRoundLimitsDoNotShareAnEmptyRoomKey(t *testing.T) {
	cfg := Config{Limiter: LimiterConfig{RoomPerMinute: Rate{PerMinute: 1, BurstTokens: 1}}}.withDefaults()
	s := &Server{cfg: cfg, limiter: NewLimiter(cfg.Limiter)}
	for _, id := range []string{"round-a", "round-b"} {
		r := httptest.NewRequest("POST", "/api/v1/rounds/"+id+"/submissions", nil)
		r.SetPathValue("roundId", id)
		r = r.WithContext(context.WithValue(r.Context(), ctxSession, sessionValue{ID: id}))
		if err := s.limit(httptest.NewRecorder(), r, routeInfo{RateClass: "room", OperationID: "createSubmission"}); err != nil {
			t.Fatalf("unrelated round %s throttled: %v", id, err)
		}
		if err := s.limit(httptest.NewRecorder(), r, routeInfo{RateClass: "room", OperationID: "createSubmission"}); err == nil {
			t.Fatal("same resource exceeded its budget")
		}
	}
}

type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (w *deadlineWriter) SetWriteDeadline(d time.Time) error { w.deadline = d; return nil }

func TestLoggingWrapperPreservesConnectionControl(t *testing.T) {
	w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	deadline := time.Now().Add(time.Second)
	if err := http.NewResponseController(&statusRecorder{ResponseWriter: w}).SetWriteDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if !w.deadline.Equal(deadline) {
		t.Fatal("deadline did not reach transport")
	}
}
