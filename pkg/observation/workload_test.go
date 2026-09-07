package observation

import (
	"context"
	"testing"

	"github.com/yachiko/spotalis/pkg/apis"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testAppName         = "api"
	testReplicaSetKind  = "ReplicaSet"
	testStatefulSetKind = "StatefulSet"
)

type staticClassifier map[string]apis.NodeType

func (c staticClassifier) ClassifyNodesByName(_ context.Context, _ []string) (map[string]apis.NodeType, error) {
	return c, nil
}

type failingClassifier struct{}

func (failingClassifier) ClassifyNodesByName(context.Context, []string) (map[string]apis.NodeType, error) {
	return nil, context.DeadlineExceeded
}

type countingClient struct {
	client.Client
	lists int
}

func (c *countingClient) List(ctx context.Context, list client.ObjectList, options ...client.ListOption) error {
	c.lists++
	return c.Client.List(ctx, list, options...)
}

func TestDeploymentSnapshotUsesOwnerUIDsAndExpressions(t *testing.T) {
	controller := true
	deploymentUID := types.UID("deployment-new")
	oldDeploymentUID := types.UID("deployment-old")
	replicaSetUID := types.UID("rs-current")
	oldReplicaSetUID := types.UID("rs-old")
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: testAppName, Namespace: "default", UID: deploymentUID}, Spec: appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{testAppName}}}}}}
	currentRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: testAppName + "-current", Namespace: "default", UID: replicaSetUID, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: testAppName, UID: deploymentUID, Controller: &controller}}}}
	oldRS := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: testAppName + "-old", Namespace: "default", UID: oldReplicaSetUID, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: testAppName, UID: oldDeploymentUID, Controller: &controller}}}}
	ready := corev1.ConditionTrue
	objects := []client.Object{
		deployment, currentRS, oldRS,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "ready-spot", Namespace: "default", Labels: map[string]string{"app": testAppName}, OwnerReferences: []metav1.OwnerReference{{Kind: testReplicaSetKind, UID: replicaSetUID, Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "spot"}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: ready}}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "default", Labels: map[string]string{"app": testAppName}, OwnerReferences: []metav1.OwnerReference{{Kind: testReplicaSetKind, UID: replicaSetUID, Controller: &controller}}}, Spec: corev1.PodSpec{NodeSelector: map[string]string{apis.CapacityTypeLabel: "on-demand"}}, Status: corev1.PodStatus{Phase: corev1.PodPending}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: "default", Labels: map[string]string{"app": testAppName}, OwnerReferences: []metav1.OwnerReference{{Kind: testReplicaSetKind, UID: oldReplicaSetUID, Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "on-demand"}},
	}
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	base := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&corev1.Pod{}, PodControllerOwnerUIDIndex, IndexPodControllerOwnerUID).WithIndex(&appsv1.ReplicaSet{}, ReplicaSetControllerOwnerUIDIndex, IndexReplicaSetControllerOwnerUID).Build()
	counting := &countingClient{Client: base}
	snapshot, err := NewService(counting, staticClassifier{"spot": apis.NodeTypeSpot}).Observe(context.Background(), deployment)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Pods) != 2 {
		t.Fatalf("owned pods = %d, want 2", len(snapshot.Pods))
	}
	states := map[string]Pod{}
	for _, pod := range snapshot.Pods {
		states[pod.Pod.Name] = pod
	}
	if states["ready-spot"].Lifecycle != LifecycleReady || states["ready-spot"].Capacity != apis.NodeTypeSpot {
		t.Fatalf("ready pod = %#v", states["ready-spot"])
	}
	if states["pending"].Lifecycle != LifecyclePendingUnscheduled || states["pending"].PlacementIntent != PlacementIntentOnDemand {
		t.Fatalf("pending pod = %#v", states["pending"])
	}
	spot, onDemand := snapshot.ActualCounts()
	if spot != 1 || onDemand != 0 {
		t.Fatalf("actual counts = %d spot, %d on-demand", spot, onDemand)
	}
	if counting.lists != 2 {
		t.Fatalf("list calls = %d, want 2 independent of unrelated Pods", counting.lists)
	}
}

func TestStatefulSetSnapshotKeepsTerminalAndTerminatingSeparate(t *testing.T) {
	controller := true
	uid := types.UID("statefulset")
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default", UID: uid}, Spec: appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}}}}
	now := metav1.Now()
	objects := []client.Object{
		sts,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "terminating", Namespace: "default", Labels: map[string]string{"app": "db"}, DeletionTimestamp: &now, Finalizers: []string{"test"}, OwnerReferences: []metav1.OwnerReference{{Kind: testStatefulSetKind, UID: uid, Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "spot"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "failed", Namespace: "default", Labels: map[string]string{"app": "db"}, OwnerReferences: []metav1.OwnerReference{{Kind: testStatefulSetKind, UID: uid, Controller: &controller}}}, Status: corev1.PodStatus{Phase: corev1.PodFailed}},
	}
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&corev1.Pod{}, PodControllerOwnerUIDIndex, IndexPodControllerOwnerUID).Build()
	snapshot, err := NewService(c, staticClassifier{"spot": apis.NodeTypeSpot}).Observe(context.Background(), sts)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]Lifecycle{}
	for _, pod := range snapshot.Pods {
		states[pod.Pod.Name] = pod.Lifecycle
	}
	if states["terminating"] != LifecycleTerminating || states["failed"] != LifecycleTerminal {
		t.Fatalf("lifecycles = %#v", states)
	}
	spot, onDemand := snapshot.ActualCounts()
	if spot != 0 || onDemand != 0 {
		t.Fatalf("terminal or terminating Pods must not count as actual capacity")
	}
}

func TestSnapshotPreservesUnknownCapacityAndClassifierFailure(t *testing.T) {
	controller := true
	uid := types.UID("statefulset")
	sts := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "default", UID: uid}, Spec: appsv1.StatefulSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}}}}
	objects := []client.Object{
		sts,
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "not-ready", Namespace: "default", Labels: map[string]string{"app": "db"}, OwnerReferences: []metav1.OwnerReference{{Kind: testStatefulSetKind, UID: uid, Controller: &controller}}}, Spec: corev1.PodSpec{NodeName: "missing-node"}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionUnknown}}}},
	}
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithIndex(&corev1.Pod{}, PodControllerOwnerUIDIndex, IndexPodControllerOwnerUID).Build()
	snapshot, err := NewService(c, failingClassifier{}).Observe(context.Background(), sts)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ClassificationError == nil {
		t.Fatal("classification error was lost")
	}
	if snapshot.Pods[0].Lifecycle != LifecycleScheduledNotReady || snapshot.Pods[0].Capacity != apis.NodeTypeUnknown {
		t.Fatalf("unexpected uncertain Pod state: %#v", snapshot.Pods[0])
	}
}
