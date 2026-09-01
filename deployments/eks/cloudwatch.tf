# ─── ADOT Collector: Prometheus → CloudWatch EMF ─────────────────────────────
#
# Deploys the AWS Distro for OpenTelemetry (ADOT) collector as a single-replica
# Deployment in the perchguard namespace. It scrapes PerchGuard's /metrics
# endpoint every 30 s and exports to CloudWatch using the EMF format.
#
# Fargate constraint: DaemonSets don't run on Fargate — this is a plain Deployment.

# ─── IAM ─────────────────────────────────────────────────────────────────────

resource "aws_iam_role" "adot" {
  name = "${var.cluster_name}-adot"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.eks.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${local.oidc_issuer}:aud" = "sts.amazonaws.com"
          "${local.oidc_issuer}:sub" = "system:serviceaccount:perchguard:adot-collector"
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "adot_cloudwatch" {
  name = "cloudwatch-emf"
  role = aws_iam_role.adot.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        # Scoped to the PerchGuard namespace — cannot pollute other namespaces.
        Effect   = "Allow"
        Action   = ["cloudwatch:PutMetricData"]
        Resource = "*"
        Condition = {
          StringEquals = { "cloudwatch:namespace" = "PerchGuard" }
        }
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
          "logs:DescribeLogStreams",
        ]
        Resource = "arn:aws:logs:${var.aws_region}:${var.aws_account_id}:log-group:/perchguard/metrics:*"
      },
    ]
  })
}

# ─── Kubernetes resources ─────────────────────────────────────────────────────

resource "kubernetes_service_account" "adot" {
  metadata {
    name      = "adot-collector"
    namespace = "perchguard"
    annotations = {
      "eks.amazonaws.com/role-arn" = aws_iam_role.adot.arn
    }
  }
  depends_on = [aws_eks_cluster.perchguard]
}

resource "kubernetes_config_map" "adot" {
  metadata {
    name      = "adot-collector-config"
    namespace = "perchguard"
  }

  data = {
    "otel-collector-config.yaml" = <<-EOT
      receivers:
        prometheus:
          config:
            scrape_configs:
              - job_name: perchguard
                scrape_interval: 30s
                metrics_path: /metrics
                static_configs:
                  - targets: ['perchguard.perchguard.svc.cluster.local:8080']

      processors:
        batch/metrics:
          timeout: 60s

      exporters:
        awsemf:
          namespace: PerchGuard
          region: ${var.aws_region}
          log_group_name: /perchguard/metrics
          log_stream_name: ${var.cluster_name}
          dimension_rollup_option: NoDimensionRollup
          metric_declarations:
            # decision-labelled counter — one CW dimension per decision type
            - dimensions: [["decision"]]
              metric_name_selectors:
                - ^perchguard_intercept_total$
            # everything else — no extra dimensions
            - dimensions: [[]]
              metric_name_selectors:
                - ^perchguard_intercept_duration_seconds.*$
                - ^perchguard_active_sessions$
                - ^perchguard_audit_ring_utilization$
                - ^perchguard_session_risk_score.*$
                - ^perchguard_semantic_firewall_duration_seconds.*$
                - ^perchguard_policy_reload_total$

      service:
        pipelines:
          metrics:
            receivers: [prometheus]
            processors: [batch/metrics]
            exporters: [awsemf]
    EOT
  }

  depends_on = [aws_eks_cluster.perchguard]
}

resource "kubernetes_deployment" "adot" {
  metadata {
    name      = "adot-collector"
    namespace = "perchguard"
    labels    = { app = "adot-collector" }
  }

  spec {
    replicas = 1

    selector {
      match_labels = { app = "adot-collector" }
    }

    template {
      metadata {
        labels = { app = "adot-collector" }
      }

      spec {
        service_account_name            = kubernetes_service_account.adot.metadata[0].name
        automount_service_account_token = true

        container {
          name  = "adot-collector"
          image = "public.ecr.aws/aws-observability/aws-otel-collector:v0.40.0"
          args  = ["--config=/conf/otel-collector-config.yaml"]

          resources {
            requests = { cpu = "50m", memory = "64Mi" }
            limits   = { cpu = "200m", memory = "128Mi" }
          }

          volume_mount {
            name       = "config"
            mount_path = "/conf"
          }
        }

        volume {
          name = "config"
          config_map {
            name = kubernetes_config_map.adot.metadata[0].name
          }
        }
      }
    }
  }

  depends_on = [kubernetes_config_map.adot]
}

# ─── CloudWatch Dashboard ─────────────────────────────────────────────────────
#
# All metrics land in the "PerchGuard" CW namespace via ADOT EMF.
# Histograms are exported as _sum + _count; we compute avg via metric math.
# Counters use RATE() to show per-minute throughput.

resource "aws_cloudwatch_dashboard" "perchguard" {
  dashboard_name = "${var.cluster_name}-governance"

  dashboard_body = jsonencode({
    widgets = [

      # ── Row 1: Admission Decisions ─────────────────────────────────────────

      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 16
        height = 6
        properties = {
          title   = "Admission Decisions (per minute)"
          region  = var.aws_region
          view    = "timeSeries"
          stacked = false
          period  = 60
          metrics = [
            [{ expression = "RATE(m1)*60", label = "ALLOW", id = "r1" }],
            [{ expression = "RATE(m2)*60", label = "DENY", id = "r2" }],
            [{ expression = "RATE(m3)*60", label = "MUTATE", id = "r3" }],
            [{ expression = "RATE(m4)*60", label = "HUMAN_REVIEW", id = "r4" }],
            [{ expression = "RATE(m5)*60", label = "TERMINATE", id = "r5" }],
            ["PerchGuard", "perchguard_intercept_total", "decision", "ALLOW", { id = "m1", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_intercept_total", "decision", "DENY", { id = "m2", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_intercept_total", "decision", "MUTATE", { id = "m3", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_intercept_total", "decision", "HUMAN_REVIEW", { id = "m4", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_intercept_total", "decision", "TERMINATE", { id = "m5", visible = false, period = 60 }],
          ]
          yAxis = { left = { min = 0, label = "decisions/min" } }
        }
      },

      {
        type   = "metric"
        x      = 16
        y      = 0
        width  = 8
        height = 6
        properties = {
          title  = "Active Sessions"
          region = var.aws_region
          view   = "singleValue"
          period = 60
          metrics = [
            ["PerchGuard", "perchguard_active_sessions", { label = "sessions" }],
          ]
        }
      },

      # ── Row 2: Latency & Risk ──────────────────────────────────────────────

      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 12
        height = 6
        properties = {
          title  = "Pipeline Latency — avg (seconds)"
          region = var.aws_region
          view   = "timeSeries"
          period = 60
          metrics = [
            [{ expression = "dur_sum / dur_count", label = "avg latency", id = "avg_lat" }],
            ["PerchGuard", "perchguard_intercept_duration_seconds_sum", { id = "dur_sum", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_intercept_duration_seconds_count", { id = "dur_count", visible = false, period = 60 }],
          ]
          yAxis = { left = { min = 0, label = "seconds" } }
        }
      },

      {
        type   = "metric"
        x      = 12
        y      = 6
        width  = 12
        height = 6
        properties = {
          title  = "Session Risk Score — avg (0–1)"
          region = var.aws_region
          view   = "timeSeries"
          period = 60
          metrics = [
            [{ expression = "risk_sum / risk_count", label = "avg risk score", id = "avg_risk" }],
            ["PerchGuard", "perchguard_session_risk_score_sum", { id = "risk_sum", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_session_risk_score_count", { id = "risk_count", visible = false, period = 60 }],
          ]
          yAxis = { left = { min = 0, max = 1, label = "risk score" } }
        }
      },

      # ── Row 3: Semantic Firewall & Ops ─────────────────────────────────────

      {
        type   = "metric"
        x      = 0
        y      = 12
        width  = 12
        height = 6
        properties = {
          title  = "Semantic Firewall Latency — avg (seconds)"
          region = var.aws_region
          view   = "timeSeries"
          period = 60
          metrics = [
            [{ expression = "sf_sum / sf_count", label = "avg LLM call", id = "avg_sf" }],
            ["PerchGuard", "perchguard_semantic_firewall_duration_seconds_sum", { id = "sf_sum", visible = false, period = 60 }],
            ["PerchGuard", "perchguard_semantic_firewall_duration_seconds_count", { id = "sf_count", visible = false, period = 60 }],
          ]
          yAxis = { left = { min = 0, label = "seconds" } }
        }
      },

      {
        type   = "metric"
        x      = 12
        y      = 12
        width  = 12
        height = 6
        properties = {
          title  = "Policy Reloads & Audit Ring Utilization"
          region = var.aws_region
          view   = "timeSeries"
          period = 60
          metrics = [
            ["PerchGuard", "perchguard_audit_ring_utilization", { label = "audit ring records", yAxis = "left" }],
            ["PerchGuard", "perchguard_policy_reload_total", { label = "policy reloads (cumul.)", yAxis = "right" }],
          ]
          yAxis = {
            left  = { min = 0, label = "records" }
            right = { min = 0, label = "reloads" }
          }
        }
      },

    ]
  })
}

output "cloudwatch_dashboard_url" {
  description = "Direct link to the PerchGuard governance dashboard in CloudWatch."
  value       = "https://${var.aws_region}.console.aws.amazon.com/cloudwatch/home?region=${var.aws_region}#dashboards:name=${var.cluster_name}-governance"
}
