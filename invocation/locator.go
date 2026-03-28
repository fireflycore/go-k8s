package invocation

import (
	"context"
	"fmt"
	"strings"

	microInvocation "github.com/fireflycore/go-micro/invocation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// serviceReader 抽象 K8s Service 查询能力，便于注入测试替身。
type serviceReader interface {
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Service, error)
}

// Locator 是 K8s invocation 的标准定位器。
//
// 它的理念非常明确：
// - 默认直接基于 service + namespace 构造标准 DNS target；
// - 若开启 ValidateService，则在返回前额外向 K8s API 校验 Service 是否存在。
type Locator struct {
	serviceReader serviceReader
	conf          Conf
}

// NewLocator 创建 K8s invocation 标准定位器。
func NewLocator(client kubernetes.Interface, conf *Conf) (*Locator, error) {
	if conf == nil {
		conf = &Conf{}
	}
	conf.Bootstrap()

	var reader serviceReader
	if client != nil {
		reader = client.CoreV1().Services(conf.Namespace)
	}

	return &Locator{
		serviceReader: reader,
		conf:          *conf,
	}, nil
}

// Resolve 把 ServiceRef 解析成标准 Service DNS target。
func (l *Locator) Resolve(ctx context.Context, ref microInvocation.ServiceRef) (microInvocation.Target, error) {
	ref = l.normalizeRef(ref)

	if l.conf.ValidateService && l.serviceReader != nil {
		if _, err := l.serviceReader.Get(ctx, ref.ServiceName(), metav1.GetOptions{}); err != nil {
			return microInvocation.Target{}, err
		}
	}

	return microInvocation.BuildTarget(ref, microInvocation.TargetOptions{
		DefaultPort:    l.conf.DefaultPort,
		ClusterDomain:  l.conf.ClusterDomain,
		ResolverScheme: l.conf.ResolverScheme,
	})
}

func (l *Locator) normalizeRef(ref microInvocation.ServiceRef) microInvocation.ServiceRef {
	if strings.TrimSpace(ref.Namespace) == "" {
		ref.Namespace = l.conf.Namespace
	}
	return ref
}

// TargetForService 是便于上层直接按 service 名构造目标的辅助方法。
//
// 它适合在“namespace 已由平台配置固定”的场景下使用，
// 从而减少业务侧反复手工组装 ServiceRef 的样板代码。
func (l *Locator) TargetForService(ctx context.Context, service string) (microInvocation.Target, error) {
	return l.Resolve(ctx, microInvocation.ServiceRef{
		Service:   service,
		Namespace: l.conf.Namespace,
	})
}

// MustNamespace 返回当前定位器使用的默认命名空间。
func (l *Locator) MustNamespace() string {
	return fmt.Sprintf("%s", l.conf.Namespace)
}
