output "cluster_name" {
  description = "EKS cluster name."
  value       = module.eks.cluster_name
}

output "cluster_endpoint" {
  description = "EKS API server endpoint."
  value       = module.eks.cluster_endpoint
}

output "configure_kubectl" {
  description = "Command to update your kubeconfig."
  value       = "aws eks update-kubeconfig --region ${var.region} --name ${module.eks.cluster_name}"
}

output "redis_primary_endpoint" {
  description = "ElastiCache primary endpoint (set as REDIS_ADDR)."
  value       = "${aws_elasticache_replication_group.redis.primary_endpoint_address}:6379"
}

output "redis_reader_endpoint" {
  description = "ElastiCache reader endpoint."
  value       = aws_elasticache_replication_group.redis.reader_endpoint_address
}

output "vpc_id" {
  value = module.vpc.vpc_id
}
