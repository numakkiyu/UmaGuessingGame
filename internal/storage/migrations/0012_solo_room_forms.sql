-- Old 'solo' rooms sometimes reserved multiplayer seats or hosted programmes.
-- Preserve participants and results; only correct the form and notify clients.
UPDATE rooms SET config_json=json_set(config_json,'$.kind',
 CASE WHEN EXISTS(SELECT 1 FROM event_sessions e WHERE e.room_id=rooms.id) THEN 'event' ELSE 'friend' END),
 room_version=room_version+1
WHERE json_extract(config_json,'$.kind')='solo' AND (
 COALESCE(json_extract(config_json,'$.maxPlayers'),1)>1 OR
 COALESCE(json_extract(config_json,'$.maxSpectators'),0)>0 OR
 COALESCE(json_extract(config_json,'$.allowSpectators'),0)=1 OR
 EXISTS(SELECT 1 FROM event_sessions e WHERE e.room_id=rooms.id)
);
