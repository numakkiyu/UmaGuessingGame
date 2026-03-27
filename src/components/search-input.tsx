"use client";

import { useDeferredValue, useMemo, useState } from "react";
import { Search } from "lucide-react";
import { AvatarImage } from "@/components/avatar-image";
import { type SearchIndexEntry } from "@/lib/validation/schemas";

type Props = {
  entries: SearchIndexEntry[];
  disabled?: boolean;
  disabledHint?: string;
  onSelect: (characterId: string) => void;
};

export function CharacterSearchInput({ entries, disabled, disabledHint, onSelect }: Props) {
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const keyword = deferredQuery.trim().toLowerCase();
  const showCandidates = keyword.length > 0;

  const filteredEntries = useMemo(() => {
    if (!keyword) return [];

    return entries
      .filter((entry) =>
        [
          entry.name_zh,
          entry.name_jp,
          entry.name_en ?? "",
          entry.name_tw ?? "",
          ...entry.aliases,
        ]
          .join(" ")
          .toLowerCase()
          .includes(keyword),
      )
      .slice(0, 8);
  }, [entries, keyword]);

  function handleSelect(characterId: string) {
    setQuery("");
    onSelect(characterId);
  }

  return (
    <section className="search-panel overflow-hidden rounded-[24px] border border-[var(--color-line)] bg-[var(--color-panel-strong)] shadow-[var(--shadow-soft)]">
      <div className="search-panel-head uma-panel-head px-4 pb-3 pt-4">
        <div className="search-panel-title flex flex-col gap-2.5 sm:flex-row sm:items-center sm:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
              猜一位马娘
            </p>
            <p className="mt-1 text-sm text-[var(--color-muted)]">
              输入中文、日文、英文或常见外号都可以。
            </p>
          </div>
          <div className="inline-flex rounded-full border border-[var(--color-line)] bg-[var(--color-panel-soft)] px-3 py-1 text-xs font-medium text-[var(--color-ink)] shadow-[var(--shadow-soft)]">
            {disabled ? (disabledHint ?? "等这一局开始后就能继续猜") : "点一下候选就会提交"}
          </div>
        </div>

        <div className="uma-input-shell search-panel-input mt-4 px-4 py-3">
          <Search className="size-4 text-[var(--color-brand-blue-deep)]" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            disabled={disabled}
            placeholder="输入一位马娘开始猜"
            className="h-8 w-full bg-transparent text-sm outline-none placeholder:text-[var(--color-muted)] sm:text-base"
          />
        </div>
      </div>

      <div className="search-panel-candidates max-h-80 overflow-y-auto p-2">
        {!showCandidates ? (
          <div className="search-panel-note rounded-[20px] border border-dashed border-[var(--color-line)] bg-[rgba(239,247,255,0.68)] px-4 py-4 text-sm leading-6 text-[var(--color-muted)]">
            输入名字后，这里会出现头像、中文名和日文名。先从你最熟的那位开始，通常更容易把范围缩小。
          </div>
        ) : null}

        {showCandidates && filteredEntries.length === 0 ? (
          <div className="rounded-[20px] bg-[rgba(239,247,255,0.72)] px-3 py-4 text-sm text-[var(--color-muted)]">
            暂时没找到这位马娘，换个名字或常见外号再试试。
          </div>
        ) : null}

        {showCandidates
          ? filteredEntries.map((entry) => (
              <button
                key={entry.id}
                type="button"
                onClick={() => handleSelect(entry.id)}
                className="mb-1 flex min-h-[60px] w-full items-center gap-3 rounded-[18px] border border-transparent bg-white/52 px-3 py-2 text-left transition hover:border-[rgba(63,136,247,0.16)] hover:bg-[rgba(229,244,255,0.72)] focus:border-[rgba(63,136,247,0.22)] focus:bg-[rgba(229,244,255,0.82)] focus:outline-none"
              >
                <div className="flex size-11 items-center justify-center overflow-hidden rounded-2xl border border-[var(--color-line)] bg-white shadow-[inset_0_1px_0_rgba(255,255,255,0.7)]">
                  <AvatarImage
                    primarySrc={entry.image_local_path}
                    proxySrc={entry.image_local_path}
                    remoteSrc={entry.image_url}
                    alt={entry.name_zh}
                    className="size-full object-cover"
                  />
                </div>
                <div className="min-w-0">
                  <p className="truncate text-sm font-semibold text-[var(--color-ink)]">
                    {entry.name_zh}
                  </p>
                  <p className="truncate text-xs text-[var(--color-muted)]">
                    {entry.name_jp}
                  </p>
                </div>
              </button>
            ))
          : null}
      </div>
    </section>
  );
}
