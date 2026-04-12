package invocation

import microInvocation "github.com/fireflycore/go-micro/invocation"

const (
	// DefaultNamespace 是 K8s invocation 标准实现的默认命名空间。
	DefaultNamespace = "default"
)

// Conf 定义 K8s invocation 标准实现的配置。
type Config struct {
	// Namespace 是默认命名空间。
	Namespace string `json:"namespace"`
	// TargetOptions 表示通用 target 构造选项。
	TargetOptions microInvocation.TargetOptions `json:"target_options"`
	// ValidateService 控制 Resolve 时是否主动向 K8s API 校验 Service 是否存在。
	//
	// 默认关闭，原因是：
	// - 标准路径下直接构造 Service DNS 通常已经足够；
	// - 避免把每次调用都绑定到一次 API Server 请求上。
	ValidateService bool `json:"validate_service"`
}

// Bootstrap 补齐默认值。
func (c *Config) Bootstrap() {
	if c.Namespace == "" {
		c.Namespace = DefaultNamespace
	}
	if c.TargetOptions.ClusterDomain == "" {
		c.TargetOptions.ClusterDomain = microInvocation.DefaultClusterDomain
	}
	if c.TargetOptions.ResolverScheme == "" {
		c.TargetOptions.ResolverScheme = microInvocation.DefaultResolverScheme
	}
}
