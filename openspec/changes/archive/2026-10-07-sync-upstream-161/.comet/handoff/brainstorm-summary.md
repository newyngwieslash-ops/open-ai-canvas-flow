# Brainstorm Summary

- Change: sync-upstream-161
- Date: 2026-10-07

## Confirmed Technical Approach

已确认（2026-10-07）：方案 A git merge + 10 文件裁决表；welcome 取本地动态品牌 effect

- 方案 A（推荐）：git merge upstream/main —— 历史连通（merge-base 125864f6），一个合并提交保留双方语义，后续同步零成本
- 方案 B：format-patch + am -3 —— 为历史不连通设计，本项目不适用
- 方案 C：手工 cherry-pick 内容 —— 只适合挑选个别功能

## Key Trade-offs and Risks

- 10 个双改文件逐个语义裁决（双方 diff 已全部核实，见 Design Doc 裁决表）
- welcome/index.tsx 是唯一语义级交织：上游删除硬编码 title 行（v1.6.1 修复 SEO 标题），本地把该行改为动态品牌 effect —— 裁决取本地动态 effect（语义为上游删除 + 本地增强的并集）
- user-data-sync.ts：上游改头部 import 与生成资产默认值，本地加 acknowledgeRemoteAssets 与冲突检测 —— hunk 级并存
- 其余 8 文件双方改动区域不相交或仅行级相邻

## Testing Strategy

- cd backend && go test ./...
- cd web && bun run build
- 迁移验证：数据目录启动确认 schema_migrations → v47（或迁移测试）
- 完整性抽查：c98c9b39（acknowledgeRemoteAssets）、853c7d2c（Midjourney 配置）对应文件本地改动仍在

## Spec Patches

None（delta spec 四条需求已覆盖设计发现，无需补场景）
