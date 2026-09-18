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
    environment = {
      KUBECONFIG = local_file.kubeconfig.filename
    }
    command = <<-EOT
      kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.1.0/standard-install.yaml
      kubectl wait --for=condition=Established crd/gateways.gateway.networking.k8s.io --timeout=60s
      kubectl wait --for=condition=Established crd/httproutes.gateway.networking.k8s.io --timeout=60s
    EOT
  }

  depends_on = [null_resource.coredns_fargate_patch, local_file.kubeconfig]
}

# ─── LoadBalancerConfiguration ────────────────────────────────────────────────
# The LBC's Gateway API integration does NOT read the legacy
# `alb.ingress.kubernetes.io/*` annotations (those are Ingress-only) — it
# reads this CRD instead, referenced from the GatewayClass below. Without it
# the ALB silently defaults to internal, no error, no warning.
#
# depends_on below does NOT by itself make this safe to apply in the same run
# as helm_release.lbc: kubernetes_manifest fetches the CRD's OpenAPI schema at
# PLAN time (to validate the GVK), which needs the CRD to already exist live in
# the cluster — Terraform's dependency graph only orders APPLY-time execution,
# it can't defer that plan-time schema fetch. Learned live 2026-09-11: a full
# untargeted apply failed with "API did not recognize GroupVersionKind... no
# matches for kind LoadBalancerConfiguration" even with depends_on set, because
# helm_release.lbc hadn't been applied yet in that same run. Fix is staging —
# see bootstrap.sh's Phase B, which now applies helm_release.lbc with -target
# before the full apply, same reasoning as null_resource.gateway_api_crds two
# blocks up.

resource "kubernetes_manifest" "gateway_lb_config" {
  manifest = {
    apiVersion = "gateway.k8s.aws/v1beta1"
    kind       = "LoadBalancerConfiguration"
    metadata = {
      name      = "perchguard-alb"
      namespace = "perchguard"
    }
    spec = {
      scheme = "internet-facing"
    }
  }

  depends_on = [
    kubernetes_namespace.perchguard,
    null_resource.gateway_api_crds,
    helm_release.lbc,
  ]
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
      parametersRef = {
        group     = "gateway.k8s.aws"
        kind      = "LoadBalancerConfiguration"
        name      = "perchguard-alb"
        namespace = "perchguard"
      }
    }
  }

  depends_on = [
    null_resource.gateway_api_crds,
    helm_release.lbc,
    kubernetes_manifest.gateway_lb_config,
  ]
}

# ─── Gateway ──────────────────────────────────────────────────────────────────
# One Gateway = one ALB. The LBC provisions it when it sees this resource.
# Scheme comes from gateway_lb_config above via the GatewayClass parametersRef.
# ip target-type: Fargate pods have no node to attach a target group to by
# instance ID, so IP mode is the only option — this appears to already be the
# LBC's default for Fargate-backed Services/target groups; if a future chart
# upgrade changes that default, it'd need to move to a TargetGroupConfiguration
# CRD (the Gateway API equivalent of the old target-type annotation).

resource "kubernetes_manifest" "gateway" {
  manifest = {
    apiVersion = "gateway.networking.k8s.io/v1"
    kind       = "Gateway"
    metadata = {
      name      = "perchguard"
      namespace = "perchguard"
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
