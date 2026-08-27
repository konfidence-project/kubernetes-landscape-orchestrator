package controller

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func artifactDeploymentsForTarget(ctx context.Context, c client.Client, target *konfidencev1alpha1.DeploymentTarget) []reconcile.Request {
	deployments := &konfidencev1alpha1.ArtifactDeploymentList{}
	if err := c.List(ctx, deployments, client.InNamespace(target.Namespace)); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		if deployment.Spec.Manifest.Type == target.Spec.DeploymentClassName {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(deployment)})
		}
	}
	return requests
}
