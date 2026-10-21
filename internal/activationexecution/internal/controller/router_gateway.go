package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ensureRouterGateway provisions konfidence-router in the landscape namespace when the landscape has none, and fails
// until the Gateway is safe to attach stage routes to. An admin-provided konfidence-router is used as is.
//
// Routes are withheld until every assigned address is cluster-routable (GEP-5093): a reachable konfidence-router lets
// any client pick a stage by setting x-konfidence-stage itself.
func ensureRouterGateway(ctx context.Context, c client.Client, namespace, className string) error {
	gateway := &gwapiv1.Gateway{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: defaultGatewayName}, gateway)
	if errors.IsNotFound(err) {
		gateway = newRouterGateway(namespace, className)
		err = c.Create(ctx, gateway)
	}
	if err != nil {
		return fmt.Errorf("ensure Gateway %s/%s: %w", namespace, defaultGatewayName, err)
	}

	if gateway.Labels[labelManagedBy] != managedByKonfidence {
		return nil
	}
	if !requestsClusterRoutability(gateway) {
		return deletePrunedGateway(ctx, c, gateway)
	}
	if !isClusterRoutable(gateway) {
		return fmt.Errorf("gateway %s/%s has no cluster-routable address yet", namespace, defaultGatewayName)
	}
	return nil
}

// deletePrunedGateway removes a provisioned Gateway whose routability request did not persist. The API server prunes
// the field when the cluster runs the standard-channel CRDs, and the implementation then exposes the Gateway with its
// default (often public) address.
func deletePrunedGateway(ctx context.Context, c client.Client, gateway *gwapiv1.Gateway) error {
	if err := client.IgnoreNotFound(c.Delete(ctx, gateway)); err != nil {
		return fmt.Errorf("delete Gateway %s/%s without routability: %w", gateway.Namespace, gateway.Name, err)
	}
	return fmt.Errorf("gateway %s/%s lost spec.addresses[].routability; install the Gateway API experimental-channel CRDs",
		gateway.Namespace, gateway.Name)
}

func newRouterGateway(namespace, className string) *gwapiv1.Gateway {
	return &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultGatewayName,
			Namespace: namespace,
			Labels:    map[string]string{labelManagedBy: managedByKonfidence},
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: gwapiv1.ObjectName(className),
			Addresses:        []gwapiv1.GatewaySpecAddress{{Routability: gwapiv1.GatewayAddressRoutabilityCluster}},
			Listeners: []gwapiv1.Listener{{
				Name:     "http",
				Protocol: gwapiv1.HTTPProtocolType,
				Port:     80,
			}},
		},
	}
}

func requestsClusterRoutability(gateway *gwapiv1.Gateway) bool {
	for _, address := range gateway.Spec.Addresses {
		if address.Routability != gwapiv1.GatewayAddressRoutabilityCluster {
			return false
		}
	}
	return len(gateway.Spec.Addresses) > 0
}

// isClusterRoutable reports whether the implementation honoured the request: GEP-5093 is Extended support, so an
// implementation may ignore it and assign a public address.
func isClusterRoutable(gateway *gwapiv1.Gateway) bool {
	if !requestsClusterRoutability(gateway) || len(gateway.Status.Addresses) == 0 {
		return false
	}
	for _, address := range gateway.Status.Addresses {
		if address.Routability == nil || *address.Routability != gwapiv1.GatewayAddressRoutabilityCluster {
			return false
		}
	}
	return true
}
