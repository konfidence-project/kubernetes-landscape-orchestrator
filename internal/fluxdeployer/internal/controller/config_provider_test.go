package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ConfigProvider", func() {
	var (
		ctx context.Context
		cp  *ConfigProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		cp = &ConfigProvider{
			Client: k8sClient,
		}
		for name, deploymentType := range map[string]string{
			"config-provider-helm":      "konfidence.cloud/helm",
			"config-provider-kustomize": "konfidence.cloud/kustomize",
		} {
			dc := &konfidencev1alpha1.DeploymentClass{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: konfidencev1alpha1.DeploymentClassSpec{
					Type:       deploymentType,
					Controller: deploymentclass.ControllerName,
				},
			}
			_ = k8sClient.Create(ctx, dc) // ignore already-exists
		}
	})

	Context("GetKubeConfigRef", func() {
		It("returns the Secret ref from a matching DeploymentTarget", func() {
			landscape := "cp-landscape-match"
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: landscape}}
			_ = k8sClient.Create(ctx, ns)

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "remote-kubeconfig", Namespace: landscape},
				Data:       map[string][]byte{"value": []byte("dummy")},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

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
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape)
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
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			dt := &konfidencev1alpha1.DeploymentTarget{
				ObjectMeta: metav1.ObjectMeta{Name: "local-target", Namespace: landscape},
				Spec: konfidencev1alpha1.DeploymentTargetSpec{
					Type: "konfidence.cloud/helm",
					Connection: konfidencev1alpha1.DeploymentTargetConnection{
						Type: "local",
					},
				},
			}
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(BeNil())
		})

		It("returns nil when no DeploymentTarget exists in the namespace", func() {
			ref, err := cp.GetKubeConfigRef(ctx, "nonexistent-landscape")
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(BeNil())
		})

		It("skips DeploymentTargets with unsupported types", func() {
			landscape := "cp-landscape-skip"
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: landscape}}
			_ = k8sClient.Create(ctx, ns)

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
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			ref, err := cp.GetKubeConfigRef(ctx, landscape)
			Expect(err).NotTo(HaveOccurred())
			Expect(ref).To(BeNil())
		})
	})

	It("GetTargetNamespace echoes landscape", func() {
		Expect(cp.GetTargetNamespace("staging")).To(Equal("staging"))
	})
})
