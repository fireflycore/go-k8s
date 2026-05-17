package config

import (
	"context"
	"sync"
	"time"

	microConfig "github.com/fireflycore/go-micro/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	k8sWatch "k8s.io/apimachinery/pkg/watch"
)

const managedLabelSelector = "managed-by=go-k8s-config,type=current"

// ClientInstance 是 go-k8s/config 的统一配置客户端实现。
// 它在 Store 之上聚合本地缓存与共享 watch，对业务只暴露 Get。
type ClientInstance struct {
	// store 保存底层 K8s Store，所有远端读取最终都回落到这里。
	store *StoreInstance
	// options 保存 Client 的运行参数，例如 cache、watch 和超时开关。
	options *microConfig.ClientOptions

	// cacheMu 保护 cache 和 watchers 两张运行时状态表的并发访问。
	cacheMu sync.RWMutex
	// cache 保存当前进程内的配置快照。
	cache map[string]clientCacheEntry
	// watchers 以共享 scope 为键，保证同一 scope 只启动一条 watch 链路。
	watchers map[string]*clientWatchHandle
}

// clientCacheEntry 表示单条缓存记录。
type clientCacheEntry struct {
	// raw 保存配置快照副本，避免调用方改写内部缓存。
	raw *microConfig.Raw
	// expiresAt 只在 watch 关闭时生效；零值表示这条缓存由 watch 持续刷新。
	expiresAt time.Time
}

// clientScope 描述一条共享 watch 的聚合范围。
type clientScope struct {
	// key 用作内部 map 的唯一标识。
	key string
	// fieldSelector 用于 per-key 模式时只订阅单个 ConfigMap。
	fieldSelector string
	// labelSelector 用于 namespace 共享监听时过滤本库托管资源。
	labelSelector string
}

// clientWatchHandle 保存某个共享 watch 的运行时状态。
type clientWatchHandle struct {
	// client 回指上层实例，便于 watch 线程刷新缓存。
	client *ClientInstance
	// scope 描述当前 handle 负责的监听范围。
	scope clientScope

	// stateMu 保护 keys 的并发读写。
	stateMu sync.RWMutex
	// keys 保存当前 scope 下已经被 Get 触达过的具体配置键。
	keys map[string]microConfig.Key
}

// NewClient 基于 Store 构造统一配置客户端。
func NewClient(store *StoreInstance, opts ...microConfig.ClientOption) (*ClientInstance, error) {
	// 没有底层 Store 时无法读取远端配置，直接返回统一错误。
	if store == nil {
		return nil, microConfig.ErrStoreIsNil
	}

	// 先收敛 Client 运行参数，再初始化运行时状态。
	raw := microConfig.NewClientOptions(opts...)
	client := &ClientInstance{
		store:    store,
		options:  raw,
		cache:    make(map[string]clientCacheEntry),
		watchers: make(map[string]*clientWatchHandle),
	}
	return client, nil
}

// Get 按配置键读取当前可用配置。
func (c *ClientInstance) Get(ctx context.Context, key microConfig.Key) (*microConfig.Raw, error) {
	// 先复用 Store 的 key 校验规则，避免缓存和远端读取出现口径不一致。
	if err := validateKey(key); err != nil {
		return nil, err
	}

	// 开启缓存时优先命中本地快照，减少对 Kubernetes API 的直接读取压力。
	if c.options.EnableCache {
		if raw, ok := c.getCache(key); ok {
			return raw, nil
		}
	}

	loadCtx, cancel := c.withTimeout(ctx)
	defer cancel()

	// 缓存 miss 后回落到底层 Store.Get。
	raw, err := c.store.Get(loadCtx, key)
	if err != nil {
		return nil, err
	}

	// 远端读取成功后再回填缓存，并按需挂载共享 watch。
	if c.options.EnableCache {
		c.putCache(key, raw)
		if c.options.WatchMode == microConfig.WatchModeOn {
			c.ensureWatch(key)
		}
	}

	return cloneRaw(raw), nil
}

// withTimeout 为单次底层读取派生超时上下文，避免慢请求长期阻塞 Get。
func (c *ClientInstance) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	// 调用方未传 ctx 时回退到 Background，保持接口可直接调用。
	if ctx == nil {
		ctx = context.Background()
	}
	// 未配置超时时只返回可取消上下文，避免额外强制截止时间。
	if c.options == nil || c.options.Timeout <= 0 {
		return context.WithCancel(ctx)
	}
	// 配置了超时时，统一在这里收敛读取上界。
	return context.WithTimeout(ctx, c.options.Timeout)
}

// getCache 尝试命中本地缓存，并顺手清理已过期记录。
func (c *ClientInstance) getCache(key microConfig.Key) (*microConfig.Raw, bool) {
	// 同一 key 在 cache、watch 和删除路径里都必须落到同一缓存键。
	cacheKey := c.store.currentName(key)

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// 未命中时直接返回 false，让调用方继续走远端读取。
	entry, ok := c.cache[cacheKey]
	if !ok {
		return nil, false
	}
	// 关闭 watch 时由 TTL 驱动失效；过期后立即删除，避免脏命中。
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.cache, cacheKey)
		return nil, false
	}
	// 命中时返回副本，避免外部修改内部缓存对象。
	return cloneRaw(entry.raw), true
}

// putCache 写入或更新一条缓存记录。
func (c *ClientInstance) putCache(key microConfig.Key, raw *microConfig.Raw) {
	// 关闭缓存或远端返回为空时无需落本地状态。
	if !c.options.EnableCache || raw == nil {
		return
	}

	cacheKey := c.store.currentName(key)
	// 写入缓存前先复制一份，避免共享底层切片和 map。
	entry := clientCacheEntry{raw: cloneRaw(raw)}
	// watch 关闭时，缓存只能依赖 TTL 失效，因此需要写过期时间。
	if c.options.WatchMode != microConfig.WatchModeOn {
		entry.expiresAt = time.Now().Add(c.options.CacheTTL)
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	// 每次写入前先清掉过期项，避免长时间累积无效数据。
	c.evictExpiredLocked()
	// 新键进入缓存前再检查容量上限，保持条目数受控。
	if _, ok := c.cache[cacheKey]; !ok {
		c.evictOverflowLocked()
	}
	// 最终把新快照写回缓存表。
	c.cache[cacheKey] = entry
}

// deleteCacheByName 在 watch 观察到删除事件时驱逐缓存。
func (c *ClientInstance) deleteCacheByName(name string) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	delete(c.cache, name)
}

// evictExpiredLocked 清理所有已过期的 TTL 缓存项。
func (c *ClientInstance) evictExpiredLocked() {
	now := time.Now()
	for key, entry := range c.cache {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			delete(c.cache, key)
		}
	}
}

// evictOverflowLocked 在容量超限时驱逐一条记录，避免缓存无限增长。
func (c *ClientInstance) evictOverflowLocked() {
	// 未设置上限，或当前仍未超量时无需驱逐。
	if c.options.CacheMaxEntries <= 0 || len(c.cache) < c.options.CacheMaxEntries {
		return
	}
	// 当前实现采用最简单的随机 map 首项淘汰；后续如有需要可再替换为 LRU。
	for key := range c.cache {
		delete(c.cache, key)
		return
	}
}

// ensureWatch 保证当前 key 所属 scope 已经挂上一条共享 watch。
func (c *ClientInstance) ensureWatch(key microConfig.Key) {
	// 先计算共享 scope，再取出具体缓存键，后续要同时登记。
	scope := buildK8sClientScope(c.store, c.options.WatchScope, key)
	cacheKey := c.store.currentName(key)

	c.cacheMu.Lock()
	handle, ok := c.watchers[scope.key]
	if !ok {
		// 第一次命中该 scope 时创建 handle，并在解锁后启动后台 watch。
		handle = &clientWatchHandle{
			client: c,
			scope:  scope,
			keys:   map[string]microConfig.Key{cacheKey: key},
		}
		c.watchers[scope.key] = handle
		c.cacheMu.Unlock()
		c.startWatch(handle)
		return
	}
	c.cacheMu.Unlock()

	// 已有共享 watch 时，只把具体 key 挂到该 handle 管理即可。
	handle.stateMu.Lock()
	handle.keys[cacheKey] = key
	handle.stateMu.Unlock()
}

// buildK8sClientScope 把抽象 WatchScope 映射为 K8s 侧的共享监听范围。
func buildK8sClientScope(store *StoreInstance, scope microConfig.WatchScope, key microConfig.Key) clientScope {
	if scope == microConfig.WatchScopePerKey {
		// PerKey 模式只监听当前配置对应的 ConfigMap 资源名。
		name := store.currentName(key)
		return clientScope{
			key:           "key:" + name,
			fieldSelector: fields.OneTermEqualSelector("metadata.name", name).String(),
		}
	}

	// Group 和 App 在 K8s 侧都收敛为 namespace 级共享 watch。
	return clientScope{
		key:           "namespace:" + store.namespace,
		labelSelector: managedLabelSelector,
	}
}

// startWatch 启动后台共享 watch，并把 ConfigMap 事件应用回本地缓存。
func (c *ClientInstance) startWatch(handle *clientWatchHandle) {
	go func() {
		for {
			// 根据 scope 选择 fieldSelector 或 labelSelector，减少无关事件噪音。
			listOptions := metav1.ListOptions{
				FieldSelector: handle.scope.fieldSelector,
				LabelSelector: handle.scope.labelSelector,
			}
			// 创建底层 watch stream；失败时短暂退避再重试。
			watcher, err := c.store.client.CoreV1().ConfigMaps(c.store.namespace).Watch(context.Background(), listOptions)
			if err != nil {
				time.Sleep(300 * time.Millisecond)
				continue
			}

			// stream 断开后继续重建；当前阶段没有额外的 Close 生命周期。
			shouldExit := c.consumeWatchStream(handle, watcher)
			if shouldExit {
				return
			}
		}
	}()
}

// consumeWatchStream 消费单条 K8s watch stream。
func (c *ClientInstance) consumeWatchStream(handle *clientWatchHandle, watcher k8sWatch.Interface) bool {
	// stream 结束时关闭 watcher，避免资源泄漏。
	defer watcher.Stop()

	for {
		// ResultChan 关闭时返回 false，交由上层重新建立 watch。
		event, ok := <-watcher.ResultChan()
		if !ok {
			return false
		}
		// 把单个 ConfigMap 事件应用到缓存。
		c.applyWatchEvent(handle, event)
	}
}

// applyWatchEvent 把单个 ConfigMap 事件翻译成缓存刷新动作。
func (c *ClientInstance) applyWatchEvent(handle *clientWatchHandle, event k8sWatch.Event) {
	// 非 ConfigMap 事件直接忽略，避免异常对象污染缓存。
	cm, ok := event.Object.(*corev1.ConfigMap)
	if !ok || cm == nil {
		return
	}

	// 只处理当前 handle 已经登记过的 key，避免 namespace 共享 watch 误伤其他配置。
	handle.stateMu.RLock()
	key, watched := handle.keys[cm.Name]
	handle.stateMu.RUnlock()
	if !watched {
		return
	}

	// 删除事件直接驱逐缓存，等待后续 Get 再回源。
	if event.Type == k8sWatch.Deleted {
		c.deleteCacheByName(cm.Name)
		return
	}

	// 非删除事件需要从固定 data.raw 键提取统一编码后的内容。
	if cm.Data == nil {
		return
	}
	rawValue, ok := cm.Data[dataKeyRaw]
	if !ok || rawValue == "" {
		return
	}

	// 解码失败时跳过该条，避免单个坏数据中断整个 watch stream。
	raw, err := c.store.decodeRaw([]byte(rawValue))
	if err != nil {
		return
	}
	// 解码成功后把最新快照覆盖进缓存。
	c.putCache(key, raw)
}

// cloneRaw 返回 Raw 的深拷贝，确保缓存与调用方之间没有共享可变状态。
func cloneRaw(raw *microConfig.Raw) *microConfig.Raw {
	if raw == nil {
		return nil
	}

	// 先复制结构体值，再分别复制引用字段。
	dst := *raw
	if raw.Content != nil {
		dst.Content = append([]byte(nil), raw.Content...)
	}
	if raw.Meta != nil {
		dst.Meta = make(map[string]string, len(raw.Meta))
		for key, value := range raw.Meta {
			dst.Meta[key] = value
		}
	}
	return &dst
}
