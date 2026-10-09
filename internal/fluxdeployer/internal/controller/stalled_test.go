package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	controllermocks "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/controller/mocks"
	fluxmocks "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/mocks"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	stalledDeploymentName = "app"
	stalledNamespace      = "default"
)

func fluxStalledConditions(reason string, observedGeneration int64) []metav1.Condition {
	return []metav1.Condition{{
		Type:               fluxmeta.StalledCondition,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            "flux gave up",
		ObservedGeneration: observedGeneration,
		LastTransitionTime: metav1.Now(),
	}}
}

func fluxMeta(name string, generation int64) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: stalledNamespace, Generation: generation}
}

// waitingTarget is a DeploymentTarget that has lacked its kubeconfig Secret for waitingFor.
func waitingTarget(namespace, class string, waitingFor time.Duration) *konfidencev1alpha1.DeploymentTarget {
	return &konfidencev1alpha1.DeploymentTarget{
		ObjectMeta: metav1.ObjectMeta{Name: "remote", Namespace: namespace, Generation: 1},
		Spec: konfidencev1alpha1.DeploymentTargetSpec{
			DeploymentClassName: class,
			Connection: konfidencev1alpha1.DeploymentTargetConnection{
				Type: "kubeconfig",
				Ref:  &konfidencev1alpha1.ConnectionRef{Kind: "Secret", Name: "remote-kubeconfig"},
			},
		},
		Status: konfidencev1alpha1.DeploymentTargetStatus{Conditions: []metav1.Condition{{
			Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             internal.DeploymentTargetReasonSecretNotFound,
			Message:            `secret "remote-kubeconfig" not found`,
			ObservedGeneration: 1,
			LastTransitionTime: metav1.NewTime(time.Now().Add(-waitingFor)),
		}}},
	}
}

var _ = Describe("Stalled condition on ArtifactDeployments", func() {
	var (
		ctx       context.Context
		mockCtrl  *gomock.Controller
		drMock    *controllermocks.MockStatusUpdater
		readyMock *controllermocks.MockStatusUpdater
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		drMock = controllermocks.NewMockStatusUpdater(mockCtrl)
		readyMock = controllermocks.NewMockStatusUpdater(mockCtrl)
		readyMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	newDeployment := func(class, resourceType string, content runtime.RawExtension) *konfidencev1alpha1.ArtifactDeployment {
		return &konfidencev1alpha1.ArtifactDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: stalledDeploymentName, Namespace: stalledNamespace, Generation: 3},
			Spec: konfidencev1alpha1.ArtifactDeploymentSpec{
				Manifest: konfidencev1alpha1.ArtifactManifest{Type: class},
				Component: konfidencev1alpha1.OCMComponent{Resources: []konfidencev1alpha1.OCMResource{
					{Name: "a", Type: resourceType, Content: content},
				}},
			},
		}
	}

	newClient := func(class string, objs ...client.Object) client.Client {
		objs = append(objs, &konfidencev1alpha1.DeploymentClass{
			ObjectMeta: metav1.ObjectMeta{Name: class},
			Spec:       konfidencev1alpha1.DeploymentClassSpec{Controller: internal.ControllerName},
		})
		return fake.NewClientBuilder().
			WithScheme(newTestScheme()).
			WithObjects(objs...).
			WithStatusSubresource(&konfidencev1alpha1.ArtifactDeployment{}).
			Build()
	}

	helmReconciler := func(cl client.Client) *HelmArtifactDeploymentReconciler {
		repo := fluxmocks.NewMockFluxHelmReconciler(mockCtrl)
		repo.EXPECT().Reconcile(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		release := fluxmocks.NewMockFluxHelmReconciler(mockCtrl)
		release.EXPECT().Reconcile(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		return &HelmArtifactDeploymentReconciler{
			Client:                        cl,
			DeploymentResultStatusUpdater: drMock,
			ReadyConditionStatusUpdater:   readyMock,
			HelmRepositoryReconciler:      repo,
			HelmReleaseReconciler:         release,
		}
	}

	kustomizeReconciler := func(cl client.Client) *KustomizeArtifactDeploymentReconciler {
		oci := fluxmocks.NewMockFluxKustomizeReconciler(mockCtrl)
		oci.EXPECT().Reconcile(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		kustomization := fluxmocks.NewMockFluxKustomizeReconciler(mockCtrl)
		kustomization.EXPECT().Reconcile(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		return &KustomizeArtifactDeploymentReconciler{
			Client:                        cl,
			DeploymentResultStatusUpdater: drMock,
			ReadyConditionStatusUpdater:   readyMock,
			OCIRepositoryReconciler:       oci,
			KustomizationReconciler:       kustomization,
		}
	}

	// reconcileStalled runs one reconcile, asserts Ready was not written and returns the Stalled condition.
	reconcileStalled := func(r reconcile.Reconciler, cl client.Client) *metav1.Condition {
		key := client.ObjectKey{Namespace: stalledNamespace, Name: stalledDeploymentName}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		got := &konfidencev1alpha1.ArtifactDeployment{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)).To(BeNil(),
			"Stalled must not write Ready")
		stalled := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.StalledCondition)
		Expect(stalled).NotTo(BeNil(), "Stalled must be written on every completed evaluation")
		Expect(stalled.ObservedGeneration).To(Equal(int64(3)))
		return stalled
	}

	Context("Helm", func() {
		helmDeployment := func() *konfidencev1alpha1.ArtifactDeployment {
			return newDeployment(internal.DeploymentClassHelm, ocmResourceTypeHelmChart, rawHelmOCIContent())
		}

		It("writes Stalled=False when nothing blocks, clearing an earlier stall", func() {
			d := helmDeployment()
			d.Status.Conditions = []metav1.Condition{{
				Type: konfidencev1alpha1.StalledCondition, Status: metav1.ConditionTrue, Reason: "RetriesExceeded",
				ObservedGeneration: 2, LastTransitionTime: metav1.Now(),
			}}
			cl := newClient(internal.DeploymentClassHelm, d)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionFalse))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.StalledReasonNotStalled))
		})

		It("mirrors a Stalled HelmRelease with Flux's reason", func() {
			release := &helmv2.HelmRelease{ObjectMeta: fluxMeta(stalledDeploymentName, 2)}
			release.Status.Conditions = fluxStalledConditions("RetriesExceeded", 2)
			cl := newClient(internal.DeploymentClassHelm, helmDeployment(), release)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal("Stalled"))
			Expect(stalled.Message).To(Equal("HelmRelease app is stalled: flux gave up"))
		})

		It("ignores a Stalled condition left over from an older HelmRelease generation", func() {
			release := &helmv2.HelmRelease{ObjectMeta: fluxMeta(stalledDeploymentName, 3)}
			release.Status.Conditions = fluxStalledConditions("RetriesExceeded", 2)
			cl := newClient(internal.DeploymentClassHelm, helmDeployment(), release)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			Expect(reconcileStalled(helmReconciler(cl), cl).Status).To(Equal(metav1.ConditionFalse))
		})

		It("mirrors a Stalled HelmChart created for the release", func() {
			chart := &sourcev1.HelmChart{ObjectMeta: fluxMeta(stalledNamespace+"-"+stalledDeploymentName, 1)}
			chart.Status.Conditions = fluxStalledConditions("InvalidChartReference", 1)
			cl := newClient(internal.DeploymentClassHelm, helmDeployment(), chart)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal("Stalled"))
		})

		It("reports the source stage before the release stage", func() {
			repository := &sourcev1.HelmRepository{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
			repository.Status.Conditions = fluxStalledConditions("URLInvalid", 1)
			release := &helmv2.HelmRelease{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
			release.Status.Conditions = fluxStalledConditions("RetriesExceeded", 1)
			cl := newClient(internal.DeploymentClassHelm, helmDeployment(), repository, release)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			Expect(reconcileStalled(helmReconciler(cl), cl).Reason).To(Equal("Stalled"))
		})

		It("reports missing DeploymentTarget secrets before any Flux stall", func() {
			release := &helmv2.HelmRelease{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
			release.Status.Conditions = fluxStalledConditions("RetriesExceeded", 1)
			target := waitingTarget(stalledNamespace, internal.DeploymentClassHelm, 2*deploymentTargetSecretsDeadline)
			cl := newClient(internal.DeploymentClassHelm, helmDeployment(), release, target)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentTargetSecretsMissing))
		})

		It("reports an artifact without a Helm chart resource", func() {
			d := newDeployment(internal.DeploymentClassHelm, "ociImage", rawHelmOCIContent())
			cl := newClient(internal.DeploymentClassHelm, d)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonResourceInvalid))
			Expect(stalled.Message).To(ContainSubstring("no OCM resource of type"))
		})

		It("reports a Helm chart resource that cannot be mapped", func() {
			d := newDeployment(internal.DeploymentClassHelm, ocmResourceTypeHelmChart, runtime.RawExtension{Raw: []byte(`{}`)})
			cl := newClient(internal.DeploymentClassHelm, d)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonResourceInvalid))
			Expect(stalled.Message).To(ContainSubstring("failed to map OCM resource"))
		})

		It("reports more than one Helm chart resource", func() {
			d := helmDeployment()
			d.Spec.Component.Resources = append(d.Spec.Component.Resources, d.Spec.Component.Resources[0])
			d.Spec.Component.Resources[1].Name = "b"
			cl := newClient(internal.DeploymentClassHelm, d)

			key := client.ObjectKeyFromObject(d)
			_, err := helmReconciler(cl).Reconcile(ctx, ctrl.Request{NamespacedName: key})
			Expect(err).To(HaveOccurred())

			got := &konfidencev1alpha1.ArtifactDeployment{}
			Expect(cl.Get(ctx, key, got)).To(Succeed())
			stalled := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.StalledCondition)
			Expect(stalled).NotTo(BeNil())
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonResourceInvalid))
		})

		It("reports deployment results that share a (name, type)", func() {
			cl := newClient(internal.DeploymentClassHelm, helmDeployment())
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("%w: services %q and %q both declare it", errDeploymentResultNotUnique, "a", "b"))

			stalled := reconcileStalled(helmReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentResultNotUnique))
			Expect(stalled.Message).To(ContainSubstring(`services "a" and "b"`))
		})
	})

	It("pairs a duplicate deployment result stall with Ready=False from the real status updaters", func() {
		d := newDeployment(internal.DeploymentClassHelm, ocmResourceTypeHelmChart, rawHelmOCIContent())
		d.Status.Conditions = []metav1.Condition{
			condition(konfidencev1alpha1.ArtifactFetchedCondition, metav1.ConditionTrue, "Fetched"),
			condition(konfidencev1alpha1.ArtifactDeployedCondition, metav1.ConditionTrue, "Deployed"),
			condition(konfidencev1alpha1.AppHealthyCondition, metav1.ConditionTrue, "Healthy"),
			condition(konfidencev1alpha1.DeploymentResultCreatedCondition, metav1.ConditionTrue, "Created"),
			condition(konfidencev1alpha1.ArtifactDeploymentReadyCondition, metav1.ConditionTrue, "Ready"),
		}
		duplicate := func(name string) *corev1.Service {
			return &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name: name, Namespace: stalledNamespace,
				Labels:      map[string]string{labelArtifactDeployment: stalledDeploymentName},
				Annotations: map[string]string{annotationDeploymentResult: "web"},
			}}
		}
		cl := newClient(internal.DeploymentClassHelm, d, duplicate("web-a"), duplicate("web-b"))
		r := helmReconciler(cl)
		r.DeploymentResultStatusUpdater = &DeploymentResultStatusUpdater{Client: cl}
		r.ReadyConditionStatusUpdater = &ReadyConditionStatusUpdater{}

		key := client.ObjectKeyFromObject(d)
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		got := &konfidencev1alpha1.ArtifactDeployment{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		stalled := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentResultNotUnique))
		Expect(meta.IsStatusConditionFalse(got.Status.Conditions, konfidencev1alpha1.DeploymentResultCreatedCondition)).To(BeTrue())
		ready := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal(reasonDeploymentResultInvalid))
	})

	It("reports a Flux stall on a Ready deployment and keeps it Ready", func() {
		d := newDeployment(internal.DeploymentClassHelm, ocmResourceTypeHelmChart, rawHelmOCIContent())
		d.Status.Conditions = []metav1.Condition{
			condition(konfidencev1alpha1.ArtifactFetchedCondition, metav1.ConditionTrue, "Fetched"),
			condition(konfidencev1alpha1.ArtifactDeployedCondition, metav1.ConditionTrue, "Deployed"),
			condition(konfidencev1alpha1.AppHealthyCondition, metav1.ConditionTrue, "Healthy"),
			condition(konfidencev1alpha1.DeploymentResultCreatedCondition, metav1.ConditionTrue, "Created"),
		}
		repository := &sourcev1.HelmRepository{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
		repository.Status.Conditions = fluxStalledConditions("URLInvalid", 1)
		cl := newClient(internal.DeploymentClassHelm, d, repository)
		r := helmReconciler(cl)
		r.ReadyConditionStatusUpdater = &ReadyConditionStatusUpdater{}
		drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

		key := client.ObjectKeyFromObject(d)
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		got := &konfidencev1alpha1.ArtifactDeployment{}
		Expect(cl.Get(ctx, key, got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)).To(BeTrue())
		stalled := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.StalledCondition)
		Expect(stalled).NotTo(BeNil())
		Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
		Expect(stalled.Reason).To(Equal("Stalled"))
	})

	Context("Kustomize", func() {
		kustomizeDeployment := func() *konfidencev1alpha1.ArtifactDeployment {
			return newDeployment(internal.DeploymentClassKustomize, ocmResourceTypeKustomize, rawKustomizeOCIContent())
		}

		It("mirrors a Stalled Kustomization with Flux's reason", func() {
			kustomization := &kustomizev1.Kustomization{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
			kustomization.Status.Conditions = fluxStalledConditions("AccessDenied", 1)
			cl := newClient(internal.DeploymentClassKustomize, kustomizeDeployment(), kustomization)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(kustomizeReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal("Stalled"))
			Expect(stalled.Message).To(Equal("Kustomization app is stalled: flux gave up"))
		})

		It("reports a Stalled OCIRepository before a Stalled Kustomization", func() {
			repository := &sourcev1.OCIRepository{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
			repository.Status.Conditions = fluxStalledConditions("URLInvalid", 1)
			kustomization := &kustomizev1.Kustomization{ObjectMeta: fluxMeta(stalledDeploymentName, 1)}
			kustomization.Status.Conditions = fluxStalledConditions("AccessDenied", 1)
			cl := newClient(internal.DeploymentClassKustomize, kustomizeDeployment(), repository, kustomization)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			Expect(reconcileStalled(kustomizeReconciler(cl), cl).Reason).To(Equal("Stalled"))
		})

		It("reports a Kustomize resource that cannot be mapped", func() {
			d := newDeployment(internal.DeploymentClassKustomize, ocmResourceTypeKustomize, runtime.RawExtension{Raw: []byte(`{}`)})
			cl := newClient(internal.DeploymentClassKustomize, d)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			stalled := reconcileStalled(kustomizeReconciler(cl), cl)
			Expect(stalled.Status).To(Equal(metav1.ConditionTrue))
			Expect(stalled.Reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonResourceInvalid))
		})

		It("writes Stalled=False when nothing blocks", func() {
			cl := newClient(internal.DeploymentClassKustomize, kustomizeDeployment())
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil)

			Expect(reconcileStalled(kustomizeReconciler(cl), cl).Status).To(Equal(metav1.ConditionFalse))
		})
	})
})

// These run against the envtest API server so the Ready condition's lastTransitionTime, the deadline's clock,
// goes through a real status round trip.
var _ = Describe("deploymentTargetSecretsStall", func() {
	const class = "sample.konfidence.cloud"

	var (
		ctx        context.Context
		namespace  string
		deployment *konfidencev1alpha1.ArtifactDeployment
	)

	BeforeEach(func() {
		ctx = context.Background()
		namespace = fmt.Sprintf("target-secrets-%d", time.Now().UnixNano())
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})).To(Succeed())
		deployment = &konfidencev1alpha1.ArtifactDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: stalledDeploymentName, Namespace: namespace},
			Spec:       konfidencev1alpha1.ArtifactDeploymentSpec{Manifest: konfidencev1alpha1.ArtifactManifest{Type: class}},
		}
	})

	createTarget := func(target *konfidencev1alpha1.DeploymentTarget) *konfidencev1alpha1.DeploymentTarget {
		status := target.Status
		Expect(k8sClient.Create(ctx, target)).To(Succeed())
		target.Status = status
		target.Status.Conditions[0].ObservedGeneration = target.Generation
		Expect(k8sClient.Status().Update(ctx, target)).To(Succeed())
		return target
	}

	It("reports a target that has waited for its Secret past the deadline", func() {
		createTarget(waitingTarget(namespace, class, deploymentTargetSecretsDeadline+time.Minute))

		cause, err := deploymentTargetSecretsStall(ctx, k8sClient, deployment, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(cause).NotTo(BeNil())
		Expect(cause.reason).To(Equal(konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentTargetSecretsMissing))
		Expect(cause.message).To(ContainSubstring(`DeploymentTarget remote has had no usable kubeconfig Secret`))
		Expect(cause.message).To(ContainSubstring(`secret "remote-kubeconfig" not found`))
	})

	It("reports a Ready deployment too, since it cannot roll out anything new", func() {
		createTarget(waitingTarget(namespace, class, deploymentTargetSecretsDeadline+time.Minute))
		deployment.Status.Conditions = []metav1.Condition{
			condition(konfidencev1alpha1.ArtifactDeploymentReadyCondition, metav1.ConditionTrue, "Ready"),
		}

		cause, err := deploymentTargetSecretsStall(ctx, k8sClient, deployment, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(cause).NotTo(BeNil())
	})

	It("waits until the deadline has passed", func() {
		createTarget(waitingTarget(namespace, class, time.Minute))

		cause, err := deploymentTargetSecretsStall(ctx, k8sClient, deployment, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(cause).To(BeNil())
	})

	It("does not report when another target for the class is ready", func() {
		createTarget(waitingTarget(namespace, class, deploymentTargetSecretsDeadline+time.Minute))
		ready := waitingTarget(namespace, class, time.Hour)
		ready.Name = "local"
		ready.Spec.Connection = konfidencev1alpha1.DeploymentTargetConnection{Type: "local"}
		ready.Status.Conditions[0].Status = metav1.ConditionTrue
		ready.Status.Conditions[0].Reason = "Accepted"
		createTarget(ready)

		cause, err := deploymentTargetSecretsStall(ctx, k8sClient, deployment, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(cause).To(BeNil())
	})

	It("ignores targets that fail for a reason other than a missing Secret", func() {
		target := waitingTarget(namespace, class, deploymentTargetSecretsDeadline+time.Minute)
		target.Status.Conditions[0].Reason = "InvalidKubeconfig"
		createTarget(target)

		cause, err := deploymentTargetSecretsStall(ctx, k8sClient, deployment, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(cause).To(BeNil())
	})

	It("ignores a Ready condition written for an older target generation", func() {
		target := createTarget(waitingTarget(namespace, class, deploymentTargetSecretsDeadline+time.Minute))
		target.Spec.Connection.Ref.Name = "fixed-kubeconfig"
		Expect(k8sClient.Update(ctx, target)).To(Succeed())

		cause, err := deploymentTargetSecretsStall(ctx, k8sClient, deployment, time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(cause).To(BeNil())
	})
})
