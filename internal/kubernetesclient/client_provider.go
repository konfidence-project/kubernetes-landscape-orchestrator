package kubernetesclient

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Destination describes local Kubernetes access or a remote kubeconfig Secret.
// SecretRef is populated only for remote destinations (Local is false).
type Destination struct {
	Local     bool
	SecretRef types.NamespacedName
}

// ClientProvider supplies clients for already resolved destinations.
type ClientProvider struct {
	local   client.Client
	runtime RuntimeClientProvider
}

// NewClientProvider binds the local client and the remote client provider.
func NewClientProvider(local client.Client, runtime RuntimeClientProvider) *ClientProvider {
	return &ClientProvider{local: local, runtime: runtime}
}

// ClientFor returns the existing local client or delegates the resolved Secret
// reference to the remote provider. Provider results are returned unchanged.
func (p *ClientProvider) ClientFor(ctx context.Context, resolution Destination) (client.Client, error) {
	if resolution.Local {
		return p.local, nil
	}
	return p.runtime.ClientFor(ctx, resolution.SecretRef)
}
