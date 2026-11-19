-- Keep the published tie arrangement as event metadata. Earlier versions only
-- recorded it in the immutable event.created audit; preserve that exact choice.
ALTER TABLE event_sessions ADD COLUMN tie_policy TEXT NOT NULL DEFAULT 'shared-rank'
    CHECK (tie_policy IN ('shared-rank', 'announced-tiebreak'));
UPDATE event_sessions SET tie_policy = COALESCE((
    SELECT CASE WHEN json_valid(a.detail_json)
        THEN json_extract(a.detail_json, '$.tiePolicy') ELSE NULL END
    FROM audit_events a
    WHERE a.event_id = event_sessions.id AND a.action = 'event.created'
      AND CASE WHEN json_valid(a.detail_json)
        THEN json_extract(a.detail_json, '$.tiePolicy') ELSE NULL END
        IN ('shared-rank', 'announced-tiebreak')
    ORDER BY a.created_at, a.id LIMIT 1
), 'shared-rank');
