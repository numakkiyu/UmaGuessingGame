package room

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

const (
	ReceiptSweepBatchSize = 256
	ReceiptSweepInterval  = time.Minute
	receiptSweepBudget    = 2 * time.Second
	// Separate the timestamp tie from later timestamps: SQLite's implicit rowid
	// index suffix is seekable with equality on expires_at. A single tuple >
	// comparison only seeks expires_at and re-scans earlier ties on every batch.
	receiptsAtExpirySQL = `SELECT rowid,expires_at,route_scope FROM idempotency_receipts
		WHERE expires_at=? AND rowid>? AND expires_at<=? ORDER BY rowid LIMIT ?`
	receiptsAfterExpirySQL = `SELECT rowid,expires_at,route_scope FROM idempotency_receipts
		WHERE expires_at>? AND expires_at<=? ORDER BY expires_at,rowid LIMIT ?`
)

type receiptSweepState struct {
	mu      sync.Mutex
	expires int64
	rowID   int64
}

// ReceiptSweepResult counts candidates, including records retained for replay safety.
type ReceiptSweepResult struct {
	Examined int
	Deleted  int
}

// RunReceiptSweeper performs one bounded batch at startup and per interval.
// The caller owns logging; cancellation stops the loop without reporting a failure.
func (s *Service) RunReceiptSweeper(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(ReceiptSweepInterval)
	defer ticker.Stop()
	for {
		if _, err := s.SweepReceipts(ctx); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// SweepReceipts reclaims expired HTTP receipts and expired gameplay receipts for
// terminal rounds. Active gameplay receipts are still replay guards after expiry.
// An index cursor bounds examined rows as well as writes, so retained rows cannot
// starve later candidates. The cursor advances only after a successful commit.
func (s *Service) SweepReceipts(ctx context.Context) (ReceiptSweepResult, error) {
	s.receiptSweep.mu.Lock()
	defer s.receiptSweep.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, receiptSweepBudget)
	defer cancel()
	var result ReceiptSweepResult
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return result, fmt.Errorf("room: begin receipt cleanup: %w", err)
	}
	defer tx.Rollback()
	type candidate struct {
		id, expires int64
		scope       string
	}
	var candidates []candidate
	expires := s.receiptSweep.expires
	if s.receiptSweep.rowID == 0 {
		expires = -1 << 63
	}
	now := s.nowMS()
	read := func(query string, args ...any) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("room: query receipt cleanup: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c candidate
			if err := rows.Scan(&c.id, &c.expires, &c.scope); err != nil {
				return fmt.Errorf("room: scan receipt cleanup: %w", err)
			}
			candidates = append(candidates, c)
		}
		return rows.Err()
	}
	if err := read(receiptsAtExpirySQL, expires, s.receiptSweep.rowID, now, ReceiptSweepBatchSize); err != nil {
		return result, err
	}
	if len(candidates) < ReceiptSweepBatchSize {
		if err := read(receiptsAfterExpirySQL, expires, now, ReceiptSweepBatchSize-len(candidates)); err != nil {
			return result, err
		}
	}
	terminal := make(map[string]bool)
	for _, c := range candidates {
		if strings.HasPrefix(c.scope, "gameplay:") {
			roundID := receiptRoundID(c.scope)
			if roundID == "" {
				continue // Unknown namespaces must explicitly establish replay safety.
			}
			closed, known := terminal[roundID]
			if !known {
				var status string
				err := tx.QueryRowContext(ctx, `SELECT status FROM rounds WHERE id=?`, roundID).Scan(&status)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return ReceiptSweepResult{}, fmt.Errorf("room: check receipt round: %w", err)
				}
				closed = status == string(modes.PhaseSettled) || status == string(modes.PhaseCancelled)
				terminal[roundID] = closed
			}
			if !closed {
				continue
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM idempotency_receipts WHERE rowid=?`, c.id); err != nil {
			return ReceiptSweepResult{}, fmt.Errorf("room: delete expired receipt: %w", err)
		}
		result.Deleted++
	}
	if err := tx.Commit(); err != nil {
		return ReceiptSweepResult{}, fmt.Errorf("room: commit receipt cleanup: %w", err)
	}
	result.Examined = len(candidates)
	if len(candidates) == ReceiptSweepBatchSize {
		last := candidates[len(candidates)-1]
		s.receiptSweep.expires, s.receiptSweep.rowID = last.expires, last.id
	} else {
		s.receiptSweep.expires, s.receiptSweep.rowID = 0, 0
	}
	return result, nil
}

// Only these domain commands currently check a terminal round before mutation.
// Do not infer safety from an arbitrary gameplay prefix or from session expiry.
func receiptRoundID(scope string) string {
	scope = strings.TrimPrefix(scope, "gameplay:")
	operation, resource, ok := strings.Cut(scope, ":")
	if !ok || resource == "" {
		return ""
	}
	switch operation {
	case "createSubmission", "createTrade":
		if !strings.Contains(resource, ":") {
			return resource
		}
	case "respondToTrade":
		roundID, tradeID, ok := strings.Cut(resource, ":")
		if ok && roundID != "" && tradeID != "" && !strings.Contains(tradeID, ":") {
			return roundID
		}
	}
	return ""
}
