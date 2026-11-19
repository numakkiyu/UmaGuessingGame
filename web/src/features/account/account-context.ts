import { createContext } from "react";
import type { PublicConfig } from "@/lib/api/types";
export const AccountConfigContext = createContext<PublicConfig["accounts"]>(undefined);
