package deployment

import "fmt"

// Matchmaking bounds public queue resources independently of private rooms.
type Matchmaking struct {
	LobbyEnabled  *bool `json:"lobbyEnabled,omitempty"`
	QueueCapacity int   `json:"queueCapacity,omitempty"`
	ProofBits     int   `json:"proofBits,omitempty"`
	IdleEnabled   *bool `json:"idleEnabled,omitempty"`
	IdleSeconds   int   `json:"idleSeconds,omitempty"`
}

func (m Matchmaking) IdleWindow() int {
	if m.IdleEnabled != nil && !*m.IdleEnabled {
		return 0
	}
	if m.IdleSeconds == 0 {
		return 60
	}
	return m.IdleSeconds
}

func (m Matchmaking) LobbyOpen() bool { return m.LobbyEnabled == nil || *m.LobbyEnabled }
func (m Matchmaking) Capacity() int {
	if m.QueueCapacity == 0 {
		return 1000
	}
	return m.QueueCapacity
}
func (m Matchmaking) Difficulty() int {
	if m.ProofBits == 0 {
		return 16
	}
	return m.ProofBits
}
func (m Matchmaking) Validate() error {
	if m.IdleSeconds != 0 && (m.IdleSeconds < 15 || m.IdleSeconds > 300) {
		return fmt.Errorf("matchmaking.idleSeconds must be 0 (default) or 15..300")
	}
	if m.QueueCapacity < 0 || m.QueueCapacity > 10000 {
		return fmt.Errorf("matchmaking.queueCapacity must be 0 (default)..10000")
	}
	if m.ProofBits != 0 && (m.ProofBits < 12 || m.ProofBits > 20) {
		return fmt.Errorf("matchmaking.proofBits must be 0 (default) or 12..20")
	}
	return nil
}
