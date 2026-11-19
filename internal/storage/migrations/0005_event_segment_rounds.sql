-- The legacy round_id remains the latest round for older clients.
CREATE TABLE event_segment_rounds (
    segment_id TEXT NOT NULL REFERENCES event_segments(id) ON DELETE CASCADE,
    round_id TEXT NOT NULL UNIQUE REFERENCES rounds(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal BETWEEN 1 AND 20),
    roster_json TEXT NOT NULL DEFAULT '[]',
    PRIMARY KEY (segment_id, ordinal)
);
INSERT INTO event_segment_rounds (segment_id, round_id, ordinal)
SELECT id, round_id, 1 FROM event_segments WHERE round_id IS NOT NULL;

-- Prior code completed multi-round items after a single round. Reopen only
-- unfinished programmes; historical closed events remain historical records.
UPDATE event_segments SET status='running'
WHERE status='completed'
  AND json_valid(config_json)
  AND json_extract(config_json,'$.roundCount') > 1
  AND event_id IN (SELECT id FROM event_sessions WHERE status IN ('draft','lobby','running'))
  AND NOT EXISTS (SELECT 1 FROM score_corrections sc WHERE sc.segment_id=event_segments.id);

-- Correct the old verbatim copy for active, uncorrected programme results only.
-- Closed records and human corrections are historical facts, never rewritten.
UPDATE segment_results SET
 score_units=(SELECT CAST(rr.score_units * es.budget * 100 /
   (rr.max_score_units * COALESCE(NULLIF(json_extract(es.config_json,'$.roundCount'),0),1)) AS INTEGER)
   FROM event_segments es JOIN round_results rr ON rr.round_id=es.round_id
   WHERE es.id=segment_results.segment_id AND rr.subject_id=segment_results.subject_id),
 max_score_units=(SELECT budget*100 FROM event_segments WHERE id=segment_results.segment_id)
WHERE segment_id IN (
 SELECT es.id FROM event_segments es JOIN event_sessions ev ON ev.id=es.event_id
 WHERE ev.status IN ('draft','lobby','running') AND es.status<>'voided'
   AND json_valid(es.config_json)
   AND COALESCE(NULLIF(json_extract(es.config_json,'$.roundCount'),0),1) BETWEEN 1 AND 20
   AND NOT EXISTS (SELECT 1 FROM score_corrections sc WHERE sc.segment_id=es.id)
   AND EXISTS (SELECT 1 FROM round_results rr WHERE rr.round_id=es.round_id
     AND rr.subject_id=segment_results.subject_id AND rr.max_score_units>0
     AND rr.score_units BETWEEN 0 AND rr.max_score_units)
);
