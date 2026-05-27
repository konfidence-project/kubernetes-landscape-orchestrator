package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	landscape "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const ActivationTaskExecutionControllerName = "kubernetes-activation-task-execution-controller"

// ActivationTaskExecutionReconciler reconciles an ActivationTaskExecution object
type ActivationTaskExecutionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const (
	XVectorId                       = "x-vector-id"
	HttpActivationTaskExecutionType = "http-k8s-service"
	Gateway                         = "gateway"
	GatewayNamespace                = "konfidence-system"
	DefaultDomain                   = "kden-showroom.msp03.shoot.gardener.cc-one.showroom.apeirora.eu"
)

var (
	Domain = GetEnv("TARGET_CLUSTER_DOMAIN", DefaultDomain)
)

// HTTPRouteConfig defines necessary configuration parameters to construct GatewayAPI httpRoute resources
type HTTPRouteConfig struct {
	HTTPRouteName string `json:"httpRouteName"`
	GatewayName   string `json:"gatewayName"`
	HostName      string `json:"hostName"`
	VectorID      string `json:"vectorId"`
	ServiceName   string `json:"serviceName"`
	Port          int32  `json:"port"`
}

type DeploymentSpec struct {
	K8sName      string               `json:"K8sName"`
	ServicePorts []corev1.ServicePort `json:"ServicePorts"`
}

// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=activationtaskexecutions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=activationtaskexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectoractivations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectoractivations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectordeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectordeployments/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ActivationTaskExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile started...")

	// get activationTaskExecution
	activationTaskExecution := &landscape.ActivationTaskExecution{}
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

func (r *ActivationTaskExecutionReconciler) reconcileActivationTaskExecution(ctx context.Context, req ctrl.Request, activationTaskExecution *landscape.ActivationTaskExecution) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling activationTaskExecution")

	// get vectorActivation
	vectorActivation := &landscape.VectorActivation{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      activationTaskExecution.Spec.VectorActivation,
	}, vectorActivation)
	if err != nil {
		return fmt.Errorf("unable to fetch vectorActivation: %w", err)
	}

	// parse httpRoute configurations from associated vectorDeployment
	httpRouteConfigs, err := r.parseHttpConfigs(ctx, req, activationTaskExecution, vectorActivation)
	if err != nil {
		return fmt.Errorf("unable to parse httpRoute configurations from vectorDeployment: %w", err)
	}

	// create httpRoutes based on configurations
	for _, httpRouteConfig := range httpRouteConfigs {
		_, err := r.getOrCreateHttpRoute(ctx, req, httpRouteConfig, vectorActivation, activationTaskExecution)
		if err != nil {
			return err
		}
	}

	// mark activationTaskExecution as successful
	meta.SetStatusCondition(&activationTaskExecution.Status.Conditions, metav1.Condition{
		Type:               landscape.ActivationTaskExecutionSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             landscape.ActivationTaskExecutionSucceeded,
		Message:            fmt.Sprintf("Successfully reconciled ActivationTaskExecution %s", activationTaskExecution.Name),
		ObservedGeneration: activationTaskExecution.Generation,
		LastTransitionTime: metav1.Now(),
	})

	log.Info("ActivationTaskExecution reconciled")
	return nil
}

func (r *ActivationTaskExecutionReconciler) getOrCreateHttpRoute(ctx context.Context, req ctrl.Request, httpRouteConfig HTTPRouteConfig, vectorActivation *landscape.VectorActivation, activationTaskExecution *landscape.ActivationTaskExecution) (*gwapiv1.HTTPRoute, error) {
	log := logf.FromContext(ctx)

	httpRoute := &gwapiv1.HTTPRoute{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      httpRouteConfig.HTTPRouteName,
	}, httpRoute)

	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("unable to fetch httpRoute: %w", err)
	}
	if err != nil && errors.IsNotFound(err) {
		log.Info("No matching httpRoute found. Creating a new one...")

		// create new httpRoute
		httpRoute, err = r.constructHttpRoute(req, httpRouteConfig, vectorActivation)
		if err != nil {
			return nil, fmt.Errorf("unable to construct httpRoute: %w", err)
		}

		if err := r.Create(ctx, httpRoute); err != nil {
			return nil, fmt.Errorf("unable to create httpRoute: %w", err)
		}
		msg := fmt.Sprintf("Created httpRoute %s", httpRoute.Name)
		r.Recorder.Event(activationTaskExecution, corev1.EventTypeNormal, "Created", msg)
		log.Info(msg)
	}

	return httpRoute, nil
}

func (r *ActivationTaskExecutionReconciler) constructHttpRoute(req ctrl.Request, httpRouteConfig HTTPRouteConfig, vectorActivation *landscape.VectorActivation) (*gwapiv1.HTTPRoute, error) {
	gatewayNamespace := gwapiv1.Namespace(GatewayNamespace)
	httpRoute := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteConfig.HTTPRouteName,
			Namespace: req.Namespace,
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name:      gwapiv1.ObjectName(httpRouteConfig.GatewayName),
						Namespace: &gatewayNamespace,
					},
				},
			},
			Hostnames: []gwapiv1.Hostname{
				gwapiv1.Hostname(httpRouteConfig.HostName),
			},
			Rules: []gwapiv1.HTTPRouteRule{
				{
					Filters: []gwapiv1.HTTPRouteFilter{
						{
							Type: gwapiv1.HTTPRouteFilterRequestHeaderModifier,
							RequestHeaderModifier: &gwapiv1.HTTPHeaderFilter{
								Add: []gwapiv1.HTTPHeader{
									{
										Name:  XVectorId,
										Value: httpRouteConfig.VectorID,
									},
								},
							},
						},
					},
					BackendRefs: []gwapiv1.HTTPBackendRef{
						{
							BackendRef: gwapiv1.BackendRef{
								BackendObjectReference: gwapiv1.BackendObjectReference{
									Name: gwapiv1.ObjectName(httpRouteConfig.ServiceName),
									Port: &httpRouteConfig.Port,
								},
							},
						},
					},
				},
			},
		},
	}

	// set vectorActivation as owner of the httpRoute
	if err := controllerutil.SetOwnerReference(vectorActivation, httpRoute, r.Scheme); err != nil {
		return nil, fmt.Errorf("unable to set owner reference for httpRoute: %w", err)
	}

	return httpRoute, nil
}

func (r *ActivationTaskExecutionReconciler) parseHttpConfigs(ctx context.Context, req ctrl.Request, activationTaskExecution *landscape.ActivationTaskExecution, vectorActivation *landscape.VectorActivation) ([]HTTPRouteConfig, error) {
	var httpRouteConfigs []HTTPRouteConfig
	vectorDeployment := &landscape.VectorDeployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: vectorActivation.Spec.VectorDeployment, Namespace: req.Namespace}, vectorDeployment); err != nil {
		return nil, fmt.Errorf("failed to get VectorDeployment %s: %w", vectorActivation.Spec.VectorDeployment, err)
	}

	for _, deploymentResult := range vectorDeployment.Status.DeploymentResults {
		if deploymentResult.Type == activationTaskExecution.Spec.Type {
			var deploymentSpec DeploymentSpec

			err := json.Unmarshal(deploymentResult.Spec.Raw, &deploymentSpec)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal deploymentResult %s for the port property: %w", deploymentResult.Name, err)
			}

			serviceName := deploymentSpec.K8sName
			hostName := fmt.Sprintf("%s.%s.%s", serviceName, vectorActivation.Spec.Stage, Domain)
			for _, servicePort := range deploymentSpec.ServicePorts {
				// for now just use the first service port
				httpRouteConfigs = append(httpRouteConfigs, HTTPRouteConfig{
					HTTPRouteName: fmt.Sprintf("%s-%s-%s", serviceName, vectorDeployment.Name, "activation"),
					GatewayName:   Gateway,
					HostName:      hostName,
					VectorID:      vectorDeployment.Name,
					ServiceName:   serviceName,
					Port:          servicePort.Port,
				})
				break
			}
		}
	}

	if len(httpRouteConfigs) == 0 {
		return nil, fmt.Errorf("no matching httpRoute configurations found in vectorDeployment %s", vectorDeployment.Name)
	}

	return httpRouteConfigs, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActivationTaskExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	activationTaskExecutionFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch o := obj.(type) {
		case *landscape.ActivationTaskExecution:
			return o.Spec.Type == HttpActivationTaskExecutionType
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&landscape.ActivationTaskExecution{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(activationTaskExecutionFilter).
		Named("activationTaskExecution").
		Complete(r)
}

func GetEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}
