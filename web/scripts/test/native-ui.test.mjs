import { createRequire } from "node:module";
import { test } from "node:test";
import assert from "node:assert/strict";
import { nativeUiRules } from "../native-ui-rules.mjs";
const require = createRequire(new URL("../../tools/toolchain/package.json", import.meta.url));
const { Linter } = require("eslint");
const linter = new Linter();
const check = code => linter.verify(code, {languageOptions:{ecmaVersion:2023,parserOptions:{ecmaFeatures:{jsx:true}},globals:{confirm:"readonly",alert:"readonly",prompt:"readonly"}},rules:nativeUiRules});
for (const code of ['window.confirm("end")', 'globalThis["alert"]("error")', 'self.prompt("name")', 'confirm("end")', 'form.reportValidity()', '<form onSubmit={save}/>', '<form noValidate={false}/>', '<button title="hint"/>', 'window.addEventListener("beforeunload", handler)', 'window.onbeforeunload = handler']) {
  test(`blocks native UI: ${code}`, () => assert.ok(check(code).some(m => m.severity === 2)));
}
for (const code of ['function confirm() { return true; } confirm()', '<Alert title="error"/>', '<form noValidate onSubmit={save}><input required/></form>', 'requestConfirmation(message, "finish", signal)']) {
  test(`allows themed UI: ${code}`, () => assert.deepEqual(check(code), []));
}
