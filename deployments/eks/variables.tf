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

# ─── Phase 10e: Kata sandbox node group ──────────────────────────────────────
# Kata needs real hardware virtualization (/dev/kvm), which no Nitro
# non-.metal instance type exposes to the guest — the existing t3.medium
# agent-workloads group structurally cannot run it. .metal is real money
# (~$4/hr for c5.metal on-demand); desired size defaults to 0 so this group
# costs nothing until deliberately scaled up for a validation run, then back
# down — same live-validate-then-tear-down pattern as the rest of Phase 10.

variable "kata_node_instance_type" {
  description = "Bare-metal EC2 instance type for the Kata-capable node group (Phase 10e). Must be a .metal type — Kata requires /dev/kvm."
  type        = string
  default     = "c5.metal"
}

variable "kata_node_desired_size" {
  description = "Desired node count for the Kata sandbox node group (Phase 10e). Defaults to 0 — scale up only for an active validation run."
  type        = number
  default     = 0
}
