package invocation

import (
	microInvocation "github.com/fireflycore/go-micro/invocation"
	"k8s.io/client-go/kubernetes"
)

// NewConnectionManager 创建基于 K8s Locator 的连接管理器。
func NewConnectionManager(client kubernetes.Interface, conf *Conf, options microInvocation.ConnectionManagerOptions) (*microInvocation.ConnectionManager, error) {
	locator, err := NewLocator(client, conf)
	if err != nil {
		return nil, err
	}
	options.Locator = locator
	return microInvocation.NewConnectionManager(options)
}
