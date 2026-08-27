package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bejix/upstream-ops/backend/connector"
	"github.com/bejix/upstream-ops/backend/storage"
)

// gateChannelAPI ListAPIKeyGroups 可被 gate 卡住，用于验证数据面不被慢渠道阻塞。
type gateChannelAPI struct {
	mu     sync.Mutex
	calls  map[uint]int
	gate   chan struct{} // 非 nil 时 ListAPIKeyGroups 阻塞至 gate 关闭
	groups map[uint][]connector.APIKeyGroup
	errs   map[uint]error
}

func (f *gateChannelAPI) callCount(id uint) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func (f *gateChannelAPI) ListAPIKeyGroups(ctx context.Context, channelID uint) ([]connector.APIKeyGroup, error) {
	f.mu.Lock()
	if f.calls == nil {
		f.calls = map[uint]int{}
	}
	f.calls[channelID]++
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := f.errs[channelID]; err != nil {
		return nil, err
	}
	return f.groups[channelID], nil
}

func (f *gateChannelAPI) ListAPIKeys(ctx context.Context, channelID uint, query connector.APIKeyQuery) (*connector.APIKeyPage, error) {
	return &connector.APIKeyPage{}, nil
}

func (f *gateChannelAPI) CreateAPIKey(ctx context.Context, channelID uint, req connector.APIKeyCreateRequest) (*connector.APIKey, error) {
	return nil, nil
}

func (f *gateChannelAPI) UpdateAPIKey(ctx context.Context, channelID uint, keyID int64, req connector.APIKeyUpdateRequest) (*connector.APIKey, error) {
	return nil, nil
}

func (f *gateChannelAPI) RevealAPIKey(ctx context.Context, channelID uint, keyID int64) (string, error) {
	return "", nil
}

func monitorRoutes(channelID uint) []storage.GatewayRoute {
	return []storage.GatewayRoute{{
		SourceKind:         storage.GatewayRouteSourceMonitor,
		SourceChannelID:    channelID,
		SourceAPIKeyCipher: "k",
	}}
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func groupsCacheSvc(fake *gateChannelAPI) *Service {
	return NewService(nil, nil, nil, nil, nil, nil, fake, nil, nil)
}

// setCacheEntry 直接写缓存，at 控制新鲜度。
func setCacheEntry(svc *Service, id uint, groups []connector.APIKeyGroup, at time.Time) {
	svc.channelGroupsCacheMu.Lock()
	defer svc.channelGroupsCacheMu.Unlock()
	svc.channelGroupsCache[id] = channelGroupsCacheEntry{at: at, groups: groups}
}

// 冷缓存 + 渠道管理面挂死：数据面调用必须立即返回，不等上游。
func TestLoadGroupsByChannelNeverBlocksOnColdMiss(t *testing.T) {
	gid := int64(1)
	fake := &gateChannelAPI{
		gate:   make(chan struct{}),
		groups: map[uint][]connector.APIKeyGroup{7: {{ID: &gid, Name: "g", Ratio: 0.5}}},
	}
	t.Cleanup(func() { close(fake.gate) })
	svc := groupsCacheSvc(fake)

	start := time.Now()
	out := svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("data-plane load blocked for %s; must return immediately", elapsed)
	}
	if got := out[7]; got != nil {
		t.Fatalf("cold miss should yield nil groups, got %+v", got)
	}
}

// 冷缓存：后台刷新完成后，后续调用能拿到分组。
func TestLoadGroupsByChannelBackgroundRefreshPopulatesCache(t *testing.T) {
	gid := int64(1)
	fake := &gateChannelAPI{
		groups: map[uint][]connector.APIKeyGroup{7: {{ID: &gid, Name: "g", Ratio: 0.5}}},
	}
	svc := groupsCacheSvc(fake)

	_ = svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
	waitUntil(t, 2*time.Second, func() bool {
		out := svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
		return len(out[7]) == 1 && out[7][0].Name == "g"
	})
}

// 过期缓存 + 上游挂死：立刻返回旧值，同时只起一个后台刷新。
func TestLoadGroupsByChannelServesStaleWhileRefreshing(t *testing.T) {
	oldID := int64(1)
	fake := &gateChannelAPI{gate: make(chan struct{})}
	t.Cleanup(func() { close(fake.gate) })
	svc := groupsCacheSvc(fake)
	stale := []connector.APIKeyGroup{{ID: &oldID, Name: "stale", Ratio: 0.3}}
	setCacheEntry(svc, 7, stale, time.Now().Add(-time.Hour))

	// 首次触发后台刷新，等它真正打到上游（并卡在 gate 上）
	_ = svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
	waitUntil(t, 2*time.Second, func() bool { return fake.callCount(7) == 1 })

	for i := 0; i < 3; i++ {
		start := time.Now()
		out := svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
		if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
			t.Fatalf("stale load blocked for %s", elapsed)
		}
		if len(out[7]) != 1 || out[7][0].Name != "stale" {
			t.Fatalf("expected stale groups served, got %+v", out[7])
		}
	}
	// 单飞：上游没返回前重复触发也只有一次调用
	if n := fake.callCount(7); n != 1 {
		t.Fatalf("expected single in-flight refresh, got %d calls", n)
	}
}

// 刷新失败：保留旧值，且失败也推后下次刷新（不连环打病态渠道）。
func TestRefreshFailureKeepsStaleGroups(t *testing.T) {
	oldID := int64(1)
	fake := &gateChannelAPI{errs: map[uint]error{7: errors.New("origin down")}}
	svc := groupsCacheSvc(fake)
	stale := []connector.APIKeyGroup{{ID: &oldID, Name: "stale", Ratio: 0.3}}
	setCacheEntry(svc, 7, stale, time.Now().Add(-time.Hour))

	_ = svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
	waitUntil(t, 2*time.Second, func() bool { return fake.callCount(7) == 1 })
	// 刷新已失败：旧值仍在，且条目已续期 → 不会立刻再次刷新
	waitUntil(t, 2*time.Second, func() bool {
		svc.channelGroupsCacheMu.Lock()
		_, busy := svc.channelGroupsRefreshing[7]
		svc.channelGroupsCacheMu.Unlock()
		return !busy
	})
	out := svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
	if len(out[7]) != 1 || out[7][0].Name != "stale" {
		t.Fatalf("failure must keep stale groups, got %+v", out[7])
	}
	if n := fake.callCount(7); n != 1 {
		t.Fatalf("failed refresh should be paced by TTL, got %d calls", n)
	}
}

// Invalidate 只标过期不清数据：数据面继续拿旧值，随后异步刷成新值。
func TestInvalidateChannelGroupsCacheKeepsServingStale(t *testing.T) {
	oldID, newID := int64(1), int64(2)
	fake := &gateChannelAPI{
		groups: map[uint][]connector.APIKeyGroup{7: {{ID: &newID, Name: "fresh", Ratio: 0.2}}},
	}
	svc := groupsCacheSvc(fake)
	setCacheEntry(svc, 7, []connector.APIKeyGroup{{ID: &oldID, Name: "old", Ratio: 0.4}}, time.Now())

	svc.InvalidateChannelGroupsCache()

	out := svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
	if len(out[7]) != 1 || out[7][0].Name != "old" {
		t.Fatalf("invalidate must keep serving old groups, got %+v", out[7])
	}
	waitUntil(t, 2*time.Second, func() bool {
		out := svc.runtime().loadGroupsByChannel(context.Background(), monitorRoutes(7))
		return len(out[7]) == 1 && out[7][0].Name == "fresh"
	})
}

// 管理面同步变体：未命中时等上游返回结果并写缓存。
func TestLoadGroupsByChannelBlockingFetchesSynchronously(t *testing.T) {
	gid := int64(1)
	fake := &gateChannelAPI{
		groups: map[uint][]connector.APIKeyGroup{7: {{ID: &gid, Name: "g", Ratio: 0.5}}},
	}
	svc := groupsCacheSvc(fake)

	out := svc.runtime().loadGroupsByChannelBlocking(context.Background(), monitorRoutes(7))
	if len(out[7]) != 1 || out[7][0].Name != "g" {
		t.Fatalf("blocking variant must fetch synchronously, got %+v", out[7])
	}
	// Invalidate 后同步变体视为未命中并重新拉取
	svc.InvalidateChannelGroupsCache()
	_ = svc.runtime().loadGroupsByChannelBlocking(context.Background(), monitorRoutes(7))
	if n := fake.callCount(7); n != 2 {
		t.Fatalf("blocking variant should refetch after invalidate, got %d calls", n)
	}
}
