package kubernetesclient

import (
	"context"
	"errors"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RuntimeClientProvider provides remote Kubernetes clients from kubeconfig Secrets.
// It does not interpret DeploymentTargets or handle local clients.
type RuntimeClientProvider interface {
	// ClientFor returns a client for an already resolved kubeconfig Secret reference.
	ClientFor(ctx context.Context, secretRef types.NamespacedName) (client.Client, error)
}

type runtimeClientProvider struct {
	reader    client.Reader
	scheme    *runtime.Scheme
	newClient func(*rest.Config, client.Options) (client.Client, error)
	mu        sync.Mutex
	cache     map[types.NamespacedName]runtimeClientCacheEntry
}

type runtimeClientCacheEntry struct {
	uid             types.UID
	resourceVersion string
	client          client.Client
}

var _ RuntimeClientProvider = (*runtimeClientProvider)(nil)

// NewRuntimeClientProvider creates a remote client provider.
// reader must be uncached, such as the manager's APIReader. The scheme must contain
// all types needed by remote clients before use. Clients are reused while the
// referenced Secret's UID and ResourceVersion remain unchanged.
func NewRuntimeClientProvider(
	reader client.Reader,
	scheme *runtime.Scheme,
) (RuntimeClientProvider, error) {
	if reader == nil {
		return nil, errors.New("runtime client provider requires a secret reader")
	}
	if scheme == nil {
		return nil, errors.New("runtime client provider requires a scheme")
	}

	return &runtimeClientProvider{
		reader:    reader,
		scheme:    scheme,
		newClient: client.New,
		cache:     make(map[types.NamespacedName]runtimeClientCacheEntry),
	}, nil
}

func (p *runtimeClientProvider) ClientFor(
	ctx context.Context,
	secretRef types.NamespacedName,
) (client.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	secret := &corev1.Secret{}
	if err := p.reader.Get(ctx, secretRef, secret); err != nil {
		return nil, fmt.Errorf("get kubeconfig secret %s: %w", secretRef, err)
	}

	if entry, ok := p.cache[secretRef]; ok &&
		entry.uid == secret.UID && entry.resourceVersion == secret.ResourceVersion {
		return entry.client, nil
	}

	kubeconfigBytes, err := KubeconfigFromSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig from secret %s: %w", secretRef, err)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		// Parse errors may contain kubeconfig data, including credentials.
		return nil, fmt.Errorf("invalid kubeconfig in secret %s", secretRef)
	}

	remoteClient, err := p.newClient(config, client.Options{Scheme: p.scheme})
	if err != nil {
		return nil, fmt.Errorf("build runtime client for secret %s: %w", secretRef, err)
	}

	p.cache[secretRef] = runtimeClientCacheEntry{
		uid:             secret.UID,
		resourceVersion: secret.ResourceVersion,
		client:          remoteClient,
	}
	return remoteClient, nil
}
