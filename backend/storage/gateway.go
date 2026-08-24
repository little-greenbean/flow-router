// 网关相关仓储：直连渠道、分组、密钥、路由、用量、模型价目覆盖。
package storage

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ---------- GatewayProviders（直连渠道） ----------

// GatewayProviders 直连上游仓储。
type GatewayProviders struct{ db *gorm.DB }

// NewGatewayProviders 构造直连渠道仓储。
func NewGatewayProviders(db *gorm.DB) *GatewayProviders { return &GatewayProviders{db: db} }

// GatewayProviderQuery 分页列表查询。
type GatewayProviderQuery struct {
	Q        string
	Page     int
	PageSize int
	// EnabledOnly 为 true 时只返回 enabled
	EnabledOnly bool
}

// GatewayProviderPage 分页结果。
type GatewayProviderPage struct {
	Items    []GatewayProvider `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Pages    int               `json:"pages"`
}

// List 分页列表。
func (r *GatewayProviders) List(q GatewayProviderQuery) (*GatewayProviderPage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	db := r.db.Model(&GatewayProvider{})
	if q.EnabledOnly {
		db = db.Where("enabled = ?", true)
	}
	if s := strings.TrimSpace(q.Q); s != "" {
		like := "%" + s + "%"
		db = db.Where("name LIKE ? OR base_url LIKE ?", like, like)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	var items []GatewayProvider
	offset := (q.Page - 1) * q.PageSize
	if err := db.Order("id DESC").Offset(offset).Limit(q.PageSize).Find(&items).Error; err != nil {
		return nil, err
	}
	pages := int(total) / q.PageSize
	if int(total)%q.PageSize != 0 {
		pages++
	}
	return &GatewayProviderPage{
		Items:    items,
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Pages:    pages,
	}, nil
}

// ListOptions 返回启用的轻量列表（路由下拉），最多 500 条。
func (r *GatewayProviders) ListOptions(q string) ([]GatewayProvider, error) {
	db := r.db.Model(&GatewayProvider{}).Where("enabled = ?", true)
	if s := strings.TrimSpace(q); s != "" {
		like := "%" + s + "%"
		db = db.Where("name LIKE ? OR base_url LIKE ?", like, like)
	}
	var items []GatewayProvider
	if err := db.Order("name ASC").Limit(500).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindByID 按主键查询。
func (r *GatewayProviders) FindByID(id uint) (*GatewayProvider, error) {
	var item GatewayProvider
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 按名称查询。
func (r *GatewayProviders) FindByName(name string) (*GatewayProvider, error) {
	var item GatewayProvider
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Create 插入记录。
func (r *GatewayProviders) Create(item *GatewayProvider) error { return r.db.Create(item).Error }

// Update 全量保存。
func (r *GatewayProviders) Update(item *GatewayProvider) error { return r.db.Save(item).Error }

// Delete 按主键删除。
func (r *GatewayProviders) Delete(id uint) error {
	return r.db.Delete(&GatewayProvider{}, id).Error
}

// GatewayGroups 网关组仓储。
type GatewayGroups struct{ db *gorm.DB }

// NewGatewayGroups 构造网关分组仓储。
func NewGatewayGroups(db *gorm.DB) *GatewayGroups { return &GatewayGroups{db: db} }

// List 分页列表。
func (r *GatewayGroups) List() ([]GatewayGroup, error) {
	var list []GatewayGroup
	// position 升序；同 position 时较新 id 在前（兼容尚未重排的旧数据）
	if err := r.db.Order("position ASC, id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// NextPosition 返回新建组应使用的 position（当前最大 + 1）。
func (r *GatewayGroups) NextPosition() (int, error) {
	var maxPos *int
	if err := r.db.Model(&GatewayGroup{}).Select("MAX(position)").Scan(&maxPos).Error; err != nil {
		return 0, err
	}
	if maxPos == nil {
		return 0, nil
	}
	return *maxPos + 1, nil
}

// Reorder 按 ids 顺序重写 position（0..n-1）。未出现在 ids 中的组保持原 position。
func (r *GatewayGroups) Reorder(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		seen := make(map[uint]struct{}, len(ids))
		for i, id := range ids {
			if id == 0 {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			if err := tx.Model(&GatewayGroup{}).Where("id = ?", id).
				Update("position", i).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// FindByID 按主键查询。
func (r *GatewayGroups) FindByID(id uint) (*GatewayGroup, error) {
	var item GatewayGroup
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 按名称查询。
func (r *GatewayGroups) FindByName(name string) (*GatewayGroup, error) {
	var item GatewayGroup
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Create 插入记录。
func (r *GatewayGroups) Create(item *GatewayGroup) error { return r.db.Create(item).Error }

// Update 全量保存。
func (r *GatewayGroups) Update(item *GatewayGroup) error { return r.db.Save(item).Error }

// Delete 按主键删除。
func (r *GatewayGroups) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("gateway_group_id = ?", id).Delete(&GatewayRoute{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&GatewayKey{}).Error; err != nil {
			return err
		}
		return tx.Delete(&GatewayGroup{}, id).Error
	})
}

// GatewayKeys 网关密钥仓储。
type GatewayKeys struct{ db *gorm.DB }

// NewGatewayKeys 构造网关密钥仓储。
func NewGatewayKeys(db *gorm.DB) *GatewayKeys { return &GatewayKeys{db: db} }

// List 分页列表。
func (r *GatewayKeys) List() ([]GatewayKey, error) {
	var list []GatewayKey
	if err := r.db.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListByGroupID 列出某分组下资源。
func (r *GatewayKeys) ListByGroupID(groupID uint) ([]GatewayKey, error) {
	var list []GatewayKey
	if err := r.db.Where("group_id = ?", groupID).Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// FindByID 按主键查询。
func (r *GatewayKeys) FindByID(id uint) (*GatewayKey, error) {
	var item GatewayKey
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByHash 按密钥哈希查询。
func (r *GatewayKeys) FindByHash(hash string) (*GatewayKey, error) {
	var item GatewayKey
	if err := r.db.Where("key_hash = ?", hash).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// FindByName 按名称查询。
func (r *GatewayKeys) FindByName(name string) (*GatewayKey, error) {
	var item GatewayKey
	if err := r.db.Where("name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Create 插入记录。
func (r *GatewayKeys) Create(item *GatewayKey) error { return r.db.Create(item).Error }

// Update 全量保存。
func (r *GatewayKeys) Update(item *GatewayKey) error { return r.db.Save(item).Error }

// Delete 按主键删除。
func (r *GatewayKeys) Delete(id uint) error {
	return r.db.Delete(&GatewayKey{}, id).Error
}

// DeleteWithGroupMutation 在同一事务内更新所属组并删除 Key。
func (r *GatewayKeys) DeleteWithGroupMutation(id uint, mutate func(*GatewayGroup) bool) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var key GatewayKey
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&key, id).Error; err != nil {
			return err
		}
		var group GatewayGroup
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&group, key.GroupID).Error; err != nil {
			return err
		}
		if mutate != nil && mutate(&group) {
			if err := tx.Model(&GatewayGroup{}).Where("id = ?", group.ID).Update("system_prompt_rules_json", group.SystemPromptRulesJSON).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&key).Error
	})
}

// TouchLastUsed 更新密钥最近使用时间。
func (r *GatewayKeys) TouchLastUsed(id uint, at time.Time) error {
	return r.db.Model(&GatewayKey{}).Where("id = ?", id).Updates(map[string]any{
		"last_used_at": at,
	}).Error
}

// AddQuotaUsed 累加密钥已用额度。
func (r *GatewayKeys) AddQuotaUsed(id uint, amount float64) error {
	if amount == 0 {
		return nil
	}
	return r.db.Model(&GatewayKey{}).Where("id = ?", id).
		UpdateColumn("quota_used", gorm.Expr("quota_used + ?", amount)).Error
}

// GatewayRoutes 网关路由仓储。
type GatewayRoutes struct{ db *gorm.DB }

// NewGatewayRoutes 构造网关路由仓储。
func NewGatewayRoutes(db *gorm.DB) *GatewayRoutes { return &GatewayRoutes{db: db} }

// ListByGroupID 列出某分组下资源。
func (r *GatewayRoutes) ListByGroupID(groupID uint) ([]GatewayRoute, error) {
	var list []GatewayRoute
	if err := r.db.Where("gateway_group_id = ?", groupID).Order("position ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	// 不过期即清 reason：调度层用 until 判断是否仍暂停；reason 作为「上次错误」保留供管理端查看
	return list, nil
}

// FindByID 按主键查询。
func (r *GatewayRoutes) FindByID(id uint) (*GatewayRoute, error) {
	var item GatewayRoute
	if err := r.db.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// SaveForGroup 全量保存某组下的路由列表。
//
// 重要：尽量保留已有 route.ID（原地 Update），避免「删表重建」导致 usage 日志里的
// route_id 悬空，从而丢失「上游密钥名 / 源分组」等展示字段。仅真正删除的路由才 Delete。
//
// position 有 (group_id, position) 唯一索引：先写临时 position 再写最终值，避免换序冲突。
func (r *GatewayRoutes) SaveForGroup(groupID uint, list []GatewayRoute) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var existing []GatewayRoute
		if err := tx.Where("gateway_group_id = ?", groupID).Find(&existing).Error; err != nil {
			return err
		}
		byID := make(map[uint]GatewayRoute, len(existing))
		for _, e := range existing {
			byID[e.ID] = e
		}
		keep := make(map[uint]struct{}, len(list))

		// 阶段 1：已有行先挪到临时 position，腾出唯一索引槽位
		tmpBase := 1_000_000
		for _, e := range existing {
			if err := tx.Model(&GatewayRoute{}).Where("id = ?", e.ID).
				Update("position", tmpBase+int(e.ID)).Error; err != nil {
				return err
			}
		}

		for i := range list {
			prev, hasPrev := byID[list[i].ID]
			if !hasPrev {
				// 客户端传来的 id 不属于本组（或不存在）→ 视为新建
				list[i].ID = 0
			} else {
				keep[list[i].ID] = struct{}{}
			}
			list[i].GatewayGroupID = groupID
			list[i].Position = i
			normalizeGatewayRoute(&list[i])

			// 保留已有上游密钥 / 暂停状态：来源未变时不丢
			sameSource := hasPrev &&
				prev.NormalizeSourceKind() == list[i].NormalizeSourceKind() &&
				prev.SourceChannelID == list[i].SourceChannelID &&
				prev.GatewayProviderID == list[i].GatewayProviderID
			if sameSource {
				list[i].SourceAPIKeyID = prev.SourceAPIKeyID
				list[i].SourceAPIKeyName = prev.SourceAPIKeyName
				list[i].SourceAPIKeyCipher = prev.SourceAPIKeyCipher
				list[i].TempUnschedulableUntil = prev.TempUnschedulableUntil
				list[i].TempUnschedulableReason = prev.TempUnschedulableReason
				list[i].TempUnschedulableAt = prev.TempUnschedulableAt
				list[i].TempUnschedulableRequestID = prev.TempUnschedulableRequestID
				list[i].RecoverSuccessStreak = prev.RecoverSuccessStreak
				list[i].CreatedAt = prev.CreatedAt
			} else {
				list[i].SourceAPIKeyID = 0
				list[i].SourceAPIKeyName = ""
				list[i].SourceAPIKeyCipher = ""
				list[i].TempUnschedulableUntil = nil
				list[i].TempUnschedulableReason = ""
				list[i].TempUnschedulableAt = nil
				list[i].TempUnschedulableRequestID = ""
				list[i].RecoverSuccessStreak = 0
			}

			if hasPrev {
				if err := tx.Save(&list[i]).Error; err != nil {
					return err
				}
			} else {
				list[i].ID = 0
				if err := tx.Create(&list[i]).Error; err != nil {
					return err
				}
			}
		}
		// 删除本次未提交的旧路由
		for id := range byID {
			if _, ok := keep[id]; ok {
				continue
			}
			if err := tx.Delete(&GatewayRoute{}, id).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeGatewayRoute(item *GatewayRoute) {
	kind := item.NormalizeSourceKind()
	item.SourceKind = kind
	if kind == GatewayRouteSourceProvider {
		item.SourceChannelID = 0
		item.SourceGroupID = nil
		item.SourceGroupName = ""
	} else {
		item.GatewayProviderID = 0
	}
	if item.Weight <= 0 {
		item.Weight = 1
	}
	if strings.TrimSpace(item.RateConvertMode) == "" {
		item.RateConvertMode = "raw"
	}
	// UA 策略：非法/空 → passthrough；非 custom 清空自定义串
	mode := strings.ToLower(strings.TrimSpace(item.UserAgentMode))
	switch mode {
	case GatewayUserAgentModeGroup, GatewayUserAgentModeCustom:
		item.UserAgentMode = mode
	default:
		item.UserAgentMode = GatewayUserAgentModePassthrough
	}
	item.UserAgentCustom = strings.TrimSpace(item.UserAgentCustom)
	if item.UserAgentMode != GatewayUserAgentModeCustom {
		item.UserAgentCustom = ""
	}
	// 非 custom 时 rate_convert_value 仅作占位，与网关路由倍率换算一致置 1；
	// billing_rate_multiplier 由前端按源分组换算写入（原值=源 ratio），
	// 运行时计费以 RateForRoute 为准，此处不强制改写为 1。
	if strings.TrimSpace(item.RateConvertMode) != "custom" && item.RateConvertValue == 0 {
		item.RateConvertValue = 1
	}
	if item.BillingRateMultiplier <= 0 {
		item.BillingRateMultiplier = 1
	}
	if item.Concurrency <= 0 {
		item.Concurrency = 10
	}
	up := strings.ToLower(strings.TrimSpace(item.UpstreamProtocol))
	switch up {
	case GatewayUpstreamProtocolOpenAI, "chat", "chat_completions":
		item.UpstreamProtocol = GatewayUpstreamProtocolOpenAIChat
	case GatewayUpstreamProtocolOpenAIChat,
		GatewayUpstreamProtocolOpenAIResponses,
		GatewayUpstreamProtocolAnthropic,
		GatewayUpstreamProtocolAuto:
		item.UpstreamProtocol = up
	case "responses":
		item.UpstreamProtocol = GatewayUpstreamProtocolOpenAIResponses
	case "":
		item.UpstreamProtocol = GatewayUpstreamProtocolAuto
	default:
		item.UpstreamProtocol = GatewayUpstreamProtocolAuto
	}
	item.SourceGroupName = strings.TrimSpace(item.SourceGroupName)
}

// Update 全量保存。
func (r *GatewayRoutes) Update(item *GatewayRoute) error { return r.db.Save(item).Error }

// UpdateSourceKey 更新路由绑定的上游密钥密文。
func (r *GatewayRoutes) UpdateSourceKey(id uint, keyID int64, keyName, keyCipher string) error {
	return r.db.Model(&GatewayRoute{}).Where("id = ?", id).Updates(map[string]any{
		"source_api_key_id":     keyID,
		"source_api_key_name":   keyName,
		"source_api_key_cipher": keyCipher,
	}).Error
}

// UpdateSourceGroupSnapshot 补全源分组显示名（保留 source_group_id）。
func (r *GatewayRoutes) UpdateSourceGroupSnapshot(id uint, groupID *int64, groupName string) error {
	updates := map[string]any{
		"source_group_name": strings.TrimSpace(groupName),
	}
	if groupID != nil {
		updates["source_group_id"] = *groupID
	}
	return r.db.Model(&GatewayRoute{}).Where("id = ?", id).Updates(updates).Error
}

// SetTempUnschedulable 写入冷却截止时间、错误详情，以及触发失败的请求时间/ request_id。
func (r *GatewayRoutes) SetTempUnschedulable(id uint, until time.Time, reason string, failedAt time.Time, requestID string) error {
	if failedAt.IsZero() {
		failedAt = time.Now()
	}
	return r.db.Model(&GatewayRoute{}).Where("id = ?", id).Updates(map[string]any{
		"temp_unschedulable_until":      until,
		"temp_unschedulable_reason":     reason,
		"temp_unschedulable_at":         failedAt,
		"temp_unschedulable_request_id": strings.TrimSpace(requestID),
		"recover_success_streak":        0,
	}).Error
}

// ClearTempUnschedulable 手动清除暂停时间与错误信息。
func (r *GatewayRoutes) ClearTempUnschedulable(id uint) error {
	// 用 Exec 强制写 NULL；GORM Updates(map) 对 nil 在部分版本会跳过
	return r.db.Exec(
		`UPDATE gateway_routes SET temp_unschedulable_until = NULL, temp_unschedulable_reason = '', temp_unschedulable_at = NULL, temp_unschedulable_request_id = '', recover_success_streak = 0, updated_at = ? WHERE id = ?`,
		time.Now(), id,
	).Error
}

// ClearTempUnschedulableUntil 仅结束临时暂停（恢复调度），保留 reason / request_id / at 供排查。
func (r *GatewayRoutes) ClearTempUnschedulableUntil(id uint) error {
	return r.db.Exec(
		`UPDATE gateway_routes SET temp_unschedulable_until = NULL, updated_at = ? WHERE id = ?`,
		time.Now(), id,
	).Error
}

// NoteSuccessForPauseError 路由请求成功时调用：
// 1) 立刻结束冷却（恢复可调度）；
// 2) 若仍有错误残留，累加连续成功次数；
// 3) 达到 RouteRecoverSuccessClearStreak 后清空「已恢复/错误/清除」相关展示字段。
func (r *GatewayRoutes) NoteSuccessForPauseError(id uint) error {
	if id == 0 {
		return nil
	}
	now := time.Now()
	// 仅处理仍有暂停/错误残留的路由，避免无意义写放大
	if err := r.db.Exec(
		`UPDATE gateway_routes
		 SET recover_success_streak = recover_success_streak + 1,
		     temp_unschedulable_until = NULL,
		     updated_at = ?
		 WHERE id = ?
		   AND (
		     (temp_unschedulable_reason IS NOT NULL AND temp_unschedulable_reason != '')
		     OR temp_unschedulable_until IS NOT NULL
		     OR (temp_unschedulable_request_id IS NOT NULL AND temp_unschedulable_request_id != '')
		     OR temp_unschedulable_at IS NOT NULL
		   )`,
		now, id,
	).Error; err != nil {
		return err
	}
	return r.db.Exec(
		`UPDATE gateway_routes
		 SET temp_unschedulable_until = NULL,
		     temp_unschedulable_reason = '',
		     temp_unschedulable_at = NULL,
		     temp_unschedulable_request_id = '',
		     recover_success_streak = 0,
		     updated_at = ?
		 WHERE id = ? AND recover_success_streak >= ?`,
		now, id, RouteRecoverSuccessClearStreak,
	).Error
}

// GatewayUsageLogs 使用记录仓储。
type GatewayUsageLogs struct{ db *gorm.DB }

// NewGatewayUsageLogs 构造用量日志仓储。
func NewGatewayUsageLogs(db *gorm.DB) *GatewayUsageLogs { return &GatewayUsageLogs{db: db} }

// GatewayDispatchStatsGroup 是主页调度健康面板的网关组聚合结果。
type GatewayDispatchStatsGroup struct {
	GatewayGroupID   uint                        `json:"gateway_group_id"`
	GatewayGroupName string                      `json:"gateway_group_name"`
	Routes           []GatewayDispatchStatsRoute `json:"routes"`
}

// GatewayDispatchStatsRoute 是单条路由在时间窗口内的尝试聚合结果。
type GatewayDispatchStatsRoute struct {
	RouteID               uint     `json:"route_id"`
	RouteName             string   `json:"route_name"`
	ProviderName          string   `json:"provider_name,omitempty"`
	SourceAPIKeyName      string   `json:"source_api_key_name,omitempty"`
	SourceGroupName       string   `json:"source_group_name,omitempty"`
	BillingRateMultiplier float64  `json:"billing_rate_multiplier"`
	RouteAvailable        bool     `json:"route_available"`
	TotalAttempts         int64    `json:"total_attempts"`
	FailedAttempts        int64    `json:"failed_attempts"`
	FailureRate           float64  `json:"failure_rate"`
	FirstTokenSamples     int64    `json:"first_token_samples"`
	AverageFirstTokenMS   *float64 `json:"average_first_token_ms"`
}

type GatewayDispatchTrendPoint struct {
	Timestamp            time.Time `json:"timestamp"`
	TTFTP50              float64   `json:"ttft_p50"`
	TTFTP90              float64   `json:"ttft_p90"`
	TTFTP95              float64   `json:"ttft_p95"`
	TTFTAvg              float64   `json:"ttft_avg"`
	TTFTMax              float64   `json:"ttft_max"`
	FinalErrorRate       float64   `json:"final_error_rate"`
	FailoverTriggerRate  float64   `json:"failover_trigger_rate"`
	FailoverRecoveryRate float64   `json:"failover_recovery_rate"`
	Requests             int64     `json:"requests"`
	RPM                  float64   `json:"rpm"`
}

type GatewayDispatchTrendRoute struct {
	RouteID      uint                        `json:"route_id"`
	RouteName    string                      `json:"route_name"`
	ProviderName string                      `json:"provider_name,omitempty"`
	Points       []GatewayDispatchTrendPoint `json:"points"`
}

type GatewayDispatchTrendGroup struct {
	GatewayGroupID   uint                        `json:"gateway_group_id"`
	GatewayGroupName string                      `json:"gateway_group_name"`
	Points           []GatewayDispatchTrendPoint `json:"points"`
	Routes           []GatewayDispatchTrendRoute `json:"routes"`
}

type GatewayDispatchTrends struct {
	From   time.Time                   `json:"from"`
	To     time.Time                   `json:"to"`
	Bucket string                      `json:"bucket"`
	Groups []GatewayDispatchTrendGroup `json:"groups"`
}

func dispatchTrendBucketLabel(bucket time.Duration) string {
	switch bucket {
	case time.Minute:
		return "1m"
	case 3 * time.Minute:
		return "3m"
	case 5 * time.Minute:
		return "5m"
	case 10 * time.Minute:
		return "10m"
	case 30 * time.Minute:
		return "30m"
	default:
		return bucket.String()
	}
}

// DispatchTrends aggregates request chains into fixed time buckets. The raw
// attempts are grouped in Go so the same logic works for SQLite and MySQL.
func (r *GatewayUsageLogs) DispatchTrends(from, to time.Time, bucket time.Duration) (GatewayDispatchTrends, error) {
	if from.IsZero() || to.IsZero() || !to.After(from) || bucket <= 0 {
		return GatewayDispatchTrends{}, fmt.Errorf("invalid dispatch trend range")
	}
	var logs []GatewayUsageLog
	query := r.db.Where("created_at >= ? AND created_at < ?", from, to)
	if isSQLite(r.db) {
		query = r.db.Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) < ?", from.Unix(), to.Unix())
	}
	if err := query.Order("created_at ASC, id ASC").Find(&logs).Error; err != nil {
		return GatewayDispatchTrends{}, err
	}
	result := GatewayDispatchTrends{From: from, To: to, Bucket: dispatchTrendBucketLabel(bucket), Groups: []GatewayDispatchTrendGroup{}}
	if len(logs) == 0 {
		return result, nil
	}
	type chain struct{ logs []GatewayUsageLog }
	chains := make(map[string]*chain)
	groupNames := make(map[uint]string)
	for _, log := range logs {
		key := fmt.Sprintf("%d:%s", log.GatewayGroupID, log.RequestID)
		if chains[key] == nil {
			chains[key] = &chain{}
		}
		chains[key].logs = append(chains[key].logs, log)
		if _, ok := groupNames[log.GatewayGroupID]; !ok {
			groupNames[log.GatewayGroupID] = fmt.Sprintf("组 #%d", log.GatewayGroupID)
		}
	}
	var dbGroups []GatewayGroup
	if err := r.db.Where("id IN ?", keysUint(groupNames)).Find(&dbGroups).Error; err == nil {
		for _, group := range dbGroups {
			groupNames[group.ID] = group.Name
		}
	}
	type bucketData struct {
		values []float64
		chains map[string]*chain
	}
	groupBuckets := make(map[uint]map[time.Time]*bucketData)
	routeBuckets := make(map[uint]map[uint]map[time.Time]*bucketData)
	for key, current := range chains {
		_ = key
		if len(current.logs) == 0 {
			continue
		}
		first := current.logs[0]
		bucketStart := from.Add(time.Duration(first.CreatedAt.Sub(from)/bucket) * bucket)
		if bucketStart.Before(from) {
			bucketStart = from
		}
		groupMap := groupBuckets[first.GatewayGroupID]
		if groupMap == nil {
			groupMap = map[time.Time]*bucketData{}
			groupBuckets[first.GatewayGroupID] = groupMap
		}
		data := groupMap[bucketStart]
		if data == nil {
			data = &bucketData{chains: map[string]*chain{}}
			groupMap[bucketStart] = data
		}
		data.chains[key] = current
		routeMap := routeBuckets[first.GatewayGroupID]
		if routeMap == nil {
			routeMap = map[uint]map[time.Time]*bucketData{}
			routeBuckets[first.GatewayGroupID] = routeMap
		}
		for _, attempt := range current.logs {
			routeDataMap := routeMap[attempt.RouteID]
			if routeDataMap == nil {
				routeDataMap = map[time.Time]*bucketData{}
				routeMap[attempt.RouteID] = routeDataMap
			}
			routeData := routeDataMap[bucketStart]
			if routeData == nil {
				routeData = &bucketData{chains: map[string]*chain{}}
				routeDataMap[bucketStart] = routeData
			}
			routeData.chains[key] = current
		}
	}
	buildPoint := func(data *bucketData, start time.Time, routeID uint) GatewayDispatchTrendPoint {
		point := GatewayDispatchTrendPoint{Timestamp: start, RPM: 0}
		values := make([]float64, 0)
		for _, current := range data.chains {
			logs := current.logs
			if routeID != 0 {
				logs = make([]GatewayUsageLog, 0, len(current.logs))
				for _, attempt := range current.logs {
					if attempt.RouteID == routeID {
						logs = append(logs, attempt)
					}
				}
				if len(logs) == 0 {
					continue
				}
			}
			point.Requests++
			final := logs[len(logs)-1]
			if !final.Success {
				point.FinalErrorRate++
			}
			triggered := false
			for _, attempt := range logs {
				if attempt.Attempt > 1 || attempt.AttemptKind == "retry" || attempt.AttemptKind == "failover" {
					triggered = true
				}
				if attempt.FirstTokenMS != nil {
					values = append(values, float64(*attempt.FirstTokenMS))
				}
			}
			if triggered {
				point.FailoverTriggerRate++
				if final.Success {
					point.FailoverRecoveryRate++
				}
			}
		}
		if point.Requests > 0 {
			point.FinalErrorRate /= float64(point.Requests)
			point.FailoverTriggerRate /= float64(point.Requests)
		}
		triggeredCount := point.FailoverTriggerRate * float64(point.Requests)
		if triggeredCount > 0 {
			point.FailoverRecoveryRate /= triggeredCount
		} else {
			point.FailoverRecoveryRate = 0
		}
		if len(values) > 0 {
			sort.Float64s(values)
			point.TTFTAvg = averageFloat(values)
			point.TTFTP50 = percentileFloat(values, .50)
			point.TTFTP90 = percentileFloat(values, .90)
			point.TTFTP95 = percentileFloat(values, .95)
			point.TTFTMax = values[len(values)-1]
		}
		point.RPM = float64(point.Requests) / bucket.Minutes()
		return point
	}
	for groupID, bucketMap := range groupBuckets {
		group := GatewayDispatchTrendGroup{GatewayGroupID: groupID, GatewayGroupName: groupNames[groupID], Points: make([]GatewayDispatchTrendPoint, 0), Routes: make([]GatewayDispatchTrendRoute, 0)}
		for start := from; start.Before(to); start = start.Add(bucket) {
			if data := bucketMap[start]; data != nil {
				group.Points = append(group.Points, buildPoint(data, start, 0))
			}
		}
		for routeID, routeMap := range routeBuckets[groupID] {
			route := GatewayDispatchTrendRoute{RouteID: routeID, RouteName: fmt.Sprintf("路由 #%d", routeID), Points: make([]GatewayDispatchTrendPoint, 0)}
			for _, bucketData := range routeMap {
				for _, current := range bucketData.chains {
					for _, log := range current.logs {
						if log.RouteID == routeID {
							if log.SourceAPIKeyName != "" {
								route.RouteName = log.SourceAPIKeyName
							}
							if route.ProviderName == "" {
								route.ProviderName = log.ProviderName
							}
							break
						}
					}
				}
			}
			for start := from; start.Before(to); start = start.Add(bucket) {
				if data := routeMap[start]; data != nil {
					route.Points = append(route.Points, buildPoint(data, start, routeID))
				}
			}
			group.Routes = append(group.Routes, route)
		}
		sort.Slice(group.Routes, func(i, j int) bool { return group.Routes[i].RouteID < group.Routes[j].RouteID })
		result.Groups = append(result.Groups, group)
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i].GatewayGroupID < result.Groups[j].GatewayGroupID })
	return result, nil
}

// ---- 调度错误分布 ----

type GatewayDispatchErrorCode struct {
	StatusCode int    `json:"status_code"`
	Label      string `json:"label"`
	Count      int    `json:"count"`
}

type GatewayDispatchErrorCategory struct {
	ErrorType string                     `json:"error_type"`
	Label     string                     `json:"label"`
	Count     int                        `json:"count"`
	Codes     []GatewayDispatchErrorCode `json:"codes"`
}

type GatewayDispatchErrorSample struct {
	Message string `json:"message"`
	// Severity 0/1/2 对应 P0/P1/P2，口径见 dispatchSeverityOf
	Severity   int       `json:"severity"`
	ErrorType  string    `json:"error_type"`
	StatusCode int       `json:"status_code"`
	Count      int       `json:"count"`
	LastSeen   time.Time `json:"last_seen"`
}

// GatewayDispatchErrorScope 是「总体 / 单个网关 / 单条路由」三层共用的统计口径。
// Requests / FinalFailed / ErrorRate / Recovered 是**请求链**口径（一条请求 = 一条
// group:request_id 链，最终失败 = 链上最后一次尝试失败）；Attempts / FailedAttempts
// 以及由此产出的 Categories / Samples 是**尝试**口径，含被顺延救回的那些失败。
// 路由层没有「最终失败」概念（顺延后可能由别的路由收尾），所以路由只填尝试口径。
type GatewayDispatchErrorScope struct {
	Requests          int     `json:"requests"`
	FinalFailed       int     `json:"final_failed"`
	ErrorRate         float64 `json:"error_rate"`
	RecoveredRequests int     `json:"recovered_requests"`
	Attempts          int     `json:"attempts"`
	FailedAttempts    int     `json:"failed_attempts"`
	AttemptErrorRate  float64 `json:"attempt_error_rate"`
	// Severity 按处理紧急度把失败尝试分成 P0/P1/P2
	Severity   GatewayDispatchSeverityCounts  `json:"severity"`
	Categories []GatewayDispatchErrorCategory `json:"categories"`
	Samples    []GatewayDispatchErrorSample   `json:"samples"`
}

type GatewayDispatchErrorRoute struct {
	RouteID      uint   `json:"route_id"`
	RouteName    string `json:"route_name"`
	ProviderName string `json:"provider_name,omitempty"`
	GatewayDispatchErrorScope
}

type GatewayDispatchErrorGroup struct {
	GatewayGroupID   uint   `json:"gateway_group_id"`
	GatewayGroupName string `json:"gateway_group_name"`
	GatewayDispatchErrorScope
	Routes []GatewayDispatchErrorRoute `json:"routes"`
}

type GatewayDispatchErrors struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	GatewayDispatchErrorScope
	Groups []GatewayDispatchErrorGroup `json:"groups"`
}

func dispatchErrorTypeLabel(errorType string) string {
	switch errorType {
	case "transport":
		return "传输错误"
	case "http":
		return "上游 HTTP 错误"
	case "config":
		return "配置错误"
	case "internal":
		return "内部错误"
	case "client":
		return "客户端断开"
	case "":
		return "未分类"
	default:
		return errorType
	}
}

func dispatchStatusCodeLabel(code int) string {
	if code <= 0 {
		return "无响应"
	}
	return strconv.Itoa(code)
}

type dispatchSampleAcc struct {
	message    string
	severity   int
	errorType  string
	statusCode int
	count      int
	lastSeen   time.Time
}

// dispatchErrorAcc 累加一个作用域内的尝试口径统计，总体/网关/路由三层共用。
type dispatchErrorAcc struct {
	attempts       int
	failedAttempts int
	severity       GatewayDispatchSeverityCounts
	typeCounts     map[string]int
	codeCounts     map[string]map[int]int
	samples        map[string]*dispatchSampleAcc
}

func newDispatchErrorAcc() *dispatchErrorAcc {
	return &dispatchErrorAcc{
		typeCounts: make(map[string]int),
		codeCounts: make(map[string]map[int]int),
		samples:    make(map[string]*dispatchSampleAcc),
	}
}

func (a *dispatchErrorAcc) add(log GatewayUsageLog) {
	a.attempts++
	if log.Success {
		return
	}
	a.failedAttempts++
	switch dispatchSeverityOf(log) {
	case dispatchSeverityP0:
		a.severity.P0++
	case dispatchSeverityP2:
		a.severity.P2++
	default:
		a.severity.P1++
	}
	a.typeCounts[log.ErrorType]++
	if a.codeCounts[log.ErrorType] == nil {
		a.codeCounts[log.ErrorType] = make(map[int]int)
	}
	a.codeCounts[log.ErrorType][log.StatusCode]++
	message := strings.TrimSpace(log.ErrorMessage)
	if message == "" {
		message = dispatchErrorTypeLabel(log.ErrorType)
	}
	if len([]rune(message)) > 160 {
		message = string([]rune(message)[:160]) + "…"
	}
	key := fmt.Sprintf("%s|%d|%s", log.ErrorType, log.StatusCode, message)
	sample := a.samples[key]
	if sample == nil {
		sample = &dispatchSampleAcc{message: message, errorType: log.ErrorType, statusCode: log.StatusCode, severity: dispatchSeverityOf(log)}
		a.samples[key] = sample
	}
	sample.count++
	if log.CreatedAt.After(sample.lastSeen) {
		sample.lastSeen = log.CreatedAt
	}
}

func (a *dispatchErrorAcc) fill(scope *GatewayDispatchErrorScope) {
	scope.Attempts = a.attempts
	scope.FailedAttempts = a.failedAttempts
	scope.Severity = a.severity
	if a.attempts > 0 {
		scope.AttemptErrorRate = float64(a.failedAttempts) / float64(a.attempts)
	}
	scope.Categories = make([]GatewayDispatchErrorCategory, 0, len(a.typeCounts))
	for errorType, count := range a.typeCounts {
		category := GatewayDispatchErrorCategory{
			ErrorType: errorType, Label: dispatchErrorTypeLabel(errorType), Count: count,
			Codes: make([]GatewayDispatchErrorCode, 0, len(a.codeCounts[errorType])),
		}
		for code, codeCount := range a.codeCounts[errorType] {
			category.Codes = append(category.Codes, GatewayDispatchErrorCode{StatusCode: code, Label: dispatchStatusCodeLabel(code), Count: codeCount})
		}
		sort.Slice(category.Codes, func(i, j int) bool {
			if category.Codes[i].Count != category.Codes[j].Count {
				return category.Codes[i].Count > category.Codes[j].Count
			}
			return category.Codes[i].StatusCode < category.Codes[j].StatusCode
		})
		scope.Categories = append(scope.Categories, category)
	}
	sort.Slice(scope.Categories, func(i, j int) bool {
		if scope.Categories[i].Count != scope.Categories[j].Count {
			return scope.Categories[i].Count > scope.Categories[j].Count
		}
		return scope.Categories[i].ErrorType < scope.Categories[j].ErrorType
	})
	scope.Samples = make([]GatewayDispatchErrorSample, 0, len(a.samples))
	for _, sample := range a.samples {
		scope.Samples = append(scope.Samples, GatewayDispatchErrorSample{
			Message: sample.message, Severity: sample.severity, ErrorType: sample.errorType, StatusCode: sample.statusCode,
			Count: sample.count, LastSeen: sample.lastSeen,
		})
	}
	sort.Slice(scope.Samples, func(i, j int) bool {
		if scope.Samples[i].Count != scope.Samples[j].Count {
			return scope.Samples[i].Count > scope.Samples[j].Count
		}
		return scope.Samples[i].Message < scope.Samples[j].Message
	})
	if len(scope.Samples) > 8 {
		scope.Samples = scope.Samples[:8]
	}
}

// DispatchErrors 汇总窗口内的失败分布，输出「总体 → 网关 → 路由」三层，
// 供前端下钻定位到具体网关和网关下的具体路由。口径见 GatewayDispatchErrorScope。
func (r *GatewayUsageLogs) DispatchErrors(from, to time.Time) (GatewayDispatchErrors, error) {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return GatewayDispatchErrors{}, fmt.Errorf("invalid dispatch error range")
	}
	var logs []GatewayUsageLog
	query := r.db.Where("created_at >= ? AND created_at < ?", from, to)
	if isSQLite(r.db) {
		query = r.db.Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) < ?", from.Unix(), to.Unix())
	}
	if err := query.Order("created_at ASC, id ASC").Find(&logs).Error; err != nil {
		return GatewayDispatchErrors{}, err
	}
	result := GatewayDispatchErrors{From: from, To: to, Groups: []GatewayDispatchErrorGroup{}}
	totalAcc := newDispatchErrorAcc()
	totalAcc.fill(&result.GatewayDispatchErrorScope)
	if len(logs) == 0 {
		return result, nil
	}

	type routeAgg struct {
		name     string
		provider string
		acc      *dispatchErrorAcc
		chains   map[string]struct{}
	}
	type groupAgg struct {
		name   string
		acc    *dispatchErrorAcc
		routes map[uint]*routeAgg
	}
	type chainState struct {
		groupID   uint
		lastOK    bool
		anyFailed bool
	}

	groups := make(map[uint]*groupAgg)
	groupOrder := make([]uint, 0)
	chains := make(map[string]*chainState)
	chainOrder := make([]string, 0)

	for _, log := range logs {
		group := groups[log.GatewayGroupID]
		if group == nil {
			group = &groupAgg{name: fmt.Sprintf("组 #%d", log.GatewayGroupID), acc: newDispatchErrorAcc(), routes: make(map[uint]*routeAgg)}
			groups[log.GatewayGroupID] = group
			groupOrder = append(groupOrder, log.GatewayGroupID)
		}
		route := group.routes[log.RouteID]
		if route == nil {
			route = &routeAgg{name: fmt.Sprintf("路由 #%d", log.RouteID), acc: newDispatchErrorAcc(), chains: make(map[string]struct{})}
			group.routes[log.RouteID] = route
		}
		// 与 DispatchTrends 保持一致：路由名取最后出现的 SourceAPIKeyName，
		// provider 取首个非空值。
		if log.SourceAPIKeyName != "" {
			route.name = log.SourceAPIKeyName
		}
		if route.provider == "" {
			route.provider = log.ProviderName
		}
		route.chains[log.RequestID] = struct{}{}

		totalAcc.add(log)
		group.acc.add(log)
		route.acc.add(log)

		key := fmt.Sprintf("%d:%s", log.GatewayGroupID, log.RequestID)
		state := chains[key]
		if state == nil {
			state = &chainState{groupID: log.GatewayGroupID}
			chains[key] = state
			chainOrder = append(chainOrder, key)
		}
		state.lastOK = log.Success
		if !log.Success {
			state.anyFailed = true
		}
	}

	groupChainStats := make(map[uint]*GatewayDispatchErrorScope)
	for _, key := range chainOrder {
		state := chains[key]
		stat := groupChainStats[state.groupID]
		if stat == nil {
			stat = &GatewayDispatchErrorScope{}
			groupChainStats[state.groupID] = stat
		}
		result.Requests++
		stat.Requests++
		if !state.lastOK {
			result.FinalFailed++
			stat.FinalFailed++
		} else if state.anyFailed {
			result.RecoveredRequests++
			stat.RecoveredRequests++
		}
	}
	if result.Requests > 0 {
		result.ErrorRate = float64(result.FinalFailed) / float64(result.Requests)
	}
	totalAcc.fill(&result.GatewayDispatchErrorScope)

	var dbGroups []GatewayGroup
	if err := r.db.Where("id IN ?", groupOrder).Find(&dbGroups).Error; err == nil {
		for _, dbGroup := range dbGroups {
			if group := groups[dbGroup.ID]; group != nil {
				group.name = dbGroup.Name
			}
		}
	}

	for _, groupID := range groupOrder {
		group := groups[groupID]
		item := GatewayDispatchErrorGroup{GatewayGroupID: groupID, GatewayGroupName: group.name, Routes: []GatewayDispatchErrorRoute{}}
		if stat := groupChainStats[groupID]; stat != nil {
			item.Requests = stat.Requests
			item.FinalFailed = stat.FinalFailed
			item.RecoveredRequests = stat.RecoveredRequests
			if stat.Requests > 0 {
				item.ErrorRate = float64(stat.FinalFailed) / float64(stat.Requests)
			}
		}
		group.acc.fill(&item.GatewayDispatchErrorScope)
		for routeID, route := range group.routes {
			routeItem := GatewayDispatchErrorRoute{RouteID: routeID, RouteName: route.name, ProviderName: route.provider}
			routeItem.Requests = len(route.chains)
			route.acc.fill(&routeItem.GatewayDispatchErrorScope)
			item.Routes = append(item.Routes, routeItem)
		}
		sort.Slice(item.Routes, func(i, j int) bool {
			if item.Routes[i].FailedAttempts != item.Routes[j].FailedAttempts {
				return item.Routes[i].FailedAttempts > item.Routes[j].FailedAttempts
			}
			return item.Routes[i].RouteID < item.Routes[j].RouteID
		})
		result.Groups = append(result.Groups, item)
	}
	sort.Slice(result.Groups, func(i, j int) bool {
		if result.Groups[i].FailedAttempts != result.Groups[j].FailedAttempts {
			return result.Groups[i].FailedAttempts > result.Groups[j].FailedAttempts
		}
		return result.Groups[i].GatewayGroupID < result.Groups[j].GatewayGroupID
	})
	return result, nil
}

// 错误分级按「要不要人工介入」划分，不是按 HTTP 状态码，也不是按用户影响面：
//
//	P0 需人工处理——认证失效 / 欠费 / 分组被删 / 配置写错，放着永远不会自愈
//	P1 上游抖动——5xx / 429 / 超时 / 传输错，可能自愈但要盯着
//	P2 噪声——客户端主动断开或取消，通常不用管
const (
	dispatchSeverityP0 = 0
	dispatchSeverityP1 = 1
	dispatchSeverityP2 = 2
)

// dispatchSeverityOf 只对失败的尝试有意义；成功返回 -1。
func dispatchSeverityOf(log GatewayUsageLog) int {
	if log.Success {
		return -1
	}
	switch log.ErrorType {
	case "client":
		return dispatchSeverityP2
	case "config":
		// 模型不在路由列表、协议转换失败之类，全都得改配置才好
		return dispatchSeverityP0
	}
	switch log.StatusCode {
	case 499:
		return dispatchSeverityP2 // 客户端提前关闭连接
	case 401, 402, 403:
		return dispatchSeverityP0 // 认证失效 / 欠费 / 无权限
	case 404:
		return dispatchSeverityP0 // 模型或路径不存在，多半是模型映射配错
	}
	return dispatchSeverityP1
}

type GatewayDispatchSeverityCounts struct {
	P0 int `json:"p0"`
	P1 int `json:"p1"`
	P2 int `json:"p2"`
}

// GatewayDispatchAttempt 是「最近请求状态」里的一格。
// 只带够画一格 + 悬浮解释的字段，不把整条日志抬出来。
// ---- 调度流向 ----
//
// 这一块回答的是「请求在网关里怎么流的」：默认把窗口内所有请求按网关分流，再分到
// 三种结局；下钻到某个网关后，按「第几跳打在哪条路由上」逐层展开——顺延本来就是
// 一条链上的流动，用桑基图画比任何汇总表都直白。
//
// 跟 DispatchErrors 的分工：那个按错误类型下钻（错在哪），这个看流量走向（去哪了）。

// dispatchFlowMaxHops 桑基图最多铺开几跳；更深的顺延收进一个「更深层顺延」节点，
// 免得少数极端链把图拉得又长又细。
const dispatchFlowMaxHops = 5

const (
	dispatchFlowNodeRoot     = "root"
	dispatchFlowNodeGateway  = "gateway"
	dispatchFlowNodeRoute    = "route"
	dispatchFlowNodeOverflow = "overflow"
	dispatchFlowNodeOutcome  = "outcome"
)

// 三种结局。拆成三个而不是「成功/失败」两个，是因为「一次就过」和「顺延了几跳才救回来」
// 对上游质量的含义完全不同，前者不用管，后者是在拿延迟换成功率。
const (
	dispatchOutcomeDirect    = "direct"
	dispatchOutcomeRecovered = "recovered"
	dispatchOutcomeFailed    = "failed"
)

type GatewayDispatchFlowNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Kind: root | gateway | route | overflow | outcome
	Kind  string `json:"kind"`
	Depth int    `json:"depth"`
	Value int    `json:"value"`

	GatewayGroupID uint `json:"gateway_group_id,omitempty"`
	RouteID        uint `json:"route_id,omitempty"`
	// Hop 路由节点在第几跳（1 = 首发）
	Hop int `json:"hop,omitempty"`
	// Alive=false 表示路由已删除，只剩历史日志，前端不给跳转
	Alive   bool   `json:"alive,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

type GatewayDispatchFlowLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  int    `json:"value"`
	// Failed=true 表示这股流量是「上一跳失败之后转走的」
	Failed bool `json:"failed"`
}

type GatewayDispatchFlow struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Scope: all（按网关分流）| gateway（按跳数分流到路由）
	Scope            string `json:"scope"`
	GatewayGroupID   uint   `json:"gateway_group_id,omitempty"`
	GatewayGroupName string `json:"gateway_group_name,omitempty"`
	Requests         int    `json:"requests"`
	Attempts         int    `json:"attempts"`
	// MaxHops 窗口内实际出现过的最深跳数（可能超过 dispatchFlowMaxHops）
	MaxHops int                       `json:"max_hops"`
	Nodes   []GatewayDispatchFlowNode `json:"nodes"`
	Links   []GatewayDispatchFlowLink `json:"links"`
	// Gateways 窗口内有流量的网关清单，**不受 groupID 过滤影响**。
	// 下钻之后前端还要用它画切换用的 tag，所以不能让前端从 nodes 里凑。
	Gateways []GatewayDispatchFlowGateway `json:"gateways"`
}

type GatewayDispatchFlowGateway struct {
	GatewayGroupID uint   `json:"gateway_group_id"`
	Name           string `json:"name"`
	Requests       int    `json:"requests"`
}

// dispatchFlowAttempt 是一条链上的一次尝试，只留画图要用的字段。
type dispatchFlowAttempt struct {
	routeID uint
	success bool
}

type dispatchFlowChain struct {
	groupID  uint
	attempts []dispatchFlowAttempt
}

// dispatchRouteIdentity 把日志快照里的来源信息拼成人能认出来的名字。
// 优先级跟前端 formatRouteIdentity 一致：来源（渠道名 / provider 名）· 源分组，
// 密钥名只在两者双双缺失时兜底——密钥名（uops-ch9-sgn-xxx）对人没有意义。
type dispatchRouteIdentity struct {
	channelID uint
	provider  string
	srcGroup  string
	keyName   string
}

func (d dispatchRouteIdentity) label(routeID uint, channelNames map[uint]string) string {
	parts := make([]string, 0, 2)
	if name := channelNames[d.channelID]; name != "" {
		parts = append(parts, name)
	} else if d.provider != "" {
		parts = append(parts, d.provider)
	}
	if d.srcGroup != "" {
		parts = append(parts, d.srcGroup)
	}
	if len(parts) > 0 {
		return strings.Join(parts, " · ")
	}
	if d.keyName != "" {
		return d.keyName
	}
	return fmt.Sprintf("路由 #%d", routeID)
}

func (d *dispatchRouteIdentity) absorb(log GatewayUsageLog) {
	// 取最后出现的非空值：路由改过名就以最新的为准
	if log.ChannelID > 0 {
		d.channelID = log.ChannelID
	}
	if log.ProviderName != "" {
		d.provider = log.ProviderName
	}
	if log.SourceGroupName != "" {
		d.srcGroup = log.SourceGroupName
	}
	if log.SourceAPIKeyName != "" {
		d.keyName = log.SourceAPIKeyName
	}
}

// dispatchFlowBuilder 累积节点和边，保证同一个 id 只出现一次。
type dispatchFlowBuilder struct {
	nodes map[string]*GatewayDispatchFlowNode
	order []string
	links map[string]*GatewayDispatchFlowLink
	edges []string
}

func newDispatchFlowBuilder() *dispatchFlowBuilder {
	return &dispatchFlowBuilder{
		nodes: make(map[string]*GatewayDispatchFlowNode),
		links: make(map[string]*GatewayDispatchFlowLink),
	}
}

func (b *dispatchFlowBuilder) node(node GatewayDispatchFlowNode) {
	if _, ok := b.nodes[node.ID]; ok {
		return
	}
	copied := node
	b.nodes[node.ID] = &copied
	b.order = append(b.order, node.ID)
}

func (b *dispatchFlowBuilder) link(source, target string, failed bool) {
	key := source + "\x00" + target
	existing := b.links[key]
	if existing == nil {
		b.links[key] = &GatewayDispatchFlowLink{Source: source, Target: target, Value: 1, Failed: failed}
		b.edges = append(b.edges, key)
		return
	}
	existing.Value++
}

// build 收口：节点权重按入边求和（根节点没有入边，用出边），再按层 + 权重排序。
func (b *dispatchFlowBuilder) build() ([]GatewayDispatchFlowNode, []GatewayDispatchFlowLink) {
	links := make([]GatewayDispatchFlowLink, 0, len(b.edges))
	for _, key := range b.edges {
		links = append(links, *b.links[key])
	}
	outgoing := make(map[string]int)
	for _, link := range links {
		if node := b.nodes[link.Target]; node != nil {
			node.Value += link.Value
		}
		outgoing[link.Source] += link.Value
	}
	nodes := make([]GatewayDispatchFlowNode, 0, len(b.order))
	for _, id := range b.order {
		node := b.nodes[id]
		if node.Value == 0 {
			node.Value = outgoing[id]
		}
		nodes = append(nodes, *node)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Depth != nodes[j].Depth {
			return nodes[i].Depth < nodes[j].Depth
		}
		if nodes[i].Value != nodes[j].Value {
			return nodes[i].Value > nodes[j].Value
		}
		return nodes[i].ID < nodes[j].ID
	})
	sort.SliceStable(links, func(i, j int) bool {
		if links[i].Value != links[j].Value {
			return links[i].Value > links[j].Value
		}
		if links[i].Source != links[j].Source {
			return links[i].Source < links[j].Source
		}
		return links[i].Target < links[j].Target
	})
	return nodes, links
}

func dispatchOutcomeOf(attempts []dispatchFlowAttempt) string {
	if len(attempts) == 0 {
		return dispatchOutcomeFailed
	}
	if !attempts[len(attempts)-1].success {
		return dispatchOutcomeFailed
	}
	if len(attempts) == 1 {
		return dispatchOutcomeDirect
	}
	return dispatchOutcomeRecovered
}

var dispatchOutcomeLabels = map[string]string{
	dispatchOutcomeDirect:    "一次过",
	dispatchOutcomeRecovered: "顺延后成功",
	dispatchOutcomeFailed:    "最终失败",
}

// DispatchFlow 汇总窗口内的请求流向。groupID = 0 时是「全部网关」视图，
// 否则下钻到该网关内部，按跳数展开到具体路由。
func (r *GatewayUsageLogs) DispatchFlow(from, to time.Time, groupID uint) (GatewayDispatchFlow, error) {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return GatewayDispatchFlow{}, fmt.Errorf("invalid dispatch flow range")
	}
	scope := "all"
	if groupID > 0 {
		scope = "gateway"
	}
	result := GatewayDispatchFlow{
		From: from, To: to, Scope: scope, GatewayGroupID: groupID,
		Nodes: []GatewayDispatchFlowNode{}, Links: []GatewayDispatchFlowLink{},
		Gateways: []GatewayDispatchFlowGateway{},
	}
	gateways, err := r.dispatchFlowGateways(from, to)
	if err != nil {
		return GatewayDispatchFlow{}, err
	}
	result.Gateways = gateways

	var logs []GatewayUsageLog
	query := r.db.Where("created_at >= ? AND created_at < ?", from, to)
	if isSQLite(r.db) {
		query = r.db.Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) < ?", from.Unix(), to.Unix())
	}
	if groupID > 0 {
		query = query.Where("gateway_group_id = ?", groupID)
	}
	if err := query.Order("created_at ASC, id ASC").Find(&logs).Error; err != nil {
		return GatewayDispatchFlow{}, err
	}
	if len(logs) == 0 {
		return result, nil
	}

	chains := make(map[string]*dispatchFlowChain)
	chainOrder := make([]string, 0)
	identities := make(map[uint]*dispatchRouteIdentity)
	groupIDs := make(map[uint]struct{})
	channelIDs := make(map[uint]struct{})
	routeIDs := make(map[uint]struct{})

	for _, log := range logs {
		key := fmt.Sprintf("%d:%s", log.GatewayGroupID, log.RequestID)
		chain := chains[key]
		if chain == nil {
			chain = &dispatchFlowChain{groupID: log.GatewayGroupID}
			chains[key] = chain
			chainOrder = append(chainOrder, key)
		}
		chain.attempts = append(chain.attempts, dispatchFlowAttempt{routeID: log.RouteID, success: log.Success})
		groupIDs[log.GatewayGroupID] = struct{}{}
		routeIDs[log.RouteID] = struct{}{}
		if log.ChannelID > 0 {
			channelIDs[log.ChannelID] = struct{}{}
		}
		identity := identities[log.RouteID]
		if identity == nil {
			identity = &dispatchRouteIdentity{}
			identities[log.RouteID] = identity
		}
		identity.absorb(log)
	}
	result.Requests = len(chainOrder)
	result.Attempts = len(logs)

	groupNames := make(map[uint]string)
	if len(groupIDs) > 0 {
		var dbGroups []GatewayGroup
		if err := r.db.Where("id IN ?", keysOfUintSet(groupIDs)).Find(&dbGroups).Error; err == nil {
			for _, group := range dbGroups {
				groupNames[group.ID] = group.Name
			}
		}
	}
	result.GatewayGroupName = groupNames[groupID]
	if groupID > 0 && result.GatewayGroupName == "" {
		result.GatewayGroupName = fmt.Sprintf("组 #%d", groupID)
	}
	channelNames := make(map[uint]string)
	if len(channelIDs) > 0 {
		var dbChannels []Channel
		if err := r.db.Where("id IN ?", keysOfUintSet(channelIDs)).Find(&dbChannels).Error; err == nil {
			for _, channel := range dbChannels {
				channelNames[channel.ID] = channel.Name
			}
		}
	}
	aliveRoutes := make(map[uint]struct{})
	if len(routeIDs) > 0 {
		var dbRoutes []GatewayRoute
		if err := r.db.Where("id IN ?", keysOfUintSet(routeIDs)).Find(&dbRoutes).Error; err == nil {
			for _, route := range dbRoutes {
				aliveRoutes[route.ID] = struct{}{}
			}
		}
	}

	builder := newDispatchFlowBuilder()
	outcomeDepth := 2
	if groupID > 0 {
		outcomeDepth = dispatchFlowMaxHops + 2
	}
	outcomeID := func(outcome string) string { return "o:" + outcome }
	ensureOutcome := func(outcome string) string {
		id := outcomeID(outcome)
		builder.node(GatewayDispatchFlowNode{
			ID: id, Label: dispatchOutcomeLabels[outcome], Kind: dispatchFlowNodeOutcome,
			Depth: outcomeDepth, Outcome: outcome,
		})
		return id
	}

	if groupID == 0 {
		// 全部网关：全部请求 → 各网关 → 三种结局
		const rootID = "root"
		builder.node(GatewayDispatchFlowNode{ID: rootID, Label: "全部请求", Kind: dispatchFlowNodeRoot, Depth: 0})
		for _, key := range chainOrder {
			chain := chains[key]
			gatewayID := fmt.Sprintf("g:%d", chain.groupID)
			name := groupNames[chain.groupID]
			if name == "" {
				name = fmt.Sprintf("组 #%d", chain.groupID)
			}
			builder.node(GatewayDispatchFlowNode{
				ID: gatewayID, Label: name, Kind: dispatchFlowNodeGateway,
				Depth: 1, GatewayGroupID: chain.groupID,
			})
			outcome := dispatchOutcomeOf(chain.attempts)
			builder.link(rootID, gatewayID, false)
			builder.link(gatewayID, ensureOutcome(outcome), outcome == dispatchOutcomeFailed)
			if hops := len(chain.attempts); hops > result.MaxHops {
				result.MaxHops = hops
			}
		}
		result.Nodes, result.Links = builder.build()
		return result, nil
	}

	// 单个网关：入口 → 第 1 跳路由 → 第 2 跳路由 → … → 结局
	rootID := fmt.Sprintf("g:%d", groupID)
	builder.node(GatewayDispatchFlowNode{
		ID: rootID, Label: result.GatewayGroupName, Kind: dispatchFlowNodeGateway,
		Depth: 0, GatewayGroupID: groupID,
	})
	overflowID := "h:more"
	for _, key := range chainOrder {
		chain := chains[key]
		if hops := len(chain.attempts); hops > result.MaxHops {
			result.MaxHops = hops
		}
		prev := rootID
		prevIsRoot := true
		overflowed := false
		for index, attempt := range chain.attempts {
			hop := index + 1
			var nodeID string
			if hop > dispatchFlowMaxHops {
				if overflowed {
					// 同一条链只在这里落一次，免得越深的链把边刷成自环般的重复计数
					continue
				}
				overflowed = true
				nodeID = overflowID
				builder.node(GatewayDispatchFlowNode{
					ID: overflowID, Label: fmt.Sprintf("第 %d 跳以后", dispatchFlowMaxHops+1),
					Kind: dispatchFlowNodeOverflow, Depth: dispatchFlowMaxHops + 1,
				})
			} else {
				nodeID = fmt.Sprintf("h%d:r%d", hop, attempt.routeID)
				identity := identities[attempt.routeID]
				label := fmt.Sprintf("路由 #%d", attempt.routeID)
				if identity != nil {
					label = identity.label(attempt.routeID, channelNames)
				}
				_, alive := aliveRoutes[attempt.routeID]
				builder.node(GatewayDispatchFlowNode{
					ID: nodeID, Label: label, Kind: dispatchFlowNodeRoute, Depth: hop,
					GatewayGroupID: groupID, RouteID: attempt.routeID, Hop: hop, Alive: alive,
				})
			}
			// 从路由节点连出去，只可能是因为这一跳失败了才转走的
			builder.link(prev, nodeID, !prevIsRoot)
			prev = nodeID
			prevIsRoot = false
		}
		if prevIsRoot {
			continue
		}
		outcome := dispatchOutcomeOf(chain.attempts)
		builder.link(prev, ensureOutcome(outcome), outcome == dispatchOutcomeFailed)
	}
	result.Nodes, result.Links = builder.build()
	return result, nil
}

// dispatchFlowGateways 统计窗口内每个网关的请求数（链级：一条 request_id 算一次）。
// 单独走一条 GROUP BY 而不是复用上面那批日志，因为下钻时那批已经按网关过滤过了。
func (r *GatewayUsageLogs) dispatchFlowGateways(from, to time.Time) ([]GatewayDispatchFlowGateway, error) {
	type row struct {
		GatewayGroupID uint
		Requests       int
	}
	var rows []row
	query := r.db.Model(&GatewayUsageLog{}).
		Select("gateway_group_id, COUNT(DISTINCT request_id) AS requests").
		Group("gateway_group_id")
	if isSQLite(r.db) {
		query = query.Where("CAST(strftime('%s', created_at) AS INTEGER) >= ? AND CAST(strftime('%s', created_at) AS INTEGER) < ?", from.Unix(), to.Unix())
	} else {
		query = query.Where("created_at >= ? AND created_at < ?", from, to)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []GatewayDispatchFlowGateway{}, nil
	}
	ids := make(map[uint]struct{}, len(rows))
	for _, item := range rows {
		ids[item.GatewayGroupID] = struct{}{}
	}
	names := make(map[uint]string)
	var dbGroups []GatewayGroup
	if err := r.db.Where("id IN ?", keysOfUintSet(ids)).Find(&dbGroups).Error; err == nil {
		for _, group := range dbGroups {
			names[group.ID] = group.Name
		}
	}
	result := make([]GatewayDispatchFlowGateway, 0, len(rows))
	for _, item := range rows {
		name := names[item.GatewayGroupID]
		if name == "" {
			name = fmt.Sprintf("组 #%d", item.GatewayGroupID)
		}
		result = append(result, GatewayDispatchFlowGateway{
			GatewayGroupID: item.GatewayGroupID, Name: name, Requests: item.Requests,
		})
	}
	// 流量大的排前面，tag 顺序才稳定
	sort.Slice(result, func(i, j int) bool {
		if result[i].Requests != result[j].Requests {
			return result[i].Requests > result[j].Requests
		}
		return result[i].GatewayGroupID < result[j].GatewayGroupID
	})
	return result, nil
}

func keysOfUintSet(set map[uint]struct{}) []uint {
	keys := make([]uint, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func averageFloat(values []float64) float64 {
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}
func percentileFloat(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}
func keysUint(values map[uint]string) []uint {
	keys := make([]uint, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

// DispatchStats 按网关组和路由聚合窗口内的每次调度尝试。
// from/to 使用半开区间 [from, to)，调用方应传入同一时区的时间。
func (r *GatewayUsageLogs) DispatchStats(from, to time.Time) ([]GatewayDispatchStatsGroup, error) {
	type aggregateRow struct {
		GatewayGroupID       uint
		RouteID              uint
		ProviderName         string
		SourceAPIKeyName     string
		SourceGroupName      string
		LoggedBillingRate    float64
		LoggedRateMultiplier float64
		TotalAttempts        int64
		FailedAttempts       int64
		FirstTokenSamples    int64
		AverageFirstTokenMS  *float64
	}

	var rows []aggregateRow
	if err := r.db.Raw(`
		WITH window_logs AS (
			SELECT id, gateway_group_id, route_id, provider_name, source_api_key_name,
				source_group_name, billing_rate_multiplier, rate_multiplier, success,
				first_token_ms, created_at
			FROM gateway_usage_logs
			WHERE created_at >= ? AND created_at < ?
		), aggregated AS (
			SELECT gateway_group_id, route_id,
				COUNT(*) AS total_attempts,
				COALESCE(SUM(CASE WHEN success = ? THEN 1 ELSE 0 END), 0) AS failed_attempts,
				COUNT(first_token_ms) AS first_token_samples,
				AVG(first_token_ms) AS average_first_token_ms
			FROM window_logs
			GROUP BY gateway_group_id, route_id
		), latest AS (
			SELECT gateway_group_id, route_id, provider_name, source_api_key_name,
				source_group_name, billing_rate_multiplier, rate_multiplier,
				ROW_NUMBER() OVER (
					PARTITION BY gateway_group_id, route_id
					ORDER BY created_at DESC, id DESC
				) AS row_num
			FROM window_logs
		)
		SELECT aggregated.gateway_group_id, aggregated.route_id,
			latest.provider_name, latest.source_api_key_name, latest.source_group_name,
			COALESCE(latest.billing_rate_multiplier, 0) AS logged_billing_rate,
			COALESCE(latest.rate_multiplier, 0) AS logged_rate_multiplier,
			aggregated.total_attempts, aggregated.failed_attempts,
			aggregated.first_token_samples, aggregated.average_first_token_ms
		FROM aggregated
		JOIN latest ON latest.gateway_group_id = aggregated.gateway_group_id
			AND latest.route_id = aggregated.route_id
			AND latest.row_num = 1
		ORDER BY aggregated.gateway_group_id ASC, aggregated.route_id ASC
	`, from, to, false).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []GatewayDispatchStatsGroup{}, nil
	}

	groupIDs := make([]uint, 0, len(rows))
	seenGroups := make(map[uint]struct{}, len(rows))
	for _, row := range rows {
		if _, ok := seenGroups[row.GatewayGroupID]; !ok {
			seenGroups[row.GatewayGroupID] = struct{}{}
			groupIDs = append(groupIDs, row.GatewayGroupID)
		}
	}
	type groupNameRow struct {
		ID   uint
		Name string
	}
	var groupNames []groupNameRow
	if err := r.db.Model(&GatewayGroup{}).Select("id, name").Where("id IN ?", groupIDs).Find(&groupNames).Error; err != nil {
		return nil, err
	}
	groupNameByID := make(map[uint]string, len(groupNames))
	for _, group := range groupNames {
		groupNameByID[group.ID] = group.Name
	}

	// 路由表是当前调度配置的权威成本来源。日志中的倍率只作为已删除路由
	// 的历史回退，避免路由重配后面板顺序被旧请求污染。
	routeIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		routeIDs = append(routeIDs, row.RouteID)
	}
	var routeConfigs []GatewayRoute
	if err := r.db.Where("id IN ?", routeIDs).Find(&routeConfigs).Error; err != nil {
		return nil, err
	}
	routeConfigByID := make(map[uint]GatewayRoute, len(routeConfigs))
	for _, route := range routeConfigs {
		routeConfigByID[route.ID] = route
	}

	groups := make([]GatewayDispatchStatsGroup, 0, len(groupIDs))
	groupIndex := make(map[uint]int, len(groupIDs))
	for _, id := range groupIDs {
		name := groupNameByID[id]
		if strings.TrimSpace(name) == "" {
			name = fmt.Sprintf("组 #%d", id)
		}
		groupIndex[id] = len(groups)
		groups = append(groups, GatewayDispatchStatsGroup{
			GatewayGroupID:   id,
			GatewayGroupName: name,
			Routes:           make([]GatewayDispatchStatsRoute, 0),
		})
	}
	for _, row := range rows {
		failureRate := 0.0
		if row.TotalAttempts > 0 {
			failureRate = float64(row.FailedAttempts) / float64(row.TotalAttempts)
		}
		providerName := strings.TrimSpace(row.ProviderName)
		sourceAPIKeyName := strings.TrimSpace(row.SourceAPIKeyName)
		sourceGroupName := strings.TrimSpace(row.SourceGroupName)
		billingRate := row.LoggedBillingRate
		route, routeAvailable := routeConfigByID[row.RouteID]
		if routeAvailable {
			if currentName := strings.TrimSpace(route.SourceAPIKeyName); currentName != "" {
				sourceAPIKeyName = currentName
			}
			if currentGroup := strings.TrimSpace(route.SourceGroupName); currentGroup != "" {
				sourceGroupName = currentGroup
			}
			if billingRate = route.BillingRateMultiplier; billingRate <= 0 {
				billingRate = row.LoggedBillingRate
			}
		}
		if billingRate <= 0 {
			billingRate = row.LoggedRateMultiplier
		}
		if billingRate <= 0 {
			billingRate = 1
		}
		if sourceAPIKeyName == "" {
			sourceAPIKeyName = providerName
		}
		if sourceGroupName == "" {
			sourceGroupName = providerName
		}
		routeName := sourceAPIKeyName
		if routeName == "" {
			routeName = sourceGroupName
		}
		if routeName == "" {
			routeName = "未命名来源"
		}
		groups[groupIndex[row.GatewayGroupID]].Routes = append(groups[groupIndex[row.GatewayGroupID]].Routes, GatewayDispatchStatsRoute{
			RouteID:               row.RouteID,
			RouteName:             routeName,
			ProviderName:          providerName,
			SourceAPIKeyName:      sourceAPIKeyName,
			SourceGroupName:       sourceGroupName,
			BillingRateMultiplier: billingRate,
			RouteAvailable:        routeAvailable,
			TotalAttempts:         row.TotalAttempts,
			FailedAttempts:        row.FailedAttempts,
			FailureRate:           failureRate,
			FirstTokenSamples:     row.FirstTokenSamples,
			AverageFirstTokenMS:   row.AverageFirstTokenMS,
		})
	}
	for i := range groups {
		sort.SliceStable(groups[i].Routes, func(a, b int) bool {
			left, right := groups[i].Routes[a], groups[i].Routes[b]
			if left.BillingRateMultiplier != right.BillingRateMultiplier {
				return left.BillingRateMultiplier < right.BillingRateMultiplier
			}
			if left.RouteID != right.RouteID {
				return left.RouteID < right.RouteID
			}
			return left.RouteName < right.RouteName
		})
	}
	return groups, nil
}

// Create 插入记录。
func (r *GatewayUsageLogs) Create(item *GatewayUsageLog) error {
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	return r.db.Create(item).Error
}

// CountBefore 统计 created_at < before 的记录数。
func (r *GatewayUsageLogs) CountBefore(before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, fmt.Errorf("before time required")
	}
	var n int64
	q := r.db.Model(&GatewayUsageLog{})
	if isSQLite(r.db) {
		q = q.Where("CAST(strftime('%s', created_at) AS INTEGER) < ?", before.Unix())
	} else {
		q = q.Where("created_at < ?", before)
	}
	err := q.Count(&n).Error
	return n, err
}

// CountAll 统计全部使用记录数。
func (r *GatewayUsageLogs) CountAll() (int64, error) {
	var n int64
	err := r.db.Model(&GatewayUsageLog{}).Count(&n).Error
	return n, err
}

// DeleteBefore 删除 created_at < before 的使用记录，返回删除行数。
func (r *GatewayUsageLogs) DeleteBefore(before time.Time) (int64, error) {
	if before.IsZero() {
		return 0, fmt.Errorf("before time required")
	}
	q := r.db.Model(&GatewayUsageLog{})
	if isSQLite(r.db) {
		q = q.Where("CAST(strftime('%s', created_at) AS INTEGER) < ?", before.Unix())
	} else {
		q = q.Where("created_at < ?", before)
	}
	res := q.Delete(&GatewayUsageLog{})
	return res.RowsAffected, res.Error
}

// DeleteAll 删除全部使用记录，返回删除行数。
// GORM 要求 Delete 带条件，故使用 1=1。
func (r *GatewayUsageLogs) DeleteAll() (int64, error) {
	res := r.db.Where("1 = 1").Delete(&GatewayUsageLog{})
	return res.RowsAffected, res.Error
}

type GatewayUsageQuery struct {
	GatewayGroupID uint
	GatewayKeyID   uint
	ChannelID      uint
	Model          string
	// RequestID 模糊匹配 request_id
	RequestID   string
	RequestType *int
	SuccessOnly *bool
	// ResultMode 结果筛选（优先于 SuccessOnly）：
	//   "" / all — 全部
	//   success / fail — 单条成功/失败（success 不含客户端断开）
	//   client / client_disconnect — 客户端主动断开（error_type=client）
	//   multi — 含重试/顺延（同 request_id 多条 或 attempt>1）
	//   multi_success — 最终成功且链路含重试/顺延（如 2/2·顺延）
	//   multi_fail — 含重试/顺延且最终失败
	ResultMode string
	From       *time.Time
	To         *time.Time
	Page       int
	PageSize   int
}

// GatewayUsageLogItem 列表展示用：原始日志 + 关联名称（非多租户用户字段）。
// 源分组 / 上游密钥名优先用日志快照字段（GatewayUsageLog.Source*）；
// 旧数据无快照时再按 route_id 回填。
type GatewayUsageLogItem struct {
	GatewayUsageLog
	GatewayKeyName   string `json:"gateway_key_name,omitempty"`
	GatewayGroupName string `json:"gateway_group_name,omitempty"`
	ChannelName      string `json:"channel_name,omitempty"`
	// ProviderName 直连渠道名：优先日志快照，否则 enrich 回填
}

type GatewayUsagePage struct {
	Items    []GatewayUsageLogItem `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
	Pages    int                   `json:"pages"`
	SumCost  float64               `json:"sum_cost"`
}

type GatewayUsageStats struct {
	TotalRequests            int64   `json:"total_requests"`
	SuccessCount             int64   `json:"success_count"`
	ErrorCount               int64   `json:"error_count"`
	TotalInputTokens         int64   `json:"total_input_tokens"`
	TotalOutputTokens        int64   `json:"total_output_tokens"`
	TotalCacheCreationTokens int64   `json:"total_cache_creation_tokens"`
	TotalCacheReadTokens     int64   `json:"total_cache_read_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	TotalCost                float64 `json:"total_cost"`
	TotalActualCost          float64 `json:"total_actual_cost"`
	AverageDurationMS        float64 `json:"average_duration_ms"`
	// RPM/TPM：近 5 分钟均值（对齐 sub2api），与筛选时间范围无关；TPM 仅 input+output
	RPM       int64                 `json:"rpm"`
	TPM       int64                 `json:"tpm"`
	Endpoints []GatewayEndpointStat `json:"endpoints"`
}

// GatewayUsageModelOption 使用记录模型下拉选项（按 requested_model 聚合）。
type GatewayUsageModelOption struct {
	Model string `json:"model"`
	Count int64  `json:"count"`
}

type GatewayEndpointStat struct {
	Endpoint string `json:"endpoint"`
	Requests int64  `json:"requests"`
}

// applyFilters 应用用量查询过滤。
func (r *GatewayUsageLogs) applyFilters(db *gorm.DB, q GatewayUsageQuery) *gorm.DB {
	if q.GatewayGroupID > 0 {
		db = db.Where("gateway_group_id = ?", q.GatewayGroupID)
	}
	if q.GatewayKeyID > 0 {
		db = db.Where("gateway_key_id = ?", q.GatewayKeyID)
	}
	if q.ChannelID > 0 {
		db = db.Where("channel_id = ?", q.ChannelID)
	}
	if m := strings.TrimSpace(q.Model); m != "" {
		// 下拉精确匹配请求模型 / 上游模型（兼容历史模糊：含 * 或 % 时走 LIKE）
		if strings.ContainsAny(m, "*%") {
			like := strings.ReplaceAll(m, "*", "%")
			if !strings.Contains(like, "%") {
				like = "%" + like + "%"
			}
			db = db.Where("requested_model LIKE ? OR upstream_model LIKE ?", like, like)
		} else {
			db = db.Where("requested_model = ? OR upstream_model = ?", m, m)
		}
	}
	if rid := strings.TrimSpace(q.RequestID); rid != "" {
		db = db.Where("request_id LIKE ?", "%"+rid+"%")
	}
	if q.RequestType != nil {
		db = db.Where("request_type = ?", *q.RequestType)
	}
	mode := strings.ToLower(strings.TrimSpace(q.ResultMode))
	switch mode {
	case "success":
		// 纯成功：不含客户端断开（新逻辑 success=true + error_type=client）
		db = db.Where(
			"success = ? AND (error_type IS NULL OR error_type = '' OR error_type != ?)",
			true, "client",
		)
	case "fail", "false", "error":
		// 真失败：不含客户端断开（旧数据可能 success=false + client）
		db = db.Where(
			"success = ? AND (error_type IS NULL OR error_type = '' OR error_type != ?)",
			false, "client",
		)
	case "client", "client_disconnect", "disconnect":
		// 流式 commit 后客户端主动断开（新旧 success 取值都覆盖）
		db = db.Where("error_type = ?", "client")
	case "multi", "retry", "failover", "chain":
		// 含重试/顺延：返回整条链路的所有 attempt 行，便于前端「查看全部 N 次尝试」
		db = db.Where(
			`request_id != '' AND request_id IN (
				SELECT request_id FROM gateway_usage_logs
				WHERE request_id != '' AND request_id IS NOT NULL
				GROUP BY request_id
				HAVING COUNT(*) > 1 OR MAX(attempt) > 1
					OR SUM(CASE WHEN attempt_kind IN ('retry','failover') THEN 1 ELSE 0 END) > 0
			)`,
		)
	case "multi_success", "failover_success", "chain_success":
		// 最终成功且链路有多次尝试（展示上即「成功 2/2 · 顺延」一类）
		db = db.Where(
			`request_id != '' AND request_id IN (
				SELECT request_id FROM gateway_usage_logs
				WHERE request_id != '' AND request_id IS NOT NULL
				GROUP BY request_id
				HAVING (COUNT(*) > 1 OR MAX(attempt) > 1
					OR SUM(CASE WHEN attempt_kind IN ('retry','failover') THEN 1 ELSE 0 END) > 0)
					AND SUM(CASE WHEN success = 1 OR success = true THEN 1 ELSE 0 END) > 0
			)`,
		)
	case "multi_fail", "chain_fail":
		db = db.Where(
			`request_id != '' AND request_id IN (
				SELECT request_id FROM gateway_usage_logs
				WHERE request_id != '' AND request_id IS NOT NULL
				GROUP BY request_id
				HAVING (COUNT(*) > 1 OR MAX(attempt) > 1
					OR SUM(CASE WHEN attempt_kind IN ('retry','failover') THEN 1 ELSE 0 END) > 0)
					AND SUM(CASE WHEN success = 1 OR success = true THEN 1 ELSE 0 END) = 0
			)`,
		)
	default:
		// all 或空：兼容旧 SuccessOnly
		if q.SuccessOnly != nil {
			db = db.Where("success = ?", *q.SuccessOnly)
		}
	}
	// 时间过滤：SQLite 存的是带时区的文本（如 "2026-07-14 18:40:18+08:00"），
	// 若直接与 RFC3339（含 T 的 ISO）字符串比较会得到 0 行（"近1小时"失效）。
	// 统一用 unix 秒比较。注意：strftime('%s', …) 返回 TEXT，
	// 与整数绑定参数比较在 SQLite 下会得到 0 行，必须 CAST 成 INTEGER。
	if q.From != nil {
		if isSQLite(db) {
			db = db.Where("CAST(strftime('%s', created_at) AS INTEGER) >= ?", q.From.Unix())
		} else {
			db = db.Where("created_at >= ?", *q.From)
		}
	}
	if q.To != nil {
		if isSQLite(db) {
			db = db.Where("CAST(strftime('%s', created_at) AS INTEGER) <= ?", q.To.Unix())
		} else {
			db = db.Where("created_at <= ?", *q.To)
		}
	}
	return db
}

func isSQLite(db *gorm.DB) bool {
	if db == nil || db.Dialector == nil {
		return false
	}
	return strings.EqualFold(db.Dialector.Name(), "sqlite")
}

// List 分页列表。
func (r *GatewayUsageLogs) List(q GatewayUsageQuery) (*GatewayUsagePage, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 20
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), q)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, err
	}
	var sum float64
	_ = db.Session(&gorm.Session{}).Select("COALESCE(SUM(actual_cost),0)").Scan(&sum).Error

	var rows []GatewayUsageLog
	offset := (q.Page - 1) * q.PageSize
	if err := db.Order("id DESC").Offset(offset).Limit(q.PageSize).Find(&rows).Error; err != nil {
		return nil, err
	}
	items, err := r.enrichUsageItems(rows)
	if err != nil {
		return nil, err
	}
	pages := int(total) / q.PageSize
	if int(total)%q.PageSize != 0 {
		pages++
	}
	return &GatewayUsagePage{
		Items:    items,
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Pages:    pages,
		SumCost:  sum,
	}, nil
}

// enrichUsageItems 补全用量展示字段。
func (r *GatewayUsageLogs) enrichUsageItems(rows []GatewayUsageLog) ([]GatewayUsageLogItem, error) {
	if len(rows) == 0 {
		return []GatewayUsageLogItem{}, nil
	}
	keyIDs := map[uint]struct{}{}
	groupIDs := map[uint]struct{}{}
	channelIDs := map[uint]struct{}{}
	providerIDs := map[uint]struct{}{}
	routeIDs := map[uint]struct{}{}
	for _, row := range rows {
		if row.GatewayKeyID > 0 {
			keyIDs[row.GatewayKeyID] = struct{}{}
		}
		if row.GatewayGroupID > 0 {
			groupIDs[row.GatewayGroupID] = struct{}{}
		}
		if row.ChannelID > 0 {
			channelIDs[row.ChannelID] = struct{}{}
		}
		if row.GatewayProviderID > 0 {
			providerIDs[row.GatewayProviderID] = struct{}{}
		}
		if row.RouteID > 0 {
			routeIDs[row.RouteID] = struct{}{}
		}
	}

	keyNames := map[uint]string{}
	if len(keyIDs) > 0 {
		ids := uintKeys(keyIDs)
		var keys []GatewayKey
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&keys).Error; err != nil {
			return nil, err
		}
		for _, k := range keys {
			keyNames[k.ID] = k.Name
		}
	}
	groupNames := map[uint]string{}
	if len(groupIDs) > 0 {
		ids := uintKeys(groupIDs)
		var groups []GatewayGroup
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&groups).Error; err != nil {
			return nil, err
		}
		for _, g := range groups {
			groupNames[g.ID] = g.Name
		}
	}
	channelNames := map[uint]string{}
	if len(channelIDs) > 0 {
		ids := uintKeys(channelIDs)
		var channels []Channel
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&channels).Error; err != nil {
			return nil, err
		}
		for _, ch := range channels {
			channelNames[ch.ID] = ch.Name
		}
	}
	providerNames := map[uint]string{}
	if len(providerIDs) > 0 {
		ids := uintKeys(providerIDs)
		var providers []GatewayProvider
		if err := r.db.Select("id", "name").Where("id IN ?", ids).Find(&providers).Error; err != nil {
			return nil, err
		}
		for _, p := range providers {
			providerNames[p.ID] = p.Name
		}
	}
	// 仅对「快照为空 / 仅 id 占位」的旧日志按 route_id 回填
	needRouteLookup := false
	for _, row := range rows {
		sg := strings.TrimSpace(row.SourceGroupName)
		if strings.TrimSpace(row.SourceAPIKeyName) == "" ||
			sg == "" || isUsageSourceGroupIDPlaceholder(sg) ||
			(sg == "" && row.SourceGroupID == nil) {
			if row.RouteID > 0 {
				needRouteLookup = true
				break
			}
		}
	}
	routeMeta := map[uint]struct {
		SourceGroupName  string
		SourceGroupID    *int64
		SourceAPIKeyName string
		SourceAPIKeyID   int64
	}{}
	if needRouteLookup && len(routeIDs) > 0 {
		ids := uintKeys(routeIDs)
		var routes []GatewayRoute
		if err := r.db.Select(
			"id", "source_group_name", "source_group_id", "source_api_key_name", "source_api_key_id",
		).Where("id IN ?", ids).Find(&routes).Error; err != nil {
			return nil, err
		}
		for _, rt := range routes {
			sg := strings.TrimSpace(rt.SourceGroupName)
			// 路由上已是真实名则原样用；空名才回退 id:N
			if sg == "" && rt.SourceGroupID != nil {
				sg = fmt.Sprintf("id:%d", *rt.SourceGroupID)
			}
			routeMeta[rt.ID] = struct {
				SourceGroupName  string
				SourceGroupID    *int64
				SourceAPIKeyName string
				SourceAPIKeyID   int64
			}{
				SourceGroupName:  sg,
				SourceGroupID:    rt.SourceGroupID,
				SourceAPIKeyName: strings.TrimSpace(rt.SourceAPIKeyName),
				SourceAPIKeyID:   rt.SourceAPIKeyID,
			}
		}
	}

	out := make([]GatewayUsageLogItem, 0, len(rows))
	for _, row := range rows {
		// 规范化快照展示：有 id 无 name 时补 "id:N"
		if strings.TrimSpace(row.SourceGroupName) == "" && row.SourceGroupID != nil {
			row.SourceGroupName = fmt.Sprintf("id:%d", *row.SourceGroupID)
		}
		// 旧日志：无快照 / 仅 id 占位时，从仍存活的 route 回填真实分组名
		if m, ok := routeMeta[row.RouteID]; ok {
			if strings.TrimSpace(row.SourceAPIKeyName) == "" && m.SourceAPIKeyName != "" {
				row.SourceAPIKeyName = m.SourceAPIKeyName
			}
			if row.SourceAPIKeyID == 0 && m.SourceAPIKeyID != 0 {
				row.SourceAPIKeyID = m.SourceAPIKeyID
			}
			routeName := strings.TrimSpace(m.SourceGroupName)
			rowName := strings.TrimSpace(row.SourceGroupName)
			if routeName != "" && !isUsageSourceGroupIDPlaceholder(routeName) {
				if rowName == "" || isUsageSourceGroupIDPlaceholder(rowName) {
					row.SourceGroupName = routeName
				}
			} else if rowName == "" && routeName != "" {
				row.SourceGroupName = routeName
			}
			if row.SourceGroupID == nil && m.SourceGroupID != nil {
				row.SourceGroupID = m.SourceGroupID
			}
		}
		if strings.TrimSpace(row.ProviderName) == "" && row.GatewayProviderID > 0 {
			row.ProviderName = providerNames[row.GatewayProviderID]
		}
		// 直连渠道：无监控渠道名时用 provider 名填 channel_name，便于旧 UI 展示
		chName := channelNames[row.ChannelID]
		if chName == "" && row.ProviderName != "" {
			chName = row.ProviderName
		}
		out = append(out, GatewayUsageLogItem{
			GatewayUsageLog:  row,
			GatewayKeyName:   keyNames[row.GatewayKeyID],
			GatewayGroupName: groupNames[row.GatewayGroupID],
			ChannelName:      chName,
		})
	}
	return out, nil
}

func uintKeys(m map[uint]struct{}) []uint {
	ids := make([]uint, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	return ids
}

// isUsageSourceGroupIDPlaceholder 识别 usage 快照里「id:31」这类无真实分组名的占位。
func isUsageSourceGroupIDPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	lower := strings.ToLower(strings.ReplaceAll(s, " ", ""))
	lower = strings.ReplaceAll(lower, "：", ":")
	if strings.HasPrefix(lower, "id:") {
		rest := strings.TrimPrefix(lower, "id:")
		return rest != "" && isAllASCIIDigits(rest)
	}
	if strings.HasPrefix(lower, "源id:") {
		rest := strings.TrimPrefix(lower, "源id:")
		return rest != "" && isAllASCIIDigits(rest)
	}
	return false
}

func isAllASCIIDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Stats 用量聚合统计。
func (r *GatewayUsageLogs) Stats(q GatewayUsageQuery) (*GatewayUsageStats, error) {
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), q)
	type aggRow struct {
		TotalRequests            int64
		SuccessCount             int64
		ErrorCount               int64
		TotalInputTokens         int64
		TotalOutputTokens        int64
		TotalCacheCreationTokens int64
		TotalCacheReadTokens     int64
		TotalCost                float64
		TotalActualCost          float64
		AvgDurationMS            float64
	}
	var row aggRow
	if err := db.Session(&gorm.Session{}).Select(`
		COUNT(*) as total_requests,
		COALESCE(SUM(CASE WHEN success THEN 1 ELSE 0 END),0) as success_count,
		COALESCE(SUM(CASE WHEN success THEN 0 ELSE 1 END),0) as error_count,
		COALESCE(SUM(input_tokens),0) as total_input_tokens,
		COALESCE(SUM(output_tokens),0) as total_output_tokens,
		COALESCE(SUM(cache_creation_tokens),0) as total_cache_creation_tokens,
		COALESCE(SUM(cache_read_tokens),0) as total_cache_read_tokens,
		COALESCE(SUM(total_cost),0) as total_cost,
		COALESCE(SUM(actual_cost),0) as total_actual_cost,
		COALESCE(AVG(duration_ms),0) as avg_duration_ms
	`).Scan(&row).Error; err != nil {
		return nil, err
	}
	var endpoints []GatewayEndpointStat
	_ = r.applyFilters(r.db.Model(&GatewayUsageLog{}), q).
		Select("inbound_endpoint as endpoint, COUNT(*) as requests").
		Where("inbound_endpoint <> ''").
		Group("inbound_endpoint").
		Order("requests DESC").
		Limit(20).
		Scan(&endpoints).Error

	totalTokens := row.TotalInputTokens + row.TotalOutputTokens + row.TotalCacheCreationTokens + row.TotalCacheReadTokens
	rpm, tpm := r.performanceRPMAndTPM(q)
	return &GatewayUsageStats{
		TotalRequests:            row.TotalRequests,
		SuccessCount:             row.SuccessCount,
		ErrorCount:               row.ErrorCount,
		TotalInputTokens:         row.TotalInputTokens,
		TotalOutputTokens:        row.TotalOutputTokens,
		TotalCacheCreationTokens: row.TotalCacheCreationTokens,
		TotalCacheReadTokens:     row.TotalCacheReadTokens,
		TotalTokens:              totalTokens,
		TotalCost:                row.TotalCost,
		TotalActualCost:          row.TotalActualCost,
		AverageDurationMS:        row.AvgDurationMS,
		RPM:                      rpm,
		TPM:                      tpm,
		Endpoints:                endpoints,
	}, nil
}

// ListModels 聚合使用记录中的 requested_model，供筛选下拉。
// 沿用组/密钥/时间筛选；忽略 model / result / request_id（避免自过滤）。
func (r *GatewayUsageLogs) ListModels(q GatewayUsageQuery) ([]GatewayUsageModelOption, error) {
	q.Model = ""
	q.RequestID = ""
	q.ResultMode = ""
	q.SuccessOnly = nil
	q.RequestType = nil

	type row struct {
		Model string
		Count int64
	}
	var rows []row
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), q)
	if err := db.Session(&gorm.Session{}).
		Select("requested_model as model, COUNT(*) as count").
		Where("requested_model IS NOT NULL AND requested_model != ''").
		Group("requested_model").
		Order("count DESC, requested_model ASC").
		Limit(500).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]GatewayUsageModelOption, 0, len(rows))
	for _, row := range rows {
		m := strings.TrimSpace(row.Model)
		if m == "" {
			continue
		}
		out = append(out, GatewayUsageModelOption{Model: m, Count: row.Count})
	}
	return out, nil
}

// performanceRPMAndTPM 近 5 分钟平均：RPM = 请求数/5，TPM = (input+output)/5。
// 沿用组/密钥/模型等筛选，但忽略调用方传入的 From/To（实时吞吐与统计窗口无关）。
func (r *GatewayUsageLogs) performanceRPMAndTPM(q GatewayUsageQuery) (rpm, tpm int64) {
	const windowMinutes int64 = 5
	from := time.Now().Add(-time.Duration(windowMinutes) * time.Minute)
	qPerf := q
	qPerf.From = &from
	qPerf.To = nil
	type perfRow struct {
		RequestCount int64
		TokenCount   int64
	}
	var row perfRow
	db := r.applyFilters(r.db.Model(&GatewayUsageLog{}), qPerf)
	if err := db.Session(&gorm.Session{}).Select(`
		COUNT(*) as request_count,
		COALESCE(SUM(input_tokens + output_tokens), 0) as token_count
	`).Scan(&row).Error; err != nil {
		return 0, 0
	}
	return row.RequestCount / windowMinutes, row.TokenCount / windowMinutes
}

// ModelPriceOverrides 价格覆盖表。
type ModelPriceOverrides struct{ db *gorm.DB }

// NewModelPriceOverrides 构造模型价目覆盖仓储。
func NewModelPriceOverrides(db *gorm.DB) *ModelPriceOverrides {
	return &ModelPriceOverrides{db: db}
}

// List 分页列表。
func (r *ModelPriceOverrides) List() ([]ModelPriceOverride, error) {
	var list []ModelPriceOverride
	if err := r.db.Order("model_name ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *ModelPriceOverrides) FindByModel(name string) (*ModelPriceOverride, error) {
	var item ModelPriceOverride
	if err := r.db.Where("model_name = ?", name).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// Upsert 按模型名插入或更新价目覆盖。
func (r *ModelPriceOverrides) Upsert(item *ModelPriceOverride) error {
	item.ModelName = strings.TrimSpace(item.ModelName)
	if item.ID == 0 {
		var existing ModelPriceOverride
		if err := r.db.Where("model_name = ?", item.ModelName).First(&existing).Error; err == nil {
			item.ID = existing.ID
			item.CreatedAt = existing.CreatedAt
		}
	}
	return r.db.Save(item).Error
}

// Delete 按主键删除。
func (r *ModelPriceOverrides) Delete(id uint) error {
	return r.db.Delete(&ModelPriceOverride{}, id).Error
}

// ListEnabledMap 返回启用中的价目覆盖 map。
func (r *ModelPriceOverrides) ListEnabledMap() (map[string]ModelPriceOverride, error) {
	list, err := r.List()
	if err != nil {
		return nil, err
	}
	out := make(map[string]ModelPriceOverride, len(list))
	for _, item := range list {
		if !item.Enabled {
			continue
		}
		out[item.ModelName] = item
	}
	return out, nil
}
