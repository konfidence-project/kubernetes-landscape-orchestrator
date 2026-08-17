// Package deploymentclass provides shared helpers for working with DeploymentClass
// resources in the kubernetes-landscape-orchestrator.
//
// The KLO registers itself as the controller for two deployment class types. All
// controllers that need to filter by ownership (DeploymentTarget, ArtifactDeployment,
// ConfigProvider) use this package as the single source of truth for:
//
//   - which controller identity string to match against DeploymentClass.spec.controller
//   - which type strings the KLO binary can actually handle (KnownTypes)
//   - how to query the current set of active types (ActiveTypes)
//   - how to map a DeploymentClass event to the affected DeploymentTargets (DeploymentTargetsForType)
package deploymentclass

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ControllerName is the value written into DeploymentClass.spec.controller for all
// classes managed by this operator. It follows the vendor-prefixed naming convention.
const ControllerName = "konfidence.cloud/kubernetes-landscape-orchestrator"

// KnownTypes is the fixed set of deployment class spec.type values that this binary
// has a concrete handler for. A DeploymentClass whose spec.type is not in this map
// will cause DeploymentTargets to be rejected with reason "UnsupportedType", even if
// spec.controller matches. This guards against forward-compatibility mismatches where
// a newer DeploymentClass CR is deployed against an older operator version.
var KnownTypes = map[string]struct{}{
	"konfidence.cloud/helm":      {},
	"konfidence.cloud/kustomize": {},
}

// ActiveTypes lists DeploymentClass resources through the provided client and returns
// the spec.type values whose spec.controller matches ControllerName.
func ActiveTypes(ctx context.Context, c client.Client) (map[string]struct{}, error) {
	list := &konfidencev1alpha1.DeploymentClassList{}
	if err := c.List(ctx, list); err != nil {
		return nil, fmt.Errorf("list DeploymentClasses: %w", err)
	}

	active := make(map[string]struct{})
	for _, dc := range list.Items {
		if dc.Spec.Controller == ControllerName {
			active[dc.Spec.Type] = struct{}{}
		}
	}
	return active, nil
}

// DeploymentTargetsForType returns reconcile.Requests for all DeploymentTarget
// resources cluster-wide whose spec.type matches the given deploymentType string.
// Used as the Watches mapper function so that DeploymentTarget and ArtifactDeployment
// controllers are re-enqueued when a DeploymentClass is created, updated, or deleted.
func DeploymentTargetsForType(ctx context.Context, c client.Client, deploymentType string) []reconcile.Request {
	list := &konfidencev1alpha1.DeploymentTargetList{}
	if err := c.List(ctx, list); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, dt := range list.Items {
		if dt.Spec.Type == deploymentType {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: dt.Namespace,
					Name:      dt.Name,
				},
			})
		}
	}
	return requests
}

// ArtifactDeploymentsForType returns reconcile.Requests for all ArtifactDeployment
// resources cluster-wide whose spec.manifest.type matches the given deploymentType
// string. Used as the Watches mapper for ArtifactDeployment controllers.
func ArtifactDeploymentsForType(ctx context.Context, c client.Client, deploymentType string) []reconcile.Request {
	list := &konfidencev1alpha1.ArtifactDeploymentList{}
	if err := c.List(ctx, list); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, ad := range list.Items {
		if ad.Spec.Manifest.Type == deploymentType {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ad.Namespace,
					Name:      ad.Name,
				},
			})
		}
	}
	return requests
}

// ArtifactDeploymentsForTarget returns requests for ArtifactDeployments in the target's
// namespace whose manifest type matches the DeploymentTarget type.
func ArtifactDeploymentsForTarget(ctx context.Context, c client.Client, target *konfidencev1alpha1.DeploymentTarget) []reconcile.Request {
	list := &konfidencev1alpha1.ArtifactDeploymentList{}
	if err := c.List(ctx, list, client.InNamespace(target.Namespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, ad := range list.Items {
		if ad.Spec.Manifest.Type == target.Spec.Type {
			requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: ad.Namespace,
				Name:      ad.Name,
			}})
		}
	}
	return requests
}
