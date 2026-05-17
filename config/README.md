# config

`go-k8s/config` 是 `go-micro/config` 的 Kubernetes 实现，使用 ConfigMap 提供最小数据面配置存储与监听能力。

> 当前主线口径：在配置中心主线交付中，`go-k8s/config` 对应 `K8s + Istio` 场景。它与 `go-consul/config` 共享统一契约，但不是要求同一个运行时产物同时引入两套实现。
>
> 当前版本口径：本包已对齐 `github.com/fireflycore/go-micro@v1.5.5`，已补齐 `Client` 实现；`Store` 保留 `Get / Put / Delete`，`Client` 负责聚合 cache 与共享 watch。
>
> 后续 cache / watch / `manage/client` 重构，统一以设计库 `design/config/plan/go-micro-config-manage-client-refactor-plan.md` 为基线。

## 能力范围

- `Store`：`Get/Put/Delete`
- `Watcher`：`Watch/Unwatch`（基于 ConfigMap Watch）
- `Store` 构造：`NewStore`
- `Client` 构造：`NewClient`
- `Options` 透传：`Config.BuildOptions`

## 存储模型

同一条配置键会映射到一个 current ConfigMap：

- current：保存当前生效配置（`data.raw`）

ConfigMap 名称采用稳定哈希生成，避免超过 K8s 资源名限制。

> 历史版本、发布流水和元信息游标不再保存在 ConfigMap 中，由控制面数据库统一承接。

## 接入方式

调用方可以按场景选择两种接入方式：

- `NewStore`：基于 Kubernetes 客户端创建数据面 `Store`
- `NewClient`：在 `Store` 之上聚合本地 cache 与共享 watch
- `Config.BuildOptions`：把 K8s 侧配置映射到统一 `microcfg.Options`

推荐：

- 需要统一 cache / watch 行为时，优先使用 `NewClient`
- 只需要最小直连读写能力时，继续使用 `Store`

## Watch 与协程数量

`go-k8s/config` 里的后台 watch 也是按 `WatchScope` 去重启动的，但和 Consul 不同，当前实现会把 `Group` 与 `App` 两种 scope 都收敛到 namespace 级共享 watch。

前提：

- 只有 `EnableCache=true`
- 且 `WatchMode=On`
- 且对应 key 至少被 `Client.Get(...)` 成功读取过一次

满足上面条件后，后台才会为该 key 所属 scope 挂 watch。

当前规则：

- `WatchScopePerKey`：一个 key 起 `1` 个协程
- `WatchScopeGroup`：同一个 namespace 下，不管有多少个 key，只起 `1` 个协程
- `WatchScopeApp`：同一个 namespace 下，不管有多少个 key，也只起 `1` 个协程

例子：

- 同一个 namespace 下 100 个 key，默认不会起 100 个，当前只起 `1` 个协程
- 如果显式使用 `WatchScopePerKey`，100 个 key 就会起 `100` 个协程
- 如果这些 key 还没被 `Client.Get(...)` 读过，则不会提前创建对应 watch

说明：

- 当前默认 `WatchScope` 来自 `go-micro/config`，默认值是 `WatchScopeGroup`
- 在 `go-k8s/config` 里，`WatchScopeGroup` 和 `WatchScopeApp` 当前都会复用 namespace 级共享 watch
- 当前实现还没有额外的 `Close` 生命周期接口，因此 watch goroutine 一旦启动，会跟随进程生命周期持续存在

## 加密语义

- `go-k8s/config` 遵循 `go-micro/config` 的统一加密语义。
- `microcfg.Raw.Encrypted=false` 时，读取方执行 `Base64 解码 -> 解压 -> 反序列化`。
- `microcfg.Raw.Encrypted=true` 时，读取方执行 `Base64 解码 -> 解密 -> 解压 -> 反序列化`。
- 不做字段级加密；如果只有部分内容需要保护，应拆成独立配置项。

## 快速开始

```go
package main

import (
	"context"

	k8sx "github.com/fireflycore/go-k8s"
	k8scfg "github.com/fireflycore/go-k8s/config"
	microcfg "github.com/fireflycore/go-micro/config"
)

func main() {
	client, err := k8sx.New(&k8sx.Conf{
		InCluster: true,
	})
	if err != nil {
		panic(err)
	}

	store, err := k8scfg.NewStore(client, &k8scfg.Config{
		Namespace: "default",
	})
	if err != nil {
		panic(err)
	}

	key := microcfg.Key{
		TenantId: "t1",
		Env:      "prod",
		AppId:    "order-service",
		Group:    "db",
		Name:     "primary",
	}

	_ = store.Put(context.Background(), key, &microcfg.Raw{
		Version: "v1",
		Content: []byte(`{"dsn":"root:root@tcp(mysql:3306)/order"}`),
	})

	clientConfig, err := k8scfg.NewClient(
		store,
		microcfg.WithClientCacheEnabled(true),
		microcfg.WithClientWatchMode(microcfg.WatchModeOn),
	)
	if err != nil {
		panic(err)
	}

	_, _ = clientConfig.Get(context.Background(), key)
}
```
