import i18n from "@/i18n";
import { type LocalNoticeKey } from "@/i18n/local-notices";
import "@/i18n/local-notices";
/** Only deliberately authored player copy may cross an error boundary verbatim. */
export class PlayerNotice extends Error {
  override readonly name = "PlayerNotice";
  constructor(message: string, readonly messageKey?: string, readonly messageNamespace = "localNotices", readonly messageValues?: Record<string, string | number>) { super(message); }
}

export class LocalizedNotice extends PlayerNotice {
  constructor(key: LocalNoticeKey) {
    super(i18n.t(key, { ns: "localNotices" }), key);
  }
}

/** Feature catalogs are registered by their route/domain module, keeping large
 * error catalogs out of the common entry chunk. Only authored keys belong here. */
export class FeatureNotice extends PlayerNotice {
  constructor(namespace: string, key: string, values?: Record<string, string | number>) {
    super(i18n.t(key, { ns: namespace, ...values }), key, namespace, values);
  }
}

export function playerMessage(error: unknown, fallback: string): string {
  return error instanceof PlayerNotice ? error.messageKey ? i18n.t(error.messageKey, { ns: error.messageNamespace, ...error.messageValues }) : error.message : fallback;
}
