package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// routerGatewayReadyTimeout bounds how long an activation waits for a provisioned Gateway. A class that no
	// controller serves never writes Gateway status, so without a deadline the activation would wait forever.
	routerGatewayReadyTimeout = 5 * time.Minute
	routerGatewayPollInterval = 15 * time.Second

	reasonWaitingForClusterAddress  = "WaitingForClusterAddress"
	reasonGatewayClassNotFound      = "GatewayClassNotFound"
	reasonRoutabilityNotPersisted   = "RoutabilityNotPersisted"
	reasonGatewayNotAccepted        = "GatewayNotAccepted"
	reasonAddressNotClusterRoutable = "AddressNotClusterRoutable"
	reasonGatewayNotReady           = "GatewayNotReady"
)

type routerGatewayPhase int

// The zero value is pending, so a state returned alongside an error never lets routes attach.
const (
	routerGatewayPending routerGatewayPhase = iota
	routerGatewayReady
	routerGatewayFailed
)

// routerGatewayState is what the activation reports while it provisions konfidence-router: pending states become an
// InProgress condition, failed states a Failed condition that fails the VectorActivation.
type routerGatewayState struct {
	phase   routerGatewayPhase
	reason  string
	message string
	// exposed marks a provisioned Gateway that may be reachable from outside the cluster and must be deleted.
	exposed bool
}

func pendingState(reason, format string, args ...any) routerGatewayState {
	return routerGatewayState{phase: routerGatewayPending, reason: reason, message: fmt.Sprintf(format, args...)}
}

func failedState(reason, format string, args ...any) routerGatewayState {
	return routerGatewayState{phase: routerGatewayFailed, reason: reason, message: fmt.Sprintf(format, args...)}
}

func exposedState(reason, format string, args ...any) routerGatewayState {
	state := failedState(reason, format, args...)
	state.exposed = true
	return state
}

// ensureRouterGateway provisions konfidence-router in the landscape namespace when the landscape has none, and
// reports whether the Gateway is safe to attach stage routes to. An admin-provided konfidence-router is used as is.
//
// Routes are withheld until every assigned address is cluster-routable (GEP-5093): a reachable konfidence-router lets
// any client pick a stage by setting x-konfidence-stage itself.
func ensureRouterGateway(
	ctx context.Context, c client.Client, namespace, className string, startedAt, now time.Time,
) (routerGatewayState, error) {
	gateway := &gwapiv1.Gateway{}
	err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: defaultGatewayName}, gateway)
	if errors.IsNotFound(err) {
		return createRouterGateway(ctx, c, namespace, className, startedAt, now)
	}
	if err != nil {
		return routerGatewayState{}, fmt.Errorf("get Gateway %s/%s: %w", namespace, defaultGatewayName, err)
	}

	if gateway.Labels[labelProvisionedBy] != provisionedByOrchestrator {
		return routerGatewayState{phase: routerGatewayReady}, nil
	}

	state := evaluateRouterGateway(gateway, now)
	if state.exposed {
		if err := client.IgnoreNotFound(c.Delete(ctx, gateway)); err != nil {
			return routerGatewayState{}, fmt.Errorf("delete exposed Gateway %s/%s: %w", namespace, defaultGatewayName, err)
		}
	}
	return state, nil
}

func createRouterGateway(
	ctx context.Context, c client.Client, namespace, className string, startedAt, now time.Time,
) (routerGatewayState, error) {
	err := c.Get(ctx, types.NamespacedName{Name: className}, &gwapiv1.GatewayClass{})
	if errors.IsNotFound(err) {
		return missingGatewayClassState(className, startedAt, now), nil
	}
	if err != nil {
		return routerGatewayState{}, fmt.Errorf("get GatewayClass %s: %w", className, err)
	}

	gateway := newRouterGateway(namespace, className)
	err = c.Create(ctx, gateway)
	if err != nil && !errors.IsAlreadyExists(err) {
		return routerGatewayState{}, fmt.Errorf("create Gateway %s/%s: %w", namespace, defaultGatewayName, err)
	}
	// Create returns the stored object, so a pruned routability request shows up here, before the implementation can
	// expose the Gateway for a whole poll interval.
	if err == nil && !requestsClusterRoutability(gateway) {
		if err := client.IgnoreNotFound(c.Delete(ctx, gateway)); err != nil {
			return routerGatewayState{}, fmt.Errorf("delete exposed Gateway %s/%s: %w", namespace, defaultGatewayName, err)
		}
		return evaluateRouterGateway(gateway, now), nil
	}
	return pendingState(reasonWaitingForClusterAddress,
		"created Gateway %s/%s, waiting for a cluster-routable address", namespace, defaultGatewayName), nil
}

// missingGatewayClassState stays pending first: a cached client may not have seen a freshly created GatewayClass.
func missingGatewayClassState(className string, startedAt, now time.Time) routerGatewayState {
	const format = "GatewayClass %s does not exist; set gatewayClassName to an installed class"
	if now.Sub(startedAt) > routerGatewayReadyTimeout {
		return failedState(reasonGatewayClassNotFound, format, className)
	}
	return pendingState(reasonGatewayClassNotFound, format, className)
}

func evaluateRouterGateway(gateway *gwapiv1.Gateway, now time.Time) routerGatewayState {
	name := gateway.Namespace + "/" + gateway.Name
	className := gateway.Spec.GatewayClassName
	if !requestsClusterRoutability(gateway) {
		return exposedState(reasonRoutabilityNotPersisted,
			"Gateway %s lost spec.addresses[].routability; install the Gateway API experimental-channel CRDs v1.7.0 or later", name)
	}
	accepted := meta.FindStatusCondition(gateway.Status.Conditions, string(gwapiv1.GatewayConditionAccepted))
	if accepted != nil && accepted.Status == metav1.ConditionFalse {
		return failedState(reasonGatewayNotAccepted, "Gateway %s was not accepted (%s): %s", name, accepted.Reason, accepted.Message)
	}
	if isClusterRoutable(gateway) {
		return routerGatewayState{phase: routerGatewayReady}
	}
	programmed := meta.IsStatusConditionTrue(gateway.Status.Conditions, string(gwapiv1.GatewayConditionProgrammed))
	if programmed && len(gateway.Status.Addresses) > 0 {
		return exposedState(reasonAddressNotClusterRoutable,
			"Gateway %s is programmed with addresses %s that are not cluster-routable; GatewayClass %s does not support GEP-5093",
			name, addressValues(gateway.Status.Addresses), className)
	}
	if now.Sub(gateway.CreationTimestamp.Time) > routerGatewayReadyTimeout {
		return failedState(reasonGatewayNotReady,
			"Gateway %s has no cluster-routable address after %s; check that a controller serves GatewayClass %s",
			name, routerGatewayReadyTimeout, className)
	}
	return pendingState(reasonWaitingForClusterAddress, "waiting for Gateway %s to report a cluster-routable address", name)
}

func addressValues(addresses []gwapiv1.GatewayStatusAddress) string {
	values := make([]string, 0, len(addresses))
	for _, address := range addresses {
		values = append(values, address.Value)
	}
	return strings.Join(values, ", ")
}

func newRouterGateway(namespace, className string) *gwapiv1.Gateway {
	return &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultGatewayName,
			Namespace: namespace,
			Labels:    map[string]string{labelProvisionedBy: provisionedByOrchestrator},
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
