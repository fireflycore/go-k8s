# Invocation

`go-k8s/invocation` 是 K8s 场景下的标准服务调用实现。

它直接对齐最终目标模型：

- `service -> service`
- Service DNS
- `K8s + Istio`
- `go-micro/invocation`

## 包定位

`go-k8s/invocation` 是新的标准实现，适合：

- K8s 集群环境
- Istio service mesh 场景
- 统一 service 级调用目标而不是实例级目标的场景

## 当前提供的能力

### Config

`Config` 用于定义 K8s invocation 的基础配置，例如：

- `Namespace`
- `DefaultPort`
- `ClusterDomain`
- `ResolverScheme`
- `ValidateService`

其中：

- `ValidateService=false` 是默认值
- 默认情况下，定位器只负责标准 Service DNS 构造
- 若开启 `ValidateService`，则会在返回前向 K8s API 校验 Service 是否存在

### Locator

`Locator` 负责把 `ServiceRef` 解析成标准 Service DNS target。

默认目标形态：

```text
dns:///auth.default.svc.cluster.local:9000
```

这条路径与 `go-consul` 的裸机实现不同，它不需要在调用侧做节点级选择。

原因是：

- service -> instance 的流量治理由 K8s + Istio 负责
- 业务侧只需要稳定服务名

### NewConnectionManager

该辅助函数用于把：

- K8s `Locator`
- `go-micro/invocation.ConnectionManager`

组合成统一连接管理入口。

## 设计约束

- K8s 是标准路径，不再模拟旧 registry 的节点模型
- 默认以 Service DNS 为唯一主目标
- 实例选择交给 K8s / Istio
- OTel 观测链路默认继续依赖 `go-micro/invocation.ConnectionManager`

## 当前进度

当前已经完成：

- `Config`
- `Locator`
- `NewConnectionManager`
- 单元测试

当前尚未做的内容：

- 与平台配置进一步对齐的 namespace / env 推导
- 更深的 Authz 接入封装
- 更完整的 README 示例扩展

## 测试

当前包已通过：

- `go test ./...`
- `go vet ./...`
