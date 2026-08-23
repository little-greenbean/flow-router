package storage

import (
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := Open(DBConfig{
		Driver:       DBDriverSQLite,
		Path:         filepath.Join(t.TempDir(), "test.db"),
		MaxOpenConns: 20,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	return db
}

func TestAggregateBalanceTrend(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []BalanceSnapshot{
		{ChannelID: 1, Balance: 10, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, Balance: 20, SampledAt: day2.Add(12 * time.Hour)},
		{ChannelID: 2, Balance: 5, SampledAt: day2.Add(10 * time.Hour)},
		{ChannelID: 1, Balance: 7, SampledAt: day1.Add(11 * time.Hour)},
		{ChannelID: 2, Balance: 3, SampledAt: day1.Add(18 * time.Hour)},
		{ChannelID: 2, Balance: 9, SampledAt: day0.Add(8 * time.Hour)},
		{ChannelID: 2, Balance: 11, SampledAt: day0.Add(22 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendBalance(&snapshot); err != nil {
			t.Fatalf("append balance: %v", err)
		}
	}

	got, err := rates.AggregateBalanceTrend(3)
	if err != nil {
		t.Fatalf("aggregate balance trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyAggregate{
		{Day: day2, Balance: 25},
		{Day: day1, Balance: 10},
		{Day: day0, Balance: 11},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Balance != want[i].Balance {
			t.Fatalf("balance %d mismatch: got %v want %v", i, got[i].Balance, want[i].Balance)
		}
	}
}

func TestChannelProxyEnabledPersists(t *testing.T) {
	db := openTestDB(t)
	channels := NewChannels(db)
	ch := &Channel{
		Name:           "proxy-channel",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		ProxyEnabled:   true,
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	got, err := channels.FindByID(ch.ID)
	if err != nil {
		t.Fatalf("find channel: %v", err)
	}
	if !got.ProxyEnabled {
		t.Fatal("proxy_enabled = false, want true")
	}
}

func TestProxyEnabledPersistsForCaptchaAndNotification(t *testing.T) {
	db := openTestDB(t)

	captchas := NewCaptchas(db)
	cfg := &CaptchaConfig{
		Name:         "solver-proxy",
		Type:         CaptchaCapSolver,
		APIKeyCipher: "x",
		Enabled:      true,
		ProxyEnabled: true,
	}
	if err := captchas.Create(cfg); err != nil {
		t.Fatalf("create captcha: %v", err)
	}
	gotCaptcha, err := captchas.FindByID(cfg.ID)
	if err != nil {
		t.Fatalf("find captcha: %v", err)
	}
	if !gotCaptcha.ProxyEnabled {
		t.Fatal("captcha proxy_enabled = false, want true")
	}

	notifies := NewNotifications(db)
	notify := &NotificationChannel{
		Name:         "notify-proxy",
		Type:         NotifyTelegram,
		ConfigCipher: "x",
		Enabled:      true,
		ProxyEnabled: true,
	}
	if err := notifies.CreateChannel(notify); err != nil {
		t.Fatalf("create notification: %v", err)
	}
	gotNotify, err := notifies.FindChannel(notify.ID)
	if err != nil {
		t.Fatalf("find notification: %v", err)
	}
	if !gotNotify.ProxyEnabled {
		t.Fatal("notification proxy_enabled = false, want true")
	}
}

func TestAggregateBalanceTrendFillsMissingDays(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []BalanceSnapshot{
		{ChannelID: 1, Balance: 10, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, Balance: 20, SampledAt: day0.Add(12 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendBalance(&snapshot); err != nil {
			t.Fatalf("append balance: %v", err)
		}
	}

	got, err := rates.AggregateBalanceTrend(3)
	if err != nil {
		t.Fatalf("aggregate balance trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyAggregate{
		{Day: day2, Balance: 10},
		{Day: day1, Balance: 0},
		{Day: day0, Balance: 20},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Balance != want[i].Balance {
			t.Fatalf("balance %d mismatch: got %v want %v", i, got[i].Balance, want[i].Balance)
		}
	}
}

func TestAggregateCostTrend(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []CostSnapshot{
		{ChannelID: 1, TodayCost: 1.1, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, TodayCost: 2.2, SampledAt: day2.Add(18 * time.Hour)},
		{ChannelID: 2, TodayCost: 0.8, SampledAt: day2.Add(10 * time.Hour)},
		{ChannelID: 1, TodayCost: 3.5, SampledAt: day1.Add(11 * time.Hour)},
		{ChannelID: 2, TodayCost: 1.2, SampledAt: day1.Add(13 * time.Hour)},
		{ChannelID: 2, TodayCost: 1.8, SampledAt: day1.Add(22 * time.Hour)},
		{ChannelID: 1, TodayCost: 4.0, SampledAt: day0.Add(8 * time.Hour)},
		{ChannelID: 2, TodayCost: 2.5, SampledAt: day0.Add(21 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendCost(&snapshot); err != nil {
			t.Fatalf("append cost: %v", err)
		}
	}

	got, err := rates.AggregateCostTrend(3)
	if err != nil {
		t.Fatalf("aggregate cost trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyCostAggregate{
		{Day: day2, Cost: 3.0},
		{Day: day1, Cost: 5.3},
		{Day: day0, Cost: 6.5},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Cost != want[i].Cost {
			t.Fatalf("cost %d mismatch: got %v want %v", i, got[i].Cost, want[i].Cost)
		}
	}
}

func TestAggregateCostTrendFillsMissingDays(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now().In(trendLocation)
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)
	day2 := day0.AddDate(0, 0, -2)

	snapshots := []CostSnapshot{
		{ChannelID: 1, TodayCost: 1.5, SampledAt: day2.Add(9 * time.Hour)},
		{ChannelID: 1, TodayCost: 2.5, SampledAt: day0.Add(12 * time.Hour)},
	}
	for _, snapshot := range snapshots {
		snapshot := snapshot
		if err := rates.AppendCost(&snapshot); err != nil {
			t.Fatalf("append cost: %v", err)
		}
	}

	got, err := rates.AggregateCostTrend(3)
	if err != nil {
		t.Fatalf("aggregate cost trend: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 days, got %d", len(got))
	}

	want := []DailyCostAggregate{
		{Day: day2, Cost: 1.5},
		{Day: day1, Cost: 0},
		{Day: day0, Cost: 2.5},
	}
	for i := range want {
		if !got[i].Day.Equal(want[i].Day) {
			t.Fatalf("day %d mismatch: got %s want %s", i, got[i].Day, want[i].Day)
		}
		if got[i].Cost != want[i].Cost {
			t.Fatalf("cost %d mismatch: got %v want %v", i, got[i].Cost, want[i].Cost)
		}
	}
}

func TestAggregateTrendUsesShanghaiDayBoundary(t *testing.T) {
	oldNow := trendNow
	trendNow = func() time.Time {
		return time.Date(2026, 6, 19, 16, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { trendNow = oldNow })

	db := openTestDB(t)
	rates := NewRates(db)

	day0 := time.Date(2026, 6, 20, 0, 0, 0, 0, trendLocation)
	day1 := day0.AddDate(0, 0, -1)

	balanceSnapshots := []BalanceSnapshot{
		{ChannelID: 1, Balance: 10, SampledAt: time.Date(2026, 6, 19, 15, 59, 0, 0, time.UTC)},
		{ChannelID: 1, Balance: 20, SampledAt: time.Date(2026, 6, 19, 16, 1, 0, 0, time.UTC)},
	}
	for _, snapshot := range balanceSnapshots {
		snapshot := snapshot
		if err := rates.AppendBalance(&snapshot); err != nil {
			t.Fatalf("append balance: %v", err)
		}
	}

	costSnapshots := []CostSnapshot{
		{ChannelID: 1, TodayCost: 1.5, SampledAt: time.Date(2026, 6, 19, 15, 59, 0, 0, time.UTC)},
		{ChannelID: 1, TodayCost: 2.5, SampledAt: time.Date(2026, 6, 19, 16, 1, 0, 0, time.UTC)},
	}
	for _, snapshot := range costSnapshots {
		snapshot := snapshot
		if err := rates.AppendCost(&snapshot); err != nil {
			t.Fatalf("append cost: %v", err)
		}
	}

	balances, err := rates.AggregateBalanceTrend(2)
	if err != nil {
		t.Fatalf("aggregate balance trend: %v", err)
	}
	if len(balances) != 2 {
		t.Fatalf("balance days = %d, want 2", len(balances))
	}
	if !balances[0].Day.Equal(day1) || balances[0].Balance != 10 {
		t.Fatalf("previous shanghai day = %#v, want day %s balance 10", balances[0], day1)
	}
	if !balances[1].Day.Equal(day0) || balances[1].Balance != 20 {
		t.Fatalf("current shanghai day = %#v, want day %s balance 20", balances[1], day0)
	}

	costs, err := rates.AggregateCostTrend(2)
	if err != nil {
		t.Fatalf("aggregate cost trend: %v", err)
	}
	if len(costs) != 2 {
		t.Fatalf("cost days = %d, want 2", len(costs))
	}
	if !costs[0].Day.Equal(day1) || costs[0].Cost != 1.5 {
		t.Fatalf("previous shanghai day cost = %#v, want day %s cost 1.5", costs[0], day1)
	}
	if !costs[1].Day.Equal(day0) || costs[1].Cost != 2.5 {
		t.Fatalf("current shanghai day cost = %#v, want day %s cost 2.5", costs[1], day0)
	}
}

func TestDeleteCostSnapshotsBefore(t *testing.T) {
	db := openTestDB(t)
	rates := NewRates(db)

	now := time.Now()
	oldSnapshot := CostSnapshot{ChannelID: 1, TodayCost: 1.2, SampledAt: now.AddDate(0, 0, -10)}
	newSnapshot := CostSnapshot{ChannelID: 1, TodayCost: 2.3, SampledAt: now.AddDate(0, 0, -2)}
	if err := rates.AppendCost(&oldSnapshot); err != nil {
		t.Fatalf("append old cost: %v", err)
	}
	if err := rates.AppendCost(&newSnapshot); err != nil {
		t.Fatalf("append new cost: %v", err)
	}

	deleted, err := rates.DeleteCostSnapshotsBefore(now.AddDate(0, 0, -5))
	if err != nil {
		t.Fatalf("delete cost snapshots: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	var count int64
	if err := db.Model(&CostSnapshot{}).Count(&count).Error; err != nil {
		t.Fatalf("count cost snapshots: %v", err)
	}
	if count != 1 {
		t.Fatalf("remaining count = %d, want 1", count)
	}
}

func TestTryClaimCooldown(t *testing.T) {
	db := openTestDB(t)
	notifications := NewNotifications(db)

	ok, err := notifications.TryClaimCooldown(1, EventBalanceLow, time.Minute)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if !ok {
		t.Fatal("first claim should succeed")
	}

	ok, err = notifications.TryClaimCooldown(1, EventBalanceLow, time.Minute)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if ok {
		t.Fatal("second claim should be blocked by cooldown")
	}

	oldTime := time.Now().Add(-2 * time.Minute)
	if err := db.Model(&NotificationCooldown{}).
		Where("channel_id = ? AND event = ?", 1, EventBalanceLow).
		Updates(map[string]any{
			"last_sent_at": oldTime,
			"updated_at":   oldTime,
		}).Error; err != nil {
		t.Fatalf("age cooldown: %v", err)
	}

	ok, err = notifications.TryClaimCooldown(1, EventBalanceLow, time.Minute)
	if err != nil {
		t.Fatalf("third claim: %v", err)
	}
	if !ok {
		t.Fatal("third claim should succeed after cooldown expires")
	}
}

func TestTryClaimCooldownConcurrent(t *testing.T) {
	db := openTestDB(t)
	notifications := NewNotifications(db)

	var claimed int32
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			ok, err := notifications.TryClaimCooldown(2, EventBalanceLow, time.Minute)
			if err != nil {
				t.Errorf("concurrent claim: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&claimed, 1)
			}
		}()
	}
	wg.Wait()

	if claimed != 1 {
		t.Fatalf("expected exactly one successful claim, got %d", claimed)
	}
}

func TestUpstreamAnnouncementsSyncDedupes(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	now := time.Now()
	items := []UpstreamAnnouncement{
		{SourceKey: "a", Title: "A", Content: "one", FirstSeenAt: now},
		{SourceKey: "a", Title: "A2", Content: "dup", FirstSeenAt: now.Add(time.Second)},
	}
	newItems, err := announcements.Sync(1, items)
	if err != nil {
		t.Fatalf("sync announcements: %v", err)
	}
	if len(newItems) != 1 {
		t.Fatalf("new items = %d, want 1", len(newItems))
	}

	exists, err := announcements.Exists(1, "a")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("expected announcement to exist")
	}
}

func TestUpstreamAnnouncementsListLatest(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	now := time.Now()
	publishedOld := now.Add(-3 * time.Hour)
	publishedNew := now.Add(-1 * time.Hour)
	items := []UpstreamAnnouncement{
		{ChannelID: 1, SourceKey: "a", Content: "body", PublishedAt: &publishedOld, FirstSeenAt: now.Add(3 * time.Minute)},
		{ChannelID: 1, SourceKey: "b", Content: "body", PublishedAt: &publishedNew, FirstSeenAt: now.Add(1 * time.Minute)},
		{ChannelID: 1, SourceKey: "c", Content: "body", FirstSeenAt: now.Add(4 * time.Minute)},
	}
	for _, item := range items {
		item := item
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create announcement: %v", err)
		}
	}

	list, err := announcements.ListLatest(2)
	if err != nil {
		t.Fatalf("list latest: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list len = %d, want 2", len(list))
	}
	if list[0].SourceKey != "b" || list[1].SourceKey != "a" {
		t.Fatalf("unexpected order: %#v", list)
	}
}

func TestUpstreamAnnouncementsDeleteByChannel(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	now := time.Now()
	if _, err := announcements.Sync(1, []UpstreamAnnouncement{{
		SourceKey:   "a",
		Content:     "one",
		FirstSeenAt: now,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}
	if _, err := announcements.Sync(2, []UpstreamAnnouncement{{
		SourceKey:   "b",
		Content:     "two",
		FirstSeenAt: now,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}

	rows, err := announcements.DeleteByChannel(1)
	if err != nil {
		t.Fatalf("delete by channel: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
	list, total, err := announcements.ListPage(1, 10)
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ChannelID != 2 {
		t.Fatalf("unexpected remaining announcements: total=%d list=%#v", total, list)
	}
}

func TestUpstreamAnnouncementsDeleteBefore(t *testing.T) {
	db := openTestDB(t)
	announcements := NewUpstreamAnnouncements(db)

	oldTime := time.Now().AddDate(0, 0, -10)
	newTime := time.Now()
	if _, err := announcements.Sync(1, []UpstreamAnnouncement{{
		SourceKey:   "old",
		Content:     "old",
		FirstSeenAt: oldTime,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}
	if _, err := announcements.Sync(1, []UpstreamAnnouncement{{
		SourceKey:   "new",
		Content:     "new",
		FirstSeenAt: newTime,
	}}); err != nil {
		t.Fatalf("sync announcements: %v", err)
	}

	rows, err := announcements.DeleteBefore(time.Now().AddDate(0, 0, -5))
	if err != nil {
		t.Fatalf("delete before: %v", err)
	}
	if rows != 1 {
		t.Fatalf("rows = %d, want 1", rows)
	}
	list, total, err := announcements.ListPage(1, 10)
	if err != nil {
		t.Fatalf("list page: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].SourceKey != "new" {
		t.Fatalf("unexpected remaining announcements: total=%d list=%#v", total, list)
	}
}

func TestUpdateCosts(t *testing.T) {
	db := openTestDB(t)
	channels := NewChannels(db)

	c := &Channel{
		Name:           "test",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(c); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	if err := channels.UpdateCosts(c.ID, 1.23, 9.87); err != nil {
		t.Fatalf("update costs: %v", err)
	}

	got, err := channels.FindByID(c.ID)
	if err != nil {
		t.Fatalf("find channel: %v", err)
	}
	if got.TodayCost == nil || *got.TodayCost != 1.23 {
		t.Fatalf("today cost mismatch: %#v", got.TodayCost)
	}
	if got.TotalCost == nil || *got.TotalCost != 9.87 {
		t.Fatalf("total cost mismatch: %#v", got.TotalCost)
	}
}

func TestHardDeleteAllowsReusingNames(t *testing.T) {
	db := openTestDB(t)

	channels := NewChannels(db)
	ch := &Channel{
		Name:           "demo",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := channels.Delete(ch.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}
	ch = &Channel{
		Name:           "demo",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("recreate channel: %v", err)
	}

	captchas := NewCaptchas(db)
	cfg := &CaptchaConfig{
		Name:         "solver",
		Type:         CaptchaCapSolver,
		APIKeyCipher: "x",
		Enabled:      true,
	}
	if err := captchas.Create(cfg); err != nil {
		t.Fatalf("create captcha: %v", err)
	}
	if err := captchas.Delete(cfg.ID); err != nil {
		t.Fatalf("delete captcha: %v", err)
	}
	cfg = &CaptchaConfig{
		Name:         "solver",
		Type:         CaptchaCapSolver,
		APIKeyCipher: "x",
		Enabled:      true,
	}
	if err := captchas.Create(cfg); err != nil {
		t.Fatalf("recreate captcha: %v", err)
	}

	notifications := NewNotifications(db)
	notify := &NotificationChannel{
		Name:         "telegram",
		Type:         NotifyTelegram,
		ConfigCipher: "x",
		Enabled:      true,
	}
	if err := notifications.CreateChannel(notify); err != nil {
		t.Fatalf("create notification channel: %v", err)
	}
	if err := notifications.DeleteChannel(notify.ID); err != nil {
		t.Fatalf("delete notification channel: %v", err)
	}
	notify = &NotificationChannel{
		Name:         "telegram",
		Type:         NotifyTelegram,
		ConfigCipher: "x",
		Enabled:      true,
	}
	if err := notifications.CreateChannel(notify); err != nil {
		t.Fatalf("recreate notification channel: %v", err)
	}
}

func TestDeleteChannelCleansScopedState(t *testing.T) {
	db := openTestDB(t)

	channels := NewChannels(db)
	ch := &Channel{
		Name:           "demo",
		Type:           ChannelTypeSub2API,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := channels.Create(ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	now := time.Now()
	if err := db.Create(&AuthSession{ChannelID: ch.ID}).Error; err != nil {
		t.Fatalf("create auth session: %v", err)
	}
	if err := db.Create(&RateSnapshot{ChannelID: ch.ID, ModelName: "old", Ratio: 1, LastSeenAt: now}).Error; err != nil {
		t.Fatalf("create rate snapshot: %v", err)
	}
	if err := db.Create(&RateChangeLog{ChannelID: ch.ID, ModelName: "old", NewRatio: 1, ChangedAt: now}).Error; err != nil {
		t.Fatalf("create rate change: %v", err)
	}
	if err := db.Create(&BalanceSnapshot{ChannelID: ch.ID, Balance: 1, SampledAt: now}).Error; err != nil {
		t.Fatalf("create balance snapshot: %v", err)
	}
	if err := db.Create(&CostSnapshot{ChannelID: ch.ID, TodayCost: 1, SampledAt: now}).Error; err != nil {
		t.Fatalf("create cost snapshot: %v", err)
	}
	if err := db.Create(&MonitorLog{ChannelID: ch.ID, Job: MonitorJobBalance, Success: true, StartedAt: now, FinishedAt: now}).Error; err != nil {
		t.Fatalf("create monitor log: %v", err)
	}
	if err := db.Create(&NotificationCooldown{ChannelID: ch.ID, Event: EventBalanceLow, LastSentAt: now}).Error; err != nil {
		t.Fatalf("create cooldown: %v", err)
	}
	if err := db.Create(&NotificationLog{ChannelID: 99, UpstreamChannelID: ch.ID, Event: EventBalanceLow, Subject: "alert", Success: true, SentAt: now}).Error; err != nil {
		t.Fatalf("create notification log: %v", err)
	}
	if err := db.Create(&NotificationLog{ChannelID: 99, Event: EventBalanceLow, Subject: "demo 余额低于阈值", Success: true, SentAt: now}).Error; err != nil {
		t.Fatalf("create legacy notification log: %v", err)
	}
	if err := db.Create(&UpstreamAnnouncement{ChannelID: ch.ID, SourceKey: "a", Content: "deleted", FirstSeenAt: now}).Error; err != nil {
		t.Fatalf("create announcement: %v", err)
	}

	if err := channels.Delete(ch.ID); err != nil {
		t.Fatalf("delete channel: %v", err)
	}

	for _, tt := range []struct {
		name  string
		model any
	}{
		{"auth_sessions", &AuthSession{}},
		{"rate_snapshots", &RateSnapshot{}},
		{"rate_change_logs", &RateChangeLog{}},
		{"balance_snapshots", &BalanceSnapshot{}},
		{"cost_snapshots", &CostSnapshot{}},
		{"monitor_logs", &MonitorLog{}},
		{"notification_cooldowns", &NotificationCooldown{}},
		{"upstream_announcements", &UpstreamAnnouncement{}},
		{"notification_logs", &NotificationLog{}},
	} {
		var count int64
		q := db.Model(tt.model).Where("channel_id = ?", ch.ID)
		if tt.name == "notification_logs" {
			q = db.Model(tt.model).Where("upstream_channel_id = ? OR subject LIKE ?", ch.ID, "%"+ch.Name+"%")
		}
		if err := q.Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", tt.name, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", tt.name, count)
		}
	}
}

func TestAutoMigrateDropsDeletedAtColumns(t *testing.T) {
	db := openTestDB(t)

	for _, ddl := range []string{
		"ALTER TABLE channels ADD COLUMN deleted_at datetime",
		"ALTER TABLE captcha_configs ADD COLUMN deleted_at datetime",
		"ALTER TABLE notification_channels ADD COLUMN deleted_at datetime",
		"CREATE INDEX idx_channels_deleted_at ON channels(deleted_at)",
		"CREATE INDEX idx_captcha_configs_deleted_at ON captcha_configs(deleted_at)",
		"CREATE INDEX idx_notification_channels_deleted_at ON notification_channels(deleted_at)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("exec %q: %v", ddl, err)
		}
	}

	now := time.Now()
	activeChannel := &Channel{
		Name:           "active-channel",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	deletedChannel := &Channel{
		Name:           "deleted-channel",
		Type:           ChannelTypeNewAPI,
		SiteURL:        "https://example.com",
		Username:       "u",
		PasswordCipher: "x",
		MonitorEnabled: true,
	}
	if err := db.Create(activeChannel).Error; err != nil {
		t.Fatalf("create active channel: %v", err)
	}
	if err := db.Create(deletedChannel).Error; err != nil {
		t.Fatalf("create deleted channel: %v", err)
	}
	if err := db.Table("channels").Where("id = ?", deletedChannel.ID).Update("deleted_at", now).Error; err != nil {
		t.Fatalf("mark deleted channel: %v", err)
	}

	activeCaptcha := &CaptchaConfig{Name: "active-captcha", Type: CaptchaCapSolver, APIKeyCipher: "x", Enabled: true}
	deletedCaptcha := &CaptchaConfig{Name: "deleted-captcha", Type: CaptchaCapSolver, APIKeyCipher: "x", Enabled: true}
	if err := db.Create(activeCaptcha).Error; err != nil {
		t.Fatalf("create active captcha: %v", err)
	}
	if err := db.Create(deletedCaptcha).Error; err != nil {
		t.Fatalf("create deleted captcha: %v", err)
	}
	if err := db.Table("captcha_configs").Where("id = ?", deletedCaptcha.ID).Update("deleted_at", now).Error; err != nil {
		t.Fatalf("mark deleted captcha: %v", err)
	}

	activeNotify := &NotificationChannel{Name: "active-notify", Type: NotifyTelegram, ConfigCipher: "x", Enabled: true}
	deletedNotify := &NotificationChannel{Name: "deleted-notify", Type: NotifyTelegram, ConfigCipher: "x", Enabled: true}
	if err := db.Create(activeNotify).Error; err != nil {
		t.Fatalf("create active notification channel: %v", err)
	}
	if err := db.Create(deletedNotify).Error; err != nil {
		t.Fatalf("create deleted notification channel: %v", err)
	}
	if err := db.Table("notification_channels").Where("id = ?", deletedNotify.ID).Update("deleted_at", now).Error; err != nil {
		t.Fatalf("mark deleted notification channel: %v", err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	for _, table := range []string{"channels", "captcha_configs", "notification_channels"} {
		hasColumn, err := tableHasColumn(db, table, "deleted_at")
		if err != nil {
			t.Fatalf("inspect %s.deleted_at: %v", table, err)
		}
		if hasColumn {
			t.Fatalf("%s.deleted_at still exists", table)
		}
	}

	var count int64
	if err := db.Model(&Channel{}).Count(&count).Error; err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("channel count = %d, want 1", count)
	}
	if err := db.Model(&CaptchaConfig{}).Count(&count).Error; err != nil {
		t.Fatalf("count captchas: %v", err)
	}
	if count != 1 {
		t.Fatalf("captcha count = %d, want 1", count)
	}
	if err := db.Model(&NotificationChannel{}).Count(&count).Error; err != nil {
		t.Fatalf("count notification channels: %v", err)
	}
	if count != 1 {
		t.Fatalf("notification channel count = %d, want 1", count)
	}
}

func TestAutoMigrateDropsObsoleteRateSiteColumns(t *testing.T) {
	db := openTestDB(t)
	if err := db.Exec("ALTER TABLE rate_snapshots ADD COLUMN site_id integer NOT NULL DEFAULT 1").Error; err != nil {
		t.Fatalf("add rate_snapshots.site_id: %v", err)
	}
	if err := db.Exec("ALTER TABLE rate_change_logs ADD COLUMN site_id integer NOT NULL DEFAULT 1").Error; err != nil {
		t.Fatalf("add rate_change_logs.site_id: %v", err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	for _, table := range []string{"rate_snapshots", "rate_change_logs"} {
		hasColumn, err := tableHasColumn(db, table, "site_id")
		if err != nil {
			t.Fatalf("inspect %s.site_id: %v", table, err)
		}
		if hasColumn {
			t.Fatalf("%s.site_id still exists", table)
		}
	}
}

func TestGatewayGroupsReorderAndListOrder(t *testing.T) {
	db := openTestDB(t)
	repo := NewGatewayGroups(db)

	a := &GatewayGroup{Name: "a", Status: GatewayGroupStatusActive}
	b := &GatewayGroup{Name: "b", Status: GatewayGroupStatusActive}
	c := &GatewayGroup{Name: "c", Status: GatewayGroupStatusActive}
	for _, g := range []*GatewayGroup{a, b, c} {
		pos, err := repo.NextPosition()
		if err != nil {
			t.Fatalf("next pos: %v", err)
		}
		g.Position = pos
		if err := repo.Create(g); err != nil {
			t.Fatalf("create %s: %v", g.Name, err)
		}
	}

	// 创建顺序 a,b,c → position 0,1,2；列表应为 a,b,c
	list, err := repo.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 || list[0].Name != "a" || list[1].Name != "b" || list[2].Name != "c" {
		t.Fatalf("initial order = %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}

	// 重排为 c, a, b
	if err := repo.Reorder([]uint{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	list, err = repo.List()
	if err != nil {
		t.Fatalf("list after reorder: %v", err)
	}
	if list[0].Name != "c" || list[1].Name != "a" || list[2].Name != "b" {
		t.Fatalf("reordered = %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}
	if list[0].Position != 0 || list[1].Position != 1 || list[2].Position != 2 {
		t.Fatalf("positions = %d %d %d", list[0].Position, list[1].Position, list[2].Position)
	}
}

func TestGatewayRoutesSaveForGroupPreservesID(t *testing.T) {
	db, err := Open(DBConfig{
		Driver:       DBDriverSQLite,
		Path:         filepath.Join(t.TempDir(), "gw-route-preserve.db"),
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	routes := NewGatewayRoutes(db)
	if err := routes.SaveForGroup(7, []GatewayRoute{
		{SourceChannelID: 1, Weight: 1, Enabled: true},
		{SourceChannelID: 2, Weight: 2, Enabled: true},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	first, err := routes.ListByGroupID(7)
	if err != nil || len(first) != 2 {
		t.Fatalf("list first: %v len=%d", err, len(first))
	}
	id0, id1 := first[0].ID, first[1].ID
	if id0 == 0 || id1 == 0 {
		t.Fatalf("ids zero: %+v", first)
	}
	// 模拟 ensure-keys 写入上游密钥
	if err := routes.UpdateSourceKey(id0, 41, "upstream-ops-gw-g7-r1", "cipher-a"); err != nil {
		t.Fatalf("key0: %v", err)
	}
	if err := routes.UpdateSourceKey(id1, 42, "k2", "cipher-b"); err != nil {
		t.Fatalf("key1: %v", err)
	}

	// 换序 + 改权重，id 应保持；密钥字段由服务端从旧行回填
	if err := routes.SaveForGroup(7, []GatewayRoute{
		{ID: id1, SourceChannelID: 2, Weight: 9, Enabled: true},
		{ID: id0, SourceChannelID: 1, Weight: 3, Enabled: true},
	}); err != nil {
		t.Fatalf("update reorder: %v", err)
	}
	second, err := routes.ListByGroupID(7)
	if err != nil || len(second) != 2 {
		t.Fatalf("list second: %v len=%d", err, len(second))
	}
	if second[0].ID != id1 || second[1].ID != id0 {
		t.Fatalf("ids not preserved: got %d,%d want %d,%d", second[0].ID, second[1].ID, id1, id0)
	}
	if second[0].Weight != 9 || second[1].Weight != 3 {
		t.Fatalf("weights: %+v", second)
	}
	if second[0].SourceAPIKeyName != "k2" || second[1].SourceAPIKeyName != "upstream-ops-gw-g7-r1" {
		t.Fatalf("keys not preserved: %+v / %+v", second[0], second[1])
	}

	// 删除一条
	if err := routes.SaveForGroup(7, []GatewayRoute{
		{ID: id0, SourceChannelID: 1, Weight: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("delete one: %v", err)
	}
	third, err := routes.ListByGroupID(7)
	if err != nil || len(third) != 1 || third[0].ID != id0 {
		t.Fatalf("after delete: %+v err=%v", third, err)
	}
}

func TestNoteSuccessForPauseErrorClearsAfterStreak(t *testing.T) {
	db := openTestDB(t)
	routes := NewGatewayRoutes(db)
	if err := routes.SaveForGroup(9, []GatewayRoute{
		{SourceChannelID: 11, Weight: 1, Enabled: true},
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	list, err := routes.ListByGroupID(9)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v len=%d", err, len(list))
	}
	id := list[0].ID

	until := time.Now().Add(5 * time.Minute)
	if err := routes.SetTempUnschedulable(id, until, "upstream HTTP error\nstatus: 503", time.Now(), "req_pause_1"); err != nil {
		t.Fatalf("set pause: %v", err)
	}

	// 无残留路由：调用应 noop
	if err := routes.NoteSuccessForPauseError(0); err != nil {
		t.Fatalf("id0: %v", err)
	}

	// 第 1、2 次成功：解除冷却，但保留错误信息
	for i := 1; i <= 2; i++ {
		if err := routes.NoteSuccessForPauseError(id); err != nil {
			t.Fatalf("success %d: %v", i, err)
		}
		got, err := routes.FindByID(id)
		if err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
		if got.TempUnschedulableUntil != nil {
			t.Fatalf("success %d: until should be cleared", i)
		}
		if got.TempUnschedulableReason == "" {
			t.Fatalf("success %d: reason should remain for UI", i)
		}
		if got.RecoverSuccessStreak != i {
			t.Fatalf("success %d: streak=%d", i, got.RecoverSuccessStreak)
		}
	}

	// 第 3 次成功：清空「已恢复/错误/清除」相关字段
	if err := routes.NoteSuccessForPauseError(id); err != nil {
		t.Fatalf("success 3: %v", err)
	}
	got, err := routes.FindByID(id)
	if err != nil {
		t.Fatalf("get final: %v", err)
	}
	if got.TempUnschedulableUntil != nil ||
		got.TempUnschedulableReason != "" ||
		got.TempUnschedulableAt != nil ||
		got.TempUnschedulableRequestID != "" ||
		got.RecoverSuccessStreak != 0 {
		t.Fatalf("expected full clear, got %+v", got)
	}

	// 无残留后再成功：不累计 streak
	if err := routes.NoteSuccessForPauseError(id); err != nil {
		t.Fatalf("success after clear: %v", err)
	}
	got, err = routes.FindByID(id)
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if got.RecoverSuccessStreak != 0 {
		t.Fatalf("streak should stay 0, got %d", got.RecoverSuccessStreak)
	}
}

func TestGatewayUsageListModels(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	now := time.Now()
	rows := []GatewayUsageLog{
		{RequestID: "r1", RequestedModel: "grok-4", UpstreamModel: "grok-4", GatewayGroupID: 1, GatewayKeyID: 1, Success: true, CreatedAt: now},
		{RequestID: "r2", RequestedModel: "grok-4", UpstreamModel: "grok-4", GatewayGroupID: 1, GatewayKeyID: 1, Success: true, CreatedAt: now},
		{RequestID: "r3", RequestedModel: "claude-sonnet", UpstreamModel: "claude-sonnet", GatewayGroupID: 1, GatewayKeyID: 2, Success: true, CreatedAt: now},
		{RequestID: "r4", RequestedModel: "gpt-4o", UpstreamModel: "gpt-4o", GatewayGroupID: 2, GatewayKeyID: 3, Success: true, CreatedAt: now},
		{RequestID: "r5", RequestedModel: "", UpstreamModel: "ignored", GatewayGroupID: 1, GatewayKeyID: 1, Success: true, CreatedAt: now},
	}
	for i := range rows {
		if err := usage.Create(&rows[i]); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	all, err := usage.ListModels(GatewayUsageQuery{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 models, got %d %+v", len(all), all)
	}
	if all[0].Model != "grok-4" || all[0].Count != 2 {
		t.Fatalf("first should be grok-4 x2, got %+v", all[0])
	}

	g1, err := usage.ListModels(GatewayUsageQuery{GatewayGroupID: 1})
	if err != nil {
		t.Fatalf("list g1: %v", err)
	}
	if len(g1) != 2 {
		t.Fatalf("group1 want 2 models, got %d %+v", len(g1), g1)
	}

	// model 筛选不应影响下拉聚合
	withModel, err := usage.ListModels(GatewayUsageQuery{Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("list with model filter ignored: %v", err)
	}
	if len(withModel) != 3 {
		t.Fatalf("model filter should be ignored, got %d", len(withModel))
	}
}

func TestGatewayUsageDispatchStats(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	if err := db.Create(&GatewayGroup{ID: 1, Name: "组一"}).Error; err != nil {
		t.Fatalf("create group 1: %v", err)
	}
	if err := db.Create(&GatewayGroup{ID: 2, Name: "组二"}).Error; err != nil {
		t.Fatalf("create group 2: %v", err)
	}
	if err := db.Create(&GatewayRoute{ID: 11, GatewayGroupID: 1, Position: 0}).Error; err != nil {
		t.Fatalf("create route 11: %v", err)
	}
	if err := db.Create(&GatewayRoute{ID: 12, GatewayGroupID: 1, Position: 1}).Error; err != nil {
		t.Fatalf("create route 12: %v", err)
	}
	if err := db.Create(&GatewayRoute{ID: 21, GatewayGroupID: 2, Position: 0}).Error; err != nil {
		t.Fatalf("create route 21: %v", err)
	}

	now := time.Now().UTC()
	inside := now.Add(-2 * time.Minute)
	rows := []GatewayUsageLog{
		{GatewayGroupID: 1, RouteID: 11, RequestID: "r1", ProviderName: "上游 A", Success: false, CreatedAt: inside, FirstTokenMS: ptrInt64(500)},
		{GatewayGroupID: 1, RouteID: 11, RequestID: "r1", ProviderName: "上游 A", Success: true, CreatedAt: inside.Add(time.Second), FirstTokenMS: ptrInt64(700)},
		{GatewayGroupID: 1, RouteID: 11, RequestID: "r2", ProviderName: "上游 A", Success: true, CreatedAt: inside.Add(2 * time.Second)},
		{GatewayGroupID: 1, RouteID: 12, RequestID: "r3", ProviderName: "上游 B", Success: false, CreatedAt: inside.Add(3 * time.Second), FirstTokenMS: ptrInt64(900)},
		{GatewayGroupID: 2, RouteID: 21, RequestID: "r4", ProviderName: "上游 C", Success: true, CreatedAt: inside.Add(4 * time.Second), FirstTokenMS: ptrInt64(100)},
		{GatewayGroupID: 1, RouteID: 11, RequestID: "outside", ProviderName: "上游 A", Success: false, CreatedAt: now.Add(-2 * time.Hour), FirstTokenMS: ptrInt64(9999)},
	}
	for i := range rows {
		if err := usage.Create(&rows[i]); err != nil {
			t.Fatalf("create usage %d: %v", i, err)
		}
	}

	groups, err := usage.DispatchStats(now.Add(-5*time.Minute), now)
	if err != nil {
		t.Fatalf("dispatch stats: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 groups, got %+v", groups)
	}
	if groups[0].GatewayGroupID != 1 || groups[0].GatewayGroupName != "组一" {
		t.Fatalf("unexpected first group: %+v", groups[0])
	}
	if len(groups[0].Routes) != 2 {
		t.Fatalf("want 2 routes in group 1, got %+v", groups[0].Routes)
	}
	first := groups[0].Routes[0]
	if first.RouteID != 11 || first.TotalAttempts != 3 || first.FailedAttempts != 1 {
		t.Fatalf("unexpected route 11 counts: %+v", first)
	}
	if first.FailureRate != 1.0/3.0 || first.FirstTokenSamples != 2 || first.AverageFirstTokenMS == nil || *first.AverageFirstTokenMS != 600 {
		t.Fatalf("unexpected route 11 metrics: %+v", first)
	}
	second := groups[0].Routes[1]
	if second.RouteID != 12 || second.FailureRate != 1 || second.AverageFirstTokenMS == nil || *second.AverageFirstTokenMS != 900 {
		t.Fatalf("unexpected route 12 metrics: %+v", second)
	}
	if groups[1].Routes[0].RouteID != 21 || groups[1].Routes[0].FailureRate != 0 {
		t.Fatalf("unexpected group 2 metrics: %+v", groups[1])
	}

	empty, err := usage.DispatchStats(now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("empty dispatch stats: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("want empty groups, got %+v", empty)
	}
}

func TestGatewayUsageDispatchTrendsAggregatesRequestChains(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	if err := db.Create(&GatewayGroup{ID: 9, Name: "趋势网关"}).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	rows := []GatewayUsageLog{
		{GatewayGroupID: 9, RouteID: 91, RequestID: "trend-1", Attempt: 1, AttemptKind: "primary", Success: false, FirstTokenMS: ptrInt64(900), CreatedAt: from.Add(30 * time.Second)},
		{GatewayGroupID: 9, RouteID: 92, RequestID: "trend-1", Attempt: 2, AttemptKind: "failover", Success: true, FirstTokenMS: ptrInt64(500), CreatedAt: from.Add(31 * time.Second)},
		{GatewayGroupID: 9, RouteID: 91, RequestID: "trend-2", Attempt: 1, AttemptKind: "primary", Success: true, FirstTokenMS: ptrInt64(700), CreatedAt: from.Add(2 * time.Minute)},
		{GatewayGroupID: 9, RouteID: 92, RequestID: "trend-3", Attempt: 1, AttemptKind: "primary", Success: false, FirstTokenMS: ptrInt64(1100), CreatedAt: from.Add(6 * time.Minute)},
	}
	for i := range rows {
		if err := usage.Create(&rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	trends, err := usage.DispatchTrends(from, to, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(trends.Groups) != 1 || len(trends.Groups[0].Points) != 2 {
		t.Fatalf("unexpected trend groups: %+v", trends.Groups)
	}
	first := trends.Groups[0].Points[0]
	if first.Requests != 2 || first.FinalErrorRate != 0 || first.FailoverTriggerRate != .5 || first.FailoverRecoveryRate != 1 || first.TTFTP50 != 700 || first.TTFTP95 != 900 {
		t.Fatalf("unexpected first point: %+v", first)
	}
	if first.RPM != .4 {
		t.Fatalf("unexpected rpm: %v", first.RPM)
	}
	var route91 *GatewayDispatchTrendRoute
	for i := range trends.Groups[0].Routes {
		if trends.Groups[0].Routes[i].RouteID == 91 {
			route91 = &trends.Groups[0].Routes[i]
			break
		}
	}
	if route91 == nil || len(route91.Points) != 1 || route91.Points[0].FinalErrorRate != .5 {
		t.Fatalf("route attempts were not isolated: %+v", route91)
	}
}

func ptrInt64(v int64) *int64 { return &v }

func TestGatewayUsageDispatchErrorsBreaksDownFailures(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	if err := db.Create(&GatewayGroup{ID: 21, Name: "错误网关"}).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	rows := []GatewayUsageLog{
		// err-1：先 429 失败，再顺延成功 → 最终成功，但计入一次失败尝试
		{GatewayGroupID: 21, RouteID: 1, RequestID: "err-1", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 429, ErrorMessage: "rate limited", CreatedAt: from.Add(10 * time.Second)},
		{GatewayGroupID: 21, RouteID: 2, RequestID: "err-1", Attempt: 2, AttemptKind: "failover", Success: true, CreatedAt: from.Add(11 * time.Second)},
		// err-2：两次都失败 → 最终失败
		{GatewayGroupID: 21, RouteID: 1, RequestID: "err-2", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 429, ErrorMessage: "rate limited", CreatedAt: from.Add(1 * time.Minute)},
		{GatewayGroupID: 21, RouteID: 2, RequestID: "err-2", Attempt: 2, AttemptKind: "failover", Success: false, ErrorType: "transport", StatusCode: 0, ErrorMessage: "dial timeout", CreatedAt: from.Add(61 * time.Second)},
		// err-3：纯成功
		{GatewayGroupID: 21, RouteID: 1, RequestID: "err-3", Attempt: 1, Success: true, CreatedAt: from.Add(2 * time.Minute)},
		// err-4：一次 500 失败
		{GatewayGroupID: 21, RouteID: 1, RequestID: "err-4", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 500, ErrorMessage: "upstream boom", CreatedAt: from.Add(3 * time.Minute)},
	}
	for i := range rows {
		if err := usage.Create(&rows[i]); err != nil {
			t.Fatal(err)
		}
	}

	got, err := usage.DispatchErrors(from, to)
	if err != nil {
		t.Fatalf("dispatch errors: %v", err)
	}
	if got.Requests != 4 {
		t.Fatalf("requests = %d, want 4", got.Requests)
	}
	if got.FinalFailed != 2 {
		t.Fatalf("final failed = %d, want 2 (err-2, err-4)", got.FinalFailed)
	}
	if got.RecoveredRequests != 1 {
		t.Fatalf("recovered = %d, want 1 (err-1)", got.RecoveredRequests)
	}
	if got.Attempts != 6 {
		t.Fatalf("attempts = %d, want 6", got.Attempts)
	}
	if got.FailedAttempts != 4 {
		t.Fatalf("failed attempts = %d, want 4", got.FailedAttempts)
	}
	if math.Abs(got.ErrorRate-0.5) > 1e-9 {
		t.Fatalf("error rate = %v, want 0.5", got.ErrorRate)
	}
	// 分类按失败尝试数降序：http 3 条（429×2 + 500×1），transport 1 条
	if len(got.Categories) != 2 {
		t.Fatalf("categories = %d, want 2", len(got.Categories))
	}
	if got.Categories[0].ErrorType != "http" || got.Categories[0].Count != 3 {
		t.Fatalf("first category = %+v, want http/3", got.Categories[0])
	}
	if got.Categories[0].Label != "上游 HTTP 错误" {
		t.Fatalf("http label = %q", got.Categories[0].Label)
	}
	if len(got.Categories[0].Codes) != 2 || got.Categories[0].Codes[0].StatusCode != 429 || got.Categories[0].Codes[0].Count != 2 {
		t.Fatalf("http codes = %+v, want 429×2 first", got.Categories[0].Codes)
	}
	if got.Categories[1].ErrorType != "transport" || got.Categories[1].Codes[0].Label != "无响应" {
		t.Fatalf("transport category = %+v", got.Categories[1])
	}
	// 相同 message 合并计数，按次数降序
	if len(got.Samples) == 0 || got.Samples[0].Message != "rate limited" || got.Samples[0].Count != 2 {
		t.Fatalf("top sample = %+v, want rate limited ×2", got.Samples)
	}
	if len(got.Groups) != 1 || got.Groups[0].GatewayGroupName != "错误网关" || got.Groups[0].FinalFailed != 2 {
		t.Fatalf("groups = %+v", got.Groups)
	}
}

func TestGatewayUsageDispatchErrorsRejectsBadRange(t *testing.T) {
	usage := NewGatewayUsageLogs(openTestDB(t))
	at := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	if _, err := usage.DispatchErrors(at, at); err == nil {
		t.Fatal("expected error for non-advancing range")
	}
	if _, err := usage.DispatchErrors(time.Time{}, at); err == nil {
		t.Fatal("expected error for zero from")
	}
}
