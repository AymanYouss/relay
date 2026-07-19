variable "region" {
  description = "AWS region to deploy into."
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Deployment environment name."
  type        = string
  default     = "prod"
}

variable "cluster_name" {
  description = "EKS cluster name."
  type        = string
  default     = "relay"
}

variable "cluster_version" {
  description = "EKS Kubernetes version."
  type        = string
  default     = "1.31"
}

variable "vpc_cidr" {
  description = "CIDR block for the VPC."
  type        = string
  default     = "10.0.0.0/16"
}

variable "node_instance_types" {
  description = "Instance types for the managed node group."
  type        = list(string)
  default     = ["m6i.large"]
}

variable "node_desired_size" {
  type    = number
  default = 3
}

variable "node_min_size" {
  type    = number
  default = 3
}

variable "node_max_size" {
  type    = number
  default = 10
}

variable "redis_node_type" {
  description = "ElastiCache node type for the Redis replication group."
  type        = string
  default     = "cache.r7g.large"
}

variable "redis_num_replicas" {
  description = "Number of read replicas per shard."
  type        = number
  default     = 1
}
