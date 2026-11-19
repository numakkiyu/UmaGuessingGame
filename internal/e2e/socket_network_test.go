package e2e

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketThroughHTTPMiddleware(t *testing.T) {
	h := newHarness(t)
	h.createSession("联机训练员")
	roomID := h.createRoom("classic")
	u, _ := url.Parse(h.base)
	headers := http.Header{"Origin": []string{h.base}}
	for _, c := range h.client.Jar.Cookies(u) {
		headers.Add("Cookie", c.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, strings.Replace(h.base, "http://", "ws://", 1)+"/api/v1/rooms/"+roomID+"/socket", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("upgrade through middleware: %v, response=%v", err, resp)
	}
	defer conn.CloseNow()
	_, raw, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(raw), `"type":"hello"`) {
		t.Fatalf("hello: %s %v", raw, err)
	}
}
