# 按变更范围执行 CI

`.github/workflows/build.yml` 始终在 main push、目标为 main 的 PR 和手动运行时触发。
`changes` 使用 `dorny/paths-filter` 对比变更文件，决定是否执行 Java、Vue 和 MySQL/Webhook E2E。
未识别的新路径会保守执行全部三项检查，避免漏测。

| 变更范围 | Java | Vue | E2E |
| --- | --- | --- | --- |
| `config-center-admin/**` | 跳过 | 运行 | 跳过 |
| `config-center/**`、`config-center-access/**` | 运行 | 跳过 | 运行 |
| Gradle 构建文件和 Wrapper | 运行 | 跳过 | 运行 |
| `scripts/**`、`deploy/**` | 跳过 | 跳过 | 运行 |
| `.github/**`、`.gitignore` 或未识别路径 | 运行 | 运行 | 运行 |
| 仅 `README.md`、`docs/**` | 跳过 | 跳过 | 跳过 |
| 手动运行 `workflow_dispatch` | 运行 | 运行 | 运行 |

混合修改取并集，例如同时修改前后端时，三个构建都会执行。

## CI Gate

`CI Gate` 使用 `always()` 运行，严格校验：

- 变更检测自身必须成功。
- 需要执行的 Job 必须返回 `success`。
- 不需要执行的 Job 必须返回 `skipped`。
- 任何检测异常、非预期跳过或执行失败都导致 Gate 失败。

如需在 GitHub 分支保护 / Ruleset 中设置必需检查，请设置 **`CI Gate`**，
不要把会按变更范围跳过的 `java`、`admin`、`MySQL + multi-instance + Webhook E2E`
作为必需检查，否则可能产生不必要的合并限制。

## 验收建议

1. 仅修改前端：应只运行 Vue 与 CI Gate。
2. 仅修改后端或 SQL：应运行 Java、E2E 与 CI Gate。
3. 仅修改文档：应只有变更检测与 CI Gate。
4. 修改 `.github/**`、混合改动或未知路径：应执行完整回归。
5. 手动触发 Build：应执行全部任务。
6. 任一应执行的任务失败时，CI Gate 必须失败。

修改模块依赖关系或增加目录时，应同时更新 `build.yml` 的筛选规则与此文档。
