package kubernetesclient_test

import (
	"context"
	"errors"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/deploymenttarget"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/kubernetesclient"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type testClient struct{ client.Client }

type testProvider struct {
	calls int
	ctx   context.Context
	ref   types.NamespacedName
	value client.Client
	err   error
}

func (p *testProvider) ClientFor(ctx context.Context, ref types.NamespacedName) (client.Client, error) {
	p.calls++
	p.ctx, p.ref = ctx, ref
	return p.value, p.err
}

func remoteTarget(namespace, name string) *konfidencev1alpha1.DeploymentTarget {
	return &konfidencev1alpha1.DeploymentTarget{
		Namespace: namespace,
		Spec: konfidencev1alpha1.DeploymentTargetSpec{
			Parameters: &konfidencev1alpha1.Parameters{Kind: "Secret", Name: name},
		},
	}
}

func TestClientProviderWithoutRemoteCall(t *testing.T) {
	local := &testClient{}
	tests := []struct {
		name   string
		target *konfidencev1alpha1.DeploymentTarget
		want   client.Client
		err    bool
	}{
		{name: "missing target", err: true},
		{name: "local target", target: &konfidencev1alpha1.DeploymentTarget{}, want: local},
		{name: "non-core API group", target: remoteTarget("ns", "config"), err: true},
		{name: "wrong kind", target: remoteTarget("ns", "config"), err: true},
		{name: "empty name", target: remoteTarget("ns", ""), err: true},
	}
	tests[2].target.Spec.Parameters.APIGroup = "example.com"
	tests[3].target.Spec.Parameters.Kind = "ConfigMap"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &testProvider{value: &testClient{}}
			resolution, err := deploymenttarget.Resolve(tt.target)
			var got client.Client
			if err == nil {
				got, err = kubernetesclient.NewClientProvider(local, provider).ClientFor(context.Background(), resolution)
			}
			if got != tt.want || (err != nil) != tt.err {
				t.Fatalf("got (%v, %v), want client %v, error %v", got, err, tt.want, tt.err)
			}
			if provider.calls != 0 {
				t.Fatalf("provider called %d times", provider.calls)
			}
		})
	}
}

func TestClientProviderDelegatesUnchanged(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	sentinel := errors.New("provider failure")
	remote := &testClient{}
	for _, tt := range []struct {
		name  string
		value client.Client
		err   error
	}{
		{name: "client", value: remote},
		{name: "error", err: sentinel},
		{name: "client and error", value: remote, err: sentinel},
	} {
		t.Run(tt.name, func(t *testing.T) {
			provider := &testProvider{value: tt.value, err: tt.err}
			resolution := kubernetesclient.Destination{
				SecretRef: types.NamespacedName{Namespace: "target-ns", Name: "credentials"},
			}
			got, err := kubernetesclient.NewClientProvider(&testClient{}, provider).ClientFor(ctx, resolution)
			if got != tt.value || err != tt.err {
				t.Fatalf("provider result changed: got (%v, %v)", got, err)
			}
			if provider.calls != 1 || provider.ctx != ctx ||
				provider.ref != (types.NamespacedName{Namespace: "target-ns", Name: "credentials"}) {
				t.Fatalf("unexpected delegation: %+v", provider)
			}
		})
	}
}

type secretReader struct {
	client.Reader
	reads int
}

func (r *secretReader) Get(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	r.reads++
	if key != (types.NamespacedName{Namespace: "ns", Name: "config"}) {
		return errors.New("unexpected Secret reference")
	}
	*obj.(*corev1.Secret) = corev1.Secret{
		UID: "uid-1", ResourceVersion: "1",
		Data: map[string][]byte{"value": []byte(`apiVersion: v1
kind: Config
clusters:
- name: remote
  cluster:
    server: https://example.invalid:6443
contexts:
- name: remote
  context:
    cluster: remote
current-context: remote
`)},
	}
	return nil
}

func TestClientProviderSharedTargetReference(t *testing.T) {
	reader := &secretReader{}
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	remote, err := kubernetesclient.NewRuntimeClientProvider(reader, scheme)
	if err != nil {
		t.Fatal(err)
	}
	provider := kubernetesclient.NewClientProvider(&testClient{}, remote)
	firstTarget, secondTarget := remoteTarget("ns", "config"), remoteTarget("ns", "config")
	firstTarget.Name, secondTarget.Name = "first", "second"
	firstResolution, err := deploymenttarget.Resolve(firstTarget)
	if err != nil {
		t.Fatal(err)
	}
	secondResolution, err := deploymenttarget.Resolve(secondTarget)
	if err != nil {
		t.Fatal(err)
	}
	// Constructing clients does not issue requests; no client operations are run.
	first, err := provider.ClientFor(context.Background(), firstResolution)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.ClientFor(context.Background(), secondResolution)
	if err != nil || first == nil || first != second || reader.reads != 2 {
		t.Fatalf("targets did not share client: err=%v, reads=%d", err, reader.reads)
	}
}
