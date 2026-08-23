#!/usr/bin/env bash
set -euo pipefail

db_path="${1:-data/upstream-ops.db}"
if [[ ! -f "$db_path" ]]; then echo "database not found: $db_path" >&2; exit 1; fi
mock_description="[flow-router:dispatch-mock:v2]"
sqlite3 "$db_path" <<'SQL'
BEGIN;
INSERT OR IGNORE INTO gateway_groups (name, description, position, status) VALUES
  ('本地 Mock OpenAI 路由', '[flow-router:dispatch-mock:v2]', 89, 'active'),
  ('本地 Mock GPT 路由', '[flow-router:dispatch-mock:v2]', 90, 'active'),
  ('本地 Mock Claude 路由', '[flow-router:dispatch-mock:v2]', 91, 'active'),
  ('本地 Mock Gemini 路由', '[flow-router:dispatch-mock:v2]', 92, 'active');
UPDATE gateway_groups SET description='[flow-router:dispatch-mock:v2]' WHERE name IN ('本地 Mock OpenAI 路由','本地 Mock GPT 路由','本地 Mock Claude 路由','本地 Mock Gemini 路由') AND description='[flow-router:dispatch-mock:v1]';
DELETE FROM gateway_routes WHERE gateway_group_id IN (SELECT id FROM gateway_groups WHERE description='[flow-router:dispatch-mock:v2]');
INSERT INTO gateway_routes (gateway_group_id, position, source_kind, source_channel_id, gateway_provider_id, source_group_name, weight, rate_convert_mode, rate_convert_value, billing_rate_multiplier, enabled, upstream_protocol, concurrency, user_agent_mode, user_agent_custom, source_api_key_id, source_api_key_name)
WITH routes(group_name,position,key_name,source_name,rate,protocol) AS (VALUES
 ('本地 Mock OpenAI 路由',0,'openai-primary-key','OpenAI 稳定池',0.08,'openai_chat'),('本地 Mock OpenAI 路由',1,'openai-backup-key','OpenAI 备用池',0.16,'openai_chat'),('本地 Mock OpenAI 路由',2,'openai-cost-key','OpenAI 低价池',0.05,'openai_chat'),('本地 Mock OpenAI 路由',3,'openai-long-key','OpenAI 长上下文池',0.22,'openai_chat'),
 ('本地 Mock GPT 路由',0,'mock-primary-key','GPT 主分组',0.14,'openai_chat'),('本地 Mock GPT 路由',1,'mock-backup-key','GPT 备用分组',0.20,'openai_chat'),('本地 Mock GPT 路由',2,'provider-c-key','GPT 低价分组',0.10,'openai_chat'),('本地 Mock GPT 路由',3,'provider-long-key','GPT 长上下文',0.25,'openai_chat'),
 ('本地 Mock Claude 路由',0,'anthropic-primary-key','Claude 稳定池',0.12,'anthropic'),('本地 Mock Claude 路由',1,'anthropic-backup-key','Claude 备用池',0.24,'anthropic'),('本地 Mock Claude 路由',2,'anthropic-fast-key','Claude 快速池',0.09,'anthropic'),
 ('本地 Mock Gemini 路由',0,'gemini-primary-key','Gemini 稳定池',0.11,'openai_chat'),('本地 Mock Gemini 路由',1,'gemini-backup-key','Gemini 备用池',0.19,'openai_chat'),('本地 Mock Gemini 路由',2,'gemini-flash-key','Gemini Flash 池',0.06,'openai_chat'))
SELECT g.id,r.position,'monitor',COALESCE((SELECT id FROM channels ORDER BY id LIMIT 1),0),0,r.source_name,1,'raw',1,r.rate,1,r.protocol,10,'passthrough','',0,r.key_name FROM routes r JOIN gateway_groups g ON g.name=r.group_name;
DELETE FROM gateway_usage_logs WHERE request_id LIKE 'mock-dispatch-v2-%';
WITH RECURSIVE minutes(n) AS (SELECT 0 UNION ALL SELECT n+1 FROM minutes WHERE n<4319),
routes(group_name,provider_name,key_name,source_name,rate,failure_mod,base_ms,model) AS (VALUES
 ('本地 Mock OpenAI 路由','OpenAI Primary','openai-primary-key','OpenAI 稳定池',0.08,97,520,'gpt-4.1'),('本地 Mock OpenAI 路由','OpenAI Backup','openai-backup-key','OpenAI 备用池',0.16,19,860,'gpt-4.1'),('本地 Mock OpenAI 路由','OpenAI Cost','openai-cost-key','OpenAI 低价池',0.05,0,410,'gpt-4o-mini'),('本地 Mock OpenAI 路由','OpenAI Long','openai-long-key','OpenAI 长上下文池',0.22,31,1280,'o3'),
 ('本地 Mock GPT 路由','GPT Primary','mock-primary-key','GPT 主分组',0.14,23,610,'gpt-5'),('本地 Mock GPT 路由','GPT Backup','mock-backup-key','GPT 备用分组',0.20,13,910,'gpt-5'),('本地 Mock GPT 路由','GPT Cost','provider-c-key','GPT 低价分组',0.10,0,470,'gpt-4o-mini'),('本地 Mock GPT 路由','GPT Long','provider-long-key','GPT 长上下文',0.25,37,1420,'o3'),
 ('本地 Mock Claude 路由','Claude Primary','anthropic-primary-key','Claude 稳定池',0.12,29,780,'claude-sonnet-4'),('本地 Mock Claude 路由','Claude Backup','anthropic-backup-key','Claude 备用池',0.24,11,1160,'claude-sonnet-4'),('本地 Mock Claude 路由','Claude Fast','anthropic-fast-key','Claude 快速池',0.09,0,480,'claude-haiku-3.5'),
 ('本地 Mock Gemini 路由','Gemini Primary','gemini-primary-key','Gemini 稳定池',0.11,41,690,'gemini-2.5-pro'),('本地 Mock Gemini 路由','Gemini Backup','gemini-backup-key','Gemini 备用池',0.19,17,980,'gemini-2.5-pro'),('本地 Mock Gemini 路由','Gemini Flash','gemini-flash-key','Gemini Flash 池',0.06,0,360,'gemini-2.5-flash'))
INSERT INTO gateway_usage_logs (gateway_group_id,gateway_key_id,route_id,channel_id,gateway_provider_id,provider_name,source_api_key_name,source_group_name,billing_rate_multiplier,rate_multiplier,request_id,attempt,attempt_kind,requested_model,upstream_model,inbound_endpoint,upstream_endpoint,inbound_protocol,upstream_protocol,billing_mode,status_code,success,duration_ms,first_token_ms,created_at)
SELECT g.id,0,rte.id,0,0,r.provider_name,r.key_name,r.source_name,r.rate,r.rate,printf('mock-dispatch-v2-%d-%d-%d',g.id,rte.id,m.n),1,CASE WHEN r.failure_mod>0 AND abs((m.n*m.n*31+m.n*17+rte.id*13)%997)<997/r.failure_mod THEN 'retry' ELSE 'primary' END,r.model,r.model,'/v1/chat/completions','/v1/chat/completions','openai','openai','token',CASE WHEN r.failure_mod>0 AND abs((m.n*m.n*31+m.n*17+rte.id*13)%997)<997/r.failure_mod THEN 502 ELSE 200 END,CASE WHEN r.failure_mod>0 AND abs((m.n*m.n*31+m.n*17+rte.id*13)%997)<997/r.failure_mod THEN 0 ELSE 1 END,CAST(r.base_ms+62*sin((m.n+r.base_ms/10.0)/23.0)+31*cos((m.n+r.base_ms/20.0)/61.0)+18*sin((m.n+r.base_ms/30.0)/7.0)+(m.n%3)*5 AS INTEGER),CASE WHEN r.failure_mod>0 AND abs((m.n*m.n*31+m.n*17+rte.id*13)%997)<997/r.failure_mod THEN NULL ELSE CAST(r.base_ms+62*sin((m.n+r.base_ms/10.0)/23.0)+31*cos((m.n+r.base_ms/20.0)/61.0)+18*sin((m.n+r.base_ms/30.0)/7.0)+(m.n%3)*5 AS INTEGER) END,datetime('now',printf('-%d minutes',m.n),printf('+%d seconds',(m.n%3)*12))
FROM routes r JOIN gateway_groups g ON g.name=r.group_name JOIN gateway_routes rte ON rte.gateway_group_id=g.id AND rte.source_api_key_name=r.key_name CROSS JOIN minutes m;
-- 每个失败主请求补一条顺延成功尝试，形成真实 request_id 链。
WITH failed AS (SELECT l.*,g.name AS group_name FROM gateway_usage_logs l JOIN gateway_groups g ON g.id=l.gateway_group_id WHERE l.request_id LIKE 'mock-dispatch-v2-%' AND l.success=0)
INSERT INTO gateway_usage_logs (gateway_group_id,gateway_key_id,route_id,channel_id,gateway_provider_id,provider_name,source_api_key_name,source_group_name,billing_rate_multiplier,rate_multiplier,request_id,attempt,attempt_kind,requested_model,upstream_model,inbound_endpoint,upstream_endpoint,inbound_protocol,upstream_protocol,billing_mode,status_code,success,duration_ms,first_token_ms,created_at)
SELECT f.gateway_group_id,0,COALESCE((SELECT id FROM gateway_routes WHERE gateway_group_id=f.gateway_group_id AND id<>f.route_id ORDER BY position LIMIT 1),f.route_id),0,0,'Failover '+f.provider_name,'failover-key','备用顺延池',1,1,f.request_id,2,'failover',f.requested_model,f.upstream_model,f.inbound_endpoint,f.upstream_endpoint,f.inbound_protocol,f.upstream_protocol,f.billing_mode,200,1,f.duration_ms+420,f.duration_ms+260,datetime(f.created_at,'+2 seconds') FROM failed f;
COMMIT;
SQL
echo "seeded 72h minute-level dispatch mock data into $db_path"
