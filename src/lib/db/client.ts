import { drizzle } from "drizzle-orm/node-postgres";
import { Pool } from "pg";
import { getServerConfig } from "@/config/server";

let pool: Pool | null = null;

export function getDb() {
  if (!pool) {
    pool = new Pool({
      connectionString: getServerConfig().databaseUrl,
    });
  }

  return drizzle(pool);
}
