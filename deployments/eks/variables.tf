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
