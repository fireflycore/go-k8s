# go-k8s

`go-k8s` 是 firefly 体系中面向 Kubernetes 场景的基础接入库，职责是提供：

- Kubernetes 客户端初始化能力
- 基于 K8s 原生资源的注册中心适配能力（`registry` 子包）
- 基于 Service DNS 的标准服务调用实现（`invocation` 子包）

## 设计定位

- `go-k8s` 不模拟 etcd/consul 的注册中心行为
- 服务实例生命周期由 Kubernetes 自身维护
- 服务发现数据来自 Service/Endpoints 等原生资源
- 网关通过 `registry.Discovery` 获取 method 对应的目标服务

## 包结构

- `conf.go`：Kubernetes 客户端配置
- `core.go`：创建 `kubernetes.Interface`
- `registry/`：`Register` 与 `Discovery` 的 K8s 实现
- `invocation/`：面向 `service -> service` 的标准调用实现

## 快速开始

```go
package main

import (
	k8sx "github.com/fireflycore/go-k8s"
)

func main() {
	client, err := k8sx.New(&k8sx.Conf{
		InCluster:  true,
		MasterURL:  "",
		KubeConfig: "",
	})
	if err != nil {
		panic(err)
	}

	_ = client
}
```

## registry 子包

`registry` 子包提供：

- `Register`：K8s 场景下的最小实现（No-op 风格）
- `Discovery`：基于 informer 监听 Service/Endpoints 的网关发现

详细说明见：

- [registry/README.md](file:///Users/lhdht/product/firefly/go-k8s/registry/README.md)
- [invocation/README.md](./invocation/README.md)

## Istio 场景索引

关于 K8s 与 Istio 的职责分界和推荐实践，见：

- [registry/README.md - Istio 场景建议](file:///Users/lhdht/product/firefly/go-k8s/registry/README.md#L73-L93)
- `invocation` 作为新的标准调用模型，会逐步成为主路径
