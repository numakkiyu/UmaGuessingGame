"use client";

import { useDeferredValue, useMemo, useState } from "react";
import { Command } from "cmdk";
import { Search } from "lucide-react";
import { type SearchIndexEntry } from "@/lib/validation/schemas";

type Props = {
  entries: SearchIndexEntry[];
  disabled?: boolean;
  onSelect: (characterId: string) => void;
};

export function CharacterSearchInput({ entries, disabled, onSelect }: Props) {
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);

  const filteredEntries = useMemo(() => {
    const keyword = deferredQuery.trim().toLowerCase();
    if (!keyword) return entries.slice(0, 12);
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
      .slice(0, 12);
  }, [deferredQuery, entries]);

  return (
    <Command
      shouldFilter={false}
      className="overflow-hidden rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel-strong)] shadow-[var(--shadow-soft)]"
    >
      <div className="border-b border-[var(--color-line)] px-4 pb-3 pt-4">
        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.22em] text-[var(--color-brand-strong)]">
              搜索马娘
            </p>
            <p className="mt-1 text-sm text-[var(--color-muted)]">
              输入中文、日文、英文或常见别名都可以。
            </p>
          </div>
          <div className="rounded-full bg-[var(--color-panel-soft)] px-3 py-1 text-xs font-medium text-[var(--color-ink)]">
            {disabled ? "暂时不能继续猜" : "选中后立刻提交"}
          </div>
        </div>

        <div className="mt-4 flex items-center gap-3 rounded-[22px] border border-[var(--color-line)] bg-white/70 px-4 py-3">
          <Search className="size-4 text-[var(--color-muted)]" />
          <Command.Input
            value={query}
            onValueChange={setQuery}
            disabled={disabled}
            placeholder="先输入一位你想猜的马娘"
            className="h-8 w-full bg-transparent text-sm outline-none placeholder:text-[var(--color-muted)] sm:text-base"
          />
        </div>
      </div>
      <Command.List className="max-h-72 overflow-y-auto p-2">
        {filteredEntries.length === 0 ? (
          <Command.Empty className="px-3 py-4 text-sm text-[var(--color-muted)]">
            暂时没找到这位马娘。
          </Command.Empty>
        ) : null}
        {filteredEntries.map((entry) => (
          <Command.Item
            key={entry.id}
            value={`${entry.id}-${entry.name_zh}`}
            onSelect={() => {
              setQuery("");
              onSelect(entry.id);
            }}
            className="mb-1 flex min-h-[60px] cursor-pointer items-center gap-3 rounded-[18px] px-3 py-2 text-left outline-none data-[selected=true]:bg-[rgba(200,108,53,0.12)]"
          >
            <div className="flex size-11 items-center justify-center overflow-hidden rounded-2xl border border-[var(--color-line)] bg-white">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={entry.image_local_path}
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
          </Command.Item>
        ))}
      </Command.List>
    </Command>
  );
}
