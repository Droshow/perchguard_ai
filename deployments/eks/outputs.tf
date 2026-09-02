output "cluster_endpoint" {
  description = "EKS API endpoint."
  value       = aws_eks_cluster.perchguard.endpoint
}

output "ecr_url" {
  description = "ECR repository URL — tag and push the PerchGuard image here."
  value       = aws_ecr_repository.perchguard.repository_url
}

output "kubeconfig_command" {
  description = "Terraform renders this file itself (local_file.kubeconfig) — just point kubectl at it. Never merged into ~/.kube/config."
  value       = "export KUBECONFIG=${local_file.kubeconfig.filename}"
}

output "alb_dns" {
  description = "ALB DNS name — PerchGuard /intercept and /api/* are live here once the Gateway is provisioned (allow ~2 minutes after apply)."
  value       = "kubectl get gateway perchguard -n perchguard -o jsonpath='{.status.addresses[0].value}'"
}
