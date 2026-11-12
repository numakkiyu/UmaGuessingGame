import type { ClassicGame, PracticePack } from "@/lib/domain/classic-practice";
// Fallback for denied/quota-limited IndexedDB. Explicitly not persistent.
export const volatilePractices=new Map<string,{game:ClassicGame;pack:PracticePack}>();
