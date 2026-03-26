import { createClient } from "redis";
import { getServerConfig } from "@/config/server";

type RedisClient = ReturnType<typeof createClient>;

let clientPromise: Promise<RedisClient> | null = null;
let clientInstance: RedisClient | null = null;

export async function getRedis() {
  if (!clientPromise) {
    clientInstance = createClient({ url: getServerConfig().redisUrl });
    clientPromise = clientInstance.connect().then(() => clientInstance as RedisClient);
  }

  return clientPromise;
}
