package monitoring

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dcm-project/environment-agent/internal/openshift/container/dcm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const instanceIDIndex = "instanceID"

// StatusMonitor watches Deployment and Pod resources for DCM-managed
// containers and publishes status change events via a StatusPublisher.
type StatusMonitor struct {
	client    kubernetes.Interface
	cfg       MonitorConfig
	publisher StatusPublisher
	logger    *slog.Logger
}

// NewStatusMonitor creates a new StatusMonitor.
func NewStatusMonitor(client kubernetes.Interface, cfg MonitorConfig, publisher StatusPublisher, logger *slog.Logger) *StatusMonitor {
	return &StatusMonitor{
		client:    client,
		cfg:       cfg,
		publisher: publisher,
		logger:    logger,
	}
}

// Start begins watching for resource changes. It blocks until ctx is cancelled.
func (m *StatusMonitor) Start(ctx context.Context) error {
	selector := dcm.Selector()

	factory := informers.NewSharedInformerFactoryWithOptions(
		m.client,
		m.cfg.ResyncPeriod,
		informers.WithNamespace(m.cfg.Namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = selector
		}),
	)

	deployInformer := factory.Apps().V1().Deployments().Informer()
	podInformer := factory.Core().V1().Pods().Informer()

	if err := deployInformer.AddIndexers(cache.Indexers{instanceIDIndex: InstanceIDIndexFunc}); err != nil {
		return fmt.Errorf("adding deployment indexer: %w", err)
	}
	if err := podInformer.AddIndexers(cache.Indexers{instanceIDIndex: InstanceIDIndexFunc}); err != nil {
		return fmt.Errorf("adding pod indexer: %w", err)
	}

	debouncer := NewDebouncer(
		time.Duration(m.cfg.DebounceMs)*time.Millisecond,
		func(event StatusEvent) {
			m.publishWithRetry(ctx, event)
		},
	)

	// Deployment event handlers.
	if _, err := deployInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			m.handleDeployEvent(obj, podInformer.GetIndexer(), debouncer)
		},
		UpdateFunc: func(_, newObj any) {
			m.handleDeployEvent(newObj, podInformer.GetIndexer(), debouncer)
		},
		DeleteFunc: func(obj any) {
			m.handleDeployDelete(obj, podInformer.GetIndexer(), debouncer)
		},
	}); err != nil {
		return fmt.Errorf("adding deployment event handler: %w", err)
	}

	// Pod event handlers.
	if _, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			m.handlePodEvent(obj, deployInformer.GetIndexer(), debouncer)
		},
		UpdateFunc: func(_, newObj any) {
			m.handlePodEvent(newObj, deployInformer.GetIndexer(), debouncer)
		},
		DeleteFunc: func(obj any) {
			m.handlePodDelete(obj, deployInformer.GetIndexer(), podInformer.GetIndexer(), debouncer)
		},
	}); err != nil {
		return fmt.Errorf("adding pod event handler: %w", err)
	}

	factory.Start(ctx.Done())
	defer factory.Shutdown()
	defer debouncer.Stop()

	synced := factory.WaitForCacheSync(ctx.Done())
	if ctx.Err() == nil {
		for typ, ok := range synced {
			if !ok {
				m.logger.Error("cache sync failed, aborting start", "type", typ)
				return fmt.Errorf("cache sync failed for %v", typ)
			}
		}
	}

	<-ctx.Done()

	return nil
}

func (m *StatusMonitor) handleDeployEvent(obj any, podIndexer cache.Indexer, debouncer *Debouncer) {
	meta, ok := obj.(metav1.Object)
	if !ok {
		return
	}
	instanceID := ExtractInstanceID(meta)
	if instanceID == "" {
		return
	}

	deploy, _ := obj.(*appsv1.Deployment)
	pod := lookupPodByIndex(podIndexer, instanceID)
	reconcileAndSubmit(instanceID, deploy, pod, debouncer)
}

func (m *StatusMonitor) handleDeployDelete(obj any, podIndexer cache.Indexer, debouncer *Debouncer) {
	instanceID := extractInstanceIDFromDelete(obj)
	if instanceID == "" {
		return
	}

	pod := lookupPodByIndex(podIndexer, instanceID)
	reconcileAndSubmit(instanceID, nil, pod, debouncer)
}

func (m *StatusMonitor) handlePodEvent(obj any, deployIndexer cache.Indexer, debouncer *Debouncer) {
	meta, ok := obj.(metav1.Object)
	if !ok {
		return
	}
	instanceID := ExtractInstanceID(meta)
	if instanceID == "" {
		return
	}

	pod, _ := obj.(*corev1.Pod)
	deploy := lookupDeployByIndex(deployIndexer, instanceID)
	reconcileAndSubmit(instanceID, deploy, pod, debouncer)
}

func (m *StatusMonitor) handlePodDelete(obj any, deployIndexer cache.Indexer, podIndexer cache.Indexer, debouncer *Debouncer) {
	instanceID := extractInstanceIDFromDelete(obj)
	if instanceID == "" {
		return
	}

	deploy := lookupDeployByIndex(deployIndexer, instanceID)
	pod := lookupPodByIndex(podIndexer, instanceID)
	reconcileAndSubmit(instanceID, deploy, pod, debouncer)
}

func extractInstanceIDFromDelete(obj any) string {
	if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = d.Obj
	}
	meta, ok := obj.(metav1.Object)
	if !ok {
		return ""
	}
	return ExtractInstanceID(meta)
}

func reconcileAndSubmit(instanceID string, deploy *appsv1.Deployment, pod *corev1.Pod, debouncer *Debouncer) {
	status, msg, publish := ReconcileStatus(deploy, pod)
	if publish {
		debouncer.Submit(instanceID, StatusEvent{
			InstanceID: instanceID,
			Status:     status,
			Message:    msg,
		})
	}
}

func (m *StatusMonitor) publishWithRetry(ctx context.Context, event StatusEvent) {
	const maxAttempts = 5
	backoff := 10 * time.Millisecond

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := m.publisher.Publish(ctx, event); err != nil {
			m.logger.Error("failed to publish status event",
				"error", err,
				"instanceID", event.InstanceID,
				"attempt", attempt,
			)
			if attempt < maxAttempts {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff *= 2
			}
			continue
		}
		return
	}
}

func lookupPodByIndex(indexer cache.Indexer, instanceID string) *corev1.Pod {
	items, err := indexer.ByIndex(instanceIDIndex, instanceID)
	if err != nil || len(items) == 0 {
		return nil
	}
	if len(items) == 1 {
		pod, _ := items[0].(*corev1.Pod)
		return pod
	}
	// Multiple pods (e.g., rolling update): select the one with the most
	// concerning phase so the reported status reflects the worst state.
	var worst *corev1.Pod
	worstPriority := -1
	for _, item := range items {
		pod, ok := item.(*corev1.Pod)
		if !ok {
			continue
		}
		p := podPhasePriority(pod.Status.Phase)
		if p > worstPriority {
			worstPriority = p
			worst = pod
		}
	}
	return worst
}

func podPhasePriority(phase corev1.PodPhase) int {
	switch phase {
	case corev1.PodFailed:
		return 4
	case corev1.PodUnknown:
		return 3
	case corev1.PodPending:
		return 2
	case corev1.PodRunning:
		return 1
	default: // Succeeded
		return 0
	}
}

func lookupDeployByIndex(indexer cache.Indexer, instanceID string) *appsv1.Deployment {
	items, err := indexer.ByIndex(instanceIDIndex, instanceID)
	if err != nil || len(items) == 0 {
		return nil
	}
	deploy, _ := items[0].(*appsv1.Deployment)
	return deploy
}
