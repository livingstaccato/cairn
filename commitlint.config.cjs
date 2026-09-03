// Deliberately .cjs: there is no package.json here declaring "type": "module",
// so Node loads a bare .js as CommonJS and an `export default` config silently
// parses as empty — commitlint then rejects every message with [empty-rules].
module.exports = { extends: ['@commitlint/config-conventional'] };
