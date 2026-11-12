package audio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// clipSpec is the on-the-wire spelling of one candidate clip.
type clipSpec struct {
	ClipID          string `json:"clipId"`
	CharacterID     string `json:"characterId"`
	AudioPath       string `json:"audioPath"`
	DurationMillis  int    `json:"durationMillis"`
	Transcript      string `json:"transcript"`
	SpeakerVerified bool   `json:"speakerVerified"`
	Reviewed        bool   `json:"reviewed"`
	Decodable       bool   `json:"decodable"`
}

// verifiedPack builds a pack carrying n fully verified clips.
func verifiedPack(n int) modes.ContentSource {
	clips := make([]clipSpec, 0, n)
	for i := 0; i < n; i++ {
		clips = append(clips, clipSpec{
			ClipID:          fmt.Sprintf("clip-%03d", i+1),
			CharacterID:     fmt.Sprintf("c%03d", i+1),
			AudioPath:       fmt.Sprintf("/assets/voices/clip-%03d.ogg", i+1),
			DurationMillis:  2400 + i*100,
			Transcript:      fmt.Sprintf("第%d段台词", i+1),
			SpeakerVerified: true,
			Reviewed:        true,
			Decodable:       true,
		})
	}
	raw, err := json.Marshal(clips)
	if err != nil {
		panic(err)
	}
	return fakeContent{ClipsSection: raw}
}

// remoteOnlyPack builds a pack whose every row carries a remote URL and no local file,
// which is the shape the real snapshot has.
func remoteOnlyPack(n int) modes.ContentSource {
	clips := make([]struct {
		ClipID      string `json:"clipId"`
		CharacterID string `json:"characterId"`
		AudioURL    string `json:"audioUrl"`
		Chs         string `json:"chs"`
	}, 0, n)
	for i := 0; i < n; i++ {
		clips = append(clips, struct {
			ClipID      string `json:"clipId"`
			CharacterID string `json:"characterId"`
			AudioURL    string `json:"audioUrl"`
			Chs         string `json:"chs"`
		}{
			ClipID:      fmt.Sprintf("clip-%05d", i+1),
			CharacterID: fmt.Sprintf("c%03d", i%133+1),
			AudioURL:    "https://example.invalid/voice.ogg",
			Chs:         "一段台词",
		})
	}
	raw, err := json.Marshal(clips)
	if err != nil {
		panic(err)
	}
	return fakeContent{ClipsSection: raw}
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-test",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// roster builds n player subjects with stable ids.
func roster(n int) []modes.Subject {
	out := make([]modes.Subject, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, modes.Subject{
			ID:   fmt.Sprintf("p%d", i+1),
			Kind: modes.SubjectPlayer,
		})
	}
	return out
}

// prepareRound runs Prepare and returns the prepared round with decoded halves.
func prepareRound(t *testing.T, pack modes.ContentSource, seed, cfg string, players int) (modes.Prepared, publicState, privateState) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: roster(players),
		Config:  json.RawMessage(cfg),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return prep, pub, priv
}

// matchesAction builds a contract-shaped audio.matches payload.
func matchesAction(pairs ...string) modes.ModeAction {
	type item struct {
		ClipID      string `json:"clipId"`
		CharacterID string `json:"characterId"`
	}
	items := make([]item, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, item{ClipID: pairs[i], CharacterID: pairs[i+1]})
	}
	body, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		Matches []item `json:"matches"`
	}{Kind: ActionKindMatches, Matches: items})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindMatches, Payload: body}
}

// correctPairs renders the solution as flat clip/character arguments.
func correctPairs(pub publicState, priv privateState) []string {
	out := make([]string, 0, len(pub.Clips)*2)
	for _, c := range pub.Clips {
		out = append(out, c.ClipID, priv.SpeakerOf[c.ClipID])
	}
	return out
}

// apply runs Apply against the current state.
func apply(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) (modes.Decision, json.RawMessage) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   state,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return dec, dec.State
}

// project decodes the projection for a viewer.
func project(t *testing.T, prep modes.Prepared, viewer modes.Viewer, phase modes.Phase, states map[string]json.RawMessage) projection {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	raw, err := New().Project(modes.ProjectInput{
		Scope:   scope("seed"),
		Viewer:  viewer,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   phase,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var out projection
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	return out
}

// settle runs Settle.
func settle(t *testing.T, prep modes.Prepared, states map[string]json.RawMessage) map[string]modes.Score {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	out := make(map[string]modes.Score, len(scores))
	for _, score := range scores {
		if err := score.Validate(); err != nil {
			t.Fatalf("invalid score for %s: %v", score.Subject.ID, err)
		}
		out[score.Subject.ID] = score
	}
	return out
}

// ---------------------------------------------------------------------------
// Part 1: the admission gate
// ---------------------------------------------------------------------------

// TestPrepareRefusesRemoteOnlyClips verifies that remote-only clips cannot start a round and the admission result explains why.
func TestPrepareRefusesRemoteOnlyClips(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("gate-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: remoteOnlyPack(15222),
	})
	if err == nil {
		t.Fatal("Prepare admitted a round built on remote-URL-only clips")
	}
	var admission *AdmissionError
	if !errors.As(err, &admission) {
		t.Fatalf("the refusal is not an AdmissionError: %v", err)
	}
	if admission.Total != 15222 {
		t.Fatalf("admission reported %d candidates, want 15222", admission.Total)
	}
	if admission.NoLocalFile != 15222 {
		t.Fatalf("admission reported %d rows without a local file, want 15222",
			admission.NoLocalFile)
	}
	if admission.ReasonCode() != "no_usable_clips" {
		t.Fatalf("reason code = %q, want no_usable_clips", admission.ReasonCode())
	}
	msg := err.Error()
	if !strings.Contains(msg, "no usable clips") {
		t.Fatalf("the refusal does not say what is missing: %q", msg)
	}
	if !strings.Contains(msg, "decodable local file") {
		t.Fatalf("the refusal does not say what is required: %q", msg)
	}
	if !strings.Contains(msg, "listening review") {
		t.Fatalf("the refusal does not name the review requirement: %q", msg)
	}
}

// TestPrepareRefusesMissingSection asserts an absent section is a different report.
func TestPrepareRefusesMissingSection(t *testing.T) {
	cases := []struct {
		name string
		pack modes.ContentSource
		want string
	}{
		{name: "nil content", pack: nil, want: "no content source"},
		{name: "empty pack", pack: fakeContent{}, want: "no clips are available"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("missing-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare admitted a round with no clips")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestPrepareRefusesUnverifiedOrUnreviewedClips asserts every gate field is load-bearing.
func TestPrepareRefusesUnverifiedOrUnreviewedClips(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*clipSpec)
		wantMsg string
		wantCnt func(*AdmissionError) int
	}{
		{
			name:    "not decodable",
			mutate:  func(c *clipSpec) { c.Decodable = false },
			wantMsg: "decodable local file",
			wantCnt: func(a *AdmissionError) int { return a.NotDecodable },
		},
		{
			name:    "speaker not verified",
			mutate:  func(c *clipSpec) { c.SpeakerVerified = false },
			wantMsg: "unverified speakers",
			wantCnt: func(a *AdmissionError) int { return a.Unverified },
		},
		{
			name:    "not reviewed",
			mutate:  func(c *clipSpec) { c.Reviewed = false },
			wantMsg: "unreviewed",
			wantCnt: func(a *AdmissionError) int { return a.Unreviewed },
		},
		{
			name:    "no local path",
			mutate:  func(c *clipSpec) { c.AudioPath = "" },
			wantMsg: "without a local decodable file",
			wantCnt: func(a *AdmissionError) int { return a.NoLocalFile },
		},
		{
			name:    "remote url only",
			mutate:  func(c *clipSpec) { c.AudioPath = "https://example.invalid/voice.ogg" },
			wantMsg: "without a local decodable file",
			wantCnt: func(a *AdmissionError) int { return a.NoLocalFile },
		},
		{
			name:    "zero duration",
			mutate:  func(c *clipSpec) { c.DurationMillis = 0 },
			wantMsg: "decodable local file",
			wantCnt: func(a *AdmissionError) int { return a.NotDecodable },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clips := make([]clipSpec, 0, 2)
			for i := 0; i < 2; i++ {
				c := clipSpec{
					ClipID:          fmt.Sprintf("clip-%03d", i+1),
					CharacterID:     fmt.Sprintf("c%03d", i+1),
					AudioPath:       fmt.Sprintf("/assets/voices/clip-%03d.ogg", i+1),
					DurationMillis:  2400,
					Transcript:      "台词",
					SpeakerVerified: true,
					Reviewed:        true,
					Decodable:       true,
				}
				tc.mutate(&c)
				clips = append(clips, c)
			}
			raw, err := json.Marshal(clips)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			_, err = New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("gate-field-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: fakeContent{ClipsSection: raw},
			})
			if err == nil {
				t.Fatalf("Prepare admitted clips with %s", tc.name)
			}
			var admission *AdmissionError
			if !errors.As(err, &admission) {
				t.Fatalf("the refusal is not an AdmissionError: %v", err)
			}
			if got := tc.wantCnt(admission); got != 2 {
				t.Fatalf("admission counted %d rejected rows for %s, want 2", got, tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("error %q does not mention %q", err, tc.wantMsg)
			}
		})
	}
}

// TestSingleUsableClipStillRefused asserts MinClips is enforced, so a round cannot start
// with fewer clips than the documented minimum.
func TestSingleUsableClipStillRefused(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("one-clip-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: verifiedPack(1),
	})
	if err == nil {
		t.Fatal("Prepare admitted a round with one usable clip")
	}
	var admission *AdmissionError
	if !errors.As(err, &admission) {
		t.Fatalf("the refusal is not an AdmissionError: %v", err)
	}
	if admission.Total != 1 {
		t.Fatalf("admission reported %d candidates, want 1", admission.Total)
	}
}

// TestUnusableClipsNeverReachPublicState asserts the gate is structural: a pack mixing
// verified and unverified clips poses only the verified ones.
func TestUnusableClipsNeverReachPublicState(t *testing.T) {
	clips := []clipSpec{
		{
			ClipID: "clip-good-1", CharacterID: "c001",
			AudioPath: "/assets/voices/good1.ogg", DurationMillis: 2000,
			Transcript: "已审校台词一", SpeakerVerified: true, Reviewed: true, Decodable: true,
		},
		{
			ClipID: "clip-good-2", CharacterID: "c002",
			AudioPath: "/assets/voices/good2.ogg", DurationMillis: 2100,
			Transcript: "已审校台词二", SpeakerVerified: true, Reviewed: true, Decodable: true,
		},
		{
			ClipID: "clip-bad", CharacterID: "c003",
			AudioPath: "/assets/voices/bad.ogg", DurationMillis: 2200,
			Transcript: "未审校台词", SpeakerVerified: true, Reviewed: false, Decodable: true,
		},
	}
	raw, err := json.Marshal(clips)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The round asks for two clips, because only two are admissible; asking for four
	// would refuse on the distinct-speaker count instead of exercising the gate.
	prep, pub, priv := prepareRound(t, fakeContent{ClipsSection: raw}, "mixed-seed", `{"clips":2}`, 2)

	if len(pub.Clips) != 2 {
		t.Fatalf("board has %d clips, want the 2 verified ones", len(pub.Clips))
	}
	for _, c := range pub.Clips {
		if priv.SpeakerOf[c.ClipID] == "c003" {
			t.Fatal("the unreviewed clip was posed")
		}
	}
	if strings.Contains(string(prep.Public), "未审校台词") {
		t.Fatal("the unreviewed transcript reached the public state")
	}
	if strings.Contains(string(prep.Public), "bad.ogg") {
		t.Fatal("the unreviewed clip's audio path reached the public state")
	}
}

// ---------------------------------------------------------------------------
// Part 2: the engine, against a verified fixture
// ---------------------------------------------------------------------------

// TestTranscriptAndSpeakerWithheldUntilReveal covers the 「说话者与文本核验」 requirement:
// neither the transcript nor the speaker may be published before the reveal.
func TestTranscriptAndSpeakerWithheldUntilReveal(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(6), "withhold-seed", `{}`, 2)

	player := modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}
	view := project(t, prep, player, modes.PhaseCollecting, nil)

	if view.Answer != nil {
		t.Fatal("the answer is visible while the round is collecting")
	}
	raw := string(mustJSON(t, view))
	for _, c := range pub.Clips {
		if transcript := priv.Transcripts[c.ClipID]; transcript != "" {
			if strings.Contains(raw, transcript) {
				t.Fatalf("the projection carries the transcript %q", transcript)
			}
		}
		if speaker := priv.SpeakerOf[c.ClipID]; speaker != "" {
			// The candidate list legitimately names every candidate, so the check is on
			// the clip object rather than the whole payload.
			_ = speaker
		}
	}
	// No clip object may carry a transcript or a speaker field.
	for _, c := range view.Clips {
		clipJSON := string(mustJSON(t, c))
		for _, forbidden := range []string{"transcript", "characterId", "speaker"} {
			if strings.Contains(clipJSON, forbidden) {
				t.Fatalf("the published clip carries %q: %s", forbidden, clipJSON)
			}
		}
	}

	// The reveal publishes both.
	settled := project(t, prep, player, modes.PhaseSettled, nil)
	if settled.Answer == nil {
		t.Fatal("the settled projection carries no reveal")
	}
	if len(settled.Answer.Clips) != len(pub.Clips) {
		t.Fatalf("the reveal lists %d clips, want %d", len(settled.Answer.Clips), len(pub.Clips))
	}
	for _, rc := range settled.Answer.Clips {
		if rc.CharacterID == "" {
			t.Fatalf("revealed clip %q has no speaker", rc.ClipID)
		}
		if rc.Transcript == "" {
			t.Fatalf("revealed clip %q has no transcript", rc.ClipID)
		}
		if rc.CharacterID != priv.SpeakerOf[rc.ClipID] {
			t.Fatalf("revealed speaker for %q = %q, answer says %q",
				rc.ClipID, rc.CharacterID, priv.SpeakerOf[rc.ClipID])
		}
	}
}

// TestClipIDsCarryNoSpeaker covers the 「非可推导答案 URL」 requirement.
//
// The board id must not encode the speaker, and the audio route is published openly
// rather than pretended to be secret — the plan says hiding it is not a guarantee.
func TestClipIDsCarryNoSpeaker(t *testing.T) {
	_, pub, priv := prepareRound(t, verifiedPack(6), "id-seed", `{}`, 2)

	ids := make(map[string]struct{}, len(pub.Clips))
	for _, c := range pub.Clips {
		if _, dup := ids[c.ClipID]; dup {
			t.Fatalf("clip id %q appears twice", c.ClipID)
		}
		ids[c.ClipID] = struct{}{}
		speaker := priv.SpeakerOf[c.ClipID]
		if speaker == "" {
			t.Fatalf("clip %q has no speaker in the answer", c.ClipID)
		}
		// The id must not contain the speaker id, nor the source clip id.
		if strings.Contains(c.ClipID, speaker) {
			t.Fatalf("clip id %q contains its speaker %q", c.ClipID, speaker)
		}
		if strings.Contains(c.ClipID, "clip-0") {
			t.Fatalf("clip id %q is the source id, which is derived from the character",
				c.ClipID)
		}
		// The audio route IS published: a browser must fetch it.
		if c.AudioPath == "" {
			t.Fatalf("clip %q publishes no audio route", c.ClipID)
		}
	}
	// The audio path must not name the speaker either.
	for _, c := range pub.Clips {
		speaker := priv.SpeakerOf[c.ClipID]
		if strings.Contains(c.AudioPath, speaker) {
			t.Fatalf("the audio route %q names the speaker %q", c.AudioPath, speaker)
		}
	}
}

// TestCorrectPairingScoresFullMarks asserts the documented 100/clipCount rate.
func TestCorrectPairingScoresFullMarks(t *testing.T) {
	for _, clips := range []int{2, 4, 6} {
		t.Run(fmt.Sprintf("clips%d", clips), func(t *testing.T) {
			cfg := fmt.Sprintf(`{"clips":%d}`, clips)
			prep, pub, priv := prepareRound(t, verifiedPack(8), "full-seed", cfg, 2)
			if len(pub.Clips) != clips {
				t.Fatalf("board has %d clips, want %d", len(pub.Clips), clips)
			}
			subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
			dec, state := apply(t, prep, subject, matchesAction(correctPairs(pub, priv)...), nil)
			if !dec.Accepted {
				t.Fatalf("the correct pairing was rejected: %q %q", dec.Code, dec.Detail)
			}
			scores := settle(t, prep, map[string]json.RawMessage{"p1": state})
			if got := scores["p1"].ScoreUnits; got != 10000 {
				t.Fatalf("score = %d units, want 10000", got)
			}
		})
	}
}

// TestPartialPairingScoresRatio asserts a partly correct pairing scores the documented
// ratio, floored.
func TestPartialPairingScoresRatio(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(8), "partial-seed", `{"clips":4}`, 2)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	// Correctly pair the first n clips and deliberately mispair the rest, by pairing each
	// remaining clip with a wrong candidate.
	pairs := correctPairs(pub, priv)
	cases := []struct {
		name    string
		correct int
		want    int
	}{
		{name: "none", correct: 0, want: 0},
		{name: "one of four", correct: 1, want: 25},
		{name: "two of four", correct: 2, want: 50},
		{name: "all four", correct: 4, want: 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Rotate the remaining speakers so each nonmatching pair stays distinct.
			submission := []string{}
			for i, c := range pub.Clips {
				target := priv.SpeakerOf[c.ClipID]
				if i >= tc.correct && tc.correct < len(pub.Clips)-1 {
					target = priv.SpeakerOf[pub.Clips[tc.correct+(i-tc.correct+1)%(len(pub.Clips)-tc.correct)].ClipID]
				}
				submission = append(submission, c.ClipID, target)
			}
			dec, state := apply(t, prep, subject, matchesAction(submission...), nil)
			if !dec.Accepted {
				t.Fatalf("submission rejected: %q %q", dec.Code, dec.Detail)
			}
			scores := settle(t, prep, map[string]json.RawMessage{"p1": state})
			if got := scores["p1"].ScoreUnits; got != tc.want*100 {
				t.Fatalf("score = %d units, want %d", got, tc.want*100)
			}
			_ = pairs
		})
	}
}

// A real accepted action persists the failure; it cannot boost an official score.
func TestPlaybackFailureIsNotAWrongAnswer(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(8), "fault", `{"clips":4}`, 2)
	pairs := []map[string]string{}
	for _, c := range pub.Clips[:3] {
		pairs = append(pairs, map[string]string{"clipId": c.ClipID, "characterId": priv.SpeakerOf[c.ClipID]})
	}
	payload := mustJSON(t, map[string]any{"kind": ActionKindMatches, "matches": pairs, "playbackFailures": []string{pub.Clips[3].ClipID}})
	dec, state := apply(t, prep, roster(1)[0], modes.ModeAction{Kind: ActionKindMatches, Payload: payload}, nil)
	if !dec.Accepted {
		t.Fatal(dec)
	}
	var st subjectState
	_ = json.Unmarshal(state, &st)
	if len(st.PlaybackFailures) != 1 {
		t.Fatal("report not persisted")
	}
	if _, err := New().Settle(modes.SettleInput{Public: prep.Public, Private: prep.Private, States: map[string]json.RawMessage{"p1": state}}); err == nil {
		t.Fatal("unresolved fault created official score")
	}
	for _, bad := range [][]string{{"unknown"}, {pub.Clips[3].ClipID, pub.Clips[3].ClipID}, {pub.Clips[0].ClipID}} {
		action := modes.ModeAction{Kind: ActionKindMatches, Payload: mustJSON(t, map[string]any{"kind": ActionKindMatches, "matches": pairs, "playbackFailures": bad})}
		dec, _ := apply(t, prep, roster(1)[0], action, nil)
		if dec.Accepted {
			t.Fatal("invalid failure accepted", bad)
		}
	}
	allFailed := []string{}
	for _, c := range pub.Clips {
		allFailed = append(allFailed, c.ClipID)
	}
	dec, _ = apply(t, prep, roster(1)[0], modes.ModeAction{Kind: ActionKindMatches, Payload: mustJSON(t, map[string]any{"kind": ActionKindMatches, "matches": []any{}, "playbackFailures": allFailed})}, nil)
	if !dec.Accepted {
		t.Fatal("all-media-failed report rejected", dec)
	}
}

// TestPreloadAndPlayBudgetArePublished covers the 「主持统一播放方案」 machine-checkable part.
func TestPreloadAndPlayBudgetArePublished(t *testing.T) {
	_, pub, _ := prepareRound(t, verifiedPack(8), "preload-seed", `{"clips":4}`, 2)
	if !pub.PreloadRequired {
		t.Fatal("the round does not require a successful preload")
	}
	for _, c := range pub.Clips {
		if c.MaxPlays != MaxPlaysPerClip {
			t.Fatalf("clip %q allows %d plays, want %d", c.ClipID, c.MaxPlays, MaxPlaysPerClip)
		}
		if c.DurationMillis <= 0 {
			t.Fatalf("clip %q publishes no duration", c.ClipID)
		}
	}
	// The scoring text must state the normalisation and that failure is not a wrong
	// answer.
	if !strings.Contains(pub.Scoring, "比例") {
		t.Fatalf("the scoring text does not describe the normalisation: %q", pub.Scoring)
	}
	if !strings.Contains(pub.Scoring, "播放失败不算答错") {
		t.Fatalf("the scoring text does not say a failure is not a wrong answer: %q", pub.Scoring)
	}
}

// TestCandidatesCoverEverySpeaker asserts the answer space contains every true speaker,
// so the round is solvable.
func TestCandidatesCoverEverySpeaker(t *testing.T) {
	_, pub, priv := prepareRound(t, verifiedPack(8), "candidates-seed", `{"clips":4}`, 2)
	known := make(map[string]struct{}, len(pub.Candidates))
	for _, c := range pub.Candidates {
		known[c.CharacterID] = struct{}{}
	}
	for _, c := range pub.Clips {
		speaker := priv.SpeakerOf[c.ClipID]
		if _, ok := known[speaker]; !ok {
			t.Fatalf("the speaker %q of clip %q is not a candidate", speaker, c.ClipID)
		}
	}
	if len(pub.Candidates) != len(pub.Clips) {
		t.Fatal("candidate count differs from published rules")
	}
}

// TestDistinctSpeakersPerBoard asserts one clip per speaker, so the pairing is unambiguous.
func TestDistinctSpeakersPerBoard(t *testing.T) {
	_, pub, priv := prepareRound(t, verifiedPack(8), "distinct-seed", `{"clips":4}`, 2)
	speakers := make(map[string]string, len(pub.Clips))
	for _, c := range pub.Clips {
		speaker := priv.SpeakerOf[c.ClipID]
		if prev, dup := speakers[speaker]; dup {
			t.Fatalf("clips %q and %q share the speaker %q", prev, c.ClipID, speaker)
		}
		speakers[speaker] = c.ClipID
	}
}

// TestInvalidSubmissionsRejected covers payload and board validation.
func TestInvalidSubmissionsRejected(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(8), "invalid-seed", `{"clips":4}`, 2)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	pairs := correctPairs(pub, priv)

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "audio.teleport", body: `{}`, want: CodeUnknownAction},
		{name: "empty payload", kind: ActionKindMatches, body: ``, want: CodeInvalidPayload},
		{name: "not json", kind: ActionKindMatches, body: `nope`, want: CodeInvalidPayload},
		{name: "kind mismatch", kind: ActionKindMatches,
			body: `{"kind":"bluff.vote","matches":[{"clipId":"x","characterId":"y"}]}`,
			want: CodeInvalidPayload},
		{name: "unknown clip", kind: ActionKindMatches,
			body: `{"kind":"audio.matches","matches":[{"clipId":"ac-nope","characterId":"c001"}]}`,
			want: CodeUnknownClip},
		{name: "unknown character", kind: ActionKindMatches,
			body: fmt.Sprintf(`{"kind":"audio.matches","matches":[{"clipId":%q,"characterId":"c-nope"}]}`,
				pub.Clips[0].ClipID), want: CodeUnknownCharacter},
		{name: "duplicate clip", kind: ActionKindMatches,
			body: fmt.Sprintf(`{"kind":"audio.matches","matches":[`+
				`{"clipId":%q,"characterId":%q},{"clipId":%q,"characterId":%q}]}`,
				pub.Clips[0].ClipID, pub.Candidates[0].CharacterID,
				pub.Clips[0].ClipID, pub.Candidates[1].CharacterID),
			want: CodeDuplicateClip},
		{name: "duplicate character", kind: ActionKindMatches,
			body: fmt.Sprintf(`{"kind":"audio.matches","matches":[`+
				`{"clipId":%q,"characterId":%q},{"clipId":%q,"characterId":%q}]}`,
				pub.Clips[0].ClipID, pub.Candidates[0].CharacterID,
				pub.Clips[1].ClipID, pub.Candidates[0].CharacterID),
			want: CodeDuplicateCharacter},
		{name: "incomplete", kind: ActionKindMatches,
			body: fmt.Sprintf(`{"kind":"audio.matches","matches":[{"clipId":%q,"characterId":%q}]}`,
				pub.Clips[0].ClipID, pub.Candidates[0].CharacterID), want: CodeIncomplete},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := apply(t, prep, subject,
				modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)}, nil)
			if dec.Accepted {
				t.Fatal("an invalid submission was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}

	// The correct pairing is still accepted.
	dec, _ := apply(t, prep, subject, matchesAction(pairs...), nil)
	if !dec.Accepted {
		t.Fatalf("the correct pairing was rejected: %q %q", dec.Code, dec.Detail)
	}
}

// TestRevisionBeforeDeadline asserts a submission may be revised.
func TestRevisionBeforeDeadline(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(8), "revise-seed", `{"clips":4}`, 2)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	// Submit a wrong pairing first.
	wrong := make([]string, 0, len(pub.Clips)*2)
	candidates := make([]string, 0, len(pub.Candidates))
	for _, c := range pub.Candidates {
		candidates = append(candidates, c.CharacterID)
	}
	for i, c := range pub.Clips {
		wrong = append(wrong, c.ClipID, candidates[i%len(candidates)])
	}
	dec, state := apply(t, prep, subject, matchesAction(wrong...), nil)
	if !dec.Accepted {
		t.Fatalf("first submission rejected: %q %q", dec.Code, dec.Detail)
	}
	first := settle(t, prep, map[string]json.RawMessage{"p1": state})

	// Revise to the correct pairing.
	dec, state = apply(t, prep, subject, matchesAction(correctPairs(pub, priv)...), state)
	if !dec.Accepted {
		t.Fatalf("revision rejected: %q %q", dec.Code, dec.Detail)
	}
	second := settle(t, prep, map[string]json.RawMessage{"p1": state})
	if got := second["p1"].ScoreUnits; got != 10000 {
		t.Fatalf("revised answer scored %d units, want 10000", got)
	}
	if first["p1"].ScoreUnits == 10000 {
		t.Fatal("the deliberately wrong answer scored full marks")
	}

	var st subjectState
	if err := json.Unmarshal(state, &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.Submissions != 2 {
		t.Fatalf("submissions = %d, want 2", st.Submissions)
	}
}

// TestFeedbackNeverRevealsCorrectness asserts a submission response does not tell the
// player which pairs are right, which would let them search the space.
func TestFeedbackNeverRevealsCorrectness(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(8), "feedback-seed", `{"clips":4}`, 2)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	rightDec, _ := apply(t, prep, subject, matchesAction(correctPairs(pub, priv)...), nil)
	candidates := make([]string, 0, len(pub.Candidates))
	for _, c := range pub.Candidates {
		candidates = append(candidates, c.CharacterID)
	}
	wrong := make([]string, 0, len(pub.Clips)*2)
	for i, c := range pub.Clips {
		wrong = append(wrong, c.ClipID, candidates[i%len(candidates)])
	}
	wrongDec, _ := apply(t, prep, subject, matchesAction(wrong...), nil)

	if !rightDec.Accepted || !wrongDec.Accepted {
		t.Fatal("a submission was rejected")
	}
	var rightFb, wrongFb map[string]any
	if err := json.Unmarshal(rightDec.Feedback, &rightFb); err != nil {
		t.Fatalf("decode feedback: %v", err)
	}
	if err := json.Unmarshal(wrongDec.Feedback, &wrongFb); err != nil {
		t.Fatalf("decode feedback: %v", err)
	}
	// Same key set, and no key named after correctness.
	rightKeys := sortedKeys(rightFb)
	wrongKeys := sortedKeys(wrongFb)
	if strings.Join(rightKeys, ",") != strings.Join(wrongKeys, ",") {
		t.Fatalf("feedback keys differ: right=%v wrong=%v", rightKeys, wrongKeys)
	}
	for _, key := range rightKeys {
		lower := strings.ToLower(key)
		for _, forbidden := range []string{"correct", "right", "wrong", "score"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("feedback carries the field %q, which reveals correctness", key)
			}
		}
	}
}

// sortedKeys returns the keys of a map in ascending order.
func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// A small insertion sort keeps the helper dependency-free.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TestNonParticipantCannotSubmit asserts a subject outside the roster is refused.
func TestNonParticipantCannotSubmit(t *testing.T) {
	prep, pub, priv := prepareRound(t, verifiedPack(8), "outsider-seed", `{"clips":4}`, 2)
	dec, _ := apply(t, prep, modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer},
		matchesAction(correctPairs(pub, priv)...), nil)
	if dec.Accepted {
		t.Fatal("a non-participant submission was accepted")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
}

// TestConfigurationBounds covers the documented configuration limits.
func TestConfigurationBounds(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{name: "default", cfg: `{}`},
		{name: "nil", cfg: ``},
		{name: "explicit", cfg: `{"clips":4,"answerSeconds":90,"maxPlays":2}`},
		{name: "one clip", cfg: `{"clips":1}`, wantErr: "outside"},
		{name: "seven clips", cfg: `{"clips":7}`, wantErr: "outside"},
		{name: "short window", cfg: `{"answerSeconds":29}`, wantErr: "outside"},
		{name: "long window", cfg: `{"answerSeconds":601}`, wantErr: "outside"},
		{name: "zero plays", cfg: `{"maxPlays":0}`},
		{name: "three plays", cfg: `{"maxPlays":3}`, wantErr: "outside"},
		{name: "negative plays", cfg: `{"maxPlays":-1}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"clips":"four"}`, wantErr: "invalid config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig rejected %s: %v", tc.cfg, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig accepted %s", tc.cfg)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestParticipantBounds covers the roster bounds.
func TestParticipantBounds(t *testing.T) {
	for _, players := range []int{0, 9} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bounds-seed"),
				Subject: roster(players),
				Config:  json.RawMessage(`{}`),
				Content: verifiedPack(8),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a %d-player round", players)
			}
		})
	}
	// A single participant is the documented solo form.
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("solo-seed"),
		Subject: roster(1),
		Config:  json.RawMessage(`{}`),
		Content: verifiedPack(8),
	})
	if err != nil {
		t.Fatalf("Prepare refused a single-participant round: %v", err)
	}
}

// TestDuplicateSubjectRefused asserts a duplicated roster entry is refused.
func TestDuplicateSubjectRefused(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p1", Kind: modes.SubjectPlayer},
	}
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("dup-seed"),
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: verifiedPack(8),
	})
	if err == nil {
		t.Fatal("Prepare accepted a duplicated subject")
	}
	if !strings.Contains(err.Error(), "duplicate subject") {
		t.Fatalf("error %q does not mention the duplicate", err)
	}
}

// TestPrepareIsDeterministic asserts the board is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	first, _, _ := prepareRound(t, verifiedPack(8), "det-seed", `{"clips":4}`, 2)
	second, _, _ := prepareRound(t, verifiedPack(8), "det-seed", `{"clips":4}`, 2)
	if string(first.Public) != string(second.Public) {
		t.Fatal("public state is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}
}

// TestBoardOrderIsShuffled asserts the board order carries no information about the
// source order.
func TestBoardOrderIsShuffled(t *testing.T) {
	_, pub, _ := prepareRound(t, verifiedPack(12), "order-seed", `{"clips":6}`, 2)
	// The published clip order must not be the source order (clip-001, clip-002, ...).
	// The ids are opaque, so the check is that the audio routes are not in source order.
	routes := make([]string, 0, len(pub.Clips))
	for _, c := range pub.Clips {
		routes = append(routes, c.AudioPath)
	}
	inOrder := true
	for i := 1; i < len(routes); i++ {
		if routes[i] < routes[i-1] {
			inOrder = false
			break
		}
	}
	if inOrder && len(routes) > 2 {
		t.Fatalf("the board is published in source order: %v", routes)
	}
}

// TestAudioRouteIsNotPresentedAsSecret asserts the payload does not claim the audio route
// is hidden, which the plan says would be a false guarantee .
func TestAudioRouteIsNotPresentedAsSecret(t *testing.T) {
	prep, _, _ := prepareRound(t, verifiedPack(8), "route-seed", `{"clips":4}`, 2)
	public := strings.ToLower(string(prep.Public))
	for _, forbidden := range []string{"hiddensrc", "obfuscated", "encryptedaudio"} {
		if strings.Contains(public, forbidden) {
			t.Fatalf("the public state claims the audio route is %q", forbidden)
		}
	}
	// The route is published, which is the honest shape.
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	for _, c := range board.Clips {
		if c.AudioPath == "" {
			t.Fatalf("clip %q publishes no route", c.ClipID)
		}
	}
}

// mustJSON marshals v or fails.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}
