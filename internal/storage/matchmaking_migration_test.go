package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMatchmakingRulesMigrationPreservesOrdinaryRooms(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"0001_init.sql", "0002_matchmaking_pairing.sql", "0003_verified_challenges.sql"} {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`
INSERT INTO sessions (id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at)
VALUES ('s','h','name','c','player',0,9999999999999);
INSERT INTO rooms (id,owner_session_id,name,status,room_version,config_json,capacity,created_at,updated_at)
VALUES ('matched','s','match','lobby',1,'{"modeId":"classic","config":{}}',8,0,0),
       ('ordinary','s','friend','lobby',1,'{"kind":"friend","modeConfig":{"modeId":"classic"},"maxPlayers":8}',8,0,0),
       ('unrelated','s','other','lobby',1,'{"modeId":"classic","config":{}}',8,0,0);
INSERT INTO matchmaking_tickets (id,session_id,mode_id,status,room_id,created_at,expires_at,updated_at)
VALUES ('t','s','classic','ready','matched',0,9999999999999,0);`); err != nil {
		t.Fatal(err)
	}
	body, err := migrationsFS.ReadFile("migrations/0004_matchmaking_rules.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	var kind, mode string
	var capacity int
	if err := db.QueryRow(`SELECT json_extract(config_json,'$.kind'), json_extract(config_json,'$.modeConfig.modeId'),capacity FROM rooms WHERE id='matched'`).Scan(&kind, &mode, &capacity); err != nil {
		t.Fatal(err)
	}
	if kind != "matchmaking" || mode != "classic" || capacity != 2 {
		t.Fatalf("repair: %s %s %d", kind, mode, capacity)
	}
	for id, want := range map[string]string{
		"ordinary":  `{"kind":"friend","modeConfig":{"modeId":"classic"},"maxPlayers":8}`,
		"unrelated": `{"modeId":"classic","config":{}}`,
	} {
		var got string
		if err := db.QueryRow(`SELECT config_json FROM rooms WHERE id=?`, id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%s changed: %s", id, got)
		}
	}
}
