package registry

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	micro "github.com/fireflycore/go-micro/registry"
)

func TestDiscoverResolveServiceFQDN(t *testing.T) {
	cli := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "user-service",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Port: 9001},
				},
			},
		},
	)

	dis, err := NewDiscover(cli, &micro.ServiceMeta{
		Env: "prod",
	}, &ServiceConf{
		Namespace:   "default",
		ResolveMode: ResolveModeServiceFQDN,
		MethodRoutes: map[string]string{
			"/acme.user.v1.UserService/Login": "user-service",
		},
		Network: &micro.Network{},
		Kernel:  &micro.ServiceKernel{},
	})
	if err != nil {
		t.Fatal(err)
	}

	go dis.Watcher()
	defer dis.Unwatch()

	var (
		nodes []*micro.ServiceNode
		appID string
	)
	for i := 0; i < 20; i++ {
		var getErr error
		nodes, appID, getErr = dis.GetService("/acme.user.v1.UserService/Login")
		if getErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if appID != "user-service" {
		t.Fatalf("unexpected appID: %s", appID)
	}
	if len(nodes) != 1 {
		t.Fatalf("unexpected nodes len: %d", len(nodes))
	}
}

func TestBuildNodeFromEndpointUsesInjectedInstanceID(t *testing.T) {
	meta := &micro.ServiceMeta{Env: "prod", AppId: "user-service", Version: "v1", InstanceId: "ins-1"}
	conf := &ServiceConf{Network: &micro.Network{}, Kernel: &micro.ServiceKernel{}}
	conf.Bootstrap()

	node := buildNodeFromEndpoint(meta, conf, "10.0.0.1:9001", "user-service")

	if node.Meta == nil {
		t.Fatal("meta should not be nil")
	}
	if node.Meta.InstanceId != "ins-1" {
		t.Fatal("instance id should come from injected meta")
	}
}
