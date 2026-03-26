"use client";

import { flexRender, getCoreRowModel, useReactTable } from "@tanstack/react-table";
import { motion } from "motion/react";
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
    cells: Record<string, { value: string; status: "correct" | "near" | "wrong" }>;
  }>;
};

export function GuessTable({ rows }: Props) {
  const data: GuessTableRow[] = rows.map((row) => ({
    displayName: row.displayName,
    avatarUrl: row.avatarUrl,
    star: row.cells.star,
    surface: row.cells.surface,
    distance: row.cells.distance,
    style: row.cells.style,
    sex: row.cells.sex,
    g1: row.cells.g1,
    g23: row.cells.g23,
    grade: row.cells.grade,
    dormitory: row.cells.dormitory,
  }));

  // TanStack Table exposes imperative helpers here; memoization warnings are expected.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <section className="overflow-hidden rounded-[28px] border border-[var(--color-line)] bg-[var(--color-panel)] shadow-[var(--shadow-panel)]">
      <div className="border-b border-[var(--color-line)] px-4 py-4 sm:px-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <p className="text-xs font-semibold uppercase tracking-[0.24em] text-[var(--color-brand-strong)]">
              结果表
            </p>
            <h2 className="mt-2 font-[var(--font-display)] text-2xl font-bold">猜测记录</h2>
            <p className="mt-1 text-sm leading-6 text-[var(--color-muted)]">
              表格里显示的是你这一猜本身的数据，颜色只负责告诉你离答案有多近。
            </p>
          </div>
          <div className="rounded-full bg-[var(--color-panel-soft)] px-3 py-1.5 text-sm font-medium text-[var(--color-ink)]">
            已留下 {rows.length} 行线索
          </div>
        </div>
      </div>

      <div className="border-b border-[var(--color-line)] bg-[rgba(255,255,255,0.56)] px-4 py-3 text-sm text-[var(--color-muted)] sm:px-5">
        最新一行就是你刚刚那一猜。手机上如果看不全，直接左右滑动表格就好。
      </div>

      <div className="overflow-x-auto overscroll-x-contain">
        <table className="min-w-[1080px] border-separate border-spacing-0">
          <thead className="sticky top-0 z-20 bg-[var(--color-panel-strong)]">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const isNameColumn = header.column.id === "displayName";
                  return (
                    <th
                      key={header.id}
                      className={[
                        "border-b border-[var(--color-line)] px-3 py-3 text-left text-xs font-semibold uppercase tracking-[0.12em] text-[var(--color-muted)]",
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
                          className="sticky left-0 z-10 border-b border-[var(--color-line)] bg-[var(--color-panel)] px-3 py-3"
                        >
                          <div className="flex items-center gap-3">
                            <div className="flex size-11 items-center justify-center overflow-hidden rounded-2xl border border-[var(--color-line)] bg-white">
                              {/* eslint-disable-next-line @next/next/no-img-element */}
                              <img
                                src={row.original.avatarUrl}
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
                    if (typeof guessCell === "string") {
                      return null;
                    }
                    return (
                      <td key={cell.id} className="border-b border-[var(--color-line)] px-2 py-2 align-top">
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
