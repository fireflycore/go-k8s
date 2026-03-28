package invocation

import (
	"context"
	"testing"

	microInvocation "github.com/fireflycore/go-micro/invocation"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestLocatorResolveBuildsServiceDNSTarget(t *testing.T) {
	locator, err := NewLocator(nil, &Conf{
		Namespace:   "default",
		DefaultPort: 9000,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	target, err := locator.Resolve(context.Background(), microInvocation.ServiceRef{
		Service:   "auth",
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if target.GRPCTarget() != "dns:///auth.default.svc.cluster.local:9000" {
		t.Fatalf("unexpected target: %s", target.GRPCTarget())
	}
}

func TestLocatorResolveValidatesServiceWhenEnabled(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "auth",
			Namespace: "default",
		},
	})

	locator, err := NewLocator(client, &Conf{
		Namespace:       "default",
		DefaultPort:     9000,
		ValidateService: true,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	target, err := locator.Resolve(context.Background(), microInvocation.ServiceRef{
		Service: "auth",
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if target.Host != "auth.default.svc.cluster.local" {
		t.Fatalf("unexpected host: %s", target.Host)
	}
}
