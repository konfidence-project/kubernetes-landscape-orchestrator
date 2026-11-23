package deploymenttarget

import (
	"context"
	"errors"
	"fmt"
	"sync"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubernetesClientProvider supplies local and remote clients for selected DeploymentTargets.
type KubernetesClientProvider struct {
	local  client.Client
	reader client.Reader
	scheme *runtime.Scheme
	mu     sync.Mutex
	cache  map[types.NamespacedName]runtimeClientCacheEntry
}

type runtimeClientCacheEntry struct {
	uid             types.UID
	resourceVersion string
	client          client.Client
}

// NewKubernetesClientProvider uses the local client's scheme for remote clients.
// Pass an uncached Secret reader, such as the manager's APIReader, so each lookup sees Secret changes.
// A cached remote client is reused only if the Secret's UID and ResourceVersion still match.
func NewKubernetesClientProvider(
	local client.Client,
	reader client.Reader,
) (*KubernetesClientProvider, error) {
	if local == nil {
		return nil, errors.New("deployment target client provider requires a local client")
	}
	if reader == nil {
		return nil, errors.New("deployment target client provider requires an API reader")
	}
	scheme := local.Scheme()
	if scheme == nil {
		return nil, errors.New("deployment target client provider requires a scheme")
	}

	return &KubernetesClientProvider{
		local:  local,
		reader: reader,
		scheme: scheme,
		cache:  make(map[types.NamespacedName]runtimeClientCacheEntry),
	}, nil
}

// ClientForReadyTarget returns the control-plane client without looking up a target.
func (p *KubernetesClientProvider) ClientForReadyTarget(
	_ context.Context,
	_ string,
	_ konfidencev1alpha1.ArtifactType,
) (client.Client, error) {
	// TODO(konfidence-project/kubernetes-landscape-orchestrator#78): Resolve routing
	// targets once activation and stage deletion support remote routes.
	return p.local, nil
}

// ClientForTarget returns the local client if spec.parameters is absent.
// Otherwise, it uses the target's kubeconfig Secret to return a remote client.
// The caller selects the target and checks readiness.
func (p *KubernetesClientProvider) ClientForTarget(
	ctx context.Context,
	target *konfidencev1alpha1.DeploymentTarget,
) (client.Client, error) {
	ref, err := kubeconfigSecretRef(target)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return p.local, nil
	}
	secretRef := *ref

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

	kubeconfigBytes, err := kubeconfigFromSecret(secret)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig from secret %s: %w", secretRef, err)
	}

	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		// Parse errors may contain kubeconfig data, including credentials.
		return nil, fmt.Errorf("invalid kubeconfig in secret %s", secretRef)
	}

	remoteClient, err := client.New(config, client.Options{Scheme: p.scheme})
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

// kubeconfigSecretRef returns nil for a local target.
func kubeconfigSecretRef(target *konfidencev1alpha1.DeploymentTarget) (*types.NamespacedName, error) {
	if target == nil {
		return nil, errors.New("deployment target is required")
	}
	params := target.Spec.Parameters
	if params == nil {
		return nil, nil
	}
	if params.APIGroup != "" || params.Kind != "Secret" || params.Name == "" {
		return nil, errors.New("parameters must refer to a Kubernetes Secret containing a kubeconfig")
	}
	return &types.NamespacedName{Namespace: target.Namespace, Name: params.Name}, nil
}

// kubeconfigFromSecret reads value, or value.yaml if value is absent, as Flux does.
// It does not validate the kubeconfig.
func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	keys := [...]string{"value", "value.yaml"}
	for _, key := range keys {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected one of %v", secret.Name, keys)
}
