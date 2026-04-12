package k8s

// Conf 定义 Kubernetes 客户端初始化配置。
type Config struct {
	// InCluster 表示是否使用集群内 ServiceAccount 配置。
	InCluster bool `json:"in_cluster"`
	// MasterURL 是可选 APIServer 地址（集群外模式）。
	MasterURL string `json:"master_url"`
	// KubeConfig 是可选 kubeconfig 文件路径（集群外模式）。
	KubeConfig string `json:"kube_config"`
	// QPS 是 client-go 请求速率上限。
	QPS int `json:"qps"`
	// Burst 是 client-go 突发请求上限。
	Burst int `json:"burst"`
}
