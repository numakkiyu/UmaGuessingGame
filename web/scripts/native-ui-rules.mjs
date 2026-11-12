// Shared by ESLint and its regression fixtures. Local functions named confirm
// remain valid; only browser globals and their qualified forms are prohibited.
const names = ["alert", "confirm", "prompt"];
export const nativeUiRules = {
  "no-restricted-globals": ["error", ...names.map(name => ({name, message: "Use the themed confirmation dialog or an inline notice"}))],
  "no-restricted-properties": ["error", ...["window", "globalThis", "self"].flatMap(object => names.map(property => ({object, property, message: "Use the themed confirmation dialog"}))),
    {property:"reportValidity",message:"Show validation errors inside the page"}],
  "no-restricted-syntax": ["error",
    {selector: "JSXOpeningElement[name.name='form']:not(:has(JSXAttribute[name.name='noValidate'][value=null]))", message: "Forms must use noValidate and inline validation"},
    {selector: "JSXOpeningElement[name.name=/^[a-z]/] > JSXAttribute[name.name='title']", message: "Use the themed Tooltip instead of a native title tooltip"},
    {selector: "CallExpression[callee.property.name='addEventListener'][arguments.0.value='beforeunload']", message: "Do not trigger browser leave-page prompts"},
    {selector: "AssignmentExpression[left.property.name='onbeforeunload']", message: "Do not trigger browser leave-page prompts"},
    {selector: "JSXAttribute[name.name='onBeforeUnload']", message: "Do not trigger browser leave-page prompts"},
  ],
};
