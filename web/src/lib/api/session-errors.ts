export type SessionErrorCode =
  | "session-storage-unavailable"
  | "session-storage-blocked"
  | "session-busy"
  | "session-restore-failed"
  | "session-create-failed";

/** Local identity failures carry a stable code, never presentation text. */
export class SessionStateError extends Error {
  constructor(readonly code: SessionErrorCode, options?: ErrorOptions) {
    super(code, options);
    this.name = "SessionStateError";
  }
}
