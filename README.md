# go-k8s

`go-k8s` 是 firefly 体系中面向 Kubernetes 场景的基础接入库，职责是提供：

- Kubernetes 客户端初始化能力

## 设计定位

- `go-k8s` 不模拟 etcd/consul 的注册中心行为
- 服务实例生命周期由 Kubernetes 自身维护
- 服务发现数据来自 Service/Endpoints 等原生资源
- K8s 场景不再沿用旧 `registry` 语义，服务调用统一围绕 Service DNS 与 mesh 收敛

## 包结构

- `conf.go`：Kubernetes 客户端配置
- `core.go`：创建 `kubernetes.Interface`

## 当前状态

- `registry` 子包已废弃并移除
- K8s 侧不再保留裸机 register/discovery 调用面
- 当前推荐统一使用 `go-micro/invocation`，并结合 Service DNS / Istio 进行服务治理

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

## Istio 场景索引

关于 K8s 与 Istio 的职责分界和推荐实践，应以 `go-micro/invocation` 的 DNS-only 模型为主路径，并把实例发现、流量治理继续下沉到 Service DNS / Istio。
