import { formatGroup } from "../question-bank";
import { type GuessRow, type QuestionBankEntry } from "../validation/schemas";

type GuessStatus = "correct" | "near" | "wrong";

const distanceOrder = ["短", "英", "中", "长"];
const runningStyleOrder = ["逃", "先", "差", "追"];
const g1Order = ["0", "1-2", "3-4", "5+"];
const g23Order = ["0", "1-3", "4-6", "7+"];

function sameSet(left: string[], right: string[]) {
  return left.length === right.length && left.every((item) => right.includes(item));
}

function hasOverlap(left: string[], right: string[]) {
  return left.some((item) => right.includes(item));
}

function hasAdjacent(left: string[], right: string[], order: string[]) {
  return left.some((item) => {
    const index = order.indexOf(item);
    return index >= 0 && [order[index - 1], order[index + 1]].some((neighbor) => neighbor && right.includes(neighbor));
  });
}

function compareOrdinal(left: string, right: string, order: string[]): GuessStatus {
  if (left === right) return "correct";
  const distance = Math.abs(order.indexOf(left) - order.indexOf(right));
  return distance === 1 ? "near" : "wrong";
}

export function compareStar(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  if (guess.star === answer.star) return "correct";
  return Math.abs(guess.star - answer.star) === 1 ? "near" : "wrong";
}

export function compareSurface(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  if (sameSet(guess.surface_group, answer.surface_group)) return "correct";
  return hasOverlap(guess.surface_group, answer.surface_group) ? "near" : "wrong";
}

export function compareDistance(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  if (sameSet(guess.distance_group, answer.distance_group)) return "correct";
  if (hasOverlap(guess.distance_group, answer.distance_group)) return "near";
  return hasAdjacent(guess.distance_group, answer.distance_group, distanceOrder) ? "near" : "wrong";
}

export function compareStyle(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  if (sameSet(guess.running_style_group, answer.running_style_group)) return "correct";
  if (hasOverlap(guess.running_style_group, answer.running_style_group)) return "near";
  return hasAdjacent(guess.running_style_group, answer.running_style_group, runningStyleOrder) ? "near" : "wrong";
}

export function compareSex(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  return guess.sex_type === answer.sex_type ? "correct" : "wrong";
}

export function compareG1Bracket(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  return compareOrdinal(guess.g1_bracket, answer.g1_bracket, g1Order);
}

export function compareG23Bracket(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  return compareOrdinal(guess.g23_bracket, answer.g23_bracket, g23Order);
}

export function compareGrade(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  return guess.school_grade === answer.school_grade ? "correct" : "wrong";
}

export function compareDormitory(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessStatus {
  return guess.dormitory === answer.dormitory ? "correct" : "wrong";
}

function formatBracketValue(
  bracket: string,
  status: GuessStatus,
  hasJpn: boolean,
) {
  if (status !== "correct" || !hasJpn) {
    return bracket;
  }
  return `${bracket}J`;
}

export function buildGuessRow(guess: QuestionBankEntry, answer: QuestionBankEntry): GuessRow {
  const g1Status = compareG1Bracket(guess, answer);
  const g23Status = compareG23Bracket(guess, answer);

  return {
    characterId: guess.id,
    displayName: guess.name_zh,
    avatarUrl: guess.image_local_path,
    avatarFallbackUrl: guess.image_url,
    cells: {
      star: { value: `${guess.star}星`, status: compareStar(guess, answer) },
      surface: { value: formatGroup(guess.surface_group), status: compareSurface(guess, answer) },
      distance: { value: formatGroup(guess.distance_group), status: compareDistance(guess, answer) },
      style: { value: formatGroup(guess.running_style_group), status: compareStyle(guess, answer) },
      sex: { value: guess.sex_type, status: compareSex(guess, answer) },
      g1: {
        value: formatBracketValue(guess.g1_bracket, g1Status, answer.g1_has_jpn),
        status: g1Status,
      },
      g23: {
        value: formatBracketValue(guess.g23_bracket, g23Status, answer.g23_has_jpn),
        status: g23Status,
      },
      grade: { value: guess.school_grade, status: compareGrade(guess, answer) },
      dormitory: { value: guess.dormitory, status: compareDormitory(guess, answer) },
    },
  };
}
