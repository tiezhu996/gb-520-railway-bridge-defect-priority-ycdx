#!/usr/bin/env sh
set -eu

project_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_root"
set -a
if [ -f .env ]; then . ./.env; else . ./.env.example; fi
set +a

(command -v jq >/dev/null 2>&1) || { echo "jq is required for API validation" >&2; exit 1; }

cleanup() { docker compose down -v --remove-orphans; }
docker compose down -v --remove-orphans
if [ "${KEEP_RUNNING:-0}" = "1" ]; then
	trap cleanup INT TERM
else
	trap cleanup EXIT INT TERM
fi

(cd backend && go test ./... && go vet ./... && go build ./...)
(cd frontend && npm ci --no-audit --no-fund && npm run typecheck && npm run build)
docker compose config --quiet
docker compose up -d --build

i=0
until curl -fsS "http://127.0.0.1:${BACKEND_PORT:-19520}/healthz" >/dev/null; do
	i=$((i+1))
	[ "$i" -lt 60 ] || { docker compose logs; exit 1; }
	sleep 2
done
curl -fsS "http://127.0.0.1:${FRONTEND_PORT:-18520}/" >/dev/null

login_token() {
	curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT:-19520}/api/auth/login" \
		-H 'Content-Type: application/json' \
		-d "{\"username\":\"$1\",\"password\":\"Admin123!\"}" | jq -er '.data.token'
}

viewer_token=$(login_token viewer)
operator_token=$(login_token operator)
reviewer_token=$(login_token reviewer)
admin_token=$(login_token admin)

curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/session" -H "Authorization: Bearer $viewer_token" | jq -e '.data.role == "viewer"' >/dev/null
viewer_write_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/bridges" -H "Authorization: Bearer $viewer_token" -H 'Content-Type: application/json' -d '{}')
[ "$viewer_write_status" = "403" ]
viewer_audit_status=$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1:${BACKEND_PORT}/api/audits" -H "Authorization: Bearer $viewer_token")
[ "$viewer_audit_status" = "403" ]
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/audits?page=1&pageSize=20" -H "Authorization: Bearer $reviewer_token" | jq -e '.data | type == "array"' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/runtime" -H "Authorization: Bearer $admin_token" | jq -e '.data.appName and .data.databaseDriver and (.data.requestLimit > 0)' >/dev/null

now=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
code="PD-SMOKE-$(date +%s)"
create_payload=$(jq -n --arg code "$code" --arg now "$now" '{code:$code,name:"空卷验收优先级决定",description:"验证不可变版本链",facility:"K42 桥梁作业区",owner:"现场处置组",category:"结构缺陷",riskLevel:"critical",metricValue:88,metricUnit:"score",effectiveAt:$now,evidence:"裂缝照片与量测记录 v1",relatedCode:"DF-001"}')
created=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities" -H "Authorization: Bearer $operator_token" -H 'X-Request-ID: smoke-create' -H 'Content-Type: application/json' -d "$create_payload")
priority_id=$(printf '%s' "$created" | jq -er '.data.id')
printf '%s' "$created" | jq -e '.data.status == "draft" and .data.version == 1 and .data.preparedBy == "operator" and (.data.revisions | length == 1)' >/dev/null

update_payload=$(jq -n --arg now "$now" '{expectedVersion:1,name:"空卷验收优先级决定",description:"复核前补充量测证据",facility:"K42 桥梁作业区",owner:"现场处置组",category:"结构缺陷",riskLevel:"critical",metricValue:93,metricUnit:"score",effectiveAt:$now,evidence:"裂缝照片、量测记录与复测记录 v2",relatedCode:"DF-001"}')
updated=$(curl -fsS -X PUT "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$priority_id" -H "Authorization: Bearer $operator_token" -H 'X-Request-ID: smoke-update' -H 'Content-Type: application/json' -d "$update_payload")
printf '%s' "$updated" | jq -e '.data.version == 2 and (.data.revisions | length == 2)' >/dev/null

transition_payload='{"status":"urgent","expectedVersion":2,"reason":"独立复核确认需立即处置"}'
operator_final_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$priority_id/transition" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$transition_payload")
[ "$operator_final_status" = "403" ]

self_code="PD-SELF-$(date +%s)"
self_payload=$(printf '%s' "$create_payload" | jq --arg code "$self_code" '.code = $code')
self_created=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d "$self_payload")
self_id=$(printf '%s' "$self_created" | jq -er '.data.id')
self_final_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$self_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d '{"status":"observe","expectedVersion":1,"reason":"不得自行复核自己的决定"}')
[ "$self_final_status" = "422" ]

curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$priority_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'X-Request-ID: smoke-review' -H 'Content-Type: application/json' -d "$transition_payload" | jq -e '.data.status == "urgent" and .data.version == 3' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$priority_id" -H "Authorization: Bearer $reviewer_token" | jq -e '
	.data.status == "urgent" and
	(.data.revisions | length == 3) and
	([.data.revisions[].evidence] == ["裂缝照片与量测记录 v1","裂缝照片、量测记录与复测记录 v2","裂缝照片、量测记录与复测记录 v2"]) and
	([.data.revisions[].actor] == ["operator","operator","reviewer"]) and
	([.data.revisions[].requestId] == ["smoke-create","smoke-update","smoke-review"])' >/dev/null

locked_status=$(curl -sS -o /dev/null -w '%{http_code}' -X PUT "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$priority_id" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$(printf '%s' "$update_payload" | jq '.expectedVersion = 3')")
[ "$locked_status" = "422" ]

# --- 待复核：现场缺陷更新后，已定稿结论转入待复核，旧版本复核必须被拒绝 ---
review_code="PD-RV-$(date +%s)"
review_create=$(jq -n --arg code "$review_code" --arg now "$now" '{code:$code,name:"待复核流程决定",description:"验证缺陷变更触发再复核",facility:"K42 桥梁作业区",owner:"现场处置组",category:"结构缺陷",riskLevel:"high",metricValue:81,metricUnit:"score",effectiveAt:$now,evidence:"裂缝量测与照片 v1",relatedCode:"DF-002"}')
review_created=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$review_create")
review_id=$(printf '%s' "$review_created" | jq -er '.data.id')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d '{"status":"restrict","expectedVersion":1,"reason":"独立复核确认限制通行"}' | jq -e '.data.status == "restrict" and .data.version == 2' >/dev/null

# 现场更新关联缺陷 DF-002 的风险等级：关联决定必须转入 review_pending
defect_update=$(jq -n --arg now "$now" '{expectedVersion:1,name:"缺陷发现示例二",facility:"铁路桥梁缺陷处置优先级区域2",owner:"质量复核组",category:"重点",riskLevel:"critical",metricValue:25.0,metricUnit:"%",effectiveAt:$now,evidence:"现场复测风险升级",relatedCode:"REL-520-02"}')
curl -fsS -X PUT "http://127.0.0.1:${BACKEND_PORT}/api/defects/2" -H "Authorization: Bearer $operator_token" -H 'X-Request-ID: smoke-defect-risk' -H 'Content-Type: application/json' -d "$defect_update" | jq -e '.data.riskLevel == "critical"' >/dev/null
pending_json=$(curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id" -H "Authorization: Bearer $reviewer_token")
printf '%s' "$pending_json" | jq -e '
	.data.status == "review_pending" and
	.data.lastFinalLevel == "restrict" and
	(.data.pendingChanged | contains("medium")) and
	(.data.pendingChanged | contains("critical")) and
	(.data.pendingReason | contains("DF-002")) and
	(.data.revisions | length == 3) and
	.data.revisions[2].kind == "reopen" and
	.data.revisions[1].status == "restrict"' >/dev/null

# 待复核期间普通定稿接口被拒绝
bypass_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d '{"status":"urgent","expectedVersion":3,"reason":"绕过复核环节"}')
[ "$bypass_status" = "422" ]

# 旧版本（v3 之前的页面）提交复核必须被 409 拒绝，防止覆盖他人刚完成的复核
stale_status=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id/review" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d '{"level":"observe","expectedVersion":2,"reason":"旧页面试图覆盖他人复核"}')
[ "$stale_status" = "409" ]

# 复核员提交新等级 urgent，待复核关闭并追加复核版本
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id/review" -H "Authorization: Bearer $reviewer_token" -H 'X-Request-ID: smoke-rereview' -H 'Content-Type: application/json' -d '{"level":"urgent","expectedVersion":3,"reason":"风险确已升至严重，改级立即处置"}' | jq -e '.data.status == "urgent" and .data.version == 4 and .data.pendingReason == ""' >/dev/null
# 历史版本保留每次复核依据（final / reopen / change 链完整）
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id" -H "Authorization: Bearer $reviewer_token" | jq -e '
	([.data.revisions[].kind] == ["draft","final","reopen","change"]) and
	([.data.revisions[].status] == ["draft","restrict","review_pending","urgent"])' >/dev/null

# 维持原优先级路径：用同一决定走第二轮待复核（处置状态变化触发），复核维持 urgent
# 先把专用决定的关联缺陷恢复到可迁移状态（新建一条关联缺陷并迁移该决定关联并不现实，
# 这里直接对已 urgent 的决定再次通过风险变化触发第二轮待复核，验证 maintain 类型）
defect_update2=$(jq -n --arg now "$now" '{expectedVersion:2,name:"缺陷发现示例二",facility:"铁路桥梁缺陷处置优先级区域2",owner:"质量复核组",category:"重点",riskLevel:"high",metricValue:25.0,metricUnit:"%",effectiveAt:$now,evidence:"现场处置后风险回落",relatedCode:"REL-520-02"}')
curl -fsS -X PUT "http://127.0.0.1:${BACKEND_PORT}/api/defects/2" -H "Authorization: Bearer $operator_token" -H 'X-Request-ID: smoke-defect-risk-2' -H 'Content-Type: application/json' -d "$defect_update2" >/dev/null
second_pending_version=$(curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id" -H "Authorization: Bearer $reviewer_token" | jq -er '.data | select(.status == "review_pending") | .version')
[ "$second_pending_version" = "5" ]
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id/review" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d "$(jq -n --argjson v "$second_pending_version" '{level:"urgent",expectedVersion:$v,reason:"虽风险回落仍需立即处置，维持原等级"}')" | jq -e '.data.status == "urgent" and .data.version == 6 and (.data.revisions[-1].kind == "maintain")' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$review_id" -H "Authorization: Bearer $reviewer_token" | jq -e '([.data.revisions[].kind] == ["draft","final","reopen","change","reopen","maintain"])' >/dev/null

# 处置状态变化触发待复核：新建缺陷+决定，缺陷 new -> verified
state_code="DF-STATE-$(date +%s)"
defect_state_payload=$(jq -n --arg code "$state_code" --arg now "$now" '{code:$code,name:"状态联动缺陷",facility:"K42 桥梁作业区",owner:"现场处置组",category:"结构缺陷",riskLevel:"medium",metricValue:40,metricUnit:"score",effectiveAt:$now,evidence:"状态联动验证证据",relatedCode:"REL-520-09"}')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/defects" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$defect_state_payload" >/dev/null
state_pd_code="PD-STATE-$(date +%s)"
state_pd_create=$(jq -n --arg code "$state_pd_code" --arg defect "$state_code" --arg now "$now" '{code:$code,name:"状态联动决定",facility:"K42 桥梁作业区",owner:"现场处置组",category:"结构缺陷",riskLevel:"medium",metricValue:40,metricUnit:"score",effectiveAt:$now,evidence:"状态联动决定证据",relatedCode:$defect}')
state_pd_id=$(curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities" -H "Authorization: Bearer $operator_token" -H 'Content-Type: application/json' -d "$state_pd_create" | jq -er '.data.id')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$state_pd_id/transition" -H "Authorization: Bearer $reviewer_token" -H 'Content-Type: application/json' -d '{"status":"observe","expectedVersion":1,"reason":"独立复核确认观察处置"}' >/dev/null
# 找到新建缺陷 id
state_defect_id=$(curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/defects?search=$state_code" -H "Authorization: Bearer $operator_token" | jq -er '.data[0].id')
curl -fsS -X POST "http://127.0.0.1:${BACKEND_PORT}/api/defects/$state_defect_id/transition" -H "Authorization: Bearer $operator_token" -H 'X-Request-ID: smoke-defect-state' -H 'Content-Type: application/json' -d '{"status":"verified","expectedVersion":1,"reason":"现场核实缺陷"}' >/dev/null
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities/$state_pd_id" -H "Authorization: Bearer $reviewer_token" | jq -e '.data.status == "review_pending" and (.data.pendingChanged | contains("new → verified")) and (.data.pendingChanged | contains("处置状态"))' >/dev/null

# 列表可筛选待复核状态，且每条待复核记录都携带变化原因
curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/priorities?status=review_pending" -H "Authorization: Bearer $reviewer_token" | jq -e '.data | type == "array" and (length >= 1) and (all(.data[]; .pendingReason != "" and .lastFinalLevel != ""))' >/dev/null

curl -fsS "http://127.0.0.1:${BACKEND_PORT}/api/audit-summary?windowHours=24" -H "Authorization: Bearer $reviewer_token" | jq -e '.data.total >= 3 and .data.transitions >= 1' >/dev/null

docker compose ps
if [ "${KEEP_RUNNING:-0}" = "1" ]; then
	echo "KEEP_RUNNING=1: containers left running for browser validation"
else
	cleanup
	trap - EXIT INT TERM
fi
