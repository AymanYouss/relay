# ElastiCache for Redis backs Relay's rate-limit counters and usage accounting.
#
# Note on the semantic vector index: the cache's KNN search uses the RediSearch
# module (redis-stack), which ElastiCache for Redis OSS does not bundle. Two
# supported topologies:
#   1. Point Relay at a redis-stack StatefulSet on EKS (deploy/k8s/redis-stack.yaml)
#      for the vector index, and use this ElastiCache cluster for counters.
#   2. Use a managed RediSearch offering (Redis Cloud) for the vector index.
# The VectorStore interface is pluggable, so a BlazeKV-backed index can replace
# RediSearch without touching the gateway.

resource "aws_security_group" "redis" {
  name        = "${var.cluster_name}-redis"
  description = "Allow Redis access from the EKS node group"
  vpc_id      = module.vpc.vpc_id

  ingress {
    description     = "Redis from cluster nodes"
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [module.eks.node_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.tags
}

resource "aws_elasticache_subnet_group" "redis" {
  name       = "${var.cluster_name}-redis"
  subnet_ids = module.vpc.private_subnets
}

resource "aws_elasticache_replication_group" "redis" {
  replication_group_id = "${var.cluster_name}-redis"
  description          = "Relay counters, rate limits and usage accounting"

  engine               = "redis"
  engine_version       = "7.1"
  node_type            = var.redis_node_type
  port                 = 6379
  parameter_group_name = "default.redis7"

  automatic_failover_enabled = true
  multi_az_enabled           = true
  num_node_groups            = 1
  replicas_per_node_group    = var.redis_num_replicas

  subnet_group_name  = aws_elasticache_subnet_group.redis.name
  security_group_ids = [aws_security_group.redis.id]

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true

  snapshot_retention_limit = 7
  maintenance_window       = "sun:05:00-sun:07:00"

  tags = local.tags
}
