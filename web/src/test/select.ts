import { fireEvent, screen } from "@testing-library/react";

/** Operate the visible Radix list, rather than mutating a browser select value. */
export async function chooseSelect(trigger: HTMLElement, value: string) {
  fireEvent.keyDown(trigger, { key: "ArrowDown" });
  const options = await screen.findAllByRole("option");
  const target = options.find(option => option.getAttribute("data-value") === value);
  if (!target) throw new Error(`Select choice unavailable: ${value}`);
  fireEvent.click(target);
}
