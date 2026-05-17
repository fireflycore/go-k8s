package config

import (
	"testing"
	"time"

	microConfig "github.com/fireflycore/go-micro/config"
)

func TestClientCacheHitWhenWatchDisabled(t *testing.T) {
	// 关闭 watch，只保留本地缓存命中行为。
	client := newTestClient(
		microConfig.WithClientCacheEnabled(true),
		microConfig.WithClientWatchMode(microConfig.WatchModeOff),
	)

	// 先手动写入一条缓存，再校验读取能命中同一份快照。
	key := testKey("redis")
	client.putCache(key, &microConfig.Raw{Content: []byte("cached")})

	raw, ok := client.getCache(key)
	if !ok {
		t.Fatal("getCache() = miss, want hit")
	}
	if string(raw.Content) != "cached" {
		t.Fatalf("cached content = %q, want %q", raw.Content, "cached")
	}
}

func TestBuildK8sClientScope(t *testing.T) {
	// 用最小 Store 构造 scope 计算输入。
	store := &StoreInstance{namespace: "default"}
	key := testKey("redis")

	// PerKey 应携带单资源 field selector。
	perKey := buildK8sClientScope(store, microConfig.WatchScopePerKey, key)
	if perKey.key == "" || perKey.fieldSelector == "" {
		t.Fatalf("perKey scope = %+v, want field selector", perKey)
	}

	// Group 在 K8s 中会收敛到 namespace 共享 watch。
	group := buildK8sClientScope(store, microConfig.WatchScopeGroup, key)
	if group.key != "namespace:default" {
		t.Fatalf("group.key = %q", group.key)
	}
	if group.labelSelector != managedLabelSelector {
		t.Fatalf("group.labelSelector = %q", group.labelSelector)
	}

	// App 同样复用 namespace 共享 watch。
	app := buildK8sClientScope(store, microConfig.WatchScopeApp, key)
	if app.key != "namespace:default" {
		t.Fatalf("app.key = %q", app.key)
	}
}

func TestBuildKeySignature(t *testing.T) {
	key := testKey("redis")
	if got := buildKeySignature(key); got != "default|app|prod|database|redis" {
		t.Fatalf("buildKeySignature() = %q", got)
	}
}

func TestClientCacheExpiresAfterTTL(t *testing.T) {
	// 使用极短 TTL，直接依赖真实时钟验证失效行为。
	client := newTestClient(
		microConfig.WithClientCacheEnabled(true),
		microConfig.WithClientCacheTTL(10*time.Millisecond),
		microConfig.WithClientWatchMode(microConfig.WatchModeOff),
	)

	key := testKey("redis")
	client.putCache(key, &microConfig.Raw{Content: []byte("cached")})

	// 等待 TTL 过期后再读取，应该看到 miss。
	time.Sleep(20 * time.Millisecond)

	if _, ok := client.getCache(key); ok {
		t.Fatal("getCache() = hit after ttl, want miss")
	}
}

func newTestClient(opts ...microConfig.ClientOption) *ClientInstance {
	// 先复用正式代码里的默认 ClientOptions，避免测试口径漂移。
	clientOptions := microConfig.NewClientOptions(opts...)
	store := &StoreInstance{
		namespace: "default",
		options:   microConfig.NewOptions(),
	}

	return &ClientInstance{
		store:    store,
		options:  clientOptions,
		cache:    make(map[string]clientCacheEntry),
		watchers: make(map[string]*clientWatchHandle),
	}
}

func testKey(name string) microConfig.Key {
	// testKey 统一返回一条稳定 key，减少各个用例里的重复样板。
	return microConfig.Key{
		Env:   "prod",
		AppId: "app",
		Group: "database",
		Name:  name,
	}
}
