import { fireEvent, screen, waitFor, within } from "@testing-library/react";

/** Close the error notice before continuing with the underlying game or form. */
export async function dismissFault() {
  const dialog = await screen.findByRole("alertdialog");
  fireEvent.click(within(dialog).getByRole("button", { name: /^(关闭|關閉|閉じる)$/ }));
  await waitFor(() => { if (screen.queryByRole("alertdialog")) throw new Error("Error notice is still open"); });
}
