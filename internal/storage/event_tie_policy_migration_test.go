package storage

import (
	"database/sql"
	"testing"
)

func TestEventTiePolicyMigrationRecoversPublishedArrangement(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Minimal historical shape isolates the new migration; all current migrations
	// and foreign keys are separately exercised by storage.Open tests.
	_, err = db.Exec(`CREATE TABLE event_sessions(id TEXT PRIMARY KEY);
 CREATE TABLE audit_events(id TEXT,event_id TEXT,action TEXT,detail_json TEXT,created_at INTEGER);
 INSERT INTO event_sessions(id) VALUES ('shared'),('announced'),('missing'),('broken'),('unknown');
 INSERT INTO audit_events VALUES
 ('a','shared','event.created','{"tiePolicy":"shared-rank"}',1),
 ('b','announced','event.created','{"tiePolicy":"announced-tiebreak"}',1),
 ('c','broken','event.created','broken-json',1),
 ('d','unknown','event.created','{"tiePolicy":"new-unknown"}',1),
 ('e','shared','event.updated','{"tiePolicy":"announced-tiebreak"}',2);`)
	if err != nil {
		t.Fatal(err)
	}
	body, err := migrationsFS.ReadFile("migrations/0013_event_tie_policy.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"shared", "announced", "missing", "broken", "unknown"} {
		var value string
		if err = db.QueryRow(`SELECT tie_policy FROM event_sessions WHERE id=?`, id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		want := "shared-rank"
		if id == "announced" {
			want = "announced-tiebreak"
		}
		if value != want {
			t.Fatalf("%s policy=%s want %s", id, value, want)
		}
	}
	if _, err = db.Exec(`UPDATE event_sessions SET tie_policy='arbitrary-rank' WHERE id='shared'`); err == nil {
		t.Fatal("invalid tie policy was accepted")
	}
}
