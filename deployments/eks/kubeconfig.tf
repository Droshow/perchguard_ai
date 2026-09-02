# ─── Project-local kubeconfig ─────────────────────────────────────────────────
# Deliberately isolated from ~/.kube/config. Every kubectl-based provisioner
# below used to call `aws eks update-kubeconfig` independently, and Terraform
# runs sibling provisioners in parallel — concurrent non-atomic writes to that
# shared file corrupted it (and everything else living in it: other clusters,
# other AWS accounts). This resource is the one writer; every other resource
# below is a reader via `environment { KUBECONFIG = local_file.kubeconfig.filename }`
# and `depends_on = [local_file.kubeconfig]`. No shelling out to the AWS CLI's
# kubeconfig writer at all, so there's no file to race on.

locals {
  kubeconfig_path = "${path.module}/.kubeconfig"
}

resource "local_file" "kubeconfig" {
  filename        = local.kubeconfig_path
  file_permission = "0600"

  content = yamlencode({
    apiVersion = "v1"
    kind       = "Config"
    clusters = [{
      name = var.cluster_name
      cluster = {
        "server"                     = aws_eks_cluster.perchguard.endpoint
        "certificate-authority-data" = aws_eks_cluster.perchguard.certificate_authority[0].data
      }
    }]
    contexts = [{
      name = var.cluster_name
      context = {
        cluster = var.cluster_name
        user    = var.cluster_name
      }
    }]
    "current-context" = var.cluster_name
    users = [{
      name = var.cluster_name
      user = {
        exec = {
          apiVersion = "client.authentication.k8s.io/v1beta1"
          command    = "aws"
          args = [
            "--region", var.aws_region,
            "eks", "get-token",
            "--cluster-name", var.cluster_name,
            "--output", "json",
          ]
        }
      }
    }]
  })

  depends_on = [aws_eks_cluster.perchguard]
}
