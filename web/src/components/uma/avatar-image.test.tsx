import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { AvatarImage } from "./avatar-image";

afterEach(cleanup);
it("a reused avatar tries the new character after a previous image exhausted its fallbacks",()=>{
  const view=render(<AvatarImage characterId="1001" src="/assets/avatars/1001.webp" remoteSrc="https://example.test/1001.png" alt="first"/>);
  fireEvent.error(screen.getByRole("img"));expect(screen.getByRole("img").getAttribute("src")).toBe("/assets/avatars/1001.webp");fireEvent.error(screen.getByRole("img"));expect(screen.getByRole("img").getAttribute("src")).toContain("party-logo");
  view.rerender(<AvatarImage characterId="1002" src="/assets/avatars/1002.webp" remoteSrc="https://example.test/1002.png" alt="second"/>);expect(screen.getByRole("img").getAttribute("src")).toBe("https://example.test/1002.png");fireEvent.error(screen.getByRole("img"));expect(screen.getByRole("img").getAttribute("src")).toBe("/assets/avatars/1002.webp");expect(screen.getByRole("img").getAttribute("alt")).toBe("second");
});
