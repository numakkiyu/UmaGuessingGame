import { createRequire } from 'node:module';
const require = createRequire(new URL('../tools/toolchain/package.json', import.meta.url));
const ts = require('typescript');

/** Read actual static declarations, including minified import{a}from"./a.js".
 * Dynamic imports, quoted examples and comments do not block the first screen.
 */
export function staticImports(code) {
  const source = ts.createSourceFile('chunk.js', code, ts.ScriptTarget.Latest, false, ts.ScriptKind.JS);
  return source.statements.flatMap(statement => {
    if (!ts.isImportDeclaration(statement) && !ts.isExportDeclaration(statement)) return [];
    const specifier = statement.moduleSpecifier;
    if (!specifier || !ts.isStringLiteral(specifier)) return [];
    const path = specifier.text;
    return /^\.\.?\/.*\.js$/.test(path) ? [path] : [];
  });
}
