# ─── Gateway API CRDs ─────────────────────────────────────────────────────────
# Install the standard Gateway API CRD bundle before the LBC chart deploys
# GatewayClass support. Uses kubectl via local-exec — cleaner than
# kubernetes_manifest for large CRD bundles with complex OpenAPI schemas.

resource "null_resource" "gateway_api_crds" {
  triggers = {
    cluster_name = aws_eks_cluster.perchguard.name
    crds_version = "v1.1.0"
  }

  provisioner "local-exec" {
    command = <<-EOT
      aws eks update-kubeconfig --name ${var.cluster_name} --region ${var.aws_region}
      kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
      kubectl wait --for=condition=Established crd/gateways.gateway.networking.k8s.io --timeout=60s
      kubectl wait --for=condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=60s
    EOT
  }

  depends_on = [null_resource.coredns_fargate_patch]
}

# ─── GatewayClass ─────────────────────────────────────────────────────────────
# Tells the LBC it owns ALBs provisioned via Gateway API.

resource "kubernetes_manifest" "gateway_class" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "GatewayClass"
    metadata = {
      name = "alb"
    }
    spec = {
      controllerName = "gateway.k8s.aws/alb"
    }
  }

  depends_on = [
    null_resource.gateway_api_crds,
    helm_release.lbc,
  ]
}

# ─── Gateway ──────────────────────────────────────────────────────────────────
# One Gateway = one ALB. The LBC provisions it when it sees this resource.
# internet-facing: ALB is public. ip: Fargate requires IP target mode.

resource "kubernetes_manifest" "gateway" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "Gateway"
    metadata = {
      name      = "perchguard"
      namespace = "perchguard"
      annotations = {
        "alb.ingress.kubernetes.io/scheme"      = "internet-facing"
        "alb.ingress.kubernetes.io/target-type" = "ip"
      }
    }
    spec = {
      gatewayClassName = "alb"
      listeners = [{
        name     = "http"
        port     = 80
        protocol = "HTTP"
        allowedRoutes = {
          namespaces = { from = "Same" }
        }
      }]
    }
  }

  depends_on = [
    kubernetes_manifest.gateway_class,
    kubernetes_namespace.perchguard,
  ]
}

# ─── HTTPRoutes ───────────────────────────────────────────────────────────────
# Agent-facing: /intercept, /validate/output, /agents/*, /healthz, /metrics
# Management: /api/*
# Both hit the same backend service — separation is for future WAF / rate-limit
# rules at the ALB listener level.

resource "kubernetes_manifest" "route_agent" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "perchguard-agent"
      namespace = "perchguard"
    }
    spec = {
      parentRefs = [{
        name = "perchguard"
      }]
      rules = [{
        matches = [
          { path = { type = "PathPrefix", value = "/intercept" } },
          { path = { type = "PathPrefix", value = "/validate" } },
          { path = { type = "PathPrefix", value = "/agents" } },
          { path = { type = "PathPrefix", value = "/healthz" } },
          { path = { type = "PathPrefix", value = "/metrics" } },
        ]
        backendRefs = [{
          name = "perchguard"
          port = 8080
        }]
      }]
    }
  }

  depends_on = [
    kubernetes_manifest.gateway,
    helm_release.perchguard,
  ]
}

resource "kubernetes_manifest" "route_api" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "HTTPRoute"
    metadata = {
      name      = "perchguard-api"
      namespace = "perchguard"
    }
    spec = {
      parentRefs = [{
        name = "perchguard"
      }]
      rules = [{
        matches = [
          { path = { type = "PathPrefix", value = "/api" } },
        ]
        backendRefs = [{
          name = "perchguard"
          port = 8080
        }]
      }]
    }
  }

  depends_on = [
    kubernetes_manifest.gateway,
    helm_release.perchguard,
  ]
}
