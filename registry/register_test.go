package registry

import (
	"testing"

	micro "github.com/fireflycore/go-micro/registry"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRegisterInstallNoop(t *testing.T) {
	cli := fake.NewSimpleClientset()
	reg, err := NewRegister(cli, &micro.Meta{
		Env:     "prod",
		AppId:   "user-service",
		Version: "v1.0.0",
	}, &ServiceConf{
		Network: &micro.Network{Internal: "127.0.0.1:9001"},
		Kernel:  &micro.Kernel{},
	})
	if err != nil {
		t.Fatal(err)
	}

	node := &micro.ServiceNode{Methods: map[string]bool{"/svc.A/Ping": true}}
	if err := reg.Install(node); err != nil {
		t.Fatal(err)
	}
	if node.Meta == nil || node.Meta.AppId != "user-service" {
		t.Fatalf("meta not injected")
	}
}
