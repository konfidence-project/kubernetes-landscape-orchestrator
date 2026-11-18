package kubernetesclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const validKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://example.com:6443
    insecure-skip-tls-verify: true
  name: test
contexts:
- context:
    cluster: test
    user: test
  name: test
current-context: test
users:
- name: test
  user:
    token: test-token
`

type testClient struct{ client.Client }

type testReader struct {
	get func(context.Context, client.ObjectKey, client.Object) error
}

func (r *testReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	return r.get(ctx, key, obj)
}

func (*testReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("unexpected List call")
}

func providerForTest(t *testing.T, reader client.Reader) *runtimeClientProvider {
	t.Helper()
	provider, err := NewRuntimeClientProvider(reader, runtime.NewScheme())
	if err != nil {
		t.Fatal(err)
	}
	return provider.(*runtimeClientProvider)
}

func configSecret() *corev1.Secret {
	return &corev1.Secret{
		UID: "uid-1", ResourceVersion: "1",
		Data: map[string][]byte{"value": []byte(validKubeconfig)},
	}
}

func TestRuntimeClientProviderCache(t *testing.T) {
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
	p := providerForTest(t, reader)
	builds := 0
	p.newClient = func(config *rest.Config, options client.Options) (client.Client, error) {
		builds++
		if config.Host != "https://example.com:6443" || options.Scheme != p.scheme {
			t.Errorf("unexpected factory dependencies: host %q, scheme %p", config.Host, options.Scheme)
		}
		return &testClient{}, nil
	}
	resolve := func(key types.NamespacedName) client.Client {
		t.Helper()
		c, err := p.ClientFor(ctx, key)
		if err != nil || c == nil {
			t.Fatalf("ClientFor(%s) = (%v, %v)", key, c, err)
		}
		return c
	}
	first := resolve(ref)
	if got := resolve(ref); got != first || builds != 1 {
		t.Fatal("unchanged Secret did not reuse client")
	}
	for _, other := range []types.NamespacedName{
		{Namespace: ref.Namespace, Name: "other"},
		{Namespace: "other-ns", Name: ref.Name},
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
	// ResourceVersions are opaque: a lexically smaller version also replaces.
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
	if builds != 5 || len(reads) != 10 {
		t.Fatalf("got %d builds and %d reads, want 5 and 10", builds, len(reads))
	}
	wantRefs := []types.NamespacedName{ref, ref,
		{Namespace: ref.Namespace, Name: "other"}, {Namespace: ref.Namespace, Name: "other"},
		{Namespace: "other-ns", Name: ref.Name}, {Namespace: "other-ns", Name: ref.Name},
		ref, ref, ref, ref,
	}
	for i := range wantRefs {
		if reads[i] != wantRefs[i] {
			t.Errorf("read %d: got %s, want %s", i, reads[i], wantRefs[i])
		}
	}
}

func TestRuntimeClientProviderFailuresPreserveCache(t *testing.T) {
	sentinel := errors.New("injected failure")
	for _, failure := range []string{"read", "missing keys", "invalid kubeconfig", "build"} {
		for _, cached := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cached=%v", failure, cached), func(t *testing.T) {
				secret := configSecret()
				var readErr, buildErr error
				reads, builds := 0, 0
				p := providerForTest(t, &testReader{get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					reads++
					if readErr != nil {
						return readErr
					}
					*obj.(*corev1.Secret) = *secret.DeepCopy()
					return nil
				}})
				p.newClient = func(*rest.Config, client.Options) (client.Client, error) {
					builds++
					if buildErr != nil {
						return &testClient{}, buildErr
					}
					return &testClient{}, nil
				}
				ref := types.NamespacedName{Namespace: "ns", Name: "config"}
				if cached {
					if _, err := p.ClientFor(context.Background(), ref); err != nil {
						t.Fatal(err)
					}
				}
				before, existed := p.cache[ref]
				buildsBefore := builds
				secret.ResourceVersion = "2"
				switch failure {
				case "read":
					readErr = sentinel
				case "missing keys":
					secret.Data = nil
				case "invalid kubeconfig":
					secret.Data["value"] = []byte("private-credential: [")
				case "build":
					buildErr = sentinel
				}
				got, err := p.ClientFor(context.Background(), ref)
				if got != nil || err == nil {
					t.Fatalf("failure returned (%v, %v)", got, err)
				}
				if !p.mu.TryLock() {
					t.Fatal("lock not released on error")
				}
				p.mu.Unlock()
				if (failure == "read" || failure == "build") && !errors.Is(err, sentinel) {
					t.Fatalf("underlying error lost: %v", err)
				}
				if strings.Contains(err.Error(), "private-credential") {
					t.Fatalf("kubeconfig leaked in error: %v", err)
				}
				after, exists := p.cache[ref]
				if after != before || exists != existed {
					t.Fatal("failure changed cache")
				}
				wantBuilds := buildsBefore
				if failure == "build" {
					wantBuilds++
				}
				if builds != wantBuilds {
					t.Fatalf("got %d builds, want %d", builds, wantBuilds)
				}
				// Recover to the old stamp: a preserved entry must still be reused.
				secret = configSecret()
				readErr, buildErr = nil, nil
				got, err = p.ClientFor(context.Background(), ref)
				if err != nil || got == nil || (cached && got != before.client) {
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

func TestRuntimeClientProviderKubeconfigKeyPriority(t *testing.T) {
	for _, tt := range []struct {
		name string
		data map[string][]byte
		host string
		err  bool
	}{
		{name: "value first", data: map[string][]byte{
			"value":      []byte(validKubeconfig),
			"value.yaml": []byte(strings.ReplaceAll(validKubeconfig, "example.com", "secondary.example.com")),
		}, host: "https://example.com:6443"},
		{name: "value.yaml only", data: map[string][]byte{"value.yaml": []byte(validKubeconfig)}, host: "https://example.com:6443"},
		{name: "empty value blocks fallback", data: map[string][]byte{"value": {}, "value.yaml": []byte(validKubeconfig)}, err: true},
		{name: "invalid value blocks fallback", data: map[string][]byte{"value": []byte("private-credential: ["), "value.yaml": []byte(validKubeconfig)}, err: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := providerForTest(t, &testReader{get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				secret := configSecret()
				secret.Data = tt.data
				*obj.(*corev1.Secret) = *secret
				return nil
			}})
			builds := 0
			p.newClient = func(config *rest.Config, _ client.Options) (client.Client, error) {
				builds++
				if config.Host != tt.host {
					t.Errorf("host = %q, want %q", config.Host, tt.host)
				}
				return &testClient{}, nil
			}
			got, err := p.ClientFor(context.Background(), types.NamespacedName{Name: "config"})
			if tt.err {
				if err == nil || got != nil || builds != 0 {
					t.Fatalf("invalid value fell back: client=%v, err=%v, builds=%d", got, err, builds)
				}
			} else if err != nil || got == nil || builds != 1 {
				t.Fatalf("client=%v, err=%v, builds=%d", got, err, builds)
			}
		})
	}
}

func TestRuntimeClientProviderConcurrentRefresh(t *testing.T) {
	const callers = 12
	ref := types.NamespacedName{Namespace: "ns", Name: "config"}
	readEntered, releaseRead := make(chan struct{}), make(chan struct{})
	buildEntered, releaseBuild := make(chan struct{}), make(chan struct{})
	var reads, builds, activeReads atomic.Int32
	var p *runtimeClientProvider
	reader := &testReader{get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		if activeReads.Add(1) != 1 {
			t.Error("concurrent Secret reads")
		}
		defer activeReads.Add(-1)
		if p.mu.TryLock() {
			p.mu.Unlock()
			t.Error("Secret read outside provider lock")
		}
		if reads.Add(1) == 1 {
			close(readEntered)
			<-releaseRead
		}
		*obj.(*corev1.Secret) = *configSecret()
		return nil
	}}
	p = providerForTest(t, reader)
	p.newClient = func(*rest.Config, client.Options) (client.Client, error) {
		if p.mu.TryLock() {
			p.mu.Unlock()
			t.Error("client build outside provider lock")
		}
		if builds.Add(1) == 1 {
			close(buildEntered)
			<-releaseBuild
		}
		return &testClient{}, nil
	}
	// Seed an older entry so this wave exercises cache replacement, not just insertion.
	old := &testClient{}
	p.cache[ref] = runtimeClientCacheEntry{uid: "uid-1", resourceVersion: "old", client: old}
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
			results[i], err = p.ClientFor(context.Background(), ref)
			if err != nil {
				t.Errorf("ClientFor: %v", err)
			}
		}()
	}
	ready.Wait()
	close(start)
	<-readEntered
	if reads.Load() != 1 || builds.Load() != 0 {
		t.Error("work advanced while first read was blocked")
	}
	close(releaseRead)
	<-buildEntered
	if reads.Load() != 1 || builds.Load() != 1 {
		t.Error("another read passed the blocked client build")
	}
	close(releaseBuild)
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
	if reads.Load() != callers || builds.Load() != 1 {
		t.Fatalf("got %d reads and %d builds", reads.Load(), builds.Load())
	}
	if !p.mu.TryLock() {
		t.Fatal("lock not released after calls")
	}
	defer p.mu.Unlock()
	if entry := p.cache[ref]; entry.client != first || entry.uid != "uid-1" || entry.resourceVersion != "1" {
		t.Fatalf("unexpected replacement entry: %+v", entry)
	}
}
