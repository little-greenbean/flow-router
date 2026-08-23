# 调度趋势实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 在本地 upstream-ops 中实现基于真实日志聚合的双层网关/路由调度趋势与丰富 mock 数据。

**Architecture:** 后端新增趋势查询方法，按请求链计算 TTFT 分位数、质量和吞吐量；前端使用 ECharts 双图和共享 dataZoom；seed 脚本生成可重复的 24 小时调度链。

**Tech Stack:** Go、Gin、GORM、SQLite/MySQL、React、ECharts、TypeScript、shell/sqlite3。

### Task 1: 后端趋势聚合

**Files:** `backend/storage/gateway.go`, `backend/storage/storage_test.go`

- [x] 增加趋势响应类型和 `DispatchTrend` 方法，读取窗口内必要日志，按网关、路由、时间桶聚合。
- [x] 以独立 `request_id` 计算最终失败、顺延触发和顺延恢复。
- [x] 添加测试覆盖多个尝试链、分位数、空窗口和桶边界。

### Task 2: API 契约

**Files:** `backend/api/gateway_admin.go`, `backend/api/gateway_dispatch_test.go`, `frontend/lib/api-types.ts`, `frontend/lib/queries.ts`

- [x] 新增 `/gateway/dispatch/trends?from=&to=&bucket=`，限制窗口和桶参数，返回网关与路由序列。
- [x] 增加 TypeScript 类型和查询 hook，保留旧 stats API。

### Task 3: ECharts 面板

**Files:** `frontend/package.json`, `frontend/pnpm-lock.yaml`, `frontend/components/monitor/dispatch-health-panel.tsx`

- [x] 引入 ECharts，替换卡片式汇总为网关趋势和路由趋势双图。
- [x] 实现 dataZoom 自动粒度、曲线/图例高亮、网关到路由联动和每分钟刷新。
- [x] 保留加载、错误和空数据状态。

### Task 4: 本地 mock

**Files:** `scripts/seed-dispatch-mock.sh`

- [x] 扩展到至少 4 个网关组和每组 3 条路由。
- [x] 生成过去 24 小时每分钟多请求，包含主请求、失败、retry/failover 成功链和不同 TTFT。
- [x] 保证重复执行只清理专用 mock 标记数据。

### Task 5: 验证

- [x] 运行 Go 测试、前端 lint/build。
- [x] 执行 seed 并请求趋势接口，验证点数、指标和路由数量。
- [x] 浏览器验证 dataZoom、网关联动、路由图例和一分钟刷新。
