-- Timer scans should visit active deadlines instead of historical rounds.
CREATE INDEX IF NOT EXISTS idx_rounds_phase_deadline
  ON rounds(status, paused_at, deadline_at)
  WHERE deadline_at IS NOT NULL;

-- Matching cleanup and terminal reconciliation resolve tickets by their room.
CREATE INDEX IF NOT EXISTS idx_matchmaking_room_status
  ON matchmaking_tickets(room_id, status)
  WHERE room_id IS NOT NULL;
