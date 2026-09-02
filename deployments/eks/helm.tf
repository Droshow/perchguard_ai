# ─── CoreDNS patch ────────────────────────────────────────────────────────────
# EKS ships CoreDNS with a compute-type: ec2 annotation that prevents it from
# scheduling on Fargate. Remove it so CoreDNS runs as a Fargate pod.

resource "null_resource" "coredns_fargate_patch" {
  triggers = {
    cluster_name = aws_eks_cluster.perchguard.name
  }

  provisioner "local-exec" {
    environment = {
      KUBECONFIG = local_file.kubeconfig.filename
    }
    command = <<-EOT
      kubectl rollout status deployment/coredns -n kube-system --timeout=3m || true
      kubectl patch deployment coredns -n kube-system --type=json \
        -p='[{"op":"remove","path":"/spec/template/metadata/annotations/eks.amazonaws.com~1compute-type"}]' \
        2>/dev/null || true
      kubectl rollout restart deployment/coredns -n kube-system
      kubectl rollout status deployment/coredns -n kube-system --timeout=5m
    EOT
  }

  depends_on = [
    aws_eks_fargate_profile.kube_system,
    aws_eks_fargate_profile.perchguard,
    local_file.kubeconfig,
  ]
}

# ─── AWS Load Balancer Controller ─────────────────────────────────────────────

resource "helm_release" "lbc" {
  name       = "aws-load-balancer-controller"
  repository = "https://aws.github.io/eks-charts"
  chart      = "aws-load-balancer-controller"
  version    = "3.3.0"
  namespace  = "kube-system"

  set {
    name  = "clusterName"
    value = var.cluster_name
  }

  set {
    name  = "serviceAccount.create"
    value = "true"
  }

  set {
    name  = "serviceAccount.name"
    value = "aws-load-balancer-controller"
  }

  set {
    name  = "serviceAccount.annotations.eks\\.amazonaws\\.com/role-arn"
    value = aws_iam_role.lbc.arn
  }

  # Enable ALB Gateway API support — chart v3.3.0 / app v3.3.0 feature gate.
  set {
    name  = "controllerConfig.featureGates.ALBGatewayAPI"
    value = "true"
  }

  # VPC ID required by the controller.
  set {
    name  = "vpcId"
    value = aws_vpc.perchguard.id
  }

  set {
    name  = "region"
    value = var.aws_region
  }

  depends_on = [
    null_resource.coredns_fargate_patch,
    null_resource.gateway_api_crds,
    aws_iam_role_policy_attachment.lbc,
  ]
}

# ─── PerchGuard ───────────────────────────────────────────────────────────────

resource "kubernetes_namespace" "perchguard" {
  metadata {
    name = "perchguard"
    labels = {
      "app.kubernetes.io/managed-by" = "terraform"
    }
  }

  depends_on = [null_resource.coredns_fargate_patch]
}

resource "helm_release" "perchguard" {
  name      = "perchguard"
  chart     = "${path.module}/../helm/perchguard"
  namespace = kubernetes_namespace.perchguard.metadata[0].name

  set {
    name  = "image.repository"
    value = "${var.aws_account_id}.dkr.ecr.${var.aws_region}.amazonaws.com/${var.cluster_name}"
  }

  set {
    name  = "image.tag"
    value = "latest"
  }

  set {
    name  = "image.pullPolicy"
    value = "Always"
  }

  # Fargate cannot use EBS — disable the PVC; audit goes to pod ephemeral storage.
  # Wire EFS here once you need persistent audit logs across pod restarts.
  set {
    name  = "audit.pvc.enabled"
    value = "false"
  }

  set {
    name  = "aws.enabled"
    value = "true"
  }

  set {
    name  = "aws.irsaRoleArn"
    value = aws_iam_role.perchguard.arn
  }

  dynamic "set_sensitive" {
    for_each = var.llm_api_key != "" ? [1] : []
    content {
      name  = "secrets.llmApiKeySecretName"
      value = kubernetes_secret.perchguard_secrets.metadata[0].name
    }
  }

  dynamic "set_sensitive" {
    for_each = var.perchguard_api_key != "" ? [1] : []
    content {
      name  = "secrets.apiKeySecretName"
      value = kubernetes_secret.perchguard_secrets.metadata[0].name
    }
  }

  # NLB — internet-facing, IP target mode required for Fargate pods.
  set {
    name  = "service.type"
    value = "LoadBalancer"
  }
  set {
    name  = "service.annotations.service\\.beta\\.kubernetes\\.io/aws-load-balancer-type"
    value = "external"
  }
  set {
    name  = "service.annotations.service\\.beta\\.kubernetes\\.io/aws-load-balancer-nlb-target-type"
    value = "ip"
  }
  set {
    name  = "service.annotations.service\\.beta\\.kubernetes\\.io/aws-load-balancer-scheme"
    value = "internet-facing"
  }

  depends_on = [
    helm_release.lbc,
    kubernetes_secret.perchguard_secrets,
  ]
}
