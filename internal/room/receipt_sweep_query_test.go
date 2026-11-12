package room

import (
	"context"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/storage"
)

// A row-count LIMIT alone does not bound the scan when many receipts expire at
// once. Verify that the runtime SQLite can seek past the cursor inside a tie,
// rather than scanning the entire timestamp group again on each batch.
func TestReceiptSweepQueriesSeekExpiryAndRowID(t *testing.T) {
	db, err := storage.Open(context.Background(), storage.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct {
		query string
		args  []any
		want  string
	}{
		{receiptsAtExpirySQL, []any{100, 10000, 200, ReceiptSweepBatchSize}, "expires_at=? AND rowid>?"},
		{receiptsAfterExpirySQL, []any{100, 200, ReceiptSweepBatchSize}, "expires_at>? AND expires_at<?"},
	} {
		rows, err := db.Read().QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+tc.query, tc.args...)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		plan := strings.Join(details, "\n")
		t.Log(plan)
		if !strings.Contains(plan, "idx_idem_expires ("+tc.want+")") || strings.Contains(plan, "SCAN") || strings.Contains(plan, "TEMP B-TREE") {
			t.Fatalf("receipt cleanup lost bounded index seek: %s", plan)
		}
	}
}
