# Relay on AWS (EKS + ElastiCache + ALB)

Terraform to provision the production footprint for Relay:

- **VPC** across 3 AZs with public, private and intra subnets (tagged for ALB
  subnet discovery).
- **EKS** cluster with a managed node group, IRSA enabled, and the core add-ons
  (CoreDNS, kube-proxy, VPC CNI, EBS CSI).
- **ElastiCache for Redis** (Multi-AZ, automatic failover, encryption in transit
  and at rest) for rate-limit counters and usage accounting.
- **AWS Load Balancer Controller** (IRSA + Helm) so the Relay `Ingress`
  provisions an internet-facing ALB.

## Layout

| File | Purpose |
| --- | --- |
| `versions.tf` | Terraform + provider versions, providers, backend stub |
| `variables.tf` | Inputs (region, sizing, versions) |
| `main.tf` | VPC and EKS |
| `elasticache.tf` | Redis replication group + security group |
| `alb.tf` | Load Balancer Controller IRSA role + Helm release |
| `outputs.tf` | Cluster/Redis endpoints and helper commands |

## Usage

```bash
cd deploy/aws
terraform init
terraform plan  -var="region=us-east-1"
terraform apply -var="region=us-east-1"

# Point kubectl at the new cluster
$(terraform output -raw configure_kubectl)
```

Then deploy the workload. Create the secret out-of-band (ideally via the External
Secrets Operator sourcing AWS Secrets Manager), setting `REDIS_ADDR` to the
Terraform output:

```bash
kubectl apply -f ../k8s/namespace.yaml
kubectl apply -f ../k8s/redis-stack.yaml       # RediSearch vector index
kubectl -n relay create secret generic relay-secrets \
  --from-literal=REDIS_ADDR=redis-stack:6379 \
  --from-literal=RELAY_ADMIN_TOKEN=$(openssl rand -hex 24) \
  --from-literal=OPENAI_API_KEY=$OPENAI_API_KEY \
  --from-literal=ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  --from-literal=RELAY_KEY_TEAM_A=$RELAY_KEY_TEAM_A \
  --from-literal=RELAY_KEY_TEAM_B=$RELAY_KEY_TEAM_B
kubectl apply -k ../k8s
```

## Redis topology

The semantic cache uses RediSearch (redis-stack) for KNN vector search, which
ElastiCache for Redis OSS does not provide. Two supported options:

1. Run the `redis-stack` StatefulSet (`deploy/k8s/redis-stack.yaml`) for the
   vector index and use the ElastiCache cluster for counters/accounting.
2. Use a managed RediSearch (Redis Cloud) and point `REDIS_ADDR` at it.

The `VectorStore` interface is pluggable, so a BlazeKV-backed index can replace
RediSearch without gateway changes.

## Teardown

```bash
terraform destroy -var="region=us-east-1"
```
