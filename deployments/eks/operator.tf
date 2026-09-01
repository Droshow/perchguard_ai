# ─── PerchGuard operator on EKS (Phase 10c) ───────────────────────────────────
# Platform substrate, not a governed workload — stays Terraform-managed, same
# tier as the CRD/Cilium above. See the shift-left note in the Phase 10c plan
# for why this is different from redteam-mcp-agent's plain-manifest treatment.

# RBAC reused as-is from k3s (ServiceAccount + Role/RoleBinding +
# ClusterRole/ClusterRoleBinding) — same file, no EKS-specific duplicate.
resource "null_resource" "perchguard_operator_rbac" {
  triggers = {
    cluster_name  = aws_eks_cluster.perchguard.name
    rbac_file_sha = filesha256("${path.module}/../k3s/perchguard-operator/rbac.yaml")
  }

  provisioner "local-exec" {
    command = <<-EOT
      aws eks update-kubeconfig --name ${var.cluster_name} --region ${var.aws_region}
      kubectl apply -f ${path.module}/../k3s/perchguard-operator/rbac.yaml
    EOT
  }

  depends_on = [
    null_resource.coredns_fargate_patch,
    kubernetes_namespace.perchguard,
  ]
}

resource "kubernetes_deployment" "perchguard_operator" {
  metadata {
    name      = "perchguard-operator"
    namespace = "perchguard"
    labels    = { app = "perchguard-operator" }
  }

  spec {
    replicas = 1

    selector {
      match_labels = { app = "perchguard-operator" }
    }

    template {
      metadata {
        labels = { app = "perchguard-operator" }
      }

      spec {
        service_account_name = "perchguard-operator"

        security_context {
          run_as_non_root = true
          run_as_user     = 65534
          run_as_group    = 65534
          seccomp_profile {
            type = "RuntimeDefault"
          }
        }

        container {
          name              = "perchguard-operator"
          image             = "${aws_ecr_repository.perchguard_operator.repository_url}:latest"
          image_pull_policy = "Always"

          port {
            name           = "probes"
            container_port = 8081
          }

          env {
            name  = "PERCHGUARD_POLICY"
            value = "/etc/perchguard/policies.yaml"
          }
          env {
            name  = "PERCHGUARD_METRICS_ADDR"
            value = ":8081"
          }

          volume_mount {
            name       = "policies"
            mount_path = "/etc/perchguard"
            read_only  = true
          }

          liveness_probe {
            http_get {
              path = "/healthz"
              port = 8081
            }
            initial_delay_seconds = 3
            period_seconds        = 10
          }
          readiness_probe {
            http_get {
              path = "/readyz"
              port = 8081
            }
            initial_delay_seconds = 2
            period_seconds        = 5
          }

          resources {
            requests = { cpu = "20m", memory = "32Mi" }
            limits   = { cpu = "100m", memory = "64Mi" }
          }

          security_context {
            allow_privilege_escalation = false
            read_only_root_filesystem  = true
            capabilities {
              drop = ["ALL"]
            }
          }
        }

        volume {
          name = "policies"
          config_map {
            # Same ConfigMap helm_release.perchguard already creates
            # ({{ include "perchguard.fullname" . }}-policies →
            # perchguard-policies) — one policy source, no second ConfigMap to
            # drift. See deployments/helm/perchguard/templates/configmap.yaml.
            name = "perchguard-policies"
          }
        }
      }
    }
  }

  depends_on = [
    helm_release.perchguard,
    null_resource.agent_isolation_policy_crd,
    null_resource.perchguard_operator_rbac,
  ]
}
