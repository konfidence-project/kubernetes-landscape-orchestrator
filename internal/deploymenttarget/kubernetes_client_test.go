package deploymenttarget

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	secretUID         = "uid-1"
	secretName        = "config"
	valueKey          = "value"
	valueYAMLKey      = "value.yaml"
	invalidKubeconfig = "invalid kubeconfig"
)

type testReader struct {
	get func(context.Context, client.ObjectKey, client.Object) error
}

func (r *testReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	return r.get(ctx, key, obj)
}

func (*testReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("unexpected List call")
}

func providerForTest(t *testing.T, reader client.Reader) *KubernetesClientProvider {
	t.Helper()
	provider, err := NewKubernetesClientProvider(fake.NewClientBuilder().Build(), reader)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func targetWithParameters(params konfidencev1alpha1.Parameters) *konfidencev1alpha1.DeploymentTarget {
	return &konfidencev1alpha1.DeploymentTarget{
		Namespace: landscapeNs,
		Spec:      konfidencev1alpha1.DeploymentTargetSpec{Parameters: &params},
	}
}

func targetForSecret(ref types.NamespacedName) *konfidencev1alpha1.DeploymentTarget {
	target := targetWithParameters(konfidencev1alpha1.Parameters{Kind: secretKind, Name: ref.Name})
	target.Namespace = ref.Namespace
	return target
}

func configSecret() *corev1.Secret {
	return &corev1.Secret{
		UID: secretUID, ResourceVersion: "1",
		Data: map[string][]byte{valueKey: []byte(validKubeconfig)},
	}
}

func TestClientForReadyTargetUsesLocalClientWithoutLookup(t *testing.T) {
	local := fake.NewClientBuilder().Build()
	provider, err := NewKubernetesClientProvider(local, &testReader{})
	if err != nil {
		t.Fatal(err)
	}

	got, err := provider.ClientForReadyTarget(t.Context(), "landscape", "helm.konfidence.cloud")
	if err != nil || got != local {
		t.Fatalf("ClientForReadyTarget() = (%v, %v), want the local client", got, err)
	}
}

func TestKubernetesClientProviderWithoutSecretRead(t *testing.T) {
	local := fake.NewClientBuilder().Build()
	for _, tt := range []struct {
		name   string
		target *konfidencev1alpha1.DeploymentTarget
		want   client.Client
	}{
		{name: "missing target"},
		{name: "local", target: &konfidencev1alpha1.DeploymentTarget{}, want: local},
		{name: "non-core group", target: targetWithParameters(konfidencev1alpha1.Parameters{APIGroup: "example.com", Kind: secretKind, Name: remoteName})},
		{name: "wrong kind", target: targetWithParameters(konfidencev1alpha1.Parameters{Kind: "ConfigMap", Name: remoteName})},
		{name: "empty name", target: targetWithParameters(konfidencev1alpha1.Parameters{Kind: secretKind})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewKubernetesClientProvider(local, &testReader{get: func(context.Context, client.ObjectKey, client.Object) error {
				t.Fatal("unexpected Secret read")
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			got, err := provider.ClientForTarget(t.Context(), tt.target)
			if got != tt.want || (err != nil) != (tt.want == nil) {
				t.Fatalf("ClientForTarget() = (%v, %v), want (%v, error=%v)", got, err, tt.want, tt.want == nil)
			}
		})
	}
}

func TestKubernetesClientProviderTargetsSharingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := konfidencev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{
		Namespace: landscapeNs, Name: remoteName,
		Data: map[string][]byte{valueKey: []byte(validKubeconfig)},
	}
	firstTarget := targetWithParameters(konfidencev1alpha1.Parameters{Kind: secretKind, Name: remoteName})
	firstTarget.Name = "first"
	firstTarget.Spec.ArtifactType = konfidencev1alpha1.MustArtifactType("helm.konfidence.cloud")
	secondTarget := firstTarget.DeepCopy()
	secondTarget.Name = "second"
	secondTarget.Spec.ArtifactType = konfidencev1alpha1.MustArtifactType("kustomize.konfidence.cloud")
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	provider, err := NewKubernetesClientProvider(reader, reader)
	if err != nil {
		t.Fatal(err)
	}

	first, err := provider.ClientForTarget(t.Context(), firstTarget)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.ClientForTarget(t.Context(), secondTarget)
	if err != nil || first == nil || first == reader || first != second {
		t.Fatalf("targets did not share a remote client: %v", err)
	}
}

func TestKubernetesClientProviderCache(t *testing.T) {
	const (
		otherName      = "other"
		otherNamespace = "other-ns"
	)
	ctx := context.Background()
	ref := types.NamespacedName{Namespace: "target-ns", Name: "credentials"}
	secret := configSecret()
	var reads []types.NamespacedName
	reader := &testReader{get: func(gotCtx context.Context, key client.ObjectKey, obj client.Object) error {
		if gotCtx != ctx {
			t.Error("reader received different context")
		}
		reads = append(reads, key)
		*obj.(*corev1.Secret) = *secret.DeepCopy()
		return nil
	}}
	scheme := runtime.NewScheme()
	p, err := NewKubernetesClientProvider(fake.NewClientBuilder().WithScheme(scheme).Build(), reader)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(key types.NamespacedName) client.Client {
		t.Helper()
		c, err := p.ClientForTarget(ctx, targetForSecret(key))
		if err != nil || c == nil {
			t.Fatalf("ClientForTarget(%s) = (%v, %v)", key, c, err)
		}
		return c
	}
	first := resolve(ref)
	if first.Scheme() != scheme {
		t.Fatal("client did not use the provider's scheme")
	}
	if got := resolve(ref); got != first {
		t.Fatal("unchanged Secret did not reuse client")
	}
	for _, other := range []types.NamespacedName{
		{Namespace: ref.Namespace, Name: otherName},
		{Namespace: otherNamespace, Name: ref.Name},
	} {
		if got := resolve(other); got == first {
			t.Fatalf("reference %s shared another reference's client", other)
		} else if resolve(other) != got {
			t.Fatalf("reference %s did not reuse its own client", other)
		}
	}
	if resolve(ref) != first {
		t.Fatal("other references displaced original client")
	}
	// ResourceVersions are opaque. Changing "1" to "0" must replace the cached client too.
	secret.ResourceVersion = "0"
	second := resolve(ref)
	if second == first {
		t.Fatal("changed ResourceVersion did not replace client")
	}
	secret.UID = "uid-2"
	third := resolve(ref)
	if third == second || third == first || resolve(ref) != third {
		t.Fatal("changed UID did not replace and cache client")
	}
	wantRefs := []types.NamespacedName{ref, ref,
		{Namespace: ref.Namespace, Name: otherName}, {Namespace: ref.Namespace, Name: otherName},
		{Namespace: otherNamespace, Name: ref.Name}, {Namespace: otherNamespace, Name: ref.Name},
		ref, ref, ref, ref,
	}
	if len(reads) != len(wantRefs) {
		t.Fatalf("got %d reads, want %d", len(reads), len(wantRefs))
	}
	for i := range wantRefs {
		if reads[i] != wantRefs[i] {
			t.Errorf("read %d: got %s, want %s", i, reads[i], wantRefs[i])
		}
	}
}

func TestKubernetesClientProviderFailuresPreserveCache(t *testing.T) {
	const (
		readFailure  = "read"
		buildFailure = "build"
	)
	sentinel := errors.New("injected failure")
	for _, failure := range []string{readFailure, "missing keys", invalidKubeconfig, buildFailure} {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cached=%v", failure, cached), func(t *testing.T) {
				secret := configSecret()
				var readErr error
				reads := 0
				p := providerForTest(t, &testReader{get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					reads++
					if readErr != nil {
						return readErr
					}
					*obj.(*corev1.Secret) = *secret.DeepCopy()
					return nil
				}})
				ref := types.NamespacedName{Namespace: "ns", Name: secretName}
				target := targetForSecret(ref)
				var before client.Client
				if cached {
					var err error
					before, err = p.ClientForTarget(context.Background(), target)
					if err != nil {
						t.Fatal(err)
					}
				}
				secret.ResourceVersion = "2"
				switch failure {
				case readFailure:
					readErr = sentinel
				case "missing keys":
					secret.Data = nil
				case invalidKubeconfig:
					secret.Data[valueKey] = []byte("private-credential: [")
				case buildFailure:
					// Valid kubeconfig with base64-encoded data that is not a PEM certificate.
					secret.Data[valueKey] = []byte(strings.Replace(validKubeconfig,
						"insecure-skip-tls-verify: true", "certificate-authority-data: aW52YWxpZA==", 1))
				}
				got, err := p.ClientForTarget(context.Background(), target)
				if got != nil || err == nil {
					t.Fatalf("failure returned (%v, %v)", got, err)
				}
				if failure == readFailure && !errors.Is(err, sentinel) {
					t.Fatalf("underlying error lost: %v", err)
				}
				if failure == buildFailure && !strings.HasPrefix(err.Error(), "build runtime client for secret "+ref.String()+": ") {
					t.Fatalf("expected a client construction error, got %v", err)
				}
				if strings.Contains(err.Error(), "private-credential") {
					t.Fatalf("kubeconfig leaked in error: %v", err)
				}
				// Restore the original Secret. If a client was cached, the provider must reuse it.
				secret = configSecret()
				readErr = nil
				got, err = p.ClientForTarget(context.Background(), target)
				if err != nil || got == nil || (cached && got != before) {
					t.Fatalf("recovery returned (%v, %v)", got, err)
				}
				wantReads := 2
				if cached {
					wantReads++
				}
				if reads != wantReads {
					t.Fatalf("got %d reads, want %d", reads, wantReads)
				}
			})
		}
	}
}

func TestKubernetesClientProviderKubeconfigKeyPriority(t *testing.T) {
	for _, tt := range []struct {
		name string
		data map[string][]byte
		err  bool
	}{
		{name: "value first", data: map[string][]byte{
			valueKey:     []byte(validKubeconfig),
			valueYAMLKey: []byte("invalid: ["),
		}},
		{name: "value.yaml only", data: map[string][]byte{valueYAMLKey: []byte(validKubeconfig)}},
		{name: "empty value blocks fallback", data: map[string][]byte{valueKey: {}, valueYAMLKey: []byte(validKubeconfig)}, err: true},
		{name: "invalid value blocks fallback", data: map[string][]byte{valueKey: []byte("private-credential: ["), valueYAMLKey: []byte(validKubeconfig)}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := providerForTest(t, &testReader{get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				secret := configSecret()
				secret.Data = tt.data
				*obj.(*corev1.Secret) = *secret
				return nil
			}})
			got, err := p.ClientForTarget(context.Background(), targetForSecret(types.NamespacedName{Name: secretName}))
			if tt.err {
				if err == nil || got != nil {
					t.Fatalf("invalid value fell back: client=%v, err=%v", got, err)
				}
			} else if err != nil || got == nil {
				t.Fatalf("client=%v, err=%v", got, err)
			}
		})
	}
}

func TestKubernetesClientProviderConcurrentRefresh(t *testing.T) {
	const callers = 12
	ref := types.NamespacedName{Namespace: "ns", Name: secretName}
	target := targetForSecret(ref)
	readEntered, releaseRead := make(chan struct{}), make(chan struct{})
	var reads, activeReads atomic.Int32
	reader := &testReader{get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		if activeReads.Add(1) != 1 {
			t.Error("concurrent Secret reads")
		}
		defer activeReads.Add(-1)
		read := reads.Add(1)
		if read == 2 {
			close(readEntered)
			<-releaseRead
		}
		secret := configSecret()
		if read > 1 {
			secret.ResourceVersion = "2"
		}
		*obj.(*corev1.Secret) = *secret
		return nil
	}}
	p := providerForTest(t, reader)
	old, err := p.ClientForTarget(t.Context(), target)
	if err != nil || old == nil {
		t.Fatalf("initial ClientForTarget: client=%v, err=%v", old, err)
	}
	start := make(chan struct{})
	var ready, done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	results := make([]client.Client, callers)
	for i := range callers {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			var err error
			results[i], err = p.ClientForTarget(context.Background(), target)
			if err != nil {
				t.Errorf("ClientForTarget: %v", err)
			}
		}()
	}
	ready.Wait()
	close(start)
	<-readEntered
	if got := reads.Load(); got != 2 {
		t.Errorf("got %d Secret reads while the refresh read was blocked; want 2", got)
	}
	close(releaseRead)
	done.Wait()
	first := results[0]
	if first == nil || first == old {
		t.Fatal("refresh did not return a replacement client")
	}
	for i, got := range results {
		if got != first {
			t.Errorf("caller %d received a different client", i)
		}
	}
	if reads.Load() != callers+1 {
		t.Fatalf("got %d reads, want %d", reads.Load(), callers+1)
	}
	if got, err := p.ClientForTarget(t.Context(), target); err != nil || got != first {
		t.Fatalf("ClientForTarget did not reuse the replacement: client=%v, err=%v", got, err)
	}
}
