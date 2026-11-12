import "@/i18n/classic";
import { matchLanguage, useTranslation } from "@/i18n";
// Ignore Enter while an input method is composing so selecting a character does not interrupt text entry
import { Search } from "lucide-react";
import { useDeferredValue, useId, useMemo, useRef, useState } from "react";
import { AvatarImage } from "./avatar-image";
import { searchCharacters, searchDisplayName, searchNameRows, type SearchableCharacter, type SearchHit } from "@/lib/search";
import { cn } from "@/lib/utils";

export type SearchInputProps<T extends SearchableCharacter> = {
  items: readonly T[];
  onSelect: (character: T) => void;
  /** 数据仍在加载：显示「正在载入」而不是「无结果」 */
  loading?: boolean;
  /** 数据加载失败：显示失败而不是「无结果」 */
  loadError?: string | null;
  disabled?: boolean;
  disabledHint?: string;
  label?: string;
  placeholder?: string;
  /** 已猜过、需要排除的角色 */
  excludeIds?: ReadonlySet<string>;
  /** 提交按钮文案；不传则不显示（选择即提交的交互） */
  submitLabel?: string;
  onSubmit?: (character: T) => void;
  /**
   * 只对屏幕阅读器保留标签文字。
   *
   * 调用方自己已经在标题里写了「这次猜谁？」时，再渲染一行可见 label 就是重复，
   * 而且那行会占掉约 24px，正好把主操作往下推。标签不能直接删——输入框需要
   * 可访问名称，所以改为 sr-only 保留。
   */
  hideLabel?: boolean;
  className?: string;
  limit?: number;
};

export function SearchInput<T extends SearchableCharacter>({
  items,
  onSelect,
  loading = false,
  loadError = null,
  disabled = false,
  disabledHint,
  label,
  placeholder,
  excludeIds,
  submitLabel,
  onSubmit,
  hideLabel = false,
  className,
  limit = 8,
}: SearchInputProps<T>) {
  const {t,i18n}=useTranslation("classic");
  const language=matchLanguage(i18n.resolvedLanguage ?? "") ?? "zh-Hans";
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [activeIndex, setActiveIndex] = useState(-1);
  // 记录上一次的查询词：查询变化时在渲染期重置高亮项，
  // 而不是在 effect 里 setState 触发级联渲染（react-hooks/set-state-in-effect）。
  const [lastTrimmed, setLastTrimmed] = useState("");
  const composingRef = useRef(false);
  const listboxId = useId();
  const inputRef = useRef<HTMLInputElement>(null);

  const deferredQuery = useDeferredValue(query);
  const trimmed = deferredQuery.trim();

  const hits = useMemo<SearchHit<T>[]>(
    () => searchCharacters(items, trimmed, { limit, ...(excludeIds ? { excludeIds } : {}) }),
    [items, trimmed, limit, excludeIds],
  );

  // 渲染期派生重置：查询词或结果集变化时把高亮项拉回第一项。
  if (lastTrimmed !== trimmed) {
    setLastTrimmed(trimmed);
    setActiveIndex(hits.length > 0 ? 0 : -1);
  }

  const showPanel = open && trimmed.length > 0 && !disabled;

  /*
   * 屏幕阅读器播报（aria-live polite）：只播报结果数量与状态。
   * 措辞刻意与下方可见面板不同，避免同一句话被读两遍——
   * 可见面板负责解释「为什么没有」，live region 只负责「有几个」。
   */
  const statusText = (() => {
    if (disabled) return disabledHint ?? t("search.disabled");
    if (loadError) return t("search.failed");
    if (loading) return t("search.loading");
    if (!trimmed) return "";
    if (hits.length === 0) return t("search.empty");
    return t("search.count",{count:hits.length});
  })();

  function commit(character: T) {
    setQuery("");
    setOpen(false);
    setActiveIndex(-1);
    onSelect(character);
    inputRef.current?.focus();
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    // 输入法组合期间不抢 Enter
    if (composingRef.current || event.nativeEvent.isComposing) return;

    if (event.key === "ArrowDown") {
      event.preventDefault();
      if (hits.length === 0) return;
      setOpen(true);
      setActiveIndex((prev) => (prev + 1) % hits.length);
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      if (hits.length === 0) return;
      setOpen(true);
      setActiveIndex((prev) => (prev <= 0 ? hits.length - 1 : prev - 1));
      return;
    }
    if (event.key === "Enter") {
      const hit = hits[activeIndex];
      if (hit) {
        event.preventDefault();
        if (submitLabel && onSubmit) onSubmit(hit.character);
        else commit(hit.character);
      }
      return;
    }
    if (event.key === "Escape") {
      if (showPanel) {
        event.preventDefault();
        setOpen(false);
      }
    }
  }

  return (
    <div className={cn("relative flex flex-col gap-2", className)}>
      <label
        htmlFor={`${listboxId}-input`}
        className={hideLabel ? "sr-only" : "text-sm font-semibold text-[var(--color-ink)]"}
      >
        {label ?? t("search.label")}
      </label>

      <div
        className={cn(
          "flex min-h-12 items-center gap-2 rounded-[var(--radius-control-lg)] px-3",
          "border border-[var(--color-line-strong)] bg-[var(--color-panel)]",
          "focus-within:outline-2 focus-within:outline-offset-1 focus-within:outline-[var(--color-blue-deep)]",
          disabled && "opacity-70",
        )}
      >
        <Search aria-hidden="true" className="size-4 shrink-0 text-[var(--color-muted)]" />
        <input
          ref={inputRef}
          id={`${listboxId}-input`}
          type="text"
          role="combobox"
          aria-expanded={showPanel}
          aria-controls={listboxId}
          aria-autocomplete="list"
          aria-activedescendant={
            showPanel && activeIndex >= 0 ? `${listboxId}-opt-${activeIndex}` : undefined
          }
          autoComplete="off"
          // 输入法友好
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          enterKeyHint="search"
          value={query}
          disabled={disabled}
          placeholder={placeholder ?? t("search.placeholder")}
          // 16px 正文，iOS 不自动放大
          className="h-12 w-full min-w-0 bg-transparent text-base text-[var(--color-ink)] outline-none placeholder:text-[var(--color-muted)]"
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          // 失焦延迟关闭，保证点击候选项时先触发 click
          onBlur={() => window.setTimeout(() => setOpen(false), 120)}
          onCompositionStart={() => {
            composingRef.current = true;
          }}
          onCompositionEnd={() => {
            composingRef.current = false;
          }}
          onKeyDown={handleKeyDown}
        />
      </div>

      {/* 结果数对屏幕阅读器播报一次（aria-live polite），不用每秒打断 */}
      <p aria-live="polite" className="sr-only">
        {statusText}
      </p>

      {showPanel ? (
        <div className="absolute left-0 right-0 top-full z-[var(--z-popover)] mt-1 overflow-hidden rounded-[var(--radius-card)] border border-[var(--color-line)] bg-[var(--color-panel)] shadow-[var(--shadow-panel)]">
          <ul id={listboxId} role="listbox" aria-label={t("search.candidates")} className="max-h-80 overflow-y-auto overscroll-contain p-1">
            {loading ? (
              <li className="px-3 py-4 text-sm text-[var(--color-muted)]">{t("search.reading")}</li>
            ) : loadError ? (
              <li className="px-3 py-4 text-sm text-[var(--color-danger)]">{loadError}</li>
            ) : hits.length === 0 ? (
              <li className="px-3 py-4 text-sm text-[var(--color-muted)]">
                {t("search.noResult",{query:trimmed})}
              </li>
            ) : (
              hits.map((hit, index) => {
                const names = searchNameRows(hit.character, language);
                return (
                <li key={hit.character.characterId}>
                  <button
                    type="button"
                    id={`${listboxId}-opt-${index}`}
                    role="option"
                    aria-selected={index === activeIndex}
                    // 触摸区 ≥44px
                    className={cn(
                      "flex min-h-14 w-full items-start gap-3 rounded-[var(--radius-control)] px-2 py-2 text-left",
                      index === activeIndex && "bg-[var(--color-panel-soft)]",
                    )}
                    onMouseEnter={() => setActiveIndex(index)}
                    onClick={() => {
                      if (submitLabel && onSubmit) onSubmit(hit.character);
                      else commit(hit.character);
                    }}
                  >
                    <AvatarImage
                      characterId={hit.character.characterId}
                      // 候选列表是公开信息，可用名字；记忆题不经过这个组件
                      alt=""
                      size="sm"
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block break-words text-base font-semibold text-[var(--color-ink)]">
                        {searchDisplayName(hit.character, language) ?? t("search.unnamed")}
                      </span>
                      {names.length > 0 ? <span data-search-names className="mt-0.5 block break-words text-[length:var(--text-aux)] leading-relaxed text-[var(--color-name-secondary)]">
                        {names.map((row, nameIndex) => <span key={row.kind} data-name-kind={row.kind} lang={row.lang}>
                          {nameIndex > 0 ? t("search.nameSeparator") : null}{row.names.join(t("search.nameSeparator"))}
                        </span>
                        )}
                      </span> : null}
                    </span>
                  </button>
                </li>
                );
              })
            )}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
