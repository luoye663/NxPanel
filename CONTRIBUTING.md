# Contributing

## Scope

- This project manages Nginx/OpenResty configuration and files, including privileged operations executed by `nxpanel-agent`.
- Changes that affect authentication, file access, path validation, Nginx rendering, or agent transactions should be treated as security-sensitive.

## Development Setup

- Go 1.25+
- Node 20+ and `pnpm`
- Nginx or OpenResty for integration scenarios

Common commands:

```bash
make test
make vet
cd webapp-react && pnpm type-check
```

If your change affects the production frontend bundle or route entry points, also run:

```bash
cd webapp-react && pnpm build
```

## How To Contribute

- Open an issue before large refactors or behavior changes.
- Keep pull requests small and focused.
- Preserve existing API contracts unless the change is intentional and documented.
- Follow the existing layering pattern: handler -> service -> repo.
- Prefer the smallest correct change.

## Code Requirements

- Do not bypass `PathPolicy`, `security.CleanAbsWithin`, or other path validation logic.
- Do not replace marker-based incremental config updates with full file rewrites for managed site configs.
- Do not expose secrets, private keys, tokens, or sensitive file contents in logs or API responses.
- For JSON handlers, prefer the shared request decode helpers in `internal/api/request.go`.

## Testing Requirements

- Backend changes must pass `make test` and `make vet`.
- Add or update tests for bug fixes and behavior changes.
- Security-sensitive changes should include regression coverage where practical.

## Commit and PR Guidance

- Enable the repository hooks once after cloning with `make setup-git-hooks`.
- Use `type(scope): subject` for commit titles; use `type(scope)!: subject` for breaking changes.
- The scope is required and uses lowercase letters, digits, `.`, `_`, `/`, or `-`.
- Allowed types are `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, and `revert`.
- For example: `fix(web): correct the failure timeout label`.
- Git-generated `Merge`, `Revert`, `fixup!`, and `squash!` messages are accepted.
- Describe behavior changes, migration impact, and test results in the pull request.
- If a change affects security boundaries, call that out explicitly in the PR description.

## AI-Assisted Contributions

- AI-assisted patches are allowed.
- The contributor submitting the change is responsible for reviewing correctness, safety, and licensing implications before submission.

---

# 贡献说明（中文）

## 适用范围

- 本项目用于管理 Nginx/OpenResty 配置与文件，部分能力通过 `nxpanel-agent` 以 root 权限执行。
- 任何涉及认证、文件访问、路径校验、Nginx 渲染、Agent 事务的改动，都应视为安全敏感改动。

## 开发环境

- Go 1.25+
- Node 20+ 与 `pnpm`
- 如需完整集成验证，建议准备 Nginx 或 OpenResty 环境

常用命令：

```bash
make test
make vet
cd webapp-react && pnpm type-check
```

如果改动影响前端生产产物、路由入口或打包行为，还应执行：

```bash
cd webapp-react && pnpm build
```

## 贡献方式

- 大改动或行为变更建议先提 issue 再开工。
- 尽量保持 PR 小而聚焦。
- 除非是明确设计决策，否则不要随意破坏现有 API 契约。
- 遵循现有分层模式：handler -> service -> repo。
- 优先选择最小且正确的改动。

## 代码要求

- 不要绕过 `PathPolicy`、`security.CleanAbsWithin` 或其他路径安全校验逻辑。
- 不要把受管站点配置的 marker 增量更新改成整文件重写。
- 不要在日志或 API 响应中暴露密钥、私钥、token 或敏感文件内容。
- 对 JSON handler，优先使用 `internal/api/request.go` 中的共享解码 helper。

## 测试要求

- 后端改动必须通过 `make test` 和 `make vet`。
- Bug 修复和行为变更应补充或更新测试。
- 安全敏感改动应尽量附带回归测试。

## Commit 与 PR 建议

- 克隆仓库后执行一次 `make setup-git-hooks`，启用仓库内置 Git hooks。
- commit 标题使用 `type(scope): 内容`；破坏性变更使用 `type(scope)!: 内容`。
- 功能区 scope 必填，只能使用小写英文字母、数字、`.`、`_`、`/` 或 `-`。
- type 允许 `feat`、`fix`、`docs`、`style`、`refactor`、`perf`、`test`、`build`、`ci`、`chore` 和 `revert`。
- 例如：`fix(web): 修复失败超时输入框描述`。
- Git 生成的 `Merge`、`Revert`、`fixup!` 和 `squash!` 提交信息允许作为例外。
- PR 描述中说明行为变化、迁移影响和测试结果。
- 若改动涉及安全边界，请在 PR 描述中显式说明。

## AI 辅助贡献

- 允许使用 AI 辅助生成 patch。
- 最终提交代码的贡献者需要自行承担正确性、安全性和许可证合规性审查责任。
