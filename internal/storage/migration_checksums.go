package storage

// Comment-only revisions accept exactly the recorded previous and current digests.
// SQL changes and unknown checksums still fail migration verification.
var migrationCommentRevisions = map[string]struct{ previous, current string }{
	"0001_init.sql":                {previous: "6724507664926e2eeb9e80d27e28d85c46d19ff5b1e373699938baa1c163b68a", current: "3e4adabb076d05b454451e10dbbcfec5e26172216c0fd0a6574f773fb29dd79f"},
	"0002_matchmaking_pairing.sql": {previous: "da861d5e0b952becf01674200c2e2f08701a8bf0a9596496bc0e91ac2bdb8a77", current: "a02b9dc3bdb78c59520a2dbca2e194ab24feb04668f487690140243687c95079"},
}

func acceptsMigrationChecksum(name, applied, current string) bool {
	if applied == current {
		return true
	}
	revision, ok := migrationCommentRevisions[name]
	return ok && applied == revision.previous && current == revision.current
}
