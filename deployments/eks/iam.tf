# ─── EKS Cluster Role ─────────────────────────────────────────────────────────

resource "aws_iam_role" "eks_cluster" {
  name = "${var.cluster_name}-eks-cluster"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "eks.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "eks_cluster_policy" {
  role       = aws_iam_role.eks_cluster.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"
}

resource "aws_iam_role_policy_attachment" "eks_vpc_resource_controller" {
  role       = aws_iam_role.eks_cluster.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSVPCResourceController"
}

# ─── Fargate Pod Execution Role ───────────────────────────────────────────────

resource "aws_iam_role" "fargate_pod_execution" {
  name = "${var.cluster_name}-fargate-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "eks-fargate-pods.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "fargate_pod_execution_policy" {
  role       = aws_iam_role.fargate_pod_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEKSFargatePodExecutionRolePolicy"
}

# ─── OIDC Provider (required for IRSA) ───────────────────────────────────────

data "tls_certificate" "eks_oidc" {
  url = aws_eks_cluster.perchguard.identity[0].oidc[0].issuer
}

resource "aws_iam_openid_connect_provider" "eks" {
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = [data.tls_certificate.eks_oidc.certificates[0].sha1_fingerprint]
  url             = aws_eks_cluster.perchguard.identity[0].oidc[0].issuer
}

locals {
  oidc_issuer = trimprefix(aws_eks_cluster.perchguard.identity[0].oidc[0].issuer, "https://")
}

# ─── IRSA: AWS Load Balancer Controller ──────────────────────────────────────

data "http" "lbc_iam_policy" {
  url = "https://raw.githubusercontent.com/kubernetes-sigs/aws-load-balancer-controller/v3.3.0/docs/install/iam_policy.json"
}

resource "aws_iam_policy" "lbc" {
  name   = "${var.cluster_name}-lbc"
  policy = data.http.lbc_iam_policy.response_body
}

resource "aws_iam_role" "lbc" {
  name = "${var.cluster_name}-lbc"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${local.oidc_issuer}:aud" = "sts.amazonaws.com"
          "${local.oidc_issuer}:sub" = "system:serviceaccount:kube-system:aws-load-balancer-controller"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lbc" {
  role       = aws_iam_role.lbc.name
  policy_arn = aws_iam_policy.lbc.arn
}

# ─── IRSA: PerchGuard ServiceAccount ─────────────────────────────────────────
# Minimal permissions: ECR pull (image is pulled by Fargate execution role,
# but this IRSA is the hook point for future Secrets Manager access).

resource "aws_iam_role" "perchguard" {
  name = "${var.cluster_name}-app"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Federated = aws_iam_openid_connect_provider.eks.arn
      }
      Action = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${local.oidc_issuer}:aud" = "sts.amazonaws.com"
          "${local.oidc_issuer}:sub" = "system:serviceaccount:perchguard:perchguard"
        }
      }
    }]
  })
}
