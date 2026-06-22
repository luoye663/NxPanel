# Security Policy

## Project Status

- This project includes privileged operations through `nxpanel-agent` and should be treated as security-sensitive software.
- Some code in this repository is developed or refined with AI assistance.
- Maintainers may review contributions, but no claim is made that the project has received a complete security audit.

## No Security Warranty

- This repository is provided as-is.
- The maintainers do not guarantee that the software is free from vulnerabilities or safe for production deployment in every environment.
- You are responsible for performing your own review, testing, hardening, and rollout controls before using this software in production.

## Reporting Vulnerabilities

- Please do not open a public issue for a suspected security vulnerability before contacting the maintainers.
- If no dedicated private reporting channel is available yet, open an issue asking for a private contact method without disclosing exploit details.

## Recommended Disclosure Content

- Affected version or commit
- Impact summary
- Reproduction steps
- Configuration prerequisites
- Suggested mitigation if known

## Hardening Recommendations

- Run `nxpanel-api` with the minimum required privileges.
- Restrict access to the API listener and agent socket.
- Rotate and protect all secrets, tokens, and certificates.
- Review allowed roots and path-related configuration carefully.
- Validate all production changes with `nginx -t` and rollback procedures.

---

# 安全说明（中文）

## 项目状态

- 本项目通过 `nxpanel-agent` 执行部分特权操作，因此应视为安全敏感软件。
- 本仓库中的部分代码由 AI 辅助生成或修订。
- 维护者可能会审查提交内容，但不声明本项目已经过完整安全审计。

## 不提供安全担保

- 本仓库按“现状”提供。
- 维护者不保证软件不存在漏洞，也不保证其在所有环境中都适合直接用于生产。
- 在生产环境使用前，你需要自行完成代码审阅、测试、加固、灰度与回滚预案。

## 漏洞报告

- 对疑似安全漏洞，请不要直接公开披露完整细节。
- 如果仓库暂时没有专门的私密提交渠道，可先开 issue 请求私下联系，并避免在公开页面贴出可利用细节。

## 建议提供的信息

- 受影响版本或 commit
- 影响范围摘要
- 复现步骤
- 所需配置前提
- 如已知，可提供缓解方案

## 加固建议

- `nxpanel-api` 应以最小必要权限运行。
- 限制 API 监听地址和 Agent Socket 的访问范围。
- 妥善保护并轮换 token、证书和其他敏感配置。
- 仔细审查允许路径白名单相关配置。
- 所有生产变更都应通过 `nginx -t` 验证，并具备回滚流程。
