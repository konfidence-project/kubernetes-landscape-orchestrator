package controller

import (
	"context"
	"fmt"
	"sync"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Retained for the follow-up in konfidence-project/kubernetes-landscape-orchestrator#93 / ADR-0038.
const (
	//nolint:unused // see TODO on ClientFor.
	connectionTypeLocal = "local"
)

// acceptedKubeconfigKeys mirrors the keys the DeploymentTarget controller accepts in a kubeconfig Secret.
//
//nolint:unused // see TODO on ClientFor.
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

	//nolint:unused // see TODO on ClientFor.
	mu sync.Mutex
	//nolint:unused // see TODO on ClientFor.
	cache map[string]cachedClient
}

//nolint:unused // see TODO on ClientFor.
type cachedClient struct {
	resourceVersion string
	client          client.Client
}

// ClientFor returns the client to apply routing resources with.
// TODO(konfidence-project/kubernetes-landscape-orchestrator#93, ADR-0038): resolve the DeploymentTarget and apply routes through it instead
// of short-circuiting to the control-plane client. Deferred until the DeploymentTarget reshape (artifactType +
// parameters, DeploymentClass CRD removal) lands and routing config has a home on the target.
func (p *LandscapeClientProvider) ClientFor(_ context.Context, _ string) (client.Client, error) {
	return p.Local, nil
}

// resolveTarget returns the single ready DeploymentTarget to apply through. With artifactType set it must match;
// otherwise all ready targets must share one connection (e.g. helm and kustomize on the same cluster).
//
// Currently unused: ClientFor short-circuits to the control-plane client. Retained for the follow-up in
// konfidence-project/kubernetes-landscape-orchestrator#93 / ADR-0038.
//
//nolint:unused // see TODO on ClientFor.
func (p *LandscapeClientProvider) resolveTarget(
	ctx context.Context,
	landscapeNs string,
	artifactType konfidencev1alpha1.ArtifactType,
) (*konfidencev1alpha1.DeploymentTarget, error) {
	targets := &konfidencev1alpha1.DeploymentTargetList{}
	if err := p.Reader.List(ctx, targets, client.InNamespace(landscapeNs)); err != nil {
		return nil, fmt.Errorf("list DeploymentTargets in namespace %q: %w", landscapeNs, err)
	}

	var ready []*konfidencev1alpha1.DeploymentTarget
	for i := range targets.Items {
		t := &targets.Items[i]
		if artifactType != "" && t.Spec.ArtifactType != artifactType {
			continue
		}
		if meta.IsStatusConditionTrue(t.Status.Conditions, konfidencev1alpha1.DeploymentTargetReadyCondition) {
			ready = append(ready, t)
		}
	}

	if len(ready) == 0 {
		return nil, fmt.Errorf("no ready DeploymentTarget in namespace %q for routing (artifactType %q)", landscapeNs, artifactType)
	}

	first := ready[0]
	for _, t := range ready[1:] {
		if !sameConnection(first, t) {
			return nil, fmt.Errorf(
				"multiple ready DeploymentTargets with different connections in namespace %q; set artifactType in the activation registration spec",
				landscapeNs,
			)
		}
	}

	return first, nil
}

//nolint:unused // see TODO on ClientFor.
func sameConnection(a, b *konfidencev1alpha1.DeploymentTarget) bool {
	if a.Spec.Parameters == nil || b.Spec.Parameters == nil {
		return false
	}

	return a.Spec.Parameters.APIGroup == b.Spec.Parameters.APIGroup &&
		a.Spec.Parameters.Kind == b.Spec.Parameters.Kind &&
		a.Spec.Parameters.Name == b.Spec.Parameters.Name
}

// remoteClient builds (and caches by Secret resourceVersion) a client for the target cluster.
//
//nolint:unused // see TODO on ClientFor.
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

//nolint:unused // see TODO on ClientFor.
func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	for _, key := range acceptedKubeconfigKeys {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected one of %v", secret.Name, acceptedKubeconfigKeys)
}
