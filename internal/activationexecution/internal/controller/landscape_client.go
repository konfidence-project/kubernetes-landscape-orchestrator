package controller

import (
	"context"
	"fmt"
	"sync"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	connectionTypeLocal = "local"
)

// acceptedKubeconfigKeys mirrors the keys the DeploymentTarget controller accepts in a kubeconfig Secret.
var acceptedKubeconfigKeys = []string{"value", "value.yaml"}

// LandscapeClientProvider returns the client used to apply routing resources into a landscape. The landscape runtime
// (Services, Gateway, HTTPRoutes) may live on a different cluster than the control plane, so routing resources are
// applied through the landscape's Kubernetes DeploymentTarget: the local (control-plane) client when the target is
// "local", otherwise a client built from the target's kubeconfig Secret.
type LandscapeClientProvider struct {
	// Local is the control-plane manager client, returned for "local" DeploymentTargets.
	Local client.Client
	// Reader reads DeploymentTargets and kubeconfig Secrets. Use an uncached reader to avoid caching all Secrets.
	Reader client.Reader
	// Scheme is used for remote clients; must include the Gateway API and core types.
	Scheme *runtime.Scheme

	mu    sync.Mutex
	cache map[string]cachedClient
}

type cachedClient struct {
	resourceVersion string
	client          client.Client
}

// ClientFor resolves the ready DeploymentTarget in the landscape namespace and returns the client to apply routing
// resources with. deploymentClassName selects the target when more than one connection exists; it may be empty when
// the landscape has a single connection.
func (p *LandscapeClientProvider) ClientFor(ctx context.Context, landscapeNs, deploymentClassName string) (client.Client, error) {
	target, err := p.resolveTarget(ctx, landscapeNs, deploymentClassName)
	if err != nil {
		return nil, err
	}

	if target.Spec.Connection.Type == connectionTypeLocal || target.Spec.Connection.Ref == nil {
		return p.Local, nil
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: landscapeNs, Name: target.Spec.Connection.Ref.Name}
	if err := p.Reader.Get(ctx, secretKey, secret); err != nil {
		return nil, fmt.Errorf("get kubeconfig secret %s: %w", secretKey, err)
	}

	return p.remoteClient(secretKey.String(), secret)
}

// resolveTarget returns the single ready DeploymentTarget to apply through. With deploymentClassName set it must match;
// otherwise all ready targets must share one connection (e.g. helm and kustomize on the same cluster).
func (p *LandscapeClientProvider) resolveTarget(ctx context.Context, landscapeNs, deploymentClassName string) (*konfidencev1alpha1.DeploymentTarget, error) {
	targets := &konfidencev1alpha1.DeploymentTargetList{}
	if err := p.Reader.List(ctx, targets, client.InNamespace(landscapeNs)); err != nil {
		return nil, fmt.Errorf("list DeploymentTargets in namespace %q: %w", landscapeNs, err)
	}

	var ready []*konfidencev1alpha1.DeploymentTarget
	for i := range targets.Items {
		t := &targets.Items[i]
		if deploymentClassName != "" && t.Spec.DeploymentClassName != deploymentClassName {
			continue
		}
		if meta.IsStatusConditionTrue(t.Status.Conditions, konfidencev1alpha1.DeploymentTargetReadyCondition) {
			ready = append(ready, t)
		}
	}

	if len(ready) == 0 {
		return nil, fmt.Errorf("no ready DeploymentTarget in namespace %q for routing (deploymentClassName %q)", landscapeNs, deploymentClassName)
	}

	first := ready[0]
	for _, t := range ready[1:] {
		if !sameConnection(first, t) {
			return nil, fmt.Errorf(
				"multiple ready DeploymentTargets with different connections in namespace %q; set deploymentClassName in the activation registration spec",
				landscapeNs,
			)
		}
	}

	return first, nil
}

func sameConnection(a, b *konfidencev1alpha1.DeploymentTarget) bool {
	if a.Spec.Connection.Type != b.Spec.Connection.Type {
		return false
	}
	ar, br := a.Spec.Connection.Ref, b.Spec.Connection.Ref
	if ar == nil || br == nil {
		return ar == br
	}
	return ar.Name == br.Name
}

// remoteClient builds (and caches by Secret resourceVersion) a client for the target cluster.
func (p *LandscapeClientProvider) remoteClient(cacheKey string, secret *corev1.Secret) (client.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if cached, ok := p.cache[cacheKey]; ok && cached.resourceVersion == secret.ResourceVersion {
		return cached.client, nil
	}

	kubeconfig, err := kubeconfigFromSecret(secret)
	if err != nil {
		return nil, err
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("invalid kubeconfig in secret %q: %w", secret.Name, err)
	}

	cl, err := client.New(restConfig, client.Options{Scheme: p.Scheme})
	if err != nil {
		return nil, fmt.Errorf("build client for secret %q: %w", secret.Name, err)
	}

	if p.cache == nil {
		p.cache = map[string]cachedClient{}
	}
	p.cache[cacheKey] = cachedClient{resourceVersion: secret.ResourceVersion, client: cl}
	return cl, nil
}

func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	for _, key := range acceptedKubeconfigKeys {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected one of %v", secret.Name, acceptedKubeconfigKeys)
}
