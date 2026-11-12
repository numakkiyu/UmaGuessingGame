// Resolve ESLint plugins from the TS 6 toolchain; typescript-eslint does not support the application's TS 7 compiler
import { createRequire } from "node:module";
import { nativeUiRules } from "./scripts/native-ui-rules.mjs";

const toolchainRequire = createRequire(new URL("./tools/toolchain/package.json", import.meta.url));

const load = async (specifier) => {
  const mod = await import(`file://${toolchainRequire.resolve(specifier)}`);
  // 这些包有的是 ESM default、有的是 CJS 具名，统一取 default 优先
  return mod.default ?? mod;
};

const js = await load("@eslint/js");
const globals = await load("globals");
const tseslint = await load("typescript-eslint");
const reactHooks = await load("eslint-plugin-react-hooks");

export default tseslint.config(
  {
    ignores: ["dist/**", "node_modules/**", "src/lib/api/schema.d.ts"],
  },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2023,
      globals: { ...globals.browser, ...globals.node },
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // 避免 any、双重断言和手写重复 API 类型。
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/consistent-type-imports": [
        "error",
        { prefer: "type-imports", fixStyle: "inline-type-imports" },
      ],
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_" },
      ],
      "no-console": ["error", { allow: ["warn", "error"] }],
      eqeqeq: ["error", "always"],
      "prefer-const": "error",
    },
  },
  { files: ["src/**/*.{ts,tsx}"], ignores: ["**/*.test.{ts,tsx}", "src/test/**"], rules: nativeUiRules },
  {
    // 倒计时类 hook 必须在 effect 里同步 setState 对齐首帧。
    //
    // react-hooks/set-state-in-effect 针对的是「effect 里 setState 触发额外渲染」的
    // 反模式，通常可以用 useMemo 或派生值消除。但倒计时的值是 Date.now() 的函数，
    // 渲染期调用 Date.now() 会被 react-hooks/purity 判为 impure（试过，报
    // "Cannot call impure function during render"）。两个规则同时满足时无解，
    // 只能在最小范围内关掉后者。
    //
    // 范围限定到两个倒计时 hook 文件，不放宽全项目。
    files: [
      "src/lib/hooks/use-countdown.ts",
      // 邻居查询：进入/离开终点时要把 neighbors 清空并复位错误，这是对服务端状态的
      // 本地镜像重置，没有可派生的等价写法。
      "src/features/modes/co-race-path-round-page.tsx",
      // 实时连接：禁用或换房间时要把 status 置回 offline 并清掉 epoch，同理。
      "src/lib/realtime/use-realtime.ts",
    ],
    rules: {
      "react-hooks/set-state-in-effect": "off",
    },
  },
  {
    // 测试与构建脚本放宽 node/console 限制。
    files: ["**/*.test.{ts,tsx}", "scripts/**/*.mjs", "src/test/**/*.{ts,tsx}"],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "no-console": "off",
      "@typescript-eslint/no-non-null-assertion": "off",
    },
  },
  {
    // 纯 Node 脚本（.mjs）：Node globals，允许 console
    files: ["scripts/**/*.mjs", "tools/**/*.mjs"],
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "no-console": "off",
    },
  },
  {
    // 浏览器审查脚本：page.evaluate 回调在页面上下文执行，
    // 因此同时需要 browser globals；这是真实的双环境脚本。
    files: ["scripts/audit-*.mjs", "scripts/e2e-*.mjs"],
    languageOptions: {
      globals: { ...globals.node, ...globals.browser },
    },
    rules: {
      "no-console": "off",
      // 用 {4} 显式表达空格数，比字面空格更易读
      "no-regex-spaces": "error",
    },
  },
);
