package registry

import (
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	micro "github.com/fireflycore/go-micro/registry"
)

type DiscoverInstance struct {
	mu sync.RWMutex

	client kubernetes.Interface
	meta   *micro.Meta
	conf   *ServiceConf

	method  micro.ServiceMethod
	service micro.ServiceDiscover

	informer cache.SharedIndexInformer

	stopCh chan struct{}
	once   sync.Once
}

const defaultNodeWeight = 100

func NewDiscover(client kubernetes.Interface, meta *micro.Meta, conf *ServiceConf) (*DiscoverInstance, error) {
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

	ist := &DiscoverInstance{
		client:  client,
		meta:    meta,
		conf:    conf,
		method:  make(micro.ServiceMethod),
		service: make(micro.ServiceDiscover),
		stopCh:  make(chan struct{}),
	}
	ist.informer = ist.buildInformer()
	_, _ = ist.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(interface{}) {
			_ = ist.refresh()
		},
		UpdateFunc: func(interface{}, interface{}) {
			_ = ist.refresh()
		},
		DeleteFunc: func(interface{}) {
			_ = ist.refresh()
		},
	})

	return ist, nil
}

func (s *DiscoverInstance) GetService(method string) ([]*micro.ServiceNode, string, error) {
	s.mu.RLock()
	appID, ok := s.method[method]
	if !ok {
		s.mu.RUnlock()
		return nil, "", micro.ErrServiceMethodNotExists
	}
	nodes, ok := s.service[appID]
	if !ok {
		s.mu.RUnlock()
		return nil, appID, micro.ErrServiceNodeNotExists
	}
	out := append([]*micro.ServiceNode(nil), nodes...)
	s.mu.RUnlock()
	return out, appID, nil
}

func (s *DiscoverInstance) Watcher() {
	go s.informer.Run(s.stopCh)
	if !cache.WaitForCacheSync(s.stopCh, s.informer.HasSynced) {
		return
	}
	_ = s.refresh()
	<-s.stopCh
}

func (s *DiscoverInstance) Unwatch() {
	s.once.Do(func() {
		close(s.stopCh)
	})
}

func (s *DiscoverInstance) refresh() error {
	nextService := make(micro.ServiceDiscover)
	nextMethod := make(micro.ServiceMethod)

	for method, appID := range s.conf.MethodRoutes {
		nodes, err := s.resolveNodes(appID)
		if err != nil || len(nodes) == 0 {
			continue
		}
		nextService[appID] = nodes
		nextMethod[method] = appID
	}

	s.mu.Lock()
	s.service = nextService
	s.method = nextMethod
	s.mu.Unlock()

	return nil
}

func (s *DiscoverInstance) resolveNodes(appID string) ([]*micro.ServiceNode, error) {
	switch s.conf.ResolveMode {
	case ResolveModeEndpoints:
		return s.resolveFromEndpoints(appID)
	default:
		return s.resolveFromServiceFQDN(appID)
	}
}

func (s *DiscoverInstance) resolveFromServiceFQDN(appID string) ([]*micro.ServiceNode, error) {
	raw, ok, err := s.informer.GetStore().GetByKey(s.conf.Namespace + "/" + appID)
	if err != nil || !ok {
		return nil, err
	}
	svc, ok := raw.(*corev1.Service)
	if !ok {
		return nil, nil
	}
	if len(svc.Spec.Ports) == 0 {
		return nil, nil
	}

	host := fmt.Sprintf("%s.%s.svc.cluster.local", appID, s.conf.Namespace)
	internal := fmt.Sprintf("%s:%d", host, svc.Spec.Ports[0].Port)
	node := buildNodeFromService(s.meta, s.conf, internal, appID, svc)
	return []*micro.ServiceNode{node}, nil
}

func (s *DiscoverInstance) resolveFromEndpoints(appID string) ([]*micro.ServiceNode, error) {
	raw, ok, err := s.informer.GetStore().GetByKey(s.conf.Namespace + "/" + appID)
	if err != nil || !ok {
		return nil, err
	}
	ep, ok := raw.(*corev1.Endpoints)
	if !ok {
		return nil, nil
	}

	nodes := make([]*micro.ServiceNode, 0)
	for _, subset := range ep.Subsets {
		if len(subset.Ports) == 0 {
			continue
		}
		port := subset.Ports[0].Port
		for _, addr := range subset.Addresses {
			internal := fmt.Sprintf("%s:%d", addr.IP, port)
			node := buildNodeFromEndpoint(s.meta, s.conf, internal, appID)
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func buildNodeFromService(meta *micro.Meta, conf *ServiceConf, internal, appID string, svc *corev1.Service) *micro.ServiceNode {
	nodeMeta := &micro.Meta{
		Env:     meta.Env,
		AppId:   appID,
		Version: meta.Version,
	}
	node := &micro.ServiceNode{
		Weight:  defaultNodeWeight,
		Methods: map[string]bool{},
		Meta:    nodeMeta,
		Network: &micro.Network{
			SN:       conf.Network.SN,
			Internal: internal,
			External: conf.Network.External,
		},
		Kernel: conf.Kernel,
	}

	if svc.Annotations != nil {
		for k := range conf.MethodRoutes {
			if svc.Annotations["route."+k] == "true" {
				node.Methods[k] = true
			}
		}
	}
	return node
}

func buildNodeFromEndpoint(meta *micro.Meta, conf *ServiceConf, internal, appID string) *micro.ServiceNode {
	return &micro.ServiceNode{
		Weight: defaultNodeWeight,
		Meta: &micro.Meta{
			Env:     meta.Env,
			AppId:   appID,
			Version: meta.Version,
		},
		Methods: map[string]bool{},
		Network: &micro.Network{
			SN:       conf.Network.SN,
			Internal: internal,
			External: conf.Network.External,
		},
		Kernel: conf.Kernel,
	}
}

func (s *DiscoverInstance) buildInformer() cache.SharedIndexInformer {
	factory := informers.NewSharedInformerFactoryWithOptions(
		s.client,
		0,
		informers.WithNamespace(s.conf.Namespace),
	)
	if s.conf.ResolveMode == ResolveModeEndpoints {
		return factory.Core().V1().Endpoints().Informer()
	}
	return factory.Core().V1().Services().Informer()
}
