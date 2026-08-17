package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("ConfigProvider", func() {
	var (
		ctx context.Context
		cp  *ConfigProvider
		c   client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		c = fake.NewClientBuilder().WithScheme(scheme.Scheme).
			WithIndex(&konfidencev1alpha1.DeploymentTarget{}, ReadyDeploymentTargetTypeField, readyDeploymentTargetTypeIndex).
			Build()
		cp = &ConfigProvider{Client: c}
	})

	markReady := func(dt *konfidencev1alpha1.DeploymentTarget) {
		meta.SetStatusCondition(&dt.Status.Conditions, metav1.Condition{
			Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
			Status:             metav1.ConditionTrue,
			Reason:             "Accepted",
			ObservedGeneration: dt.Generation,
		})
	}

	Context("GetKubeConfigRef", func() {
		It("returns the Secret ref from a matching DeploymentTarget", func() {
			landscape := "cp-landscape-match"
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: landscape}}
			Expect(c.Create(ctx, ns)).To(Succeed())

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "remote-kubeconfig", Namespace: landscape},
				Data:       map[string][]byte{"value": []byte("dummy")},
			}
			Expect(c.Create(ctx, secret)).To(Succeed())

			dt := &konfidencev1alpha1.DeploymentTarget{
				ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: landscape},
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					Type: "konfidence.cloud/helm",
					Connection: konfidencev1alpha1.DeploymentTargetConnection{
						Type: "kubeconfig",
						Ref:  &konfidencev1alpha1.ConnectionRef{Kind: "Secret", Name: "remote-kubeconfig"},
					},
				},
			}
			markReady(dt)
			Expect(c.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape, "konfidence.cloud/helm")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).NotTo(BeNil())
			Expect(ref.SecretRef).NotTo(BeNil())
			Expect(ref.SecretRef.Name).To(Equal("remote-kubeconfig"))
			// Key is intentionally empty so Flux uses its default.
			Expect(ref.SecretRef.Key).To(BeEmpty())
		})

		It("returns nil for a local DeploymentTarget without a Secret reference", func() {
			landscape := "cp-landscape-local"
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: landscape}}
			Expect(c.Create(ctx, ns)).To(Succeed())

			dt := &konfidencev1alpha1.DeploymentTarget{
				ObjectMeta: metav1.ObjectMeta{Name: "local-target", Namespace: landscape},
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					Type: "konfidence.cloud/helm",
					Connection: konfidencev1alpha1.DeploymentTargetConnection{
						Type: "local",
					},
				},
			}
			markReady(dt)
			Expect(c.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape, "konfidence.cloud/helm")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(BeNil())
		})

		It("returns an error when no ready DeploymentTarget exists in the namespace", func() {
			landscape := "cp-landscape-not-ready"
			dt := &konfidencev1alpha1.DeploymentTarget{
				ObjectMeta: metav1.ObjectMeta{Name: "not-ready-target", Namespace: landscape},
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					Type:       "konfidence.cloud/helm",
					Connection: konfidencev1alpha1.DeploymentTargetConnection{Type: "local"},
				},
			}
			Expect(c.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape, "konfidence.cloud/helm")
			Expect(err).To(MatchError(&DeploymentTargetNotReadyError{
				Namespace:      landscape,
				DeploymentType: "konfidence.cloud/helm",
			}))
			Expect(ref).To(BeNil())
		})

		It("skips DeploymentTargets with unsupported types", func() {
			landscape := "cp-landscape-skip"
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: landscape}}
			Expect(c.Create(ctx, ns)).To(Succeed())

			dt := &konfidencev1alpha1.DeploymentTarget{
				ObjectMeta: metav1.ObjectMeta{Name: "other-target", Namespace: landscape},
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					Type: "some.other/deployer",
					Connection: konfidencev1alpha1.DeploymentTargetConnection{
						Type: "kubeconfig",
						Ref:  &konfidencev1alpha1.ConnectionRef{Kind: "Secret", Name: "some-secret"},
					},
				},
			}
			markReady(dt)
			Expect(c.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape, "konfidence.cloud/helm")
			Expect(err).To(HaveOccurred())
			Expect(ref).To(BeNil())
		})
	})

	It("GetTargetNamespace echoes landscape", func() {
		Expect(cp.GetTargetNamespace("staging")).To(Equal("staging"))
	})
})
