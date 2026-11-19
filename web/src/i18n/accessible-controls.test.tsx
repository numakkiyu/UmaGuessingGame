import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "@/components/ui/sheet";
import { Table, TableBody, TableCell, TableRow } from "@/components/ui/table";
import { changeLanguage, LANGUAGES } from ".";
import { common } from "./common";

afterEach(async () => { cleanup(); await changeLanguage("zh-Hans"); });

it("keeps table and sheet controls accessible in each language without closing the sheet", async () => {
  render(<>
    <Table><TableBody><TableRow><TableCell>123</TableCell></TableRow></TableBody></Table>
    <Sheet defaultOpen modal={false}><SheetContent>
      <SheetTitle>Test sheet</SheetTitle>
      <SheetDescription>Test description</SheetDescription>
    </SheetContent></Sheet>
  </>);
  for (const lng of LANGUAGES) {
    await act(async () => { await changeLanguage(lng); });
    expect(screen.getByRole("region", { name: common[lng].access.tableScroll }).tabIndex).toBe(0);
    expect(screen.getByRole("button", { name: common[lng].actions.close })).toBeTruthy();
    expect(screen.getByRole("dialog").getAttribute("data-state")).toBe("open");
  }
});

it("preserves a context-specific close label when the caller supplies one", async () => {
  render(<Sheet defaultOpen><SheetContent closeLabel="Custom close">
    <SheetTitle>Test sheet</SheetTitle><SheetDescription>Test description</SheetDescription>
  </SheetContent></Sheet>);
  await act(async () => { await changeLanguage("ja"); });
  expect(screen.getByRole("button", { name: "Custom close" })).toBeTruthy();
  expect(screen.queryByRole("button", { name: common.ja.actions.close })).toBeNull();
});
