-- Keep the exact agreed rules through queueing and room creation.
ALTER TABLE matchmaking_tickets ADD COLUMN mode_config_json TEXT NOT NULL
    DEFAULT '{"config":{},"modeId":"classic"}';

-- Repair only the old matchmaker shape, never ordinary room configurations.
UPDATE rooms
SET config_json = json_object(
    'kind', 'matchmaking', 'modeConfig', json(config_json),
    'maxPlayers', 2, 'maxSpectators', 0,
    'visibility', 'invite', 'allowSpectators', json('false')),
    capacity = 2
WHERE json_valid(config_json)
  AND json_extract(config_json, '$.kind') IS NULL
  AND json_extract(config_json, '$.modeId') = 'classic'
  AND id IN (SELECT room_id FROM matchmaking_tickets WHERE room_id IS NOT NULL);
