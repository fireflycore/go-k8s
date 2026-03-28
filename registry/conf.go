package registry

import micro "github.com/fireflycore/go-micro/registry"

const (
	// ResolveModeServiceFQDN 表示返回 service FQDN 目标。
	ResolveModeServiceFQDN = "service_fqdn"
	// ResolveModeEndpoints 表示返回 endpoints 实例目标。
	ResolveModeEndpoints = "endpoints"
)

// ServiceConf 定义 K8s 发现配置。
type ServiceConf struct {
	// Namespace 是资源所在命名空间。
	Namespace string `json:"namespace"`

	// ResolveMode 控制节点解析模式。
	ResolveMode string `json:"resolve_mode"`
	// MethodRoutes 保存 method 到 service 的映射。
	MethodRoutes map[string]string `json:"method_routes"`
	// SyncInterval 是兼容字段，保留用于后续轮询场景。
	SyncInterval uint32 `json:"sync_interval"`

	// Network 保存网络元信息模板。
	Network *micro.Network `json:"network"`
	// Kernel 保存运行时元信息模板。
	Kernel *micro.ServiceKernel `json:"kernel"`
}

// Bootstrap 补齐 K8s 发现配置默认值。
func (c *ServiceConf) Bootstrap() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
	if c.ResolveMode == "" {
		c.ResolveMode = ResolveModeServiceFQDN
	}
	if c.SyncInterval == 0 {
		c.SyncInterval = 5
	}
	if c.MethodRoutes == nil {
		c.MethodRoutes = map[string]string{}
	}
	if c.Network == nil {
		c.Network = &micro.Network{}
	}
	c.Network.Bootstrap()
	if c.Kernel == nil {
		c.Kernel = &micro.ServiceKernel{}
	}
	c.Kernel.Bootstrap()
}

// GatewayConf 定义网关 K8s 发现配置。
type GatewayConf struct {
	// Namespace 是网关查询服务的命名空间。
	Namespace string `json:"namespace"`
}

// Bootstrap 补齐网关配置默认值。
func (c *GatewayConf) Bootstrap() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
}
