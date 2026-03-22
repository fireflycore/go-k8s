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

// DiscoverInstance 封装 K8s 场景下的服务发现逻辑。
type DiscoverInstance struct {
	// mu 保护 method/service 索引的一致性。
	mu sync.RWMutex

	// client 是 Kubernetes 客户端。
	client kubernetes.Interface
	// meta 是环境元数据模板。
	meta   *micro.Meta
	// conf 是发现配置。
	conf   *ServiceConf

	// method 保存 method -> appId 映射。
	method  micro.ServiceMethod
	// service 保存 appId -> nodes 映射。
	service micro.ServiceDiscover

	// informer 监听 Service 或 Endpoints 变更。
	informer cache.SharedIndexInformer

	// stopCh 用于停止 Watcher。
	stopCh chan struct{}
	// once 确保 stopCh 只关闭一次。
	once   sync.Once

	// watchEventCallback 用于向外透传服务变更事件。
	watchEventCallback micro.WatchEventFunc
}

const defaultNodeWeight = 100

// NewDiscover 创建 K8s 发现器实例并返回统一发现接口。
func NewDiscover(client kubernetes.Interface, meta *micro.Meta, conf *ServiceConf) (micro.Discovery, error) {
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

// GetService 根据 method 获取服务节点列表。
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

// Watcher 启动 informer 并在变更时刷新本地索引。
func (s *DiscoverInstance) Watcher() {
	go s.informer.Run(s.stopCh)
	if !cache.WaitForCacheSync(s.stopCh, s.informer.HasSynced) {
		return
	}
	_ = s.refresh()
	<-s.stopCh
}

// Unwatch 停止发现监听流程。
func (s *DiscoverInstance) Unwatch() {
	s.once.Do(func() {
		close(s.stopCh)
	})
}

// WatchEvent 注册服务变更回调。
func (s *DiscoverInstance) WatchEvent(callback micro.WatchEventFunc) {
	s.watchEventCallback = callback
}

// refresh 基于 MethodRoutes 全量重建发现索引。
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
	before := s.service
	events := buildDiscoveryEvents(before, nextService)
	s.service = nextService
	s.method = nextMethod
	s.mu.Unlock()

	s.dispatchEvents(events)

	return nil
}

// resolveNodes 按配置模式解析目标服务节点。
func (s *DiscoverInstance) resolveNodes(appID string) ([]*micro.ServiceNode, error) {
	switch s.conf.ResolveMode {
	case ResolveModeEndpoints:
		return s.resolveFromEndpoints(appID)
	default:
		return s.resolveFromServiceFQDN(appID)
	}
}

// resolveFromServiceFQDN 以 service FQDN 形式返回目标节点。
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

// resolveFromEndpoints 从 Endpoints 资源解析实例节点列表。
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

// buildNodeFromService 基于 Service 构造一个逻辑节点。
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

// buildNodeFromEndpoint 基于 Endpoints 地址构造一个逻辑节点。
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

// buildInformer 根据解析模式创建对应的资源 informer。
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

func (s *DiscoverInstance) dispatchEvents(events []micro.ServiceEvent) {
	if s.watchEventCallback == nil || len(events) == 0 {
		return
	}
	for _, event := range events {
		item := event
		go s.watchEventCallback(&item)
	}
}

func buildDiscoveryEvents(before, after micro.ServiceDiscover) []micro.ServiceEvent {
	events := make([]micro.ServiceEvent, 0, len(before)+len(after))
	for appID, nodes := range after {
		prev, ok := before[appID]
		if !ok {
			for _, node := range nodes {
				events = append(events, micro.ServiceEvent{Type: micro.EventAdd, Service: node})
			}
			continue
		}
		if !sameDiscoveryNodes(prev, nodes) {
			for _, node := range nodes {
				events = append(events, micro.ServiceEvent{Type: micro.EventUpdate, Service: node})
			}
		}
	}

	for appID, nodes := range before {
		if _, ok := after[appID]; ok {
			continue
		}
		for _, node := range nodes {
			events = append(events, micro.ServiceEvent{Type: micro.EventDelete, Service: node})
		}
	}
	return events
}

func sameDiscoveryNodes(left, right []*micro.ServiceNode) bool {
	if len(left) != len(right) {
		return false
	}
	lm := make(map[string]struct{}, len(left))
	for _, item := range left {
		if item == nil || item.Meta == nil || item.Network == nil {
			continue
		}
		lm[item.Meta.AppId+"|"+item.Meta.InstanceId+"|"+item.Network.Internal] = struct{}{}
	}
	for _, item := range right {
		if item == nil || item.Meta == nil || item.Network == nil {
			continue
		}
		if _, ok := lm[item.Meta.AppId+"|"+item.Meta.InstanceId+"|"+item.Network.Internal]; !ok {
			return false
		}
	}
	return true
}
