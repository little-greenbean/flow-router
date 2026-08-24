package storage

import (
	"fmt"
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
		// err-1：路由 1 先 429 失败，路由 2 顺延成功 → 链最终成功，但路由 1 记一次失败尝试
		{GatewayGroupID: 21, RouteID: 1, SourceAPIKeyName: "primary", ProviderName: "P1", RequestID: "err-1", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 429, ErrorMessage: "rate limited", CreatedAt: from.Add(10 * time.Second)},
		{GatewayGroupID: 21, RouteID: 2, SourceAPIKeyName: "backup", ProviderName: "P2", RequestID: "err-1", Attempt: 2, AttemptKind: "failover", Success: true, CreatedAt: from.Add(11 * time.Second)},
		// err-2：两条路由都失败 → 链最终失败
		{GatewayGroupID: 21, RouteID: 1, SourceAPIKeyName: "primary", RequestID: "err-2", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 429, ErrorMessage: "rate limited", CreatedAt: from.Add(1 * time.Minute)},
		{GatewayGroupID: 21, RouteID: 2, SourceAPIKeyName: "backup", RequestID: "err-2", Attempt: 2, AttemptKind: "failover", Success: false, ErrorType: "transport", StatusCode: 0, ErrorMessage: "dial timeout", CreatedAt: from.Add(61 * time.Second)},
		// err-3：纯成功
		{GatewayGroupID: 21, RouteID: 1, SourceAPIKeyName: "primary", RequestID: "err-3", Attempt: 1, Success: true, CreatedAt: from.Add(2 * time.Minute)},
		// err-4：路由 1 一次 500 失败，无顺延
		{GatewayGroupID: 21, RouteID: 1, SourceAPIKeyName: "primary", RequestID: "err-4", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 500, ErrorMessage: "upstream boom", CreatedAt: from.Add(3 * time.Minute)},
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
	// 总体：链口径
	if got.Requests != 4 || got.FinalFailed != 2 || got.RecoveredRequests != 1 {
		t.Fatalf("totals = requests %d / final %d / recovered %d, want 4/2/1", got.Requests, got.FinalFailed, got.RecoveredRequests)
	}
	if math.Abs(got.ErrorRate-0.5) > 1e-9 {
		t.Fatalf("error rate = %v, want 0.5", got.ErrorRate)
	}
	// 总体：尝试口径
	if got.Attempts != 6 || got.FailedAttempts != 4 {
		t.Fatalf("attempts = %d / failed %d, want 6/4", got.Attempts, got.FailedAttempts)
	}
	if len(got.Categories) != 2 || got.Categories[0].ErrorType != "http" || got.Categories[0].Count != 3 {
		t.Fatalf("categories = %+v, want http/3 first", got.Categories)
	}
	if len(got.Categories[0].Codes) != 2 || got.Categories[0].Codes[0].StatusCode != 429 || got.Categories[0].Codes[0].Count != 2 {
		t.Fatalf("http codes = %+v, want 429x2 first", got.Categories[0].Codes)
	}
	if got.Categories[1].Codes[0].Label != "无响应" {
		t.Fatalf("transport code label = %q, want 无响应", got.Categories[1].Codes[0].Label)
	}

	// 网关层
	if len(got.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(got.Groups))
	}
	group := got.Groups[0]
	if group.GatewayGroupName != "错误网关" || group.Requests != 4 || group.FinalFailed != 2 || group.RecoveredRequests != 1 {
		t.Fatalf("group = %+v", group.GatewayDispatchErrorScope)
	}

	// 路由层：只有尝试口径。路由 1 = 4 次尝试 3 次失败，路由 2 = 2 次尝试 1 次失败。
	if len(group.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(group.Routes))
	}
	byName := map[string]GatewayDispatchErrorRoute{}
	for _, route := range group.Routes {
		byName[route.RouteName] = route
	}
	primary, ok := byName["primary"]
	if !ok {
		t.Fatalf("routes = %+v, want one named primary", group.Routes)
	}
	if primary.ProviderName != "P1" || primary.Attempts != 4 || primary.FailedAttempts != 3 || primary.Requests != 4 {
		t.Fatalf("primary = %+v provider=%q", primary.GatewayDispatchErrorScope, primary.ProviderName)
	}
	if math.Abs(primary.AttemptErrorRate-0.75) > 1e-9 {
		t.Fatalf("primary attempt error rate = %v, want 0.75", primary.AttemptErrorRate)
	}
	if len(primary.Categories) != 1 || primary.Categories[0].ErrorType != "http" || primary.Categories[0].Count != 3 {
		t.Fatalf("primary categories = %+v, want http/3 only", primary.Categories)
	}
	backup := byName["backup"]
	if backup.Attempts != 2 || backup.FailedAttempts != 1 || backup.Requests != 2 {
		t.Fatalf("backup = %+v", backup.GatewayDispatchErrorScope)
	}
	if len(backup.Categories) != 1 || backup.Categories[0].ErrorType != "transport" {
		t.Fatalf("backup categories = %+v, want transport only", backup.Categories)
	}
	// 路由按失败尝试数降序
	if group.Routes[0].RouteName != "primary" {
		t.Fatalf("route order = %v, want primary first", []string{group.Routes[0].RouteName, group.Routes[1].RouteName})
	}
	// 同 message 合并计数
	if len(got.Samples) == 0 || got.Samples[0].Message != "rate limited" || got.Samples[0].Count != 2 {
		t.Fatalf("top sample = %+v", got.Samples)
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

// dispatchFlowFixture 造一份可复用的调度记录：
//
//	fl-1 路由 11 直接成功
//	fl-2 路由 11 失败 → 顺延路由 12 成功
//	fl-3 路由 11 失败 → 路由 12 失败 → 最终失败
//	fl-4 另一个网关（32）里路由 21 直接成功
func dispatchFlowFixture(t *testing.T) (*GatewayUsageLogs, time.Time, time.Time) {
	t.Helper()
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	if err := db.Create(&GatewayGroup{ID: 31, Name: "主网关"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&GatewayGroup{ID: 32, Name: "备网关"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&Channel{ID: 7, Name: "渠道七", Type: ChannelTypeNewAPI, SiteURL: "https://x", Username: "u"}).Error; err != nil {
		t.Fatal(err)
	}
	// 路由 11 还在，路由 12 已删除（只剩历史日志）
	if err := db.Create(&GatewayRoute{ID: 11, GatewayGroupID: 31, Position: 0, SourceChannelID: 7, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(10 * time.Minute)
	rows := []GatewayUsageLog{
		{GatewayGroupID: 31, RouteID: 11, ChannelID: 7, SourceGroupName: "源组A", RequestID: "fl-1", Attempt: 1, Success: true, CreatedAt: from.Add(1 * time.Second)},
		{GatewayGroupID: 31, RouteID: 11, ChannelID: 7, SourceGroupName: "源组A", RequestID: "fl-2", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 403, ErrorMessage: "余额不足", CreatedAt: from.Add(2 * time.Second)},
		{GatewayGroupID: 31, RouteID: 12, ProviderName: "供应商B", RequestID: "fl-2", Attempt: 2, AttemptKind: "failover", Success: true, CreatedAt: from.Add(3 * time.Second)},
		{GatewayGroupID: 31, RouteID: 11, ChannelID: 7, RequestID: "fl-3", Attempt: 1, Success: false, ErrorType: "http", StatusCode: 403, ErrorMessage: "余额不足", CreatedAt: from.Add(4 * time.Second)},
		{GatewayGroupID: 31, RouteID: 12, ProviderName: "供应商B", RequestID: "fl-3", Attempt: 2, AttemptKind: "failover", Success: false, ErrorType: "http", StatusCode: 502, ErrorMessage: "网关错误", CreatedAt: from.Add(5 * time.Second)},
		{GatewayGroupID: 32, RouteID: 21, RequestID: "fl-4", Attempt: 1, Success: true, CreatedAt: from.Add(6 * time.Second)},
	}
	for i := range rows {
		if err := usage.Create(&rows[i]); err != nil {
			t.Fatal(err)
		}
	}
	return usage, from, to
}

func flowLink(t *testing.T, flow GatewayDispatchFlow, source, target string) GatewayDispatchFlowLink {
	t.Helper()
	for _, link := range flow.Links {
		if link.Source == source && link.Target == target {
			return link
		}
	}
	t.Fatalf("link %s -> %s not found in %+v", source, target, flow.Links)
	return GatewayDispatchFlowLink{}
}

func flowNode(t *testing.T, flow GatewayDispatchFlow, id string) GatewayDispatchFlowNode {
	t.Helper()
	for _, node := range flow.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("node %s not found", id)
	return GatewayDispatchFlowNode{}
}

func TestGatewayUsageDispatchFlowSplitsByGateway(t *testing.T) {
	usage, from, to := dispatchFlowFixture(t)
	flow, err := usage.DispatchFlow(from, to, 0, DispatchFlowFailoverFilterAny)
	if err != nil {
		t.Fatalf("dispatch flow: %v", err)
	}
	if flow.Scope != "all" {
		t.Fatalf("scope = %q, want all", flow.Scope)
	}
	// 4 条链、6 次尝试：链级和尝试级是两个口径，别混
	if flow.Requests != 4 || flow.Attempts != 6 {
		t.Fatalf("requests/attempts = %d/%d, want 4/6", flow.Requests, flow.Attempts)
	}
	if flow.MaxHops != 2 {
		t.Fatalf("max hops = %d, want 2", flow.MaxHops)
	}
	if link := flowLink(t, flow, "root", "g:31"); link.Value != 3 {
		t.Fatalf("root -> g:31 = %d, want 3", link.Value)
	}
	if link := flowLink(t, flow, "root", "g:32"); link.Value != 1 {
		t.Fatalf("root -> g:32 = %d, want 1", link.Value)
	}
	// fl-1 一次过、fl-2 顺延后成功、fl-3 最终失败
	if link := flowLink(t, flow, "g:31", "o:direct"); link.Value != 1 || link.Failed {
		t.Fatalf("g:31 -> direct = %+v, want value 1 / failed false", link)
	}
	if link := flowLink(t, flow, "g:31", "o:recovered"); link.Value != 1 {
		t.Fatalf("g:31 -> recovered = %d, want 1", link.Value)
	}
	if link := flowLink(t, flow, "g:31", "o:failed"); link.Value != 1 || !link.Failed {
		t.Fatalf("g:31 -> failed = %+v, want value 1 / failed true", link)
	}
	if node := flowNode(t, flow, "g:31"); node.Value != 3 || node.Label != "主网关" || node.Depth != 1 {
		t.Fatalf("g:31 node = %+v", node)
	}
	// 根节点没有入边，权重要按出边算，否则画出来是 0 宽
	if node := flowNode(t, flow, "root"); node.Value != 4 {
		t.Fatalf("root value = %d, want 4", node.Value)
	}
}

func TestGatewayUsageDispatchFlowDrillsIntoRoutesByHop(t *testing.T) {
	usage, from, to := dispatchFlowFixture(t)
	flow, err := usage.DispatchFlow(from, to, 31, DispatchFlowFailoverFilterAny)
	if err != nil {
		t.Fatalf("dispatch flow: %v", err)
	}
	if flow.Scope != "gateway" || flow.GatewayGroupName != "主网关" {
		t.Fatalf("scope = %q / %q", flow.Scope, flow.GatewayGroupName)
	}
	// 只算该网关的链，另一个网关的 fl-4 不该混进来
	if flow.Requests != 3 {
		t.Fatalf("requests = %d, want 3", flow.Requests)
	}
	// 三条链都从路由 11 起跳
	if link := flowLink(t, flow, "g:31", "h1:r11"); link.Value != 3 || link.Failed {
		t.Fatalf("entry -> h1:r11 = %+v, want value 3 / failed false", link)
	}
	// 路由 11 失败两次，都顺延到路由 12；这条边必须标成 failed
	if link := flowLink(t, flow, "h1:r11", "h2:r12"); link.Value != 2 || !link.Failed {
		t.Fatalf("h1:r11 -> h2:r12 = %+v, want value 2 / failed true", link)
	}
	if link := flowLink(t, flow, "h1:r11", "o:direct"); link.Value != 1 {
		t.Fatalf("h1:r11 -> direct = %d, want 1", link.Value)
	}
	if link := flowLink(t, flow, "h2:r12", "o:recovered"); link.Value != 1 {
		t.Fatalf("h2:r12 -> recovered = %d, want 1", link.Value)
	}
	if link := flowLink(t, flow, "h2:r12", "o:failed"); link.Value != 1 || !link.Failed {
		t.Fatalf("h2:r12 -> failed = %+v", link)
	}
	// 身份用来源 · 源分组，不用密钥名；provider 类路由退回 provider 名
	first := flowNode(t, flow, "h1:r11")
	if first.Label != "渠道七 · 源组A" || first.Hop != 1 || !first.Alive {
		t.Fatalf("h1:r11 node = %+v", first)
	}
	second := flowNode(t, flow, "h2:r12")
	if second.Label != "供应商B" || second.Hop != 2 {
		t.Fatalf("h2:r12 node = %+v", second)
	}
	// 路由 12 已删除 → alive=false，前端不给跳转
	if second.Alive {
		t.Fatalf("h2:r12 alive = true, want false (已删除)")
	}
	// 同一条路由出现在不同跳必须是不同节点，否则桑基图会成环画不出来
	if first.RouteID != 11 || second.RouteID != 12 {
		t.Fatalf("route ids = %d/%d", first.RouteID, second.RouteID)
	}
	// 结局列紧跟在这个网关实际用到的最深一跳（2）后面，不是永远留到理论上限，
	// 否则第 3～7 列全是空的，右边一大片死白
	outcome := flowNode(t, flow, "o:direct")
	if outcome.Depth != 3 {
		t.Fatalf("outcome depth = %d, want 3 (maxHops 2 + 1)", outcome.Depth)
	}
}

func TestGatewayUsageDispatchFlowFiltersByFailoverCount(t *testing.T) {
	usage, from, to := dispatchFlowFixture(t)

	// fl-1 零顺延（一次过），fl-2/fl-3 各顺延一次
	zero, err := usage.DispatchFlow(from, to, 31, 0)
	if err != nil {
		t.Fatalf("dispatch flow: %v", err)
	}
	if zero.Requests != 1 || zero.Attempts != 1 {
		t.Fatalf("filter=0 requests/attempts = %d/%d, want 1/1", zero.Requests, zero.Attempts)
	}
	if link := flowLink(t, zero, "h1:r11", "o:direct"); link.Value != 1 {
		t.Fatalf("filter=0 h1:r11 -> direct = %d, want 1", link.Value)
	}
	// fl-2/fl-3 顺延过，这一档不该出现
	for _, link := range zero.Links {
		if link.Target == "h2:r12" {
			t.Fatalf("filter=0 不该包含顺延过的链，却有 %+v", link)
		}
	}

	one, err := usage.DispatchFlow(from, to, 31, 1)
	if err != nil {
		t.Fatalf("dispatch flow: %v", err)
	}
	if one.Requests != 2 || one.Attempts != 4 {
		t.Fatalf("filter=1 requests/attempts = %d/%d, want 2/4", one.Requests, one.Attempts)
	}
	// fl-1 零顺延不该混进「顺延 1 次」这一档
	for _, link := range one.Links {
		if link.Target == "o:direct" {
			t.Fatalf("filter=1 不该有 direct 分流，实际 %+v", link)
		}
	}

	// 这份数据里没有任何链顺延 2 次——筛选结果应该是空的，而不是报错或退回全部
	two, err := usage.DispatchFlow(from, to, 31, 2)
	if err != nil {
		t.Fatalf("dispatch flow: %v", err)
	}
	if two.Requests != 0 || len(two.Nodes) != 1 || two.Nodes[0].ID != "g:31" {
		t.Fatalf("filter=2 = %+v, want 0 requests / 只剩网关根节点", two)
	}
}

func TestDispatchFlowFailoverCountAndFilterBuckets(t *testing.T) {
	count := dispatchFlowFailoverCount([]dispatchFlowAttempt{
		{attemptKind: "primary"},
		{attemptKind: "failover"},
		{attemptKind: "retry"},
		{attemptKind: "failover"},
	})
	if count != 2 {
		t.Fatalf("failover count = %d, want 2 (重试不算顺延)", count)
	}

	cases := []struct {
		name   string
		count  int
		filter int
		want   bool
	}{
		{"不筛选总是命中", 0, DispatchFlowFailoverFilterAny, true},
		{"精确匹配", 2, 2, true},
		{"精确不匹配", 2, 3, false},
		{"达到溢出档算命中", dispatchFlowFailoverFilterOverflow, dispatchFlowFailoverFilterOverflow, true},
		{"超过溢出档也算命中", dispatchFlowFailoverFilterOverflow + 3, dispatchFlowFailoverFilterOverflow, true},
		{"没到溢出档不该命中", dispatchFlowFailoverFilterOverflow - 1, dispatchFlowFailoverFilterOverflow, false},
	}
	for _, tc := range cases {
		if got := dispatchFlowMatchesFailoverFilter(tc.count, tc.filter); got != tc.want {
			t.Errorf("%s: matches(%d, %d) = %v, want %v", tc.name, tc.count, tc.filter, got, tc.want)
		}
	}
}

func TestGatewayUsageDispatchFlowCollapsesDeepChains(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	from := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	// 一条链连着顺延 8 跳，超过 dispatchFlowMaxHops
	hops := dispatchFlowMaxHops + 3
	for i := 0; i < hops; i++ {
		row := GatewayUsageLog{
			GatewayGroupID: 41, RouteID: uint(100 + i), RequestID: "deep", Attempt: i + 1,
			Success: i == hops-1, CreatedAt: from.Add(time.Duration(i) * time.Second),
		}
		if i > 0 {
			row.AttemptKind = "failover"
		}
		if err := usage.Create(&row); err != nil {
			t.Fatal(err)
		}
	}
	flow, err := usage.DispatchFlow(from, to, 41, DispatchFlowFailoverFilterAny)
	if err != nil {
		t.Fatal(err)
	}
	if flow.MaxHops != hops {
		t.Fatalf("max hops = %d, want %d (实际深度要如实报出来)", flow.MaxHops, hops)
	}
	// 超过上限的那几跳收进一个节点，而且同一条链只落一次——否则会重复计数
	if link := flowLink(t, flow, fmt.Sprintf("h%d:r%d", dispatchFlowMaxHops, 100+dispatchFlowMaxHops-1), "h:more"); link.Value != 1 {
		t.Fatalf("overflow link = %d, want 1", link.Value)
	}
	if node := flowNode(t, flow, "h:more"); node.Value != 1 || node.Kind != dispatchFlowNodeOverflow {
		t.Fatalf("overflow node = %+v", node)
	}
	if link := flowLink(t, flow, "h:more", "o:recovered"); link.Value != 1 {
		t.Fatalf("overflow -> recovered = %d, want 1", link.Value)
	}
	for _, node := range flow.Nodes {
		if node.Hop > dispatchFlowMaxHops {
			t.Fatalf("node %s 的 hop=%d 超过上限，应该收进 h:more", node.ID, node.Hop)
		}
	}
}

func TestGatewayUsageDispatchFlowListsGatewaysRegardlessOfDrill(t *testing.T) {
	usage, from, to := dispatchFlowFixture(t)
	// 下钻到 31 之后，tag 列表仍然要能看到 32——否则切不回去
	flow, err := usage.DispatchFlow(from, to, 31, DispatchFlowFailoverFilterAny)
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.Gateways) != 2 {
		t.Fatalf("gateways = %+v, want 2 (不受下钻过滤影响)", flow.Gateways)
	}
	// 流量大的排前面
	if flow.Gateways[0].GatewayGroupID != 31 || flow.Gateways[0].Requests != 3 {
		t.Fatalf("first gateway = %+v, want 31 / 3 requests", flow.Gateways[0])
	}
	if flow.Gateways[1].GatewayGroupID != 32 || flow.Gateways[1].Name != "备网关" || flow.Gateways[1].Requests != 1 {
		t.Fatalf("second gateway = %+v", flow.Gateways[1])
	}
}

func TestGatewayUsageDispatchFlowRejectsBadRange(t *testing.T) {
	db := openTestDB(t)
	usage := NewGatewayUsageLogs(db)
	now := time.Now().UTC()
	if _, err := usage.DispatchFlow(now, now, 0, DispatchFlowFailoverFilterAny); err == nil {
		t.Fatal("zero-duration range should fail")
	}
	if _, err := usage.DispatchFlow(time.Time{}, now, 0, DispatchFlowFailoverFilterAny); err == nil {
		t.Fatal("zero from should fail")
	}
}

func TestDispatchSeverityClassifiesByUrgency(t *testing.T) {
	cases := []struct {
		name string
		log  GatewayUsageLog
		want int
	}{
		{"成功不分级", GatewayUsageLog{Success: true}, -1},
		{"401 认证失效需人工", GatewayUsageLog{ErrorType: "http", StatusCode: 401}, dispatchSeverityP0},
		{"403 欠费需人工", GatewayUsageLog{ErrorType: "http", StatusCode: 403}, dispatchSeverityP0},
		{"404 映射配错需人工", GatewayUsageLog{ErrorType: "http", StatusCode: 404}, dispatchSeverityP0},
		{"配置错误需人工", GatewayUsageLog{ErrorType: "config", StatusCode: 0}, dispatchSeverityP0},
		{"429 限流算抖动", GatewayUsageLog{ErrorType: "http", StatusCode: 429}, dispatchSeverityP1},
		{"502 算抖动", GatewayUsageLog{ErrorType: "http", StatusCode: 502}, dispatchSeverityP1},
		{"传输超时算抖动", GatewayUsageLog{ErrorType: "transport", StatusCode: 0}, dispatchSeverityP1},
		{"客户端断开是噪声", GatewayUsageLog{ErrorType: "client", StatusCode: 0}, dispatchSeverityP2},
		{"499 是噪声", GatewayUsageLog{ErrorType: "", StatusCode: 499}, dispatchSeverityP2},
	}
	for _, tc := range cases {
		if got := dispatchSeverityOf(tc.log); got != tc.want {
			t.Errorf("%s: severity = %d, want %d", tc.name, got, tc.want)
		}
	}
}
