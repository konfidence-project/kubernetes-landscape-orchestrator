// Package vectordata hosts the Kubernetes-runtime implementor for the Konfidence VectorData CRD defined in
// `github.com/konfidence-project/konfidence/api/v1alpha1`. The Star vector-deployment-controller emits a
// runtime-agnostic VectorData CR (with the OCM-resolved authored bytes and aggregated DeploymentResults). This
// package watches those CRs and materialises them as a Kubernetes ConfigMap in the same (landscape) namespace,
// using the data layout the in-process `cmd/vectordata` service consumes:
//
//   - features.json                                — the authored "features" subset of the OCM envelope.
//   - authored.json                                — the authored "authored" subset of the OCM envelope.
//   - deploymentResults.<componentBasename>.json   — one key per artifact, value = the DeploymentResult.Spec JSON.
//
// The ConfigMap is named `vector-data-<vectorDataName>` (matching the `ConfigMapPrefix` the configuration service
// expects). On VectorData deletion an explicit finalizer drives ConfigMap teardown so cleanup is deterministic
// rather than relying solely on Kubernetes garbage collection.
package vectordata

import (
	"github.com/go-logr/logr"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/vectordata/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// OperatorFlagName is the `--controllers` flag value that enables this sub-controller in the orchestrator binary.
const OperatorFlagName = "VectorData"

// SetupControllers registers the VectorData reconciler with the manager.
func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := (&controller.VectorDataReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder(controller.VectorDataControllerName),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "VectorData")
		return err
	}
	return nil
}
