package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	controllermocks "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/controller/mocks"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func rawKustomizeOCIContent() runtime.RawExtension {
	return runtime.RawExtension{Raw: []byte(`{"type":"ociArtifact","imageReference":"host.example.com/kustomize/podinfo:v0.1.0"}`)}
}

var _ = Describe("KustomizeArtifactDeployment Controller", func() { //nolint:dupl
	var (
		mockCtrl     *gomock.Controller
		artifactMock *controllermocks.MockArtifactDeployer
		drMock       *controllermocks.MockStatusUpdater
		readyMock    *controllermocks.MockStatusUpdater
		ctx          context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		mockCtrl = gomock.NewController(GinkgoT())
		artifactMock = controllermocks.NewMockArtifactDeployer(mockCtrl)
		drMock = controllermocks.NewMockStatusUpdater(mockCtrl)
		readyMock = controllermocks.NewMockStatusUpdater(mockCtrl)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	newFixture := func(name string, resources []konfidencev1alpha1.OCMResource) (*KustomizeArtifactDeploymentReconciler, client.Client, *konfidencev1alpha1.ArtifactDeployment) { //nolint:lll
		d := &konfidencev1alpha1.ArtifactDeployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Generation: 3},
			Spec: konfidencev1alpha1.ArtifactDeploymentSpec{
				Manifest:  konfidencev1alpha1.ArtifactManifest{Type: manifestTypeKustomize},
				Component: konfidencev1alpha1.OCMComponent{Resources: resources},
			},
		}
		dc := &konfidencev1alpha1.DeploymentClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-kustomize"},
			Spec: konfidencev1alpha1.DeploymentClassSpec{
				Type:       manifestTypeKustomize,
				Controller: deploymentclass.ControllerName,
			},
		}
		cl := fake.NewClientBuilder().
			WithScheme(newTestScheme()).
			WithObjects(d, dc).
			WithStatusSubresource(&konfidencev1alpha1.ArtifactDeployment{}).
			Build()
		r := &KustomizeArtifactDeploymentReconciler{
			Client:                        cl,
			DeploymentResultStatusUpdater: drMock,
			ReadyConditionStatusUpdater:   readyMock,
			ArtifactDeployer:              artifactMock,
		}
		return r, cl, d
	}

	Context("When the deployment contains multiple kustomize resources", func() {
		It("patches Ready=False with reason MultipleKustomizeResources, returns an error, and never calls the sub-reconcilers", func() {
			r, cl, d := newFixture("multi-kustomize", []konfidencev1alpha1.OCMResource{
				{Name: "a", Type: ocmResourceTypeKustomize, Content: rawKustomizeOCIContent()},
				{Name: "b", Type: ocmResourceTypeKustomize, Content: rawKustomizeOCIContent()},
				{Name: "c", Type: "other"}, //nolint:goconst
			})
			artifactMock.EXPECT().Reconcile(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error {
					_, err := singleOCMResource(deployment, ocmResourceTypeKustomize, "MultipleKustomizeResources")
					return err
				}).Times(1)

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: d.Namespace, Name: d.Name},
			})

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`expected exactly one OCM resource of type "kustomize", found 2`))

			got := &konfidencev1alpha1.ArtifactDeployment{}
			Expect(cl.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: d.Name}, got)).To(Succeed())

			ready := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)
			Expect(ready).NotTo(BeNil(), "Ready condition should be set")
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal("MultipleKustomizeResources"))
			Expect(ready.Message).To(ContainSubstring("found 2"))
			Expect(ready.ObservedGeneration).To(Equal(d.Generation))
		})
	})

	Context("When the deployment contains exactly one kustomize resource", func() {
		It("invokes OCIRepository, Kustomization, and status updaters, resulting in Ready=True", func() {
			r, cl, d := newFixture("single-kustomize", []konfidencev1alpha1.OCMResource{
				{Name: "a", Type: ocmResourceTypeKustomize, Content: rawKustomizeOCIContent()},
				{Name: "b", Type: "other"}, //nolint:goconst
			})

			artifactMock.EXPECT().Reconcile(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			drMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			readyMock.EXPECT().MutateStatus(gomock.Any(), gomock.Any()).DoAndReturn(setReadyTrue).Times(1)

			_, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{Namespace: d.Namespace, Name: d.Name},
			})
			Expect(err).NotTo(HaveOccurred())

			got := &konfidencev1alpha1.ArtifactDeployment{}
			Expect(cl.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: d.Name}, got)).To(Succeed())
			Expect(meta.IsStatusConditionTrue(got.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)).To(BeTrue())
		})

		It("sets Ready=False when no ready DeploymentTarget exists", func() {
			r, cl, d := newFixture("kustomize-no-target", []konfidencev1alpha1.OCMResource{
				{Name: "a", Type: ocmResourceTypeKustomize, Content: rawKustomizeOCIContent()},
			})

			artifactMock.EXPECT().Reconcile(gomock.Any(), gomock.Any()).Return(
				&DeploymentTargetNotReadyError{Namespace: d.Namespace, DeploymentType: manifestTypeKustomize}).Times(1)

			_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: d.Namespace, Name: d.Name}})
			Expect(err).NotTo(HaveOccurred())

			got := &konfidencev1alpha1.ArtifactDeployment{}
			Expect(cl.Get(ctx, types.NamespacedName{Namespace: d.Namespace, Name: d.Name}, got)).To(Succeed())
			ready := meta.FindStatusCondition(got.Status.Conditions, konfidencev1alpha1.ArtifactDeploymentReadyCondition)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			Expect(ready.Reason).To(Equal(ArtifactDeploymentReasonDeploymentTargetNotReady))
		})
	})
})
