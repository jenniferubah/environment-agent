package monitoring

import (
	v1alpha1 "github.com/dcm-project/environment-agent/api/container/v1alpha1"
	k8sutil "github.com/dcm-project/environment-agent/internal/openshift/container/kubernetes"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// ReconcileStatus derives the DCM container status from the current state of
// a Deployment and its associated Pod. It returns the status, a human-readable
// message, and whether the result should be published.
func ReconcileStatus(deploy *appsv1.Deployment, pod *corev1.Pod) (v1alpha1.ContainerStatus, string, bool) {
	if deploy == nil && pod == nil {
		return v1alpha1.DELETED, "resource no longer exists", true
	}

	if pod != nil {
		status, ok := k8sutil.MapPodPhaseToStatus(pod.Status.Phase)
		if !ok {
			return "", "", false
		}
		msg := string(pod.Status.Phase)
		if status == v1alpha1.FAILED {
			if reason := extractPodFailureReason(pod); reason != "" {
				msg = reason
			}
		}
		return status, msg, true
	}

	// Deploy-only path (pod == nil).
	if deploy.Spec.Replicas != nil && *deploy.Spec.Replicas == 0 {
		return v1alpha1.FAILED, "deployment scaled to zero", true
	}
	for _, c := range deploy.Status.Conditions {
		if c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue {
			msg := "replica failure"
			if c.Message != "" {
				msg = c.Message
			}
			return v1alpha1.FAILED, msg, true
		}
	}
	return v1alpha1.PENDING, "waiting for pods", true
}

// extractPodFailureReason attempts to find a specific failure reason from
// pod container statuses (e.g., CrashLoopBackOff).
func extractPodFailureReason(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			return cs.State.Waiting.Reason
		}
		if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			return cs.State.Terminated.Reason
		}
	}
	return ""
}
