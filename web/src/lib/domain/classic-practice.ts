import { LocalizedNotice } from "@/lib/player-notice";
import { FIELD_ORDER, type NormalizedGuessRow, type NormalizedCell } from "@/lib/api/classic";
import type { components } from "@/lib/api/schema";
type Schemas = components["schemas"];

export type PracticeCharacter = Schemas["ClassicPracticeCharacter"];
export type PracticePack = Schemas["ClassicPracticePack"];
export type ClassicGame = {
  schemaVersion: 1;
  modeId: "classic";
  gameId: string;
  packKey: string;
  rulesVersion: string;
  answerId: string;
  wrongBudget: number;
  guesses: string[];
  ended: boolean;
  createdAt: number;
  updatedAt: number;
  revision: number;
};

type Status = NormalizedCell["status"];
const ladders = { distance: ["短","英","中","长"], style: ["逃","先","差","追"], g1: ["0","1-2","3-4","5+"], g23: ["0","1-3","4-6","7+"] };
const equality = (a: string, b: string): Status => a && b && a === b ? "correct" : "wrong";
function ordinal(a: string, b: string, order: string[]): Status {
  if (!a || !b) return "wrong";
  if (a === b) return "correct";
  const x = order.indexOf(a), y = order.indexOf(b);
  return x >= 0 && y >= 0 && Math.abs(x - y) === 1 ? "near" : "wrong";
}
function group(a: string[], b: string[], order?: string[]): Status {
  if (!a.length || !b.length) return "wrong";
  const x = new Set(a), y = new Set(b);
  if (x.size === y.size && [...x].every(v => y.has(v))) return "correct";
  if (a.some(v => y.has(v))) return "near";
  if (order && a.some(v => order.includes(v) && b.some(u => order.includes(u) && Math.abs(order.indexOf(v)-order.indexOf(u)) === 1))) return "near";
  return "wrong";
}
export function compareClassic(guess: PracticeCharacter, answer: PracticeCharacter, attempt: number): NormalizedGuessRow {
  const star: Status = !guess.starKnown || !answer.starKnown ? "wrong" : guess.star === answer.star ? "correct" : Math.abs(guess.star-answer.star) === 1 ? "near" : "wrong";
  const g1 = ordinal(guess.g1, answer.g1, ladders.g1), g23 = ordinal(guess.g23, answer.g23, ladders.g23);
  const statuses: Status[] = [star,group(guess.surface,answer.surface),group(guess.distance,answer.distance,ladders.distance),group(guess.runningStyle,answer.runningStyle,ladders.style),equality(guess.sex,answer.sex),g1,g23,equality(guess.grade,answer.grade),equality(guess.dormitory,answer.dormitory)];
  const values = [guess.starKnown ? `${guess.star}星` : "未知",guess.surface.join("/"),guess.distance.join("/"),guess.runningStyle.join("/"),guess.sex,guess.g1+(g1==="correct"&&answer.g1HasJpn&&guess.g1?"J":""),guess.g23+(g23==="correct"&&answer.g23HasJpn&&guess.g23?"J":""),guess.grade,guess.dormitory];
  return { characterId: guess.characterId, displayName: guess.displayName, localizedNames: guess.localizedNames, avatarPath: guess.imagePath, avatarRemoteUrl: null, attempt, correct: guess.characterId === answer.characterId,
    cells: FIELD_ORDER.map((field, i) => ({field, value:values[i]!, status:statuses[i]!})) };
}
export function classicProgress(game: ClassicGame, characters: readonly PracticeCharacter[]) {
  const byId = new Map(characters.map(c => [c.characterId,c]));
  const answer = byId.get(game.answerId);
  if (!answer) throw new LocalizedNotice("missingQuestion");
  const rows = game.guesses.map((id, i) => {
    const guess = byId.get(id);
    if (!guess) throw new LocalizedNotice("unknownGuess");
    return compareClassic(guess,answer,i+1);
  });
  const solved = rows.some(r=>r.correct);
  const remainingWrong = Math.max(0,game.wrongBudget-rows.filter(r=>!r.correct).length);
  return {answer,rows,solved,remainingWrong,finished:game.ended || solved || remainingWrong === 0};
}
export function guessClassic(game: ClassicGame, characters: readonly PracticeCharacter[], characterId: string, now: number): ClassicGame {
  if (classicProgress(game,characters).finished) throw new LocalizedNotice("ended");
  if (game.guesses.includes(characterId)) throw new LocalizedNotice("duplicate");
  if (!characters.some(c=>c.characterId===characterId)) throw new LocalizedNotice("unknownCharacter");
  return {...game, guesses:[...game.guesses,characterId], updatedAt:now, revision:game.revision+1};
}
