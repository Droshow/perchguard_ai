variable "cluster_name" {
  description = "EKS cluster name — also used as a prefix for all resources."
  type        = string
  default     = "perchguard"
}

variable "aws_region" {
  description = "AWS region to deploy into."
  type        = string
  default     = "eu-central-1"
}

variable "aws_account_id" {
  description = "AWS account ID. Used to construct IRSA trust policies and ECR URLs."
  type        = string
}

variable "kubernetes_version" {
  description = "EKS Kubernetes version."
  type        = string
  default     = "1.31"
}

variable "llm_api_key" {
  description = "Anthropic API key for the semantic firewall. Leave empty to disable semantic firewall."
  type        = string
  sensitive   = true
  default     = ""
}

variable "perchguard_api_key" {
  description = "Management API bearer key (pgmk-...). Leave empty — PerchGuard auto-generates and logs it at startup."
  type        = string
  sensitive   = true
  default     = ""
}

# ─── Phase 10c: agent workload node group ────────────────────────────────────
# Real EC2 cost, unlike Fargate's per-pod-second billing — kept small for a
# lab/portfolio budget. Cilium's DaemonSet + Kata (later) need a real kubelet,
# which Fargate structurally cannot provide.

variable "agent_node_instance_type" {
  description = "EC2 instance type for the isolated-agent node group (Phase 10c)."
  type        = string
  default     = "t3.medium"
}

variable "agent_node_desired_size" {
  description = "Desired node count for the isolated-agent node group (Phase 10c)."
  type        = number
  default     = 1
}
