# ─── VPC ──────────────────────────────────────────────────────────────────────
# Public subnets for the ALB; private subnets for Fargate pods.
# Fargate ENIs do not get public IPs — they reach the internet via NAT gateway.
# NAT gateway sits in the public subnet; Fargate pods route through it.

resource "aws_vpc" "perchguard" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.cluster_name}-vpc" }
}

resource "aws_internet_gateway" "perchguard" {
  vpc_id = aws_vpc.perchguard.id
  tags   = { Name = "${var.cluster_name}-igw" }
}

resource "aws_subnet" "public" {
  for_each = {
    a = { az = "${var.aws_region}a", cidr = "10.0.1.0/24" }
    b = { az = "${var.aws_region}b", cidr = "10.0.2.0/24" }
  }

  vpc_id                  = aws_vpc.perchguard.id
  cidr_block              = each.value.cidr
  availability_zone       = each.value.az
  map_public_ip_on_launch = true

  tags = {
    Name                                        = "${var.cluster_name}-public-${each.key}"
    # Required for the AWS Load Balancer Controller to discover subnets for public ALBs.
    "kubernetes.io/role/elb"                    = "1"
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
  }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.perchguard.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.perchguard.id
  }

  tags = { Name = "${var.cluster_name}-public-rt" }
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

# ─── NAT Gateway (one AZ — lab cost trade-off) ───────────────────────────────
# Fargate pod ENIs are private — they have no public IP and cannot reach ECR
# directly via the IGW. A NAT gateway in the public subnet provides the
# outbound internet path for image pulls and AWS API calls.

resource "aws_eip" "nat" {
  domain     = "vpc"
  depends_on = [aws_internet_gateway.perchguard]
  tags       = { Name = "${var.cluster_name}-nat-eip" }
}

resource "aws_nat_gateway" "perchguard" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public["a"].id
  tags          = { Name = "${var.cluster_name}-nat" }
  depends_on    = [aws_internet_gateway.perchguard]
}

# Private subnets — Fargate pods run here, route outbound via NAT
resource "aws_subnet" "private" {
  for_each = {
    a = { az = "${var.aws_region}a", cidr = "10.0.10.0/24" }
    b = { az = "${var.aws_region}b", cidr = "10.0.11.0/24" }
  }

  vpc_id            = aws_vpc.perchguard.id
  cidr_block        = each.value.cidr
  availability_zone = each.value.az

  tags = {
    Name                                        = "${var.cluster_name}-private-${each.key}"
    "kubernetes.io/role/internal-elb"           = "1"
    "kubernetes.io/cluster/${var.cluster_name}" = "shared"
  }
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.perchguard.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.perchguard.id
  }

  tags = { Name = "${var.cluster_name}-private-rt" }
}

resource "aws_route_table_association" "private" {
  for_each = aws_subnet.private

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}

# ─── Security Group ───────────────────────────────────────────────────────────

resource "aws_security_group" "cluster" {
  name        = "${var.cluster_name}-cluster"
  description = "EKS cluster control plane SG"
  vpc_id      = aws_vpc.perchguard.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.cluster_name}-cluster-sg" }
}

# ─── EKS Cluster ──────────────────────────────────────────────────────────────

resource "aws_eks_cluster" "perchguard" {
  name     = var.cluster_name
  role_arn = aws_iam_role.eks_cluster.arn
  version  = var.kubernetes_version

  vpc_config {
    subnet_ids              = [for s in aws_subnet.public : s.id]
    security_group_ids      = [aws_security_group.cluster.id]
    endpoint_public_access  = true
    endpoint_private_access = false
    public_access_cidrs     = ["0.0.0.0/0"]
  }

  depends_on = [
    aws_iam_role_policy_attachment.eks_cluster_policy,
    aws_iam_role_policy_attachment.eks_vpc_resource_controller,
  ]

  tags = { Name = var.cluster_name }
}

# ─── Fargate Profiles ─────────────────────────────────────────────────────────

# perchguard namespace — PerchGuard pods
resource "aws_eks_fargate_profile" "perchguard" {
  cluster_name           = aws_eks_cluster.perchguard.name
  fargate_profile_name   = "perchguard"
  pod_execution_role_arn = aws_iam_role.fargate_pod_execution.arn
  subnet_ids             = [for s in aws_subnet.private : s.id]

  selector { namespace = "perchguard" }

  depends_on = [aws_iam_role_policy_attachment.fargate_pod_execution_policy]
}

# kube-system — CoreDNS + AWS LB Controller
resource "aws_eks_fargate_profile" "kube_system" {
  cluster_name           = aws_eks_cluster.perchguard.name
  fargate_profile_name   = "kube-system"
  pod_execution_role_arn = aws_iam_role.fargate_pod_execution.arn
  subnet_ids             = [for s in aws_subnet.private : s.id]

  selector { namespace = "kube-system" }

  depends_on = [aws_iam_role_policy_attachment.fargate_pod_execution_policy]
}

# ─── ECR ──────────────────────────────────────────────────────────────────────

resource "aws_ecr_repository" "perchguard" {
  name                 = var.cluster_name
  image_tag_mutability = "MUTABLE"
  force_delete         = true # terraform destroy removes the repo and all images

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = { Name = var.cluster_name }
}
