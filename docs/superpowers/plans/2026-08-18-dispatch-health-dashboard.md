# 调度情况首页展示实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在主页按时间窗口展示网关组下各路由的失败率和平均首字时间。

**Architecture:** 后端在现有 `GatewayUsageLogs` 仓储中新增一次 SQL 聚合，按 `gateway_group_id, route_id` 统计窗口内的路由尝试，并批量补齐当前网关组/路由名称；管理 API 暴露一个批量端点。前端通过现有查询刷新机制请求该端点，在首页用独立面板和窗口切换按钮呈现分组路由指标。

**Tech Stack:** Go、Gin、GORM/SQLite 测试、React、TypeScript、Tailwind、现有 `apiFetch`/`useApi` 查询模式。

---

### Task 1: 后端聚合数据结构与仓储方法

**Files:**
- Modify: `backend/storage/gateway.go`
- Test: `backend/storage/storage_test.go`

- [ ] **Step 1: Write the failing test**

  在 `storage_test.go` 使用 `openTestDB` 写入两个网关组、三条路由和窗口内外的 `GatewayUsageLog`，调用 `NewGatewayUsageLogs(db).DispatchStats(from, to)`，断言：窗口外记录被排除；同一请求的失败/成功尝试分别计数；失败率按失败数/总数；空 `FirstTokenMS` 不进入均值；结果按组和路由稳定排序；没有日志时返回空组列表。

- [ ] **Step 2: Run the focused test and verify it fails**

  Run `go test ./backend/storage -run TestGatewayUsageDispatchStats -count=1`。
  Expected: 编译失败，提示 `DispatchStats` 未定义。

- [ ] **Step 3: Implement the minimal aggregation**

  在 `gateway.go` 定义 `GatewayDispatchStatsGroup`、`GatewayDispatchStatsRoute`，并实现 `DispatchStats(from, to time.Time) ([]GatewayDispatchStatsGroup, error)`：

  ```go
  type GatewayDispatchStatsGroup struct {
      GatewayGroupID uint                         `json:"gateway_group_id"`
      GatewayGroupName string                     `json:"gateway_group_name"`
      Routes          []GatewayDispatchStatsRoute `json:"routes"`
  }
  type GatewayDispatchStatsRoute struct {
      RouteID uint `json:"route_id"`
      RouteName string `json:"route_name"`
      ProviderName string `json:"provider_name,omitempty"`
      TotalAttempts int64 `json:"total_attempts"`
      FailedAttempts int64 `json:"failed_attempts"`
      FailureRate float64 `json:"failure_rate"`
      FirstTokenSamples int64 `json:"first_token_samples"`
      AverageFirstTokenMS *float64 `json:"average_first_token_ms"`
  }
  ```

  查询 `created_at >= from AND created_at < to`，使用 `COUNT(*)`、`SUM(CASE WHEN success = false THEN 1 ELSE 0 END)`、`COUNT(first_token_ms)` 和 `AVG(first_token_ms)`，按 ID 排序；当前路由找不到时使用 `route #<id>`，网关组找不到时使用 `group #<id>`。

- [ ] **Step 4: Run the focused test and verify it passes**

  Run `go test ./backend/storage -run TestGatewayUsageDispatchStats -count=1`，Expected: PASS。

- [ ] **Step 5: Commit**

  `git add backend/storage/gateway.go backend/storage/storage_test.go && git commit -m "feat: aggregate gateway dispatch health"`

### Task 2: 管理 API 与窗口校验

**Files:**
- Modify: `backend/api/gateway_admin.go`
- Test: `backend/api/gateway_admin_test.go` 或现有网关 API 测试文件

- [ ] **Step 1: Write the failing handler test**

  构造带网关用量仓储的 Gin 测试路由，调用 `GET /api/gateway/dispatch/stats?window=5m`，断言响应包含 `window/from/to/groups`；再调用 `window=2m`，断言返回 400。

- [ ] **Step 2: Run the focused test and verify it fails**

  Run `go test ./backend/api -run TestGatewayDispatchStats -count=1`，Expected: 路由不存在或 handler 未定义。

- [ ] **Step 3: Implement endpoint and whitelist parser**

  在 `registerGatewayAdmin` 注册 `GET /gateway/dispatch/stats`。添加 `parseDispatchWindow`，支持 `1m,5m,30m,1h,4h,8h,12h,24h`，空值默认 `5m`，非法值返回 400；handler 用 `time.Now().UTC()` 计算 `[from,to)`，调用仓储方法并返回：

  ```go
  gin.H{"window": window, "from": from, "to": to, "groups": groups}
  ```

- [ ] **Step 4: Run the focused test and verify it passes**

  Run `go test ./backend/api -run TestGatewayDispatchStats -count=1`，Expected: PASS。

- [ ] **Step 5: Commit**

  `git add backend/api/gateway_admin.go backend/api/gateway_admin_test.go && git commit -m "feat: expose gateway dispatch stats"`

### Task 3: 前端类型、查询与纯函数

**Files:**
- Modify: `frontend/lib/api-types.ts`
- Modify: `frontend/lib/queries.ts`
- Create: `frontend/components/monitor/dispatch-health-utils.ts`
- Create: `frontend/components/monitor/dispatch-health-utils.test.ts`

- [ ] **Step 1: Write failing utility tests**

  覆盖时间窗口常量顺序、`formatFailureRate(0.123)` 返回 `12.3%`、高失败率分级、`formatFirstToken(null)` 返回 `暂无数据`、浮点毫秒四舍五入。

- [ ] **Step 2: Run tests and verify they fail**

  Run `node --test --experimental-strip-types frontend/components/monitor/dispatch-health-utils.test.ts`，Expected: 模块/函数未定义。

- [ ] **Step 3: Implement types and query hook**

  在 `api-types.ts` 增加响应接口；在 `queries.ts` 增加 `useGatewayDispatchStats(window)`，使用 `useApi` 请求 `/gateway/dispatch/stats?window=...`；窗口类型固定为八个白名单值。

- [ ] **Step 4: Implement utilities and run tests**

  实现格式化与颜色分级纯函数，运行同一 Node 测试命令，Expected: PASS。

- [ ] **Step 5: Commit**

  `git add frontend/lib/api-types.ts frontend/lib/queries.ts frontend/components/monitor/dispatch-health-utils.ts frontend/components/monitor/dispatch-health-utils.test.ts && git commit -m "feat: add dispatch health query types"`

### Task 4: 首页调度情况面板

**Files:**
- Create: `frontend/components/monitor/dispatch-health-panel.tsx`
- Modify: `frontend/app/page.tsx`

- [ ] **Step 1: Implement the panel against the typed hook**

  使用 `useState<DispatchWindow>("5m")` 和 `useGatewayDispatchStats(window)`；用单选按钮展示八个窗口；按后端 `groups` 渲染每组表格，列出路由、尝试次数、失败率、平均首字时间。加载/错误/空结果分别显示占位、错误文本和“暂无调度记录”。

- [ ] **Step 2: Mount and run lint**

  在 `frontend/app/page.tsx` 的 `KpiRow` 后挂载 `<DispatchHealthPanel />`，执行 `pnpm --dir frontend lint`，Expected: 无 lint 错误。

- [ ] **Step 3: Build and commit**

  执行 `pnpm --dir frontend build`，Expected: 构建成功；然后提交：
  `git add frontend/components/monitor/dispatch-health-panel.tsx frontend/app/page.tsx && git commit -m "feat: show dispatch health on dashboard"`

### Task 5: 全量验证与本地手动检查

**Files:**
- No new files.

- [ ] **Step 1: Run backend tests**

  `go test ./...`，Expected: 所有包 PASS。

- [ ] **Step 2: Run frontend checks**

  `pnpm --dir frontend lint && pnpm --dir frontend build`，Expected: 两条命令均成功。

- [ ] **Step 3: Verify UI manually**

  启动本地前后端，打开主页，确认默认 5 分钟、切换八个窗口会刷新、网关组和路由数据不重叠、无数据和错误状态可见。

- [ ] **Step 4: Commit any verification-only fixes**

  若手动检查发现布局或类型问题，先补对应测试再修复，并以 `fix: polish dispatch health dashboard` 提交。
