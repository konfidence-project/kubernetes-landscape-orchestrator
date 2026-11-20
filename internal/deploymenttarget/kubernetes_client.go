package deploymenttarget

import (
	"context"
	"errors"
	"fmt"
	"sync"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	targethelpers "github.com/konfidence-project/konfidence/pkg/deployer/deploymenttarget"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KubernetesClientProvider resolves local and remote clients for ready DeploymentTargets.
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
// Pass an uncached reader, such as the manager's APIReader, to read DeploymentTargets and Secrets.
// The provider reuses remote clients while the Secret's UID and ResourceVersion stay the same.
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

// ClientForReadyTarget finds exactly one ready DeploymentTarget for the given namespace and artifact type.
// If the target has no parameters, it returns the local client.
// Otherwise, it returns a remote client using the target's kubeconfig Secret.
func (p *KubernetesClientProvider) ClientForReadyTarget(
	ctx context.Context,
	namespace string,
	artifactType konfidencev1alpha1.ArtifactType,
) (client.Client, error) {
	target, err := targethelpers.FindReady(ctx, p.reader, namespace, artifactType)
	if err != nil {
		return nil, err
	}
	targetClient, err := p.clientForTarget(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("get Kubernetes client for DeploymentTarget %s: %w", client.ObjectKeyFromObject(target), err)
	}
	return targetClient, nil
}

func (p *KubernetesClientProvider) clientForTarget(
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

// kubeconfigFromSecret reads value first, then value.yaml, following Flux's convention.
// It falls back to value.yaml only if value is absent. It does not validate the contents.
func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	keys := [...]string{"value", "value.yaml"}
	for _, key := range keys {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected one of %v", secret.Name, keys)
}
