# ─── Kata sandbox layer — .metal node group + injection webhook (Phase 10e) ──
#
# Same "scoped to a node subset" precedent as cilium.tf: this node group carries
# BOTH perchguard/workload-class=isolated-agent (so Cilium's existing node
# affinity in cilium.tf covers it with zero changes there) AND
# perchguard/kata-capable=true (so only Kata-aware workloads land here). Two
# stacked taints mean a pod must tolerate both to schedule — see
# deployments/k3s/kata/kata-deploy.yaml and redteam-mcp-agent/deployment.yaml.
#
# Real hardware virtualization (/dev/kvm) requires a bare-metal instance type —
# desired_size defaults to 0 (variables.tf) so this costs nothing until scaled
# up for a validation run, then back down, same as the rest of Phase 10's
# live-validate-then-tear-down pattern.

resource "aws_eks_node_group" "kata_sandbox" {
  cluster_name    = aws_eks_cluster.perchguard.name
  node_group_name = "kata-sandbox"
  node_role_arn   = aws_iam_role.agent_node_group.arn
  subnet_ids      = [for s in aws_subnet.private : s.id]

  scaling_config {
    desired_size = var.kata_node_desired_size
    min_size     = 0
    max_size     = max(var.kata_node_desired_size, 1)
  }

  instance_types = [var.kata_node_instance_type]
  ami_type       = "AL2_x86_64"

  labels = {
    "perchguard/workload-class" = "isolated-agent"
    "perchguard/kata-capable"   = "true"
  }

  taint {
    key    = "perchguard.io/agent-workload"
    value  = "true"
    effect = "NO_SCHEDULE"
  }

  taint {
    key    = "perchguard.io/kata-workload"
    value  = "true"
    effect = "NO_SCHEDULE"
  }

  depends_on = [
    aws_iam_role_policy_attachment.agent_node_worker_policy,
    aws_iam_role_policy_attachment.agent_node_cni_policy,
    aws_iam_role_policy_attachment.agent_node_ecr_readonly,
  ]
}

# ─── Webhook serving cert ─────────────────────────────────────────────────────
# Self-signed and self-CA'd (is_ca_certificate = true) — a single-service
# internal webhook cert doesn't need a real CA chain, the apiserver just needs
# a caBundle that verifies the leaf it's presented. Rotation is a `terraform
# apply` (new cert, new secret, new caBundle in the same object) — acceptable
# for a lab/portfolio deployment, revisit if this becomes multi-cluster/prod.

resource "tls_private_key" "kata_webhook" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "tls_self_signed_cert" "kata_webhook" {
  private_key_pem = tls_private_key.kata_webhook.private_key_pem

  subject {
    common_name = "perchguard-operator.perchguard.svc"
  }

  dns_names = [
    "perchguard-operator.perchguard.svc",
    "perchguard-operator.perchguard.svc.cluster.local",
  ]

  validity_period_hours = 8760 # 1 year
  is_ca_certificate     = true

  allowed_uses = [
    "key_encipherment",
    "digital_signature",
    "server_auth",
  ]
}

resource "kubernetes_secret" "perchguard_kata_webhook_tls" {
  metadata {
    name      = "perchguard-kata-webhook-tls"
    namespace = kubernetes_namespace.perchguard.metadata[0].name
  }

  type = "kubernetes.io/tls"

  data = {
    "tls.crt" = tls_self_signed_cert.kata_webhook.cert_pem
    "tls.key" = tls_private_key.kata_webhook.private_key_pem
  }
}

# ─── Apply kata-deploy + the mutating webhook ────────────────────────────────
# Same local-exec/kubectl precedent as operator.tf's RBAC apply. The webhook
# manifest's caBundle placeholder is templated in here rather than baked in
# statically (deployments/k3s/webhook-config.yaml's older approach) — this cert
# is generated fresh by Terraform every apply.

resource "local_file" "kata_mutating_webhook_rendered" {
  filename = "${path.module}/.generated/kata-mutating-webhook.yaml"
  content = templatefile("${path.module}/../k3s/kata/mutating-webhook.yaml", {
    WEBHOOK_CA_BUNDLE = base64encode(tls_self_signed_cert.kata_webhook.cert_pem)
  })
}

resource "null_resource" "kata_deploy_apply" {
  triggers = {
    cluster_name       = aws_eks_cluster.perchguard.name
    kata_deploy_sha    = filesha256("${path.module}/../k3s/kata/kata-deploy.yaml")
    webhook_config_sha = local_file.kata_mutating_webhook_rendered.content_md5
  }

  provisioner "local-exec" {
    environment = {
      KUBECONFIG = local_file.kubeconfig.filename
    }
    command = <<-EOT
      kubectl apply -f ${path.module}/../k3s/kata/kata-deploy.yaml
      kubectl apply -f ${local_file.kata_mutating_webhook_rendered.filename}
    EOT
  }

  depends_on = [
    aws_eks_node_group.kata_sandbox,
    kubernetes_deployment.perchguard_operator,
    kubernetes_secret.perchguard_kata_webhook_tls,
    null_resource.coredns_fargate_patch,
    local_file.kubeconfig,
  ]
}
