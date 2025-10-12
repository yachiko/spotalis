# Tutorial: Run Spotalis on Kind

Stability: Stable

Stated Goal: Deploy Spotalis locally and confirm it mutates a workload (nodeSelector injection) and logs a replica distribution calculation.

Estimated Time: 5–10 minutes.

## Prerequisites
- Docker available (Kind requires it)
- Kind installed (`kind version` works)
- `kubectl` configured
- (Optional) Go toolchain if using `make run`

## 1. Create a Kind Cluster
Preferred (uses project script & config):
```bash
make kind
```
This runs `scripts/setup-kind.sh` which applies the canonical `kind.yaml`, sets the context to `kind-spotalis`, and performs any helper setup.

Manual alternative (equivalent core commands):
```bash
kind create cluster --name spotalis --config kind.yaml
kubectl config use-context kind-spotalis
```
Verify:
```bash
kubectl get nodes -o wide
```

## 2. Create and Label an Application Namespace
Spotalis requires the enablement label on the namespace AND the workload.

```bash
kubectl create namespace workloads || true
kubectl label namespace workloads spotalis.io/enabled=true --overwrite
kubectl get ns workloads --show-labels | grep spotalis.io/enabled
```

(You can choose another namespace name; just keep it consistent below.)

## 3. Deploy Spotalis Controller


```bash
make deploy
kubectl -n spotalis-system get pods
```


Wait until controller pod reports readiness (pod Ready=1/1 or logs show startup completed).

## 4. Create a Sample Workload (Enable via Label)
Create a Deployment with the enablement label and tuning annotations.
```bash
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: spot-demo
  labels:
    app: spot-demo
    spotalis.io/enabled: "true" # enable Spotalis
  annotations:
    spotalis.io/spot-percentage: "70"
    spotalis.io/min-on-demand: "1"
spec:
  replicas: 5
  selector:
    matchLabels:
      app: spot-demo
  template:
    metadata:
      labels:
        app: spot-demo
    spec:
      containers:
      - name: nginx
        image: nginx:1.27-alpine
EOF
```

Monitor creation:
```bash
kubectl rollout status deployment/spot-demo
```

## 5. Verify Mutation
Inspect one of the pods to ensure nodeSelector (or other injected fields) were applied.
```bash
POD=$(kubectl get pods -l app=spot-demo -o jsonpath='{.items[0].metadata.name}')
kubectl get pod "$POD" -o json | jq '.spec.nodeSelector'
```
(Expect injected selector keys referencing spot/on-demand classification, depending on implementation.)

## 6. Observe Replica Distribution Log
Check controller logs for a line describing desired vs actual spot/on-demand counts.
```bash
kubectl -n spotalis-system logs -l app=spotalis-controller --tail=200 | grep -i 'distribution' || true
```
Look for a message containing percentage, min-on-demand, or calculated target numbers.

## 7. Adjust (Optional)
Try changing percentage to 50 to watch redistribution:
```bash
kubectl annotate deployment/spot-demo spotalis.io/spot-percentage="50" --overwrite
```
Re-check logs after a short delay.

## 8. Cleanup
```bash
kubectl delete deployment/spot-demo || true
make undeploy || true
kind delete cluster --name spotalis
```
If you only ran locally (Option B) you can skip `make undeploy`.

## Success Criteria
- Deployment rolled out.
- Pod(s) show mutation (nodeSelector or related field).
- Controller logs show distribution calculation.
 - Both the namespace and the Deployment have `spotalis.io/enabled=true`.

If any criterion fails, proceed to the mutation debugging guide (forthcoming) or inspect controller logs with:
```bash
kubectl -n spotalis-system logs -l app=spotalis-controller --tail=500
```

## Related
- Workload labels & annotations: `../reference/labels-and-annotations.md`
- Tune spot percentage: `../how-to/tune-spot-percentage.md`
- Debug mutation issues: `../how-to/debug-mutation.md`
