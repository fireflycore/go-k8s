package registry

import micro "github.com/fireflycore/go-micro/registry"

const (
	ResolveModeServiceFQDN = "service_fqdn"
	ResolveModeEndpoints   = "endpoints"
)

type ServiceConf struct {
	Namespace string `json:"namespace"`

	ResolveMode  string            `json:"resolve_mode"`
	MethodRoutes map[string]string `json:"method_routes"`
	SyncInterval uint32            `json:"sync_interval"`

	Network *micro.Network `json:"network"`
	Kernel  *micro.Kernel  `json:"kernel"`
}

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
		c.Kernel = &micro.Kernel{}
	}
	c.Kernel.Bootstrap()
}

type GatewayConf struct {
	Namespace string `json:"namespace"`
}

func (c *GatewayConf) Bootstrap() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}
}
