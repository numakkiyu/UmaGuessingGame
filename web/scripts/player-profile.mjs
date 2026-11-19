/** Choose the shared nickname through the same profile editor used by players. */
export async function setPlayerName(page, name) {
  await page.locator('.uma-trainer-trigger:visible').click();
  const dialog = page.getByRole('dialog');
  await dialog.locator('#trainer-name').fill(name);
  await dialog.getByRole('button', { name: '就这样出发', exact: true }).click();
  await dialog.waitFor({ state: 'hidden' });
}
