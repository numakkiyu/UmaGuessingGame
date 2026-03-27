"use client";

import { useMemo } from "react";
import { flexRender, getCoreRowModel, useReactTable } from "@tanstack/react-table";
import { motion } from "motion/react";
import { AvatarImage } from "@/components/avatar-image";
import { StatusCell } from "@/components/status-cell";

const columns = [
  { accessorKey: "displayName", header: "角色" },
  { accessorKey: "star", header: "星级" },
  { accessorKey: "surface", header: "场地" },
  { accessorKey: "distance", header: "距离" },
  { accessorKey: "style", header: "跑法" },
  { accessorKey: "sex", header: "牡/牝" },
  { accessorKey: "g1", header: "GI档" },
  { accessorKey: "g23", header: "GII/GIII档" },
  { accessorKey: "grade", header: "学年" },
  { accessorKey: "dormitory", header: "宿舍" },
];

type GuessCell = {
  value: string;
  status: "correct" | "near" | "wrong";
};

type GuessTableRow = {
  displayName: string;
  avatarUrl: string;
  avatarFallbackUrl?: string | null;
  star: GuessCell;
  surface: GuessCell;
  distance: GuessCell;
  style: GuessCell;
  sex: GuessCell;
  g1: GuessCell;
  g23: GuessCell;
  grade: GuessCell;
  dormitory: GuessCell;
};

type Props = {
  rows: Array<{
    characterId: string;
    displayName: string;
    avatarUrl: string;
    avatarFallbackUrl?: string | null;
    cells: Record<string, { value: string; status: "correct" | "near" | "wrong" }>;
  }>;
};

export function GuessTable({ rows }: Props) {
  const data: GuessTableRow[] = useMemo(() => rows.map((row) => ({
    displayName: row.displayName,
    avatarUrl: row.avatarUrl,
    avatarFallbackUrl: row.avatarFallbackUrl ?? null,
    star: row.cells.star,
    surface: row.cells.surface,
    distance: row.cells.distance,
    style: row.cells.style,
    sex: row.cells.sex,
    g1: row.cells.g1,
    g23: row.cells.g23,
    grade: row.cells.grade,
    dormitory: row.cells.dormitory,
  })), [rows]);

  const coreRowModel = useMemo(() => getCoreRowModel<GuessTableRow>(), []);

  // TanStack Table is the intended table state source here.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: coreRowModel,
  });

  return (
    <section className="guess-table overflow-hidden rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel)] shadow-[var(--shadow-panel)]">
      <div className="guess-table-head border-b border-[var(--color-line)] bg-[linear-gradient(180deg,rgba(239,209,172,0.18),rgba(255,250,242,0))] px-4 py-4 sm:px-5">
        <div className="guess-table-head-inner flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
              线索记录
            </p>
            <h2 className="mt-2 font-[var(--font-display)] text-2xl font-bold">猜测记录</h2>
            <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
              每一行都是你刚刚猜的那位马娘。先看左边几列，再顺着颜色把范围继续缩小。
            </p>
          </div>
          <div className="rounded-full border border-[rgba(104,79,48,0.1)] bg-[var(--color-panel-soft)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
            已留下 {rows.length} 行线索
          </div>
        </div>
      </div>
      <div className="guess-table-scroll overflow-x-scroll overscroll-x-contain [touch-action:pan-x] [-webkit-overflow-scrolling:touch] bg-[linear-gradient(180deg,rgba(255,255,255,0.18),rgba(255,255,255,0))]">
        <table className="min-w-[980px] border-separate border-spacing-0 sm:min-w-[1080px]">
          <thead className="sticky top-0 z-20 bg-[var(--color-panel-strong)]">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const isNameColumn = header.column.id === "displayName";
                  return (
                    <th
                      key={header.id}
                      className={[
                        "border-b border-[var(--color-line)] px-3 py-3 text-center text-xs font-semibold uppercase tracking-[0.12em] text-[var(--color-muted)]",
                        isNameColumn ? "sticky left-0 z-30 bg-[var(--color-panel-strong)]" : "",
                      ].join(" ")}
                    >
                      {flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {table.getRowModel().rows.length === 0 ? (
              <tr>
                <td
                  colSpan={columns.length}
                  className="px-4 py-10 text-center text-sm text-[var(--color-muted)]"
                >
                  开始新一局后，先在上方输入一位马娘。
                </td>
              </tr>
            ) : null}
            {table.getRowModel().rows.map((row, index) => {
              const isLatestRow = index === table.getRowModel().rows.length - 1;
              return (
                <motion.tr
                  key={row.id}
                  initial={isLatestRow ? { opacity: 0, y: 12 } : false}
                  animate={{ opacity: 1, y: 0 }}
                  className={isLatestRow ? "bg-[rgba(239,209,172,0.1)]" : ""}
                >
                  {row.getVisibleCells().map((cell) => {
                    const key = cell.column.id;
                    if (key === "displayName") {
                      return (
                        <td
                          key={cell.id}
                          className="sticky left-0 z-10 border-b border-[var(--color-line)] bg-[var(--color-panel)] px-3 py-3 shadow-[6px_0_16px_rgba(245,239,226,0.96)]"
                        >
                          <div className="flex min-w-[140px] items-center gap-3">
                            <div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-2xl border border-[var(--color-line)] bg-white sm:size-11">
                              <AvatarImage
                                primarySrc={row.original.avatarUrl}
                                proxySrc={row.original.avatarUrl}
                                remoteSrc={row.original.avatarFallbackUrl}
                                alt={row.original.displayName}
                                className="size-full object-cover"
                              />
                            </div>
                            <div className="min-w-0">
                              <span className="block truncate font-semibold text-[var(--color-ink)]">
                                {row.original.displayName}
                              </span>
                              {isLatestRow ? (
                                <span className="mt-1 inline-flex rounded-full bg-[rgba(200,108,53,0.12)] px-2 py-0.5 text-xs font-medium text-[var(--color-brand-strong)]">
                                  最新一猜
                                </span>
                              ) : null}
                            </div>
                          </div>
                        </td>
                      );
                    }

                    const guessCell = row.original[key as keyof GuessTableRow];
                    if (
                      !guessCell ||
                      typeof guessCell === "string" ||
                      !("value" in guessCell) ||
                      !("status" in guessCell)
                    ) {
                      return null;
                    }
                    return (
                      <td key={cell.id} className="border-b border-[var(--color-line)] px-2 py-2 align-middle">
                        <StatusCell
                          value={guessCell.value}
                          status={guessCell.status}
                          emphasized={isLatestRow}
                        />
                      </td>
                    );
                  })}
                </motion.tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </section>
  );
}
