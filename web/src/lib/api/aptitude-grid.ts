import { validLocalizedNames, type LocalizedNames } from "@/i18n/entities";
// Older projections may omit cellId; the fallback matches the server's cell-<row>-<col> format
import type { components } from "./types";

type Schemas = components["schemas"];

export type GridCondition = {
  axis: string;
  value: string;
};

export type GridClaim = {
  subjectId: string;
  characterId: string;
  name: string;
  localizedNames?: LocalizedNames;
  imagePath: string | null;
  turn: number;
  /** 仅揭晓后存在：这一格是否真的满足两项条件 */
  satisfied: boolean | null;
};

export type GridCell = {
  index: number;
  row: number;
  col: number;
  rowCondition: GridCondition;
  colCondition: GridCondition;
  claim: GridClaim | null;
  /** 提交 grid.claim 时用的标识（服务端下发优先，缺失按位置回退） */
  cellId: string;
};

export type GridPoolCard = {
  id: string;
  name: string;
  localizedNames?: LocalizedNames;
  imagePath: string | null;
};

export type GridSelf = {
  subjectId: string;
  claims: number[];
  attempts: number;
  consecutiveTimeouts: number;
  timedOut: boolean;
};

export type GridAptitudes = {
  characterId: string;
  surface: Record<string, string>;
  distance: Record<string, string>;
  style: Record<string, string>;
};

export type GridRound = {
  phase: string;
  variant: string;
  rowAxis: string;
  cells: GridCell[];
  pool: GridPoolCard[];
  solvable: boolean;
  closed: boolean;
  turnSubjectId: string | null;
  self: GridSelf | null;
  subjects: { subjectId: string; claims: number; timedOut: boolean }[];
  /** 仅揭晓后 */
  aptitudes: GridAptitudes[];
};

/** 轴的中文名 */
const AXIS_LABELS: Record<string, string> = {
  surface: "场地",
  distance: "距离",
  style: "跑法",
};

export function axisLabel(axis: string): string {
  return AXIS_LABELS[axis] ?? "条件";
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : null;
}

function num(value: unknown): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function str(value: unknown): string | null {
  return typeof value === "string" && value !== "" ? value : null;
}

function normalizeCondition(value: unknown): GridCondition {
  const record = asRecord(value) ?? {};
  return {
    axis: str(record["axis"]) ?? "",
    value: str(record["value"]) ?? "",
  };
}

/**
 * 计算提交用的 cellId。
 *
 * 优先服务端下发的 cellId；缺失时按服务端内部同一公式回退
 * （internal/modes/aptitudegrid: cellIDFor(row, col) = fmt.Sprintf("cell-%d-%d", row, col)）。
 * 这个回退依赖服务端实现细节，属于临时措施。
 */
function resolveCellId(record: Record<string, unknown>, row: number, col: number): string {
  const fromServer = str(record["cellId"]);
  if (fromServer) return fromServer;
  return `cell-${row}-${col}`;
}

function normalizeClaim(value: unknown): GridClaim | null {
  const record = asRecord(value);
  if (!record) return null;
  const characterId = str(record["characterId"]);
  if (!characterId) return null;
  return {
    subjectId: str(record["subjectId"]) ?? "",
    characterId,
    name: str(record["name"]) === characterId ? "" : str(record["name"]) ?? "",
    ...(validLocalizedNames(record["localizedNames"]) && record["localizedNames"] ? { localizedNames: record["localizedNames"] } : {}),
    imagePath: str(record["imagePath"]),
    turn: num(record["turn"]) ?? 0,
    satisfied: typeof record["satisfied"] === "boolean" ? record["satisfied"] : null,
  };
}

function normalizeAptitudeTable(value: unknown): Record<string, string> {
  const record = asRecord(value);
  if (!record) return {};
  const out: Record<string, string> = {};
  for (const [key, grade] of Object.entries(record)) {
    if (typeof grade === "string" && grade !== "") out[key] = grade;
  }
  return out;
}

export function normalizeGridRound(view: Schemas["RoundView"] | null | undefined): GridRound | null {
  if (!view) return null;
  const publicState = asRecord(view.publicState);
  if (!publicState) return null;

  const config = asRecord(publicState["config"]) ?? {};

  const cells: GridCell[] = [];
  const rawCells = publicState["cells"];
  if (Array.isArray(rawCells)) {
    for (const entry of rawCells) {
      const record = asRecord(entry);
      if (!record) continue;
      const row = num(record["row"]) ?? 0;
      const col = num(record["col"]) ?? 0;
      cells.push({
        index: num(record["index"]) ?? row * 3 + col,
        row,
        col,
        rowCondition: normalizeCondition(record["rowCondition"]),
        colCondition: normalizeCondition(record["colCondition"]),
        claim: normalizeClaim(record["claim"]),
        cellId: resolveCellId(record, row, col),
      });
    }
  }
  cells.sort((a, b) => a.index - b.index);

  const pool: GridPoolCard[] = [];
  const rawPool = publicState["pool"];
  if (Array.isArray(rawPool)) {
    for (const entry of rawPool) {
      const record = asRecord(entry);
      if (!record) continue;
      const id = str(record["id"]);
      if (!id) continue;
      pool.push({
        id,
        name: str(record["name"]) === id ? "" : str(record["name"]) ?? "",
        ...(validLocalizedNames(record["localizedNames"]) && record["localizedNames"] ? { localizedNames: record["localizedNames"] } : {}),
        imagePath: str(record["imagePath"]),
      });
    }
  }

  const selfRecord = asRecord(publicState["self"]);
  const claimsRaw = selfRecord?.["claims"];
  const self: GridSelf | null = selfRecord
    ? {
        subjectId: str(selfRecord?.["subjectId"]) ?? "",
        claims: Array.isArray(claimsRaw)
          ? claimsRaw.filter((v): v is number => typeof v === "number" && Number.isFinite(v))
          : [],
        attempts: num(selfRecord["attempts"]) ?? 0,
        consecutiveTimeouts: num(selfRecord["consecutiveTimeouts"]) ?? 0,
        timedOut: selfRecord["timedOut"] === true,
      }
    : null;

  const subjects: { subjectId: string; claims: number; timedOut: boolean }[] = [];
  const rawSubjects = publicState["subjects"];
  if (Array.isArray(rawSubjects)) {
    for (const entry of rawSubjects) {
      const record = asRecord(entry);
      if (!record) continue;
      const subjectId = str(record["subjectId"]);
      if (!subjectId) continue;
      subjects.push({
        subjectId,
        claims: num(record["claims"]) ?? 0,
        timedOut: record["timedOut"] === true,
      });
    }
  }

  const aptitudes: GridAptitudes[] = [];
  const rawAptitudes = publicState["aptitudes"];
  if (Array.isArray(rawAptitudes)) {
    for (const entry of rawAptitudes) {
      const record = asRecord(entry);
      if (!record) continue;
      const characterId = str(record["characterId"]);
      if (!characterId) continue;
      const apt = asRecord(record["aptitudes"]) ?? {};
      aptitudes.push({
        characterId,
        surface: normalizeAptitudeTable(apt["surface"]),
        distance: normalizeAptitudeTable(apt["distance"]),
        style: normalizeAptitudeTable(apt["style"]),
      });
    }
  }

  return {
    phase: typeof publicState["phase"] === "string" ? publicState["phase"] : "",
    variant: str(config["variant"]) ?? "challenge",
    rowAxis: str(config["rowAxis"]) ?? "distance",
    cells,
    pool,
    solvable: publicState["solvable"] !== false,
    closed: publicState["closed"] === true,
    turnSubjectId: str(publicState["turnSubjectId"]),
    self,
    subjects,
    aptitudes,
  };
}

/** 棋盘的行列标签（去重后按出现顺序，用于表头） */
export function boardAxes(cells: readonly GridCell[]): {
  rowAxis: string;
  colAxis: string;
  rowLabels: string[];
  colLabels: string[];
} {
  const rowAxis = cells[0]?.rowCondition.axis ?? "distance";
  const colAxis = cells[0]?.colCondition.axis ?? "style";
  const rowLabels: string[] = [];
  const colLabels: string[] = [];
  for (const cell of cells) {
    if (!rowLabels.includes(cell.rowCondition.value)) rowLabels.push(cell.rowCondition.value);
    if (!colLabels.includes(cell.colCondition.value)) colLabels.push(cell.colCondition.value);
  }
  return { rowAxis, colAxis, rowLabels, colLabels };
}

/** 挑战模式得分：floor(100 × 正确格数 / 9) */
export function challengeScore(correctCells: number): number {
  return Math.floor((100 * correctCells) / 9);
}
