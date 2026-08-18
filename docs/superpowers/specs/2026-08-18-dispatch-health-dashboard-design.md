# 调度情况首页展示设计

## 目标

在主页增加“调度情况”面板，按可选时间窗口展示每个网关组下每条路由的请求失败率与平均首字时间，帮助快速判断故障转移和上游响应质量。

## 指标口径

- 统计对象是 `gateway_usage_logs` 中的每一次路由尝试，而不是去重后的客户端请求。
- 路由失败率 = `success=false` 的尝试数 / 该路由全部尝试数。
- 一次请求发生重试或故障转移时，每条被尝试的路由分别计数；前一路由失败、后一路由成功不会互相抵消。
- 平均首字时间只对 `first_token_ms` 非空且大于等于 0 的日志求平均；没有样本时返回空值，前端显示“暂无数据”。
- 时间窗口以服务端当前时间为终点，使用 `created_at >= now-window`；允许的窗口为 1m、5m、30m、1h、4h、8h、12h、24h。
- 不受窗口内请求数量限制；空窗口返回空列表而不是错误。

## 后端设计

新增管理接口：

```text
GET /api/gateway/dispatch/stats?window=5m
```

响应：

```json
{
  "window": "5m",
  "from": "2026-08-18T10:00:00Z",
  "to": "2026-08-18T10:05:00Z",
  "groups": [
    {
      "gateway_group_id": 2,
      "gateway_group_name": "GPT Pro 网关组",
      "routes": [
        {
          "route_id": 11,
          "route_name": "主路由",
          "provider_name": "Provider A",
          "total_attempts": 20,
          "failed_attempts": 2,
          "failure_rate": 0.1,
          "first_token_samples": 18,
          "average_first_token_ms": 842.5
        }
      ]
    }
  ]
}
```

实现放在 `storage.GatewayUsageLogs` 的独立聚合方法中，通过 SQL `GROUP BY gateway_group_id, route_id` 计算尝试数、失败数、首字样本数和首字均值；再批量加载网关组与路由名称。日志中已保存 provider 快照，路由名称优先使用当前路由配置，找不到时使用 `route #<id>`，确保历史日志仍可定位。

接口只接受白名单窗口值，缺省使用 5m，非法值返回 400。数据库错误返回 500。注册位置与现有 `/api/gateway/usage/*` 一致，并保持鉴权。

## 前端设计

新增 `DispatchHealthPanel`，挂载到 `frontend/app/page.tsx`，位置在 KPI 行之后、余额/倍率区域之前。

- 使用现有 Card、Select/Button、刷新上下文和 `apiFetch` 模式。
- 时间窗口使用单选分段按钮，默认 5 分钟；切换时只重新请求接口。
- 每个网关组显示标题和路由表格，列为：路由、尝试次数、失败率、平均首字时间。
- 失败率颜色：0% 为成功色，>0% 且 <20% 为警告色，>=20% 为危险色；百分比保留 1 位小数。
- 平均首字时间显示整数毫秒；无样本显示“暂无数据”。
- 加载中保留面板骨架/占位，接口错误显示简短错误提示，不影响主页其它区域。
- 全局刷新会触发重新请求；组件卸载时忽略过期响应。

## 测试策略

- Go：为聚合方法添加 SQLite/GORM 测试，覆盖成功/失败混合、空首字时间、窗口过滤、重复请求的多次尝试和无数据结果；为 handler 添加合法/非法窗口测试。
- TypeScript：测试窗口选项、失败率格式化和无数据展示所需的纯函数；运行现有 lint/build。
- 验证命令：`go test ./...`、`pnpm lint`、`pnpm build`，并在本地首页手动切换全部时间窗口。

## 非目标

- 不改变网关实际路由排序、重试、冷却或 AI 决策逻辑。
- 不新增定时采样表；指标实时从已有使用日志聚合。
- 不在本次增加按模型、按 API Key 或按客户端的额外筛选。
