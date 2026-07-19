data "aws_availability_zones" "available" {
  state = "available"
}

locals {
  azs = slice(data.aws_availability_zones.available.names, 0, 3)
  tags = {
    Project = "relay"
  }
}

# --- Networking ---
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 5.13"

  name = "${var.cluster_name}-vpc"
  cidr = var.vpc_cidr
  azs  = local.azs

  private_subnets = [for i in range(3) : cidrsubnet(var.vpc_cidr, 4, i)]
  public_subnets  = [for i in range(3) : cidrsubnet(var.vpc_cidr, 4, i + 8)]
  intra_subnets   = [for i in range(3) : cidrsubnet(var.vpc_cidr, 4, i + 12)]

  enable_nat_gateway   = true
  single_nat_gateway   = true
  enable_dns_hostnames = true

  # Tags required by the AWS Load Balancer Controller for subnet discovery.
  public_subnet_tags = {
    "kubernetes.io/role/elb"                        = "1"
    "kubernetes.io/cluster/${var.cluster_name}"     = "shared"
  }
  private_subnet_tags = {
    "kubernetes.io/role/internal-elb"               = "1"
    "kubernetes.io/cluster/${var.cluster_name}"     = "shared"
  }

  tags = local.tags
}

# --- EKS cluster ---
module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.24"

  cluster_name    = var.cluster_name
  cluster_version = var.cluster_version

  cluster_endpoint_public_access = true

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.private_subnets

  enable_irsa = true

  cluster_addons = {
    coredns                = {}
    kube-proxy             = {}
    vpc-cni                = {}
    aws-ebs-csi-driver     = {}
  }

  eks_managed_node_group_defaults = {
    ami_type = "AL2023_x86_64_STANDARD"
  }

  eks_managed_node_groups = {
    general = {
      instance_types = var.node_instance_types
      desired_size   = var.node_desired_size
      min_size       = var.node_min_size
      max_size       = var.node_max_size
      labels = {
        workload = "relay"
      }
    }
  }

  tags = local.tags
}
