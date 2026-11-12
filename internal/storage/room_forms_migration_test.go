package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"regexp"
	"testing"
)

func TestRoomFormsMigrationPreservesMembersAndCreationIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.sqlite")
	db, err := sql.Open("sqlite", path)
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
	apply := func(before bool) {
		t.Helper()
		for _, file := range files {
			if (file.Name() < "0009") != before {
				continue
			}
			raw, err := migrationsFS.ReadFile("migrations/" + file.Name())
			if err != nil {
				t.Fatal(err)
			}
			exec(string(raw))
		}
	}
	apply(true)
	for i := 0; i < 401; i++ {
		id := fmt.Sprint(i)
		exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,'同名',?,'player',0,9999999999999)`, id, "hash"+id, "csrf"+id)
	}
	for _, id := range []string{"single", "multi", "event"} {
		seats := 1
		if id == "multi" {
			seats = 8
		}
		exec(`INSERT INTO rooms(id,owner_session_id,name,status,config_json,created_at,updated_at,creation_key,creation_hash) VALUES(?,'0',?,'lobby',?,0,0,?,?)`, id, id, fmt.Sprintf(`{"kind":"solo","maxPlayers":%d,"maxSpectators":0,"allowSpectators":false}`, seats), "key-"+id, "hash-"+id)
	}
	exec(`INSERT INTO event_sessions(id,room_id,host_session_id,name,status,score_budget,event_version,created_at,updated_at,creation_key,creation_hash) VALUES('ev','event','0','节目','draft',10000,1,0,0,'event-key','hash')`)
	for i := 0; i < 400; i++ {
		exec(`INSERT INTO memberships(id,room_id,session_id,role,ready,joined_at) VALUES(?,'multi',?,'player',0,?)`, "member"+fmt.Sprint(i), fmt.Sprint(i), i)
	}
	apply(false)
	rows, err := db.Query(`SELECT id,public_code,entry_number FROM memberships WHERE room_id='multi' ORDER BY entry_number`)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	stable := map[string]string{}
	count := 0
	for rows.Next() {
		var id, code string
		var number int
		if err := rows.Scan(&id, &code, &number); err != nil {
			t.Fatal(err)
		}
		count++
		if number != count || codes[code] || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
			t.Fatal("invalid/mutated migrated labels")
		}
		codes[code] = true
		stable[id] = code
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if count != 400 {
		t.Fatal("migration dropped memberships")
	}
	exec(`INSERT INTO memberships(id,room_id,session_id,role,ready,joined_at) VALUES('new','multi','400','player',0,401)`)
	var code string
	if err := db.QueryRow(`SELECT public_code FROM memberships WHERE id='new'`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if codes[code] || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		t.Fatal("new label invalid or reused")
	}
	if _, err := db.Exec(`UPDATE memberships SET public_code=? WHERE id='new'`, stable["member0"]); err == nil {
		t.Fatal("database accepted a duplicate room label")
	}
	for id, want := range map[string]string{"single": "solo", "multi": "friend", "event": "event"} {
		var kind string
		if err := db.QueryRow(`SELECT json_extract(config_json,'$.kind') FROM rooms WHERE id=?`, id).Scan(&kind); err != nil || kind != want {
			t.Fatalf("form %s=%s err=%v", id, kind, err)
		}
	}
	exec(`UPDATE rooms SET owner_session_id='400' WHERE id='event'`)
	exec(`UPDATE event_sessions SET host_session_id='400' WHERE id='ev'`)
	for _, table := range []string{"rooms", "event_sessions"} {
		var original string
		if err := db.QueryRow("SELECT creator_session_id FROM "+table+" WHERE id=?", map[string]string{"rooms": "event", "event_sessions": "ev"}[table]).Scan(&original); err != nil || original != "0" {
			t.Fatalf("creation owner followed host: %s %v", original, err)
		}
	}
	// The new host may have used the same request key in another room: changing
	// current ownership must never collide with that person's creation commands.
	exec(`INSERT INTO rooms(id,owner_session_id,name,status,config_json,created_at,updated_at,creation_key,creation_hash) VALUES('other','400','other','lobby','{}',0,0,'key-event','other')`)
	exec(`INSERT INTO event_sessions(id,room_id,host_session_id,name,status,score_budget,event_version,created_at,updated_at,creation_key,creation_hash) VALUES('other-event','other','400','other','draft',10000,1,0,0,'event-key','other')`)
	exec(`UPDATE memberships SET left_at=10 WHERE id='member0'`)
	exec(`UPDATE memberships SET left_at=NULL WHERE id='member0'`)
	var restored string
	_ = db.QueryRow(`SELECT public_code FROM memberships WHERE id='member0'`).Scan(&restored)
	if restored != stable["member0"] {
		t.Fatal("re-entry changed label")
	}
}
