// Package observation builds authoritative, cache-backed workload snapshots.
package observation

import (
	"context"
	"fmt"
	"time"

	"github.com/yachiko/spotalis/pkg/apis"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// PodControllerOwnerUIDIndex indexes controller owner UIDs on Pods.
	PodControllerOwnerUIDIndex = "spotalis.io/pod-controller-owner-uid"
	// ReplicaSetControllerOwnerUIDIndex indexes controller owner UIDs on ReplicaSets.
	ReplicaSetControllerOwnerUIDIndex = "spotalis.io/replicaset-controller-owner-uid"
)

// NodeClassifier classifies assigned Nodes in a single batch.
type NodeClassifier interface {
	ClassifyNodesByName(context.Context, []string) (map[string]apis.NodeType, error)
}

// Lifecycle is the mutually exclusive lifecycle state for an owned Pod.
type Lifecycle string

const (
	// LifecycleTerminal identifies a completed or failed Pod.
	LifecycleTerminal Lifecycle = "terminal"
	// LifecycleTerminating identifies a Pod with a deletion timestamp.
	LifecycleTerminating Lifecycle = "terminating"
	// LifecyclePendingUnscheduled identifies a Pending Pod without a Node assignment.
	LifecyclePendingUnscheduled Lifecycle = "pending_unscheduled"
	// LifecycleScheduledNotReady identifies an assigned Pod that is not Ready.
	LifecycleScheduledNotReady Lifecycle = "scheduled_not_ready"
	// LifecycleReady identifies an assigned Pod with a true Ready condition.
	LifecycleReady Lifecycle = "ready"
)

// PlacementIntent records a Pod's requested capacity, not its assigned capacity.
type PlacementIntent string

const (
	// PlacementIntentUnknown means the Pod does not express a recognized capacity preference.
	PlacementIntentUnknown PlacementIntent = "unknown"
	// PlacementIntentSpot means the Pod requests spot capacity.
	PlacementIntentSpot PlacementIntent = "spot"
	// PlacementIntentOnDemand means the Pod requests on-demand capacity.
	PlacementIntentOnDemand PlacementIntent = "on-demand"
)

// Pod is one owned workload Pod and its independent lifecycle and capacity evidence.
type Pod struct {
	Pod             corev1.Pod
	Lifecycle       Lifecycle
	Capacity        apis.NodeType
	PlacementIntent PlacementIntent
}

// Snapshot is an ephemeral, UID-keyed workload observation.
type Snapshot struct {
	WorkloadRef         corev1.ObjectReference
	Generation          int64
	ObservedAt          time.Time
	PodResourceVersion  string
	Pods                []Pod
	ClassificationError error
}

// IsFresh reports whether the snapshot was captured within maxAge.
func (s *Snapshot) IsFresh(now time.Time, maxAge time.Duration) bool {
	return s != nil && !s.ObservedAt.IsZero() && now.Sub(s.ObservedAt) <= maxAge
}

// ActualCounts returns assigned, nonterminating Pods with known actual capacity.
func (s *Snapshot) ActualCounts() (spot, onDemand int32) {
	for _, pod := range s.Pods {
		if pod.Lifecycle == LifecycleTerminal || pod.Lifecycle == LifecycleTerminating {
			continue
		}
		switch pod.Capacity {
		case apis.NodeTypeSpot:
			spot++
		case apis.NodeTypeOnDemand:
			onDemand++
		case apis.NodeTypeUnknown:
			// Unknown capacity is intentionally excluded from actual totals.
		}
	}
	return spot, onDemand
}

// EligiblePods returns Ready Pods with proven capacity for voluntary disruption.
func (s *Snapshot) EligiblePods() (spot, onDemand []corev1.Pod) {
	for _, observed := range s.Pods {
		if observed.Lifecycle != LifecycleReady {
			continue
		}
		switch observed.Capacity {
		case apis.NodeTypeSpot:
			spot = append(spot, observed.Pod)
		case apis.NodeTypeOnDemand:
			onDemand = append(onDemand, observed.Pod)
		case apis.NodeTypeUnknown:
			// Unknown capacity is not eligible for voluntary disruption.
		}
	}
	return spot, onDemand
}

// Service observes Deployment and StatefulSet Pods using controller-owner UID indexes.
type Service struct {
	client     client.Client
	classifier NodeClassifier
	now        func() time.Time
}

// NewService creates a cache-backed workload observation service.
func NewService(c client.Client, classifier NodeClassifier) *Service {
	return &Service{client: c, classifier: classifier, now: time.Now}
}

// Observe returns an ownership-verified snapshot for a Deployment or StatefulSet.
func (s *Service) Observe(ctx context.Context, workload client.Object) (*Snapshot, error) {
	selector, err := selectorFor(workload)
	if err != nil {
		return nil, err
	}

	var pods []corev1.Pod
	switch typed := workload.(type) {
	case *appsv1.Deployment:
		pods, err = s.deploymentPods(ctx, typed, selector)
	case *appsv1.StatefulSet:
		pods, err = s.statefulSetPods(ctx, typed, selector)
	default:
		return nil, fmt.Errorf("unsupported workload type %T", workload)
	}
	if err != nil {
		return nil, err
	}

	apiVersion, kind := workloadIdentity(workload)
	snapshot := &Snapshot{
		WorkloadRef: corev1.ObjectReference{APIVersion: apiVersion, Kind: kind, Namespace: workload.GetNamespace(), Name: workload.GetName(), UID: workload.GetUID()},
		Generation:  workload.GetGeneration(),
		ObservedAt:  s.now(),
		Pods:        make([]Pod, len(pods)),
	}

	nodeNames := make([]string, 0, len(pods))
	for i := range pods {
		if pods[i].Spec.NodeName != "" && pods[i].DeletionTimestamp == nil && !isTerminal(&pods[i]) {
			nodeNames = append(nodeNames, pods[i].Spec.NodeName)
		}
	}
	classifications := map[string]apis.NodeType{}
	if s.classifier != nil && len(nodeNames) > 0 {
		classifications, snapshot.ClassificationError = s.classifier.ClassifyNodesByName(ctx, nodeNames)
		if snapshot.ClassificationError != nil {
			classifications = map[string]apis.NodeType{}
		}
	}
	for i := range pods {
		snapshot.Pods[i] = Pod{
			Pod:             pods[i],
			Lifecycle:       lifecycleFor(&pods[i]),
			Capacity:        capacityFor(&pods[i], classifications),
			PlacementIntent: intentFor(&pods[i]),
		}
	}
	return snapshot, nil
}

func workloadIdentity(workload client.Object) (string, string) {
	switch workload.(type) {
	case *appsv1.Deployment:
		return appsv1.SchemeGroupVersion.String(), "Deployment"
	case *appsv1.StatefulSet:
		return appsv1.SchemeGroupVersion.String(), "StatefulSet"
	default:
		gvk := workload.GetObjectKind().GroupVersionKind()
		return gvk.GroupVersion().String(), gvk.Kind
	}
}

func (s *Service) statefulSetPods(ctx context.Context, sts *appsv1.StatefulSet, selector labels.Selector) ([]corev1.Pod, error) {
	var list corev1.PodList
	if err := s.client.List(ctx, &list, client.InNamespace(sts.Namespace), client.MatchingFields{PodControllerOwnerUIDIndex: string(sts.UID)}); err != nil {
		return nil, fmt.Errorf("list StatefulSet pods: %w", err)
	}
	return matchingPods(list.Items, selector), nil
}

func (s *Service) deploymentPods(ctx context.Context, deployment *appsv1.Deployment, selector labels.Selector) ([]corev1.Pod, error) {
	var replicaSets appsv1.ReplicaSetList
	if err := s.client.List(ctx, &replicaSets, client.InNamespace(deployment.Namespace), client.MatchingFields{ReplicaSetControllerOwnerUIDIndex: string(deployment.UID)}); err != nil {
		return nil, fmt.Errorf("list Deployment ReplicaSets: %w", err)
	}

	result := make([]corev1.Pod, 0)
	for i := range replicaSets.Items {
		var pods corev1.PodList
		if err := s.client.List(ctx, &pods, client.InNamespace(deployment.Namespace), client.MatchingFields{PodControllerOwnerUIDIndex: string(replicaSets.Items[i].UID)}); err != nil {
			return nil, fmt.Errorf("list ReplicaSet pods: %w", err)
		}
		result = append(result, matchingPods(pods.Items, selector)...)
	}
	return result, nil
}

func selectorFor(workload client.Object) (labels.Selector, error) {
	switch typed := workload.(type) {
	case *appsv1.Deployment:
		if typed.Spec.Selector == nil {
			return nil, fmt.Errorf("deployment has no selector")
		}
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	case *appsv1.StatefulSet:
		if typed.Spec.Selector == nil {
			return nil, fmt.Errorf("StatefulSet has no selector")
		}
		return metav1.LabelSelectorAsSelector(typed.Spec.Selector)
	default:
		return nil, fmt.Errorf("unsupported workload type %T", workload)
	}
}

func matchingPods(pods []corev1.Pod, selector labels.Selector) []corev1.Pod {
	matched := make([]corev1.Pod, 0, len(pods))
	for i := range pods {
		if selector.Matches(labels.Set(pods[i].Labels)) {
			matched = append(matched, pods[i])
		}
	}
	return matched
}

func controllerOwnerUID(object metav1.Object) types.UID {
	for _, owner := range object.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			return owner.UID
		}
	}
	return ""
}

// IndexPodControllerOwnerUID extracts the controller owner UID for cache indexes.
func IndexPodControllerOwnerUID(object client.Object) []string {
	uid := controllerOwnerUID(object)
	if uid == "" {
		return nil
	}
	return []string{string(uid)}
}

// IndexReplicaSetControllerOwnerUID extracts the controller owner UID for cache indexes.
func IndexReplicaSetControllerOwnerUID(object client.Object) []string {
	return IndexPodControllerOwnerUID(object)
}

func lifecycleFor(pod *corev1.Pod) Lifecycle {
	if isTerminal(pod) {
		return LifecycleTerminal
	}
	if pod.DeletionTimestamp != nil {
		return LifecycleTerminating
	}
	if pod.Spec.NodeName == "" && pod.Status.Phase == corev1.PodPending {
		return LifecyclePendingUnscheduled
	}
	if isReady(pod) {
		return LifecycleReady
	}
	return LifecycleScheduledNotReady
}

func isTerminal(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

func isReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func capacityFor(pod *corev1.Pod, classifications map[string]apis.NodeType) apis.NodeType {
	if pod.Spec.NodeName == "" || pod.DeletionTimestamp != nil || isTerminal(pod) {
		return apis.NodeTypeUnknown
	}
	if capacity, ok := classifications[pod.Spec.NodeName]; ok {
		return capacity
	}
	return apis.NodeTypeUnknown
}

func intentFor(pod *corev1.Pod) PlacementIntent {
	value := pod.Spec.NodeSelector[apis.CapacityTypeLabel]
	switch value {
	case string(apis.NodeTypeSpot):
		return PlacementIntentSpot
	case string(apis.NodeTypeOnDemand):
		return PlacementIntentOnDemand
	default:
		return PlacementIntentUnknown
	}
}
