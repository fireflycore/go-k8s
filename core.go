package k8s

import (
	"errors"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func New(c *Conf) (kubernetes.Interface, error) {
	if c == nil {
		return nil, errors.New("k8s: conf is nil")
	}

	var (
		cfg *rest.Config
		err error
	)

	if c.InCluster {
		cfg, err = rest.InClusterConfig()
	} else {
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

	return kubernetes.NewForConfig(cfg)
}
