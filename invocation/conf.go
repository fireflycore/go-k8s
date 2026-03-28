package invocation

import microInvocation "github.com/fireflycore/go-micro/invocation"

const (
	// DefaultNamespace 是 K8s invocation 标准实现的默认命名空间。
	DefaultNamespace = "default"
)

// Conf 定义 K8s invocation 标准实现的配置。
type Conf struct {
	// Namespace 是默认命名空间。
	Namespace string `json:"namespace"`
	// DefaultPort 是默认 gRPC 端口。
	DefaultPort uint16 `json:"default_port"`
	// ClusterDomain 是 Service DNS 所使用的集群域。
	ClusterDomain string `json:"cluster_domain"`
	// ResolverScheme 是最终 gRPC target 使用的 resolver scheme。
	ResolverScheme string `json:"resolver_scheme"`
	// ValidateService 控制 Resolve 时是否主动向 K8s API 校验 Service 是否存在。
	//
	// 默认关闭，原因是：
	// - 标准路径下直接构造 Service DNS 通常已经足够；
	// - 避免把每次调用都绑定到一次 API Server 请求上。
	ValidateService bool `json:"validate_service"`
}

// Bootstrap 补齐默认值。
func (c *Conf) Bootstrap() {
	if c.Namespace == "" {
		c.Namespace = DefaultNamespace
	}
	if c.ClusterDomain == "" {
		c.ClusterDomain = microInvocation.DefaultClusterDomain
	}
	if c.ResolverScheme == "" {
		c.ResolverScheme = microInvocation.DefaultResolverScheme
	}
}
