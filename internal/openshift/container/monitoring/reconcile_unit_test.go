package monitoring_test

import (
	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	"github.com/dcm-project/environment-agent/internal/openshift/container/monitoring"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Status Reconciliation", func() {
	// Helper to build a Deployment with optional conditions and replica count.
	buildDeployment := func(conditions []appsv1.DeploymentCondition, replicas int32) *appsv1.Deployment {
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "test-deploy"},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{Conditions: conditions},
		}
	}

	// Helper to build a Pod with the given phase.
	buildPod := func(phase corev1.PodPhase) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
			Status:     corev1.PodStatus{Phase: phase},
		}
	}

	It("should derive status from Pod phase when both resources exist (TC-U031)", func() {
		deploy := buildDeployment([]appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
		}, 1)
		pod := buildPod(corev1.PodRunning)

		status, _, publish := monitoring.ReconcileStatus(deploy, pod)

		Expect(publish).To(BeTrue())
		Expect(status).To(Equal(v1alpha1.RUNNING))
	})

	It("should return PENDING when Deployment Available=False and no Pod (TC-U032)", func() {
		deploy := buildDeployment([]appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionFalse},
		}, 1)

		status, _, publish := monitoring.ReconcileStatus(deploy, nil)

		Expect(publish).To(BeTrue())
		Expect(status).To(Equal(v1alpha1.PENDING))
	})

	It("should return FAILED when Deployment ReplicaFailure=True and no Pod (TC-U033)", func() {
		deploy := buildDeployment([]appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentReplicaFailure, Status: corev1.ConditionTrue},
		}, 1)

		status, _, publish := monitoring.ReconcileStatus(deploy, nil)

		Expect(publish).To(BeTrue())
		Expect(status).To(Equal(v1alpha1.FAILED))
	})

	It("should return FAILED when Deployment Replicas=0 and no Pod (TC-U034)", func() {
		deploy := buildDeployment(nil, 0)

		status, _, publish := monitoring.ReconcileStatus(deploy, nil)

		Expect(publish).To(BeTrue())
		Expect(status).To(Equal(v1alpha1.FAILED))
	})

	It("should return PENDING when Deployment Available=True but no Pod exists (TC-U060)", func() {
		deploy := buildDeployment([]appsv1.DeploymentCondition{
			{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionTrue},
		}, 1)

		status, _, publish := monitoring.ReconcileStatus(deploy, nil)

		Expect(publish).To(BeTrue())
		Expect(status).To(Equal(v1alpha1.PENDING))
	})

	It("should return DELETED when neither resource exists (TC-U035)", func() {
		status, _, publish := monitoring.ReconcileStatus(nil, nil)

		Expect(publish).To(BeTrue())
		Expect(status).To(Equal(v1alpha1.DELETED))
	})
})
