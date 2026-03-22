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
- `(*DiscoverInstance).WatchEvent`：订阅服务变更回调

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
	dis.WatchEvent(func(event *micro.ServiceEvent) {
		_ = event
	})

	nodes, appID, err := dis.GetService("/acme.user.v1.UserService/Login")
	if err != nil {
		panic(err)
	}

	_, _ = appID, nodes
}
```

## Istio 场景建议

在启用 Istio 后，建议按下面边界划分职责：

- `go-k8s/registry` 负责：
  - method -> service 的目标解析
  - 基于 K8s Service/Endpoints 的基础可达信息维护
- Istio 负责：
  - 灰度发布（权重、版本、Header 路由）
  - 故障转移、重试、超时、熔断等流量治理
  - mTLS 与服务间安全策略
- 网关负责：
  - 鉴权、审计、入口限流
  - 调用 `GetService` 获取目标服务，再转发到集群服务域名

推荐实践：

- `ResolveMode` 优先使用 `service_fqdn`，让负载策略尽量下沉给 Service + Istio
- `MethodRoutes` 只维护业务路由归属，不维护实例级策略
- 版本分流尽量通过 Istio `VirtualService/DestinationRule` 管理，不在网关重复实现
