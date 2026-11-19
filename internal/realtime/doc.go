// Package realtime delivers notifications after committed outbox writes.
//
// Business writes and authorized snapshots use REST. Streams only notify clients
// to fetch changed state, keeping authorization and projection on one path.
//
// Delivery is at least once. Clients deduplicate by eventId because a dispatcher
// crash can redeliver a committed row before dispatched_at is saved.
//
// Cursors are HMAC-authenticated and bound to the room, membership and server
// epoch. Private filtering permits sequence gaps, so clients must not infer
// message loss by subtracting cursor positions. Resync signals or newer room
// versions trigger snapshot recovery. The wire shape is in api/realtime.schema.json.
package realtime
