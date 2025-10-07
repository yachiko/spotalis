# Node Classifier Configuration

## Overview

The Node Classifier identifies which nodes in your cluster are spot (preemptible) instances versus on-demand (regular) instances. This is critical for Spotalis to distribute pods according to your desired spot percentage.

## Default Configuration

By default, Spotalis uses **Karpenter labels** to identify node types:

- **Spot nodes**: `karpenter.sh/capacity-type: spot`
- **On-demand nodes**: `karpenter.sh/capacity-type: on-demand`

This assumes you're using [Karpenter](https://karpenter.sh/) for node provisioning, which is the recommended approach for AWS EKS clusters.

## Why Karpenter Labels?

Karpenter provides a consistent, cloud-agnostic way to label nodes. While cloud providers have their own labels (AWS, GCP, Azure), we can't reliably detect which cloud you're using at runtime. Therefore, we default to Karpenter's standard labels.

**If you're not using Karpenter**, you can easily override these labels in your configuration.

## Overriding for Your Cloud Provider

### Configuration File

Edit your `config.yaml` to specify custom label selectors:

```yaml
controllers:
  nodeClassifier:
    # For AWS without Karpenter
    spotLabels:
      - matchLabels:
          node.kubernetes.io/lifecycle: "spot"
    onDemandLabels:
      - matchLabels:
          node.kubernetes.io/lifecycle: "normal"
```

### Environment Variables

You can also override via environment variables (useful for Kubernetes deployments):

```bash
# Not directly supported - use configuration file for node classifier
# Environment variable support for node classifier labels is planned for future release
```

## Cloud Provider Examples

### AWS (without Karpenter)

```yaml
controllers:
  nodeClassifier:
    spotLabels:
      - matchLabels:
          node.kubernetes.io/lifecycle: "spot"
    onDemandLabels:
      - matchLabels:
          node.kubernetes.io/lifecycle: "normal"
```

### GCP (Google Kubernetes Engine)

```yaml
controllers:
  nodeClassifier:
    spotLabels:
      - matchLabels:
          cloud.google.com/gke-preemptible: "true"
    onDemandLabels:
      # GCP doesn't have a standard on-demand label
      # You may need to use node pools or custom labels
      - matchLabels:
          your-custom-label/pool: "on-demand"
```

### Azure (AKS)

```yaml
controllers:
  nodeClassifier:
    spotLabels:
      - matchLabels:
          kubernetes.azure.com/scalesetpriority: "spot"
    onDemandLabels:
      - matchLabels:
          kubernetes.azure.com/scalesetpriority: "regular"
```

## Multiple Label Patterns

You can configure multiple label selectors to support different node types in the same cluster:

```yaml
controllers:
  nodeClassifier:
    spotLabels:
      # Karpenter nodes
      - matchLabels:
          karpenter.sh/capacity-type: "spot"
      # Legacy AWS nodes
      - matchLabels:
          node.kubernetes.io/lifecycle: "spot"
      # Custom node pool
      - matchLabels:
          your-org/node-type: "spot"
    onDemandLabels:
      - matchLabels:
          karpenter.sh/capacity-type: "on-demand"
      - matchLabels:
          node.kubernetes.io/lifecycle: "normal"
```

## Validation

After configuring node classifier labels, verify they work:

```bash
# Check your node labels
kubectl get nodes --show-labels

# Check if Spotalis can identify nodes
kubectl port-forward -n spotalis-system svc/spotalis-metrics 8080:8080

# Query metrics to see node distribution
curl http://localhost:8080/metrics | grep spotalis_workload_replicas
```

You should see metrics showing spot vs on-demand replicas:

```
spotalis_workload_replicas{namespace="default",workload="my-app",type="spot"} 7
spotalis_workload_replicas{namespace="default",workload="my-app",type="on-demand"} 3
```

## Troubleshooting

### All nodes classified as "unknown"

**Symptom**: Metrics show 0 spot and 0 on-demand nodes, or pods aren't being distributed.

**Solution**: Your node labels don't match the configured selectors. Check:

1. What labels your nodes actually have:
   ```bash
   kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.labels}{"\n"}{end}'
   ```

2. Update your `nodeClassifier` configuration to match those labels.

### Pods all going to one node type

**Symptom**: All pods scheduled on spot or all on on-demand, not distributed.

**Solution**: Ensure both `spotLabels` and `onDemandLabels` are configured and match actual nodes in your cluster.

### Mixed Karpenter and cloud-native nodes

**Symptom**: Some nodes use Karpenter labels, others use cloud provider labels.

**Solution**: Configure multiple label selectors (see "Multiple Label Patterns" above).

## Reference

- Default configuration: `pkg/config/consolidated.go`
- Implementation: `pkg/apis/node_classification.go`
- Example config: `examples/configs/config.yaml`
