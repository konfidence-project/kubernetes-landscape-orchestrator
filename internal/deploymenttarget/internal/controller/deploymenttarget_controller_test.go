package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// minimalValidKubeconfig is a syntactically valid kubeconfig that passes clientcmd.RESTConfigFromKubeConfig.
const minimalValidKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://example.com:6443
    insecure-skip-tls-verify: true
  name: test-cluster
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
current-context: test-context
users:
- name: test-user
  user:
    token: test-token
`

var _ = Describe("DeploymentTargetReconciler", func() {
	const (
		namespace = "test-landscape"
		timeout   = 5 * time.Second
		interval  = 100 * time.Millisecond
	)

	BeforeEach(func() {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		_ = k8sClient.Create(ctx, ns) // ignore already-exists
		for name, deploymentType := range map[string]string{
			"test-helm":      "konfidence.cloud/helm",
			"test-kustomize": "konfidence.cloud/kustomize",
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

	newDeploymentTarget := func(name, deploymentType, connectionType, secretName string) *konfidencev1alpha1.DeploymentTarget {
		return &konfidencev1alpha1.DeploymentTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: konfidencev1alpha1.DeploymentTargetSpec{
				Type: deploymentType,
				Connection: konfidencev1alpha1.DeploymentTargetConnection{
					Type: connectionType,
					Ref: &konfidencev1alpha1.ConnectionRef{
						Kind: "Secret",
						Name: secretName,
					},
				},
			},
		}
	}

	newKubeconfigSecret := func(name string, data map[string][]byte) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Data: data,
		}
	}

	getCondition := func(name, condType string) *metav1.Condition {
		dt := &konfidencev1alpha1.DeploymentTarget{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, dt)
		if err != nil {
			return nil
		}
		return meta.FindStatusCondition(dt.Status.Conditions, condType)
	}

	Describe("with a valid kubeconfig Secret", func() {
		It("should set Ready=True", func() {
			secret := newKubeconfigSecret("valid-kubeconfig", map[string][]byte{
				"value": []byte(minimalValidKubeconfig),
			})
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			dt := newDeploymentTarget("dt-valid", "konfidence.cloud/helm", "kubeconfig", "valid-kubeconfig")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-valid", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", "Accepted"),
			))
		})
	})

	Describe("with a local connection", func() {
		It("should set Ready=True without a Secret reference", func() {
			dt := newDeploymentTarget("dt-local", "konfidence.cloud/helm", "local", "")
			dt.Spec.Connection.Ref = nil
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-local", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionTrue),
				HaveField("Reason", DeploymentTargetReasonAccepted),
			))
		})
	})

	Describe("with a Secret using the value.yaml key", func() {
		It("should set Ready=True", func() {
			secret := newKubeconfigSecret("valid-kubeconfig-yaml", map[string][]byte{
				"value.yaml": []byte(minimalValidKubeconfig),
			})
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			dt := newDeploymentTarget("dt-valid-yaml", "konfidence.cloud/kustomize", "kubeconfig", "valid-kubeconfig-yaml")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-valid-yaml", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionTrue),
			))
		})
	})

	Describe("when the Secret does not exist", func() {
		It("should set Ready=False with SecretNotFound", func() {
			dt := newDeploymentTarget("dt-missing-secret", "konfidence.cloud/helm", "kubeconfig", "does-not-exist")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-missing-secret", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "SecretNotFound"),
			))
		})
	})

	Describe("when the Secret has no kubeconfig key", func() {
		It("should set Ready=False with InvalidSecret", func() {
			secret := newKubeconfigSecret("empty-secret", map[string][]byte{
				"something-else": []byte("data"),
			})
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			dt := newDeploymentTarget("dt-invalid-secret", "konfidence.cloud/helm", "kubeconfig", "empty-secret")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-invalid-secret", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "InvalidSecret"),
			))
		})
	})

	Describe("when the Secret contains an invalid kubeconfig", func() {
		It("should set Ready=False with InvalidKubeconfig", func() {
			secret := newKubeconfigSecret("bad-kubeconfig", map[string][]byte{
				"value": []byte("this is not a kubeconfig"),
			})
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			dt := newDeploymentTarget("dt-bad-kubeconfig", "konfidence.cloud/helm", "kubeconfig", "bad-kubeconfig")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-bad-kubeconfig", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "InvalidKubeconfig"),
			))
		})
	})

	Describe("when connection type is not kubeconfig", func() {
		It("should set Ready=False with UnsupportedConnectionType", func() {
			dt := newDeploymentTarget("dt-unsupported-conn", "konfidence.cloud/helm", "aws-credentials", "some-secret")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			Eventually(func() *metav1.Condition {
				return getCondition("dt-unsupported-conn", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, timeout, interval).Should(And(
				Not(BeNil()),
				HaveField("Status", metav1.ConditionFalse),
				HaveField("Reason", "UnsupportedConnectionType"),
			))
		})
	})

	Describe("when spec.type is not supported by this controller", func() {
		It("should not set any condition", func() {
			dt := newDeploymentTarget("dt-unknown-type", "some.other/deployer", "kubeconfig", "some-secret")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			// Give reconciler time to (not) act
			Consistently(func() *metav1.Condition {
				return getCondition("dt-unknown-type", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, 1*time.Second, interval).Should(BeNil())
		})
	})
})
