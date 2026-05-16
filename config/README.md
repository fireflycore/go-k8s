# config

`go-k8s/config` 是 `go-micro/config` 的 Kubernetes 实现，使用 ConfigMap 提供最小数据面配置存储与监听能力。

> 当前主线口径：在配置中心主线交付中，`go-k8s/config` 对应 `K8s + Istio` 场景。它与 `go-consul/config` 共享统一契约，但不是要求同一个运行时产物同时引入两套实现。
>
> 当前版本口径：本包已对齐 `github.com/fireflycore/go-micro@v1.5.4`，`Store` 只保留 `Get / Put / Delete`，监听能力由独立 `Watcher` 接口承载。

## 能力范围

- `Store`：`Get/Put/Delete`
- `Watcher`：`Watch/Unwatch`（基于 ConfigMap Watch）
- `Store` 构造：`NewStore`
- `Options` 透传：`Config.BuildOptions`

## 存储模型

同一条配置键会映射到一个 current ConfigMap：

- current：保存当前生效配置（`data.raw`）

ConfigMap 名称采用稳定哈希生成，避免超过 K8s 资源名限制。

> 历史版本、发布流水和元信息游标不再保存在 ConfigMap 中，由控制面数据库统一承接。

## 接入方式

调用方直接创建 `Store`，再配合 `go-micro/config` 的 `StoreParams` / `LoadStoreConfig` 读取业务配置：

- `NewStore`：基于 Kubernetes 客户端创建数据面 `Store`
- `Config.BuildOptions`：把 K8s 侧配置映射到统一 `microcfg.Options`

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

	_, _ = store.Get(context.Background(), key)
}
```
