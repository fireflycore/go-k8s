package registry

import (
	"fmt"

	micro "github.com/fireflycore/go-micro/registry"
	"k8s.io/client-go/kubernetes"
)

type RegisterInstance struct {
	client kubernetes.Interface
	meta   *micro.Meta
	conf   *ServiceConf
}

func NewRegister(client kubernetes.Interface, meta *micro.Meta, conf *ServiceConf) (*RegisterInstance, error) {
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

func (s *RegisterInstance) Install(service *micro.ServiceNode) error {
	if service == nil {
		return micro.ErrServiceNodeIsNil
	}
	if service.Meta == nil {
		service.Meta = s.meta
	}
	return nil
}

func (s *RegisterInstance) Uninstall() error {
	return nil
}
