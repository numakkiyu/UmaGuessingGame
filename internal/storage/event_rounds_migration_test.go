package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestEventRoundsMigrationPreservesClosedAndCorrectedHistory(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"0001_init.sql", "0002_matchmaking_pairing.sql", "0003_verified_challenges.sql", "0004_matchmaking_rules.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO sessions (id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES ('s','h','host','c','player',0,9999999999999);`); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		id, status string
		corrected  bool
	}{{"active", "running", false}, {"closed", "completed", false}, {"cancelled", "cancelled", false}, {"corrected", "running", true}} {
		id := item.id
		statements := []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO rooms(id,owner_session_id,name,status,config_json,capacity,created_at,updated_at) VALUES (?,'s','room','lobby','{}',20,0,0)`, []any{"room-" + id}},
			{`INSERT INTO event_sessions(id,room_id,host_session_id,name,status,created_at,updated_at) VALUES (?,?,'s','event',?,0,0)`, []any{"event-" + id, "room-" + id, item.status}},
			{`INSERT INTO rounds(id,room_id,ordinal,mode_id,rules_version,dataset_version,content_pack_version,status,seed,created_at,updated_at) VALUES (?,?,1,'sort','v2','d','c','settled','seed',0,0)`, []any{"round-" + id, "room-" + id}},
			{`INSERT INTO event_segments(id,event_id,ordinal,mode_id,name,status,budget,round_id,config_json) VALUES (?,?,1,'sort','segment','completed',60,?,'{"roundCount":2,"competitive":true}')`, []any{"seg-" + id, "event-" + id, "round-" + id}},
			{`INSERT INTO round_results(round_id,subject_id,score_units,max_score_units,explanation_json,settled_version) VALUES (?,'player',2500,10000,'{}',1)`, []any{"round-" + id}},
			{`INSERT INTO segment_results(segment_id,subject_id,score_units,max_score_units) VALUES (?,'player',2500,10000)`, []any{"seg-" + id}},
		}
		for _, statement := range statements {
			if _, err = db.Exec(statement.sql, statement.args...); err != nil {
				t.Fatal(err)
			}
		}
		if item.corrected {
			if _, err = db.Exec(`INSERT INTO score_corrections(id,event_id,segment_id,subject_id,reason,before_units,after_units,corrected_by,created_at) VALUES ('correction',?,?,'player','manual reason',2500,1234,'s',0)`, "event-"+id, "seg-"+id); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`UPDATE segment_results SET score_units=1234 WHERE segment_id=?`, "seg-"+id); err != nil {
				t.Fatal(err)
			}
		}
	}
	body, err := migrationsFS.ReadFile("migrations/0005_event_segment_rounds.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"active", "closed", "cancelled", "corrected"} {
		var phase, round, roster string
		var ordinal, score, max, raw int
		if err = db.QueryRow(`SELECT es.status,sr.round_id,sr.ordinal,sr.roster_json,result.score_units,result.max_score_units,rr.score_units FROM event_segments es JOIN event_segment_rounds sr ON sr.segment_id=es.id JOIN segment_results result ON result.segment_id=es.id JOIN round_results rr ON rr.round_id=sr.round_id WHERE es.id=?`, "seg-"+id).Scan(&phase, &round, &ordinal, &roster, &score, &max, &raw); err != nil {
			t.Fatal(err)
		}
		wantPhase, wantScore, wantMax := "completed", 2500, 10000
		if id == "active" {
			wantPhase, wantScore, wantMax = "running", 750, 6000
		}
		if id == "corrected" {
			wantScore = 1234
		}
		if phase != wantPhase || score != wantScore || max != wantMax || round != "round-"+id || ordinal != 1 || roster != "[]" || raw != 2500 {
			t.Fatalf("%s migration: phase=%s score=%d/%d round=%s ordinal=%d roster=%s raw=%d", id, phase, score, max, round, ordinal, roster, raw)
		}
	}
}
