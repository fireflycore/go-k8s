# config

`go-k8s/config` 是 `go-micro/config` 的 Kubernetes 实现，使用 ConfigMap 提供统一的配置存储与监听能力。

## 能力范围

- `Store`：`Get/GetByQuery/Put/Delete`
- 版本能力：`PutVersion/GetVersion/ListVersions`
- 元信息能力：`GetMeta/PutMeta`
- `Watcher`：`Watch/Unwatch`（基于 ConfigMap Watch）
- `loader` 辅助：`NewStoreFromLoader`、`LoadConfigFromStore`

## 存储模型

同一条配置键会映射到三个 ConfigMap：

- current：保存当前生效配置（`data.item`）
- versions：保存历史版本快照（`data.{version}`）
- meta：保存版本游标元信息（`data.meta`）

ConfigMap 名称采用稳定哈希生成，避免超过 K8s 资源名限制。

## Loader 辅助

当调用方已经接入 `go-micro/config` 的 `LoaderParams` / `StoreParams` 体系时，可以直接使用：

- `NewStoreFromLoader`：先按 local / remote 规则加载 `k8s.Conf`，再创建 `Store`
- `LoadConfigFromStore`：从 `Store` 读取配置并解码为目标类型

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

	store, err := k8scfg.NewStore(client, &k8scfg.Conf{
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

	_ = store.Put(context.Background(), key, &microcfg.Item{
		Version: "v1",
		Content: []byte(`{"dsn":"root:root@tcp(mysql:3306)/order"}`),
	})

	_, _ = store.Get(context.Background(), key)
}
```
