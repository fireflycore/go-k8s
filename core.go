package k8s

import (
	"errors"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// New 根据配置创建 Kubernetes 客户端接口。
func New(c *Conf) (kubernetes.Interface, error) {
	// 配置不能为空。
	if c == nil {
		return nil, errors.New("k8s: conf is nil")
	}

	var (
		cfg *rest.Config
		err error
	)

	if c.InCluster {
		// 集群内模式，读取 Pod 挂载凭据。
		cfg, err = rest.InClusterConfig()
	} else {
		// 集群外模式，从 kubeconfig 构建配置。
		cfg, err = clientcmd.BuildConfigFromFlags(c.MasterURL, c.KubeConfig)
	}
	if err != nil {
		return nil, err
	}

	if c.QPS > 0 {
		cfg.QPS = float32(c.QPS)
	}
	if c.Burst > 0 {
		cfg.Burst = c.Burst
	}

	// 返回 client-go 统一客户端接口。
	return kubernetes.NewForConfig(cfg)
}
