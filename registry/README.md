# registry

`registry` 包实现了 `go-micro/registry` 在 Kubernetes 场景下的适配逻辑。

## 核心能力

- `NewRegister`：创建注册器
- `(*RegisterInstance).Install`：注册入口（No-op 风格，保持调用面一致）
- `(*RegisterInstance).Uninstall`：注销入口（No-op）
- `NewDiscover`：创建发现器
- `(*DiscoverInstance).Watcher`：启动 informer 监听并构建路由索引
- `(*DiscoverInstance).GetService`：按 method 获取目标节点
- `(*DiscoverInstance).Unwatch`：停止监听

## 配置模型

`ServiceConf` 关键字段：

- `Namespace`：查询资源所属命名空间
- `ResolveMode`：
  - `service_fqdn`：返回 `service.namespace.svc.cluster.local:port`
  - `endpoints`：返回 Endpoints 下的 IP:Port 列表
- `MethodRoutes`：method -> appId（Service 名）映射
- `SyncInterval`：保留字段，当前 informer 模式下主要由事件驱动刷新

## 发现机制

- 启动 `Watcher` 后，会根据 `ResolveMode` 选择监听对象：
  - `service_fqdn` -> Service informer
  - `endpoints` -> Endpoints informer
- `Add/Update/Delete` 事件触发后会刷新两级缓存：
  - `method -> appId`
  - `appId -> []*ServiceNode`

## 网关示例

```go
package main

import (
	"github.com/fireflycore/go-k8s/registry"
	micro "github.com/fireflycore/go-micro/registry"
	"k8s.io/client-go/kubernetes"
)

func useDiscovery(client kubernetes.Interface) {
	dis, err := registry.NewDiscover(client, &micro.Meta{
		Env: "prod",
	}, &registry.ServiceConf{
		Namespace:   "default",
		ResolveMode: registry.ResolveModeServiceFQDN,
		MethodRoutes: map[string]string{
			"/acme.user.v1.UserService/Login": "user-service",
		},
		Network: &micro.Network{},
		Kernel:  &micro.Kernel{},
	})
	if err != nil {
		panic(err)
	}

	go dis.Watcher()
	defer dis.Unwatch()

	nodes, appID, err := dis.GetService("/acme.user.v1.UserService/Login")
	if err != nil {
		panic(err)
	}

	_, _ = appID, nodes
}
```
