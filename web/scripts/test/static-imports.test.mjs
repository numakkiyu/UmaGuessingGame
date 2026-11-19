import { test } from 'node:test';
import assert from 'node:assert/strict';
import { staticImports } from '../static-imports.mjs';

test('follows compressed bundler imports, side effects, and re-exports', () => {
  assert.deepEqual(staticImports('import{a as x}from"./a.js";import b from"../b.js";import"./effects.js";export{z}from"./z.js";export*from"./all.js";'), ['./a.js', '../b.js', './effects.js', './z.js', './all.js']);
});
test('ignores dynamic chunks, comments, examples and dependency preload strings', () => {
  assert.deepEqual(staticImports('const deps=["./async.js"];import("./async.js");const example=\'import"./example.js"\';/* import"./comment.js" */import{ok}from"./real.js";'), ['./real.js']);
});
test('supports ordinary declarations without counting package imports or CSS', () => {
  assert.deepEqual(staticImports('import { x } from "./x.js";\nimport React from "react";\nimport "./base.css";'), ['./x.js']);
});
