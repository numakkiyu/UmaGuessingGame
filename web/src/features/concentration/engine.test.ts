import { describe, expect, it } from "vitest";
import { CATALOG_VERSION, FIELDS, QUESTION_COUNT, createGame, decodeGame, emptyAnswer, gameResult, normalize, parseBirthday, profiles, questions, rankFor, scoreAnswer, translations, weights, type Answer, type Profile } from "./engine";
import legacyRoster from "../../../../data/manual/concentration-profiles.json";
import { existsSync } from "node:fs";
import { resolve } from "node:path";

const find = (id: string) => profiles.find(profile => profile.id === id)!;
function perfect(profile: Profile): Answer {
  return { name: profile.chineseNames[0]!, translation: translations(profile).find(name => normalize(name) !== normalize(profile.chineseNames[0]!)) ?? "",
    japanese: profile.names.ja, english: profile.english ?? "", cv: profile.cv[0]!,
    birthday: `${profile.birthday.month}/${profile.birthday.day}`, height: `${profile.height}cm` };
}

describe("concentration catalog and source-backed scoring", () => {
  it("includes the entire current roster and all 133 legacy characters, with local assets and sources", () => {
    expect(profiles).toHaveLength(147);
    expect(new Set(profiles.map(profile => profile.id)).size).toBe(profiles.length);
    expect(legacyRoster.legacyRoster).toHaveLength(133);
    expect(legacyRoster.legacyRoster.every(id => profiles.some(profile => profile.id === id))).toBe(true);
    for (const profile of profiles) {
      const path = `public${profile.imagePath}`;
      expect(existsSync(resolve(process.cwd(), path)), profile.id).toBe(true);
      expect(profile.sources.length).toBeGreaterThan(0);
      expect(profile.sources.every(url => /^https:\/\/(umamusume.jp|wiki.biligame.com|zh.moegirl.org.cn)\//.test(url))).toBe(true);
      expect(parseBirthday(`${profile.birthday.month}/${profile.birthday.day}`)).toEqual(profile.birthday);
      expect(Object.values(weights(profile)).reduce((a, b) => a + b, 0)).toBe(20);
      expect(scoreAnswer(profile, perfect(profile)).total).toBe(20);
      expect(scoreAnswer(profile, emptyAnswer()).total).toBe(0);
    }
  });
  it("uses official profiles for birthday, height and Latin stage names", () => {
    expect(profiles.find(p => p.names.ja === "タップダンスシチー")?.birthday).toEqual({ month: 3, day: 16 });
    expect(profiles.find(p => p.names.ja === "シュヴァルグラン")?.height).toBe(160);
    expect(find("1003").cv).toEqual(["Machico"]);
    expect(find("1004").cv).toEqual(["Lynn"]);
    expect(scoreAnswer(find("1004"), { ...emptyAnswer(), cv: "琳恩·斯塔尔" }).points.cv).toBe(0);
  });
  it("normalizes script variants, widths, casing and spaces without fuzzy typos", () => {
    const special = find("1001");
    const answer = { ...perfect(special), name: " 特別週 ", english: "ＳＰＥＣＩＡＬ　ＷＥＥＫ", cv: "和气杏未", birthday: "０５月０２日", height: "１５８㎝" };
    expect(scoreAnswer(special, answer).total).toBe(20);
    expect(scoreAnswer(find("1006"), { ...perfect(find("1006")), name: "小绿帽" }).points.name).toBe(0);
    expect(scoreAnswer(special, { ...perfect(special), japanese: "特别周" }).points.japanese).toBe(0);
  });
  it("awards alternate translations only once and requires a different Chinese name", () => {
    const teio = find("1003");
    expect(weights(teio).name).toBe(6);
    expect(scoreAnswer(teio, { ...emptyAnswer(), name: "东海帝皇", translation: "東海帝皇" }).total).toBe(6);
    expect(scoreAnswer(teio, { ...emptyAnswer(), name: "东海帝皇", translation: "东海帝王、东海帝王" }).total).toBe(12);
    expect(scoreAnswer(teio, { ...emptyAnswer(), translation: teio.names.ja }).total).toBe(0);
    expect(weights(find("1001")).name).toBe(12);
    expect(scoreAnswer(find("1001"), { ...emptyAnswer(), name: "特别周" }).total).toBe(12);
  });
  it("accepts only valid month/day and whole centimetres", () => {
    for (const date of ["5/2", "05-02", "5月2日", "05.02"]) expect(parseBirthday(date)).toEqual({ month: 5, day: 2 });
    for (const date of ["2/30", "0/2", "13/1", "2026-05-02", "5/0", "5/2garbage"]) expect(parseBirthday(date)).toBeNull();
    expect(scoreAnswer(find("1001"), { ...emptyAnswer(), height: "158.1" }).total).toBe(0);
  });
  it("keeps Sakura Chitose O distinct from Sakura Chiyono O and uses only the current Ines Fujin cast", () => {
    expect(scoreAnswer(find("1126"), { ...emptyAnswer(), name: "樱花千代王" }).total).toBe(0);
    expect(scoreAnswer(find("1126"), { ...emptyAnswer(), name: "樱花千岁王", translation: "樱花千代王" }).total).toBe(12);
    expect(scoreAnswer(find("1069"), { ...emptyAnswer(), name: "樱花千代王" }).points.name).toBeGreaterThan(0);
    expect(find("1031").cv).toEqual(["長江里加", "长江里加"]);
    for (const cv of ["岭内知美", "岭内知美[1]→长江里加", "岭内知美<ref>（2023年1月10日前）</ref>→长江里加"]) {
      expect(scoreAnswer(find("1031"), { ...emptyAnswer(), cv }).points.cv).toBe(0);
    }
  });
  it("scores every accepted name and CV alias without changing its field weight", () => {
    for (const profile of profiles) {
      for (const name of [...profile.chineseNames, profile.names.ja]) expect(scoreAnswer(profile, { ...emptyAnswer(), name }).points.name, profile.id).toBe(weights(profile).name);
      for (const cv of profile.cv) expect(scoreAnswer(profile, { ...emptyAnswer(), cv }).points.cv, profile.id).toBe(1);
    }
  });
});

describe("draw, result and resume", () => {
  it("draws 20 unique characters deterministically at all unsigned seed boundaries", () => {
    for (const seed of [0, 1, 42, 2147483648, 4294967295]) {
      expect(questions(seed)).toEqual(questions(seed));
      expect(new Set(questions(seed).map(p => p.id)).size).toBe(QUESTION_COUNT);
    }
    expect(questions(1)).not.toEqual(questions(2));
    for (const seed of [-1, 1.5, 4294967296, NaN]) expect(() => questions(seed)).toThrow();
    expect(() => questions(1, 2, [find("1001"), find("1001")])).toThrow();
  });
  it("reaches zero and full results and calculates completeness without automatic translation points", () => {
    const game = createGame(42, 1000);
    expect(gameResult(game)).toMatchObject({ score: 0, concentration: 0, rank: "D" });
    const perfectGame = { ...game, answers: questions(42).map(perfect) };
    expect(gameResult(perfectGame)).toMatchObject({ score: 400, accuracy: 100, completeness: 100, relevance: 100, concentration: 100, rank: "SSS" });
    const namesOnly = { ...game, answers: questions(42).map(p => ({ ...emptyAnswer(), name: p.chineseNames[0]! })) };
    expect(gameResult(namesOnly).accuracy).toBe(100);
    expect(gameResult(namesOnly).completeness).toBe(0);
  });
  it("restores drafts and completed results while rejecting corrupt or incompatible records", () => {
    const game = { ...createGame(42, 1000), activeIndex: 8 };
    game.answers[8] = { ...emptyAnswer(), name: "特别周" };
    expect(decodeGame(JSON.stringify(game))).toEqual(game);
    expect(decodeGame(JSON.stringify({ ...game, finishedAt: 5000 }))?.finishedAt).toBe(5000);
    for (const patch of [{ seed: -1 }, { catalogVersion: CATALOG_VERSION + "bad" }, { activeIndex: 20 }, { answers: [] }, { startedAt: -1 }, { finishedAt: 999 }, { modeId: "classic" }, { schemaVersion: 2 }]) {
      expect(decodeGame(JSON.stringify({ ...game, ...patch }))).toBeNull();
    }
    expect(decodeGame("broken")).toBeNull();
    expect(decodeGame("x".repeat(100001))).toBeNull();
    for (const key of FIELDS) expect(decodeGame(JSON.stringify({ ...game, answers: game.answers.map((a, i) => i === 0 ? { ...a, [key]: "x".repeat(121) } : a) }))).toBeNull();
  });
  it("preserves answers and question order from the reviewed previous catalog", () => {
    const game = { ...createGame(42, 1000), activeIndex: 8 };
    game.answers[8] = { ...emptyAnswer(), name: "我的作答" };
    expect(decodeGame(JSON.stringify({ ...game, catalogVersion: "1430926db96589b2" }))).toEqual(game);
    expect(decodeGame(JSON.stringify({ ...game, catalogVersion: "1430926db96589b2", finishedAt: 5000 }))).toEqual({ ...game, finishedAt: 5000 });
  });
  it.each([[0, "D"], [29, "D"], [30, "C"], [39, "C"], [40, "B"], [59, "B"], [60, "A"], [79, "A"], [80, "S"], [89, "S"], [90, "SS"], [94, "SS"], [95, "SSS"], [100, "SSS"]] as const)("rates %s percent as %s", (value, rank) => {
    expect(rankFor(value)).toBe(rank);
  });
});
