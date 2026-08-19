#!/usr/bin/env bash
set -euo pipefail

db_path="${1:-data/upstream-ops.db}"

if [[ ! -f "$db_path" ]]; then
  echo "database not found: $db_path" >&2
  exit 1
fi

mock_description="[flow-router:dispatch-mock:v1]"
conflicting_group_count="$(sqlite3 "$db_path" "SELECT COUNT(*) FROM gateway_groups WHERE name IN ('本地 Mock GPT 路由', '本地 Mock OpenAI 路由', '本地 Mock Claude 路由') AND description <> '$mock_description';")"
if [[ "$conflicting_group_count" != "0" ]]; then
  echo "refusing to seed: a mock group name is already used by a non-mock gateway group" >&2
  exit 1
fi

sqlite3 "$db_path" <<'SQL'
BEGIN;

INSERT OR IGNORE INTO gateway_groups (name, description, position, status)
VALUES
  ('本地 Mock GPT 路由', '[flow-router:dispatch-mock:v1]', 89, 'active'),
  ('本地 Mock OpenAI 路由', '[flow-router:dispatch-mock:v1]', 90, 'active'),
  ('本地 Mock Claude 路由', '[flow-router:dispatch-mock:v1]', 91, 'active');

-- 让首页调度统计与网关配置一一对应，点击统计项可以定位真实路由行。
-- 这些是本地预览路由，只清理带有精确专用标记的分组，不触碰其他路由。
DELETE FROM gateway_routes
WHERE gateway_group_id IN (
  SELECT id FROM gateway_groups
  WHERE name IN ('本地 Mock GPT 路由', '本地 Mock OpenAI 路由', '本地 Mock Claude 路由')
    AND description = '[flow-router:dispatch-mock:v1]'
);

INSERT INTO gateway_routes (
  gateway_group_id, position, source_kind, source_channel_id,
  gateway_provider_id, source_group_name, weight, rate_convert_mode,
  rate_convert_value, billing_rate_multiplier, enabled, upstream_protocol,
  concurrency, user_agent_mode, user_agent_custom, source_api_key_id,
  source_api_key_name
)
WITH mock_routes(group_name, position, source_api_key_name, source_group_name, billing_rate_multiplier, upstream_protocol) AS (
  VALUES
    ('本地 Mock GPT 路由', 0, 'mock-primary-key', 'GPT 主分组', 0.14, 'openai_chat'),
    ('本地 Mock GPT 路由', 1, 'mock-backup-key', 'GPT 备用分组', 0.20, 'openai_chat'),
    ('本地 Mock GPT 路由', 2, 'provider-c-key', 'GPT 低价分组', 0.10, 'openai_chat'),
    ('本地 Mock OpenAI 路由', 0, 'openai-primary-key', 'OpenAI 稳定池', 0.08, 'openai_chat'),
    ('本地 Mock OpenAI 路由', 1, 'openai-backup-key', 'OpenAI 备用池', 0.16, 'openai_chat'),
    ('本地 Mock OpenAI 路由', 2, 'openai-cost-key', 'OpenAI 低价池', 0.05, 'openai_chat'),
    ('本地 Mock Claude 路由', 0, 'anthropic-primary-key', 'Claude 稳定池', 0.12, 'anthropic'),
    ('本地 Mock Claude 路由', 1, 'anthropic-backup-key', 'Claude 备用池', 0.24, 'anthropic')
)
SELECT
  gateway_groups.id,
  mock_routes.position,
  'monitor',
  COALESCE((SELECT id FROM channels ORDER BY id LIMIT 1), 0),
  0,
  mock_routes.source_group_name,
  1,
  'raw',
  1,
  mock_routes.billing_rate_multiplier,
  1,
  mock_routes.upstream_protocol,
  10,
  'passthrough',
  '',
  0,
  mock_routes.source_api_key_name
FROM mock_routes
JOIN gateway_groups ON gateway_groups.name = mock_routes.group_name;

DELETE FROM gateway_usage_logs WHERE request_id LIKE 'mock-dispatch-%';

WITH RECURSIVE
  attempts(n) AS (
    SELECT 1
    UNION ALL
    SELECT n + 1 FROM attempts WHERE n < 24
  ),
  routes(group_name, provider_name, source_api_key_name, source_group_name, billing_rate_multiplier, failure_mod, first_token_ms) AS (
    VALUES
      ('本地 Mock OpenAI 路由', 'OpenAI Primary', 'openai-primary-key', 'OpenAI 稳定池', 0.08, 99, 420),
      ('本地 Mock OpenAI 路由', 'OpenAI Backup', 'openai-backup-key', 'OpenAI 备用池', 0.16, 3, 730),
      ('本地 Mock OpenAI 路由', 'OpenAI Cost Saver', 'openai-cost-key', 'OpenAI 低价池', 0.05, 8, 515),
      ('本地 Mock GPT 路由', 'Mock Primary', 'mock-primary-key', 'GPT 主分组', 0.14, 9, 505),
      ('本地 Mock GPT 路由', 'Mock Backup', 'mock-backup-key', 'GPT 备用分组', 0.2, 3, 680),
      ('本地 Mock GPT 路由', 'Provider C', 'provider-c-key', 'GPT 低价分组', 0.1, 99, 438),
      ('本地 Mock Claude 路由', 'Anthropic Primary', 'anthropic-primary-key', 'Claude 稳定池', 0.12, 99, 815),
      ('本地 Mock Claude 路由', 'Anthropic Backup', 'anthropic-backup-key', 'Claude 备用池', 0.24, 8, 1040)
  )
INSERT INTO gateway_usage_logs (
  gateway_group_id, gateway_key_id, route_id, channel_id, gateway_provider_id,
  provider_name, source_api_key_name, source_group_name, billing_rate_multiplier, rate_multiplier,
  request_id, attempt, attempt_kind, requested_model,
  upstream_model, inbound_endpoint, upstream_endpoint, inbound_protocol,
  upstream_protocol, billing_mode, status_code, success, duration_ms,
  first_token_ms, created_at
)
SELECT
  gateway_groups.id,
  0,
  gateway_routes.id,
  0,
  0,
  routes.provider_name,
  routes.source_api_key_name,
  routes.source_group_name,
  routes.billing_rate_multiplier,
  routes.billing_rate_multiplier,
  printf('mock-dispatch-%d-%d-%d', gateway_groups.id, gateway_routes.id, attempts.n),
  attempts.n,
  CASE WHEN attempts.n % routes.failure_mod = 0 THEN 'failover' ELSE 'primary' END,
  CASE
    WHEN routes.provider_name LIKE 'Anthropic%' THEN 'claude-3-7-sonnet'
    ELSE 'gpt-5.6'
  END,
  CASE
    WHEN routes.provider_name LIKE 'Anthropic%' THEN 'claude-3-7-sonnet'
    ELSE 'gpt-5.6'
  END,
  '/v1/chat/completions',
  '/v1/chat/completions',
  'openai',
  'openai',
  'token',
  CASE WHEN attempts.n % routes.failure_mod = 0 THEN 502 ELSE 200 END,
  CASE WHEN attempts.n % routes.failure_mod = 0 THEN 0 ELSE 1 END,
  routes.first_token_ms + (attempts.n % 5) * 22,
  CASE WHEN attempts.n % routes.failure_mod = 0 THEN NULL ELSE routes.first_token_ms + (attempts.n % 5) * 22 END,
  datetime(
    'now',
    printf(
      '-%d minutes',
      CASE WHEN attempts.n <= 6 THEN attempts.n - 1 ELSE attempts.n * 60 END
    )
  )
FROM routes
JOIN gateway_groups ON gateway_groups.name = routes.group_name
JOIN gateway_routes ON gateway_routes.gateway_group_id = gateway_groups.id
  AND gateway_routes.source_api_key_name = routes.source_api_key_name
CROSS JOIN attempts;

COMMIT;
SQL

echo "seeded mock dispatch data into $db_path"
