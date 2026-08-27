package controller

import (
	"time"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		_ = k8sClient.Create(ctx, ns)
		for name := range internal.KnownClasses {
			dc := &konfidencev1alpha1.DeploymentClass{
				ObjectMeta: metav1.ObjectMeta{Name: name},
				Spec: konfidencev1alpha1.DeploymentClassSpec{
					Controller: internal.ControllerName,
				},
			}
			_ = k8sClient.Create(ctx, dc)
		}
	})

	newDeploymentTarget := func(name, deploymentClassName, connectionType, secretName string) *konfidencev1alpha1.DeploymentTarget {
		return &konfidencev1alpha1.DeploymentTarget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: konfidencev1alpha1.DeploymentTargetSpec{
				DeploymentClassName: deploymentClassName,
				Connection: konfidencev1alpha1.DeploymentTargetConnection{
					Type: connectionType,
					Ref: &konfidencev1alpha1.ConnectionRef{
						Kind: kindSecret,
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
				acceptedKubeconfigKeys[0]: []byte(minimalValidKubeconfig),
			})
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			dt := newDeploymentTarget("dt-valid", "helm.konfidence.cloud", "kubeconfig", "valid-kubeconfig")
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
			dt := newDeploymentTarget("dt-local", "helm.konfidence.cloud", "local", "")
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

			dt := newDeploymentTarget("dt-valid-yaml", "kustomize.konfidence.cloud", "kubeconfig", "valid-kubeconfig-yaml")
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
			dt := newDeploymentTarget("dt-missing-secret", "helm.konfidence.cloud", "kubeconfig", "does-not-exist")
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

			dt := newDeploymentTarget("dt-invalid-secret", "helm.konfidence.cloud", "kubeconfig", "empty-secret")
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
				acceptedKubeconfigKeys[0]: []byte("this is not a kubeconfig"),
			})
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			dt := newDeploymentTarget("dt-bad-kubeconfig", "helm.konfidence.cloud", "kubeconfig", "bad-kubeconfig")
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

	Describe("when connection type is not known", func() {
		It("should set Ready=False with UnsupportedConnectionType", func() {
			dt := newDeploymentTarget("dt-unsupported-conn", "helm.konfidence.cloud", "aws-credentials", "some-secret")
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

	Describe("when spec.deploymentClassName is not supported by this controller", func() {
		It("should not set any condition", func() {
			dt := newDeploymentTarget("dt-unknown-type", "deployer.some.other", "kubeconfig", "some-secret")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())

			// Give reconciler time to (not) act
			Consistently(func() *metav1.Condition {
				return getCondition("dt-unknown-type", konfidencev1alpha1.DeploymentTargetReadyCondition)
			}, 5*time.Second, interval).Should(BeNil())
		})
	})

	Describe("when the DeploymentClass belongs to another deployer", func() {
		It("does not modify the DeploymentTarget status", func() {
			class := &konfidencev1alpha1.DeploymentClass{
				ObjectMeta: metav1.ObjectMeta{Name: "foreign.example.com"},
				Spec: konfidencev1alpha1.DeploymentClassSpec{
					Controller: "example.com/other-deployer",
				},
			}
			Expect(k8sClient.Create(ctx, class)).To(Succeed())

			dt := newDeploymentTarget("dt-foreign", class.Name, "kubeconfig", "foreign-secret")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())
			dt.Status.Conditions = []metav1.Condition{{
				Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             "AcceptedByOtherDeployer",
				LastTransitionTime: metav1.Now(),
			}}
			Expect(k8sClient.Status().Update(ctx, dt)).To(Succeed())

			reconciler := &DeploymentTargetReconciler{Client: k8sClient}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(dt)})
			Expect(err).NotTo(HaveOccurred())

			condition := getCondition(dt.Name, konfidencev1alpha1.DeploymentTargetReadyCondition)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal("AcceptedByOtherDeployer"))
		})
	})

	Describe("when the referenced DeploymentClass was deleted", func() {
		It("leaves orphan cleanup to Konfidence core", func() {
			dt := newDeploymentTarget("dt-orphan", "deleted.example.com", "kubeconfig", "orphan-secret")
			Expect(k8sClient.Create(ctx, dt)).To(Succeed())
			dt.Status.Conditions = []metav1.Condition{{
				Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
				Status:             metav1.ConditionTrue,
				Reason:             DeploymentTargetReasonAccepted,
				LastTransitionTime: metav1.Now(),
			}}
			Expect(k8sClient.Status().Update(ctx, dt)).To(Succeed())

			reconciler := &DeploymentTargetReconciler{Client: k8sClient}
			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(dt)})
			Expect(err).NotTo(HaveOccurred())

			condition := getCondition(dt.Name, konfidencev1alpha1.DeploymentTargetReadyCondition)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(DeploymentTargetReasonAccepted))
		})
	})
})
