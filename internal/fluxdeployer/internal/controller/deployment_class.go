package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func deploymentClassActive(ctx context.Context, c client.Client, deploymentClassName string) (bool, error) {
	class := &konfidencev1alpha1.DeploymentClass{}
	if err := c.Get(ctx, client.ObjectKey{Name: deploymentClassName}, class); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get DeploymentClass %q: %w", deploymentClassName, err)
	}
	return class.Spec.Controller == internal.ControllerName, nil
}

func artifactDeploymentsForClass(ctx context.Context, c client.Client, class *konfidencev1alpha1.DeploymentClass) []reconcile.Request {
	deployments := &konfidencev1alpha1.ArtifactDeploymentList{}
	if err := c.List(ctx, deployments); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for i := range deployments.Items {
		deployment := &deployments.Items[i]
		if deployment.Spec.Manifest.Type == class.Name {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(deployment)})
		}
	}
	return requests
}

func deploymentClassEventHandler(c client.Client, deploymentClassName string) handler.EventHandler {
	enqueue := func(ctx context.Context, q workqueue.TypedRateLimitingInterface[reconcile.Request], classes ...*konfidencev1alpha1.DeploymentClass) {
		requests := make(map[reconcile.Request]struct{})
		for _, class := range classes {
			if class.Name != deploymentClassName || class.Spec.Controller != internal.ControllerName {
				continue
			}
			for _, request := range artifactDeploymentsForClass(ctx, c, class) {
				requests[request] = struct{}{}
			}
		}
		for request := range requests {
			q.Add(request)
		}
	}

	return handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object.(*konfidencev1alpha1.DeploymentClass))
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q,
				e.ObjectOld.(*konfidencev1alpha1.DeploymentClass),
				e.ObjectNew.(*konfidencev1alpha1.DeploymentClass),
			)
		},
		DeleteFunc: func(ctx context.Context, e event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			enqueue(ctx, q, e.Object.(*konfidencev1alpha1.DeploymentClass))
		},
	}
}
