# K8s Secret holding LLM API key and optional management API key.
# Created in Terraform so secrets are never committed to source.
# If both variables are empty the secret is created with empty values —
# PerchGuard auto-generates the API key at startup.

resource "kubernetes_secret" "perchguard_secrets" {
  metadata {
    name      = "perchguard-secrets"
    namespace = kubernetes_namespace.perchguard.metadata[0].name
  }

  data = {
    "key"     = var.perchguard_api_key  # matches values.yaml secrets.apiKeySecretKey: "key"
    "api-key" = var.llm_api_key         # matches values.yaml secrets.llmApiKeySecretKey: "api-key"
  }

  type = "Opaque"
}
