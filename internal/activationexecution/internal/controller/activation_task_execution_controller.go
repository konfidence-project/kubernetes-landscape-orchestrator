package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentresult"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const ActivationTaskExecutionControllerName = "kubernetes-activation-task-execution-controller"

// ActivationTaskExecutionReconciler reconciles an ActivationTaskExecution object. On activation it maintains a single
// per-stage HTTPRoute on the configured Gateway: match the x-konfidence-stage header plus the request path (or the
// x-konfidence-service header), inject the routing-key header for the active vector, and back it with the vector's
// deployment-result Services.
type ActivationTaskExecutionReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Recorder  events.EventRecorder
	Landscape *LandscapeClientProvider
}

// gatewayRouteConfig is the optional per-landscape configuration carried verbatim in the ActivationTaskRegistration
// (hence the ActivationTaskExecution) Spec.Spec.
type gatewayRouteConfig struct {
	// GatewayName is the Gateway the HTTPRoutes attach to; defaults to konfidence-router.
	GatewayName string `json:"gatewayName,omitempty"`
	// GatewayNamespace is the Gateway namespace; defaults to the landscape namespace.
	GatewayNamespace string `json:"gatewayNamespace,omitempty"`
	// DeploymentClassName selects the Kubernetes DeploymentTarget to apply routes through when the landscape has more
	// than one connection; optional otherwise.
	DeploymentClassName string `json:"deploymentClassName,omitempty"`
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=activationtaskexecutions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=activationtaskexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectoractivations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectoractivations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectordeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectordeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=stages,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=stageversions,verbs=get;list;watch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ActivationTaskExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile started...")

	activationTaskExecution := &konfidencev1alpha1.ActivationTaskExecution{}
	if err := r.Get(ctx, req.NamespacedName, activationTaskExecution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalActivationTaskExecution := activationTaskExecution.DeepCopy()
	patch := client.MergeFrom(originalActivationTaskExecution)
	err := r.reconcileActivationTaskExecution(ctx, req, activationTaskExecution)

	if !reflect.DeepEqual(activationTaskExecution.Status, originalActivationTaskExecution.Status) {
		if patchError := r.Client.Status().Patch(ctx, activationTaskExecution, patch); patchError != nil {
			patchErrorMessage := "unable to update activationTaskExecution status"

			if err != nil {
				reconcileError := fmt.Errorf("an error occurred while reconciling activationTaskExecution: %w", err)
				return ctrl.Result{}, fmt.Errorf("%s: %w; %w", patchErrorMessage, patchError, reconcileError)
			}

			return ctrl.Result{}, fmt.Errorf("%s: %w", patchErrorMessage, patchError)
		}
	}

	return ctrl.Result{}, err
}

func (r *ActivationTaskExecutionReconciler) reconcileActivationTaskExecution(
	ctx context.Context, req ctrl.Request, activationTaskExecution *konfidencev1alpha1.ActivationTaskExecution,
) error {
	log := logf.FromContext(ctx)

	vectorActivation := &konfidencev1alpha1.VectorActivation{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      activationTaskExecution.Spec.VectorActivation,
	}, vectorActivation); err != nil {
		return fmt.Errorf("unable to fetch vectorActivation: %w", err)
	}

	// A superseded execution (an older activation re-reconciled, e.g. on restart) must not revert the stage's route.
	superseded, err := r.isSuperseded(ctx, req.Namespace, vectorActivation)
	if err != nil {
		return err
	}
	if superseded {
		log.Info("skipping superseded activationTaskExecution", "stage", vectorActivation.Spec.Stage)
		r.markSucceeded(activationTaskExecution)
		return nil
	}

	config, err := parseGatewayRouteConfig(activationTaskExecution)
	if err != nil {
		return err
	}

	vectorDeployment := &konfidencev1alpha1.VectorDeployment{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      vectorActivation.Spec.VectorDeployment,
	}, vectorDeployment); err != nil {
		return fmt.Errorf("failed to get VectorDeployment %s: %w", vectorActivation.Spec.VectorDeployment, err)
	}

	rules := buildRouteRules(vectorActivation.Spec.Stage, vectorDeployment)
	if len(rules) == 0 {
		log.Info("no routable deployment results, skipping route", "stage", vectorActivation.Spec.Stage)
		r.markSucceeded(activationTaskExecution)
		return nil
	}

	// Bind the route lifecycle to the Stage before creating it: the route may live on another cluster, so cleanup is
	// driven by a Stage finalizer rather than owner references.
	if err := r.ensureStageFinalizer(ctx, req.Namespace, vectorActivation.Spec.Stage); err != nil {
		return err
	}

	landscapeClient, err := r.Landscape.ClientFor(ctx, req.Namespace, config.DeploymentClassName)
	if err != nil {
		return fmt.Errorf("resolve landscape client: %w", err)
	}

	if err := r.applyStageRoute(ctx, landscapeClient, req.Namespace, vectorActivation.Spec.Stage, config, rules); err != nil {
		return err
	}

	r.Recorder.Eventf(activationTaskExecution, nil, corev1.EventTypeNormal, "RouteApplied", "RouteApplied",
		"Applied HTTPRoute for stage %s", vectorActivation.Spec.Stage)
	r.markSucceeded(activationTaskExecution)
	log.Info("ActivationTaskExecution reconciled", "stage", vectorActivation.Spec.Stage)
	return nil
}

// isSuperseded reports whether a newer StageVersion is already active for the stage than the one this execution
// activates. The stage's ActiveStageVersion is only set after an activation succeeds, so an in-flight flip (active
// still points at the previous version) is not superseded; only a strictly newer active version supersedes.
func (r *ActivationTaskExecutionReconciler) isSuperseded(
	ctx context.Context, namespace string, vectorActivation *konfidencev1alpha1.VectorActivation,
) (bool, error) {
	stage := &konfidencev1alpha1.Stage{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: vectorActivation.Spec.Stage}, stage); err != nil {
		if errors.IsNotFound(err) {
			// The stage is gone (being deleted); cleanup is handled by the finalizer. Treat as superseded.
			return true, nil
		}
		return false, fmt.Errorf("get Stage %s: %w", vectorActivation.Spec.Stage, err)
	}

	active := stage.Status.ActiveStageVersion
	if active == nil || active.Name == vectorActivation.Spec.StageVersion {
		return false, nil
	}

	mine := &konfidencev1alpha1.StageVersion{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: vectorActivation.Spec.StageVersion}, mine); err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("get StageVersion %s: %w", vectorActivation.Spec.StageVersion, err)
	}
	activeSV := &konfidencev1alpha1.StageVersion{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: active.Name}, activeSV); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get active StageVersion %s: %w", active.Name, err)
	}

	return mine.CreationTimestamp.Before(&activeSV.CreationTimestamp), nil
}

func (r *ActivationTaskExecutionReconciler) ensureStageFinalizer(ctx context.Context, namespace, stageName string) error {
	stage := &konfidencev1alpha1.Stage{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: stageName}, stage); err != nil {
		return fmt.Errorf("get Stage %s: %w", stageName, err)
	}
	if controllerutil.ContainsFinalizer(stage, stageRouteFinalizer) {
		return nil
	}
	patch := client.MergeFrom(stage.DeepCopy())
	controllerutil.AddFinalizer(stage, stageRouteFinalizer)
	if err := r.Patch(ctx, stage, patch); err != nil {
		return fmt.Errorf("add route finalizer to Stage %s: %w", stageName, err)
	}
	return nil
}

func (r *ActivationTaskExecutionReconciler) applyStageRoute(
	ctx context.Context,
	landscapeClient client.Client,
	namespace, stage string,
	config gatewayRouteConfig,
	rules []gwapiv1.HTTPRouteRule,
) error {
	parentRef := gwapiv1.ParentReference{Name: gwapiv1.ObjectName(config.GatewayName)}
	// An unset namespace binds to a Gateway in the route's own (landscape) namespace.
	if config.GatewayNamespace != "" {
		ns := gwapiv1.Namespace(config.GatewayNamespace)
		parentRef.Namespace = &ns
	}

	route := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      StageRouteName(stage),
			Namespace: namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, landscapeClient, route, func() error {
		route.Spec = gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{parentRef},
			},
			Rules: rules,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply HTTPRoute for stage %s: %w", stage, err)
	}
	return nil
}

// buildRouteRules builds one rule per http-k8s-service deployment result. Each rule matches the stage header plus the
// service header (x-konfidence-service=<result-name>), injects the routing-key header for the active vector, and backs
// the result's Service. Selection is header-only and therefore authoritative: the admin edge sets x-konfidence-service
// (e.g. derived from the request path). Gateway API ranks path above headers and cannot match a missing header, so a
// path match here could never be overridden by the service header.
func buildRouteRules(stage string, vectorDeployment *konfidencev1alpha1.VectorDeployment) []gwapiv1.HTTPRouteRule {
	type routeTarget struct {
		name    string
		service string
		port    gwapiv1.PortNumber
	}
	var targets []routeTarget

	for _, deploymentResults := range vectorDeployment.Status.DeploymentResults {
		for _, result := range deploymentResults {
			// TODO: gate north-south exposure on an explicit ingress whitelist opt-in. Today every
			// http-k8s-service deployment result is exposed; being an (east-west) deployment result must
			// not auto-expose a service to the edge. Add the opt-in filter (continue unless whitelisted)
			// here once the annotation/field is decided.
			if result.Type != deploymentresult.TypeHTTPK8sService {
				continue
			}
			var spec deploymentresult.ServiceSpec
			if err := json.Unmarshal(result.Spec.Raw, &spec); err != nil {
				continue
			}
			if len(spec.ServicePorts) == 0 {
				continue
			}
			targets = append(targets, routeTarget{name: result.Name, service: spec.K8sName, port: spec.ServicePorts[0].Port})
		}
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })

	stageHeader := gwapiv1.HTTPHeaderMatch{Name: headerStage, Value: stage}

	rules := make([]gwapiv1.HTTPRouteRule, 0, len(targets))
	for _, t := range targets {
		port := t.port
		rules = append(rules, gwapiv1.HTTPRouteRule{
			Matches: []gwapiv1.HTTPRouteMatch{
				{
					Headers: []gwapiv1.HTTPHeaderMatch{stageHeader, {Name: headerService, Value: t.name}},
				},
			},
			Filters: []gwapiv1.HTTPRouteFilter{{
				Type: gwapiv1.HTTPRouteFilterRequestHeaderModifier,
				RequestHeaderModifier: &gwapiv1.HTTPHeaderFilter{
					// Overwrite (not append) the routing key so a client-supplied value cannot be
					// spoofed; strip the match-only headers so they do not leak to the backend.
					Set:    []gwapiv1.HTTPHeader{{Name: headerRoutingKey, Value: vectorDeployment.Name}},
					Remove: []string{headerStage, headerService},
				},
			}},
			BackendRefs: []gwapiv1.HTTPBackendRef{{
				BackendRef: gwapiv1.BackendRef{
					BackendObjectReference: gwapiv1.BackendObjectReference{
						Name: gwapiv1.ObjectName(t.service),
						Port: &port,
					},
				},
			}},
		})
	}
	return rules
}

func parseGatewayRouteConfig(activationTaskExecution *konfidencev1alpha1.ActivationTaskExecution) (gatewayRouteConfig, error) {
	config := gatewayRouteConfig{}
	if raw := activationTaskExecution.Spec.Spec.Raw; len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return config, fmt.Errorf("parse gateway-route config: %w", err)
		}
	}
	if config.GatewayName == "" {
		config.GatewayName = defaultGatewayName
	}
	// An empty gateway namespace resolves to the landscape namespace at apply time.
	return config, nil
}

func (r *ActivationTaskExecutionReconciler) markSucceeded(activationTaskExecution *konfidencev1alpha1.ActivationTaskExecution) {
	meta.SetStatusCondition(&activationTaskExecution.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.ActivationTaskExecutionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             konfidencev1alpha1.ActivationTaskExecutionSucceeded,
		Message:            fmt.Sprintf("Successfully reconciled ActivationTaskExecution %s", activationTaskExecution.Name),
		ObservedGeneration: activationTaskExecution.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

// StageRouteName is the deterministic per-stage HTTPRoute name.
func StageRouteName(stage string) string {
	return stageRouteNamePrefix + stage
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActivationTaskExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	activationTaskExecutionFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch o := obj.(type) {
		case *konfidencev1alpha1.ActivationTaskExecution:
			return o.Spec.Type == TypeGatewayRoute
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.ActivationTaskExecution{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		WithEventFilter(activationTaskExecutionFilter).
		Named("activationTaskExecution").
		Complete(r)
}
