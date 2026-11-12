package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestResultKindMigrationKeepsScoresAndRoomBoundaries(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	files, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Name() >= "0008_result_subject_kind.sql" {
			continue
		}
		raw, err := migrationsFS.ReadFile("migrations/" + file.Name())
		if err != nil {
			t.Fatal(err)
		}
		exec(string(raw))
	}
	exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES('s','h','host','c','player',0,9999999999999)`)
	for _, id := range []string{"one", "two"} {
		exec(`INSERT INTO rooms(id,owner_session_id,name,status,config_json,created_at,updated_at) VALUES(?,'s','room','finished','{}',0,0)`, id)
		exec(`INSERT INTO rounds(id,room_id,ordinal,mode_id,rules_version,dataset_version,content_pack_version,status,seed,created_at,updated_at) VALUES(?,?,1,'sort','v2','d','c','settled','seed',0,0)`, "r-"+id, id)
	}
	exec(`INSERT INTO teams(id,room_id,label,created_at) VALUES('team','one','胡萝卜队',0)`)
	for _, subject := range []string{"team", "person"} {
		for _, round := range []string{"r-one", "r-two"} {
			exec(`INSERT INTO round_results(round_id,subject_id,score_units,max_score_units,explanation_json,settled_version) VALUES(?,?,1234,10000,'{"corrected":true}',7)`, round, subject)
		}
	}
	raw, err := migrationsFS.ReadFile("migrations/0008_result_subject_kind.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(string(raw))
	for _, subject := range []string{"team", "person"} {
		for _, round := range []string{"r-one", "r-two"} {
			var kind, explanation string
			var score, max, version int
			err := db.QueryRow(`SELECT subject_kind,score_units,max_score_units,explanation_json,settled_version FROM round_results WHERE round_id=? AND subject_id=?`, round, subject).Scan(&kind, &score, &max, &explanation, &version)
			if err != nil {
				t.Fatal(err)
			}
			want := "player"
			if subject == "team" && round == "r-one" {
				want = "team"
			}
			if kind != want || score != 1234 || max != 10000 || explanation != `{"corrected":true}` || version != 7 {
				t.Fatalf("%s/%s changed historical value: %s %d/%d %s v%d", round, subject, kind, score, max, explanation, version)
			}
		}
	}
}
