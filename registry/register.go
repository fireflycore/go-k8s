package registry

import (
	"fmt"

	micro "github.com/fireflycore/go-micro/registry"
	"k8s.io/client-go/kubernetes"
)

// RegisterInstance 封装 K8s 场景下的注册调用面。
type RegisterInstance struct {
	// client 是 Kubernetes 客户端。
	client kubernetes.Interface
	// meta 是服务元数据模板。
	meta   *micro.Meta
	// conf 是注册配置。
	conf   *ServiceConf
}

// NewRegister 创建 K8s 注册器实例并返回统一注册接口。
func NewRegister(client kubernetes.Interface, meta *micro.Meta, conf *ServiceConf) (micro.Register, error) {
	if client == nil {
		return nil, fmt.Errorf(micro.ErrClientIsNilFormat, "k8s")
	}
	if meta == nil {
		return nil, micro.ErrServiceMetaIsNil
	}
	if conf == nil {
		return nil, micro.ErrServiceConfIsNil
	}
	conf.Bootstrap()

	return &RegisterInstance{
		client: client,
		meta:   meta,
		conf:   conf,
	}, nil
}

// Install 在 K8s 场景下保持 No-op 行为，仅补齐必要元数据。
func (s *RegisterInstance) Install(service *micro.ServiceNode) error {
	if service == nil {
		return micro.ErrServiceNodeIsNil
	}
	if service.Meta == nil {
		service.Meta = s.meta
	}
	return nil
}

// Uninstall 在 K8s 场景下保持 No-op 行为。
func (s *RegisterInstance) Uninstall() error {
	return nil
}
