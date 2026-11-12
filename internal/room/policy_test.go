package room

import (
	"context"
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"testing"
)

func TestDisabledFormsRejectedBeforeAllocatingRoomOrTicket(t *testing.T) {
	registry, _ := modes.NewRegistry(classic.New())
	policy := deployment.Policy{Forms: map[string]bool{"event": false, "matchmaking": false}, Modes: map[string]map[string]bool{"classic": {"friend": false}}}
	// A nil database deliberately ensures denial occurs before persistent allocation.
	s := New(nil, Config{Policy: policy}, Options{Modes: registry})
	defer s.Close()
	raw := json.RawMessage(`{"modeId":"classic"}`)
	for _, kind := range []string{"event", "friend", "matchmaking"} {
		_, err := s.CreateRoom(context.Background(), "s", RoomCreateInput{Name: "训练室", Kind: kind, ModeConfig: raw, MaxPlayers: 2, Visibility: "invite"})
		if pe := problem.As(err); pe == nil || pe.Code != "play-disabled" {
			t.Fatalf("%s not denied: %v", kind, err)
		}
	}
	if _, err := s.CreateMatchmakingTicket(context.Background(), "s", CreateTicketInput{ModeConfig: raw}); problem.As(err).Code != "play-disabled" {
		t.Fatal(err)
	}
	if _, err := s.CreateEvent(context.Background(), "s", CreateEventInput{}); problem.As(err).Code != "play-disabled" {
		t.Fatal(err)
	}
}
