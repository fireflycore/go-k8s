package k8s

type Conf struct {
	InCluster  bool   `json:"in_cluster"`
	MasterURL  string `json:"master_url"`
	KubeConfig string `json:"kube_config"`
	QPS        int    `json:"qps"`
	Burst      int    `json:"burst"`
}
