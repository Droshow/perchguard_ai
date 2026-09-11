# ─── Cilium CNI — scoped to the isolated-agent node group only (Phase 10c) ────
#
# The rest of the cluster (kube-system, perchguard, LBC, ADOT — all Fargate)
# keeps the default VPC CNI (aws-node) untouched. Only the new EC2 node group
# runs Cilium, which is what makes FQDN-level egress enforcement
# (CiliumNetworkPolicy, pkg/operator/render/cilium.go) possible for governed
# agent workloads without touching the stable Fargate-based control plane.
#
# This is the first real validation of "one CNI scoped to a node subset while
# another handles the rest" in this repo — see the "Real risk" note in the
# Phase 10c plan. Because IsolationEnforcer only ever writes when
# spec.mode == Enforce (shipped default: Observe), the worst case if this is
# wrong is that the agent-workloads node group's pods lose connectivity, not a
# cluster-wide outage — recoverable by destroying just that node group.

# ─── Exclude the new node group from VPC CNI's aws-node DaemonSet ────────────
# Same local-exec precedent as null_resource.coredns_fargate_patch (gateway.tf).

resource "null_resource" "aws_node_exclude_agent_nodes" {
  triggers = {
    cluster_name   = aws_eks_cluster.perchguard.name
    workload_class = "isolated-agent"
  }

  provisioner "local-exec" {
    environment = {
      KUBECONFIG = local_file.kubeconfig.filename
    }
    command = <<-EOT
      kubectl patch daemonset aws-node -n kube-system --type merge -p \
        '{"spec":{"template":{"spec":{"affinity":{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"perchguard/workload-class","operator":"NotIn","values":["isolated-agent"]}]}]}}}}}}}'
    EOT
  }

  depends_on = [
    aws_eks_node_group.agent_workloads,
    null_resource.coredns_fargate_patch,
    local_file.kubeconfig,
  ]
}

# ─── Cilium — primary CNI on the isolated-agent node group only ──────────────
# ipam.mode=eni reuses VPC CNI's IP allocation model (no overlay/BGP setup);
# affinity/tolerations scope Cilium's own agent DaemonSet to exactly the nodes
# aws-node was just excluded from, via the taint on aws_eks_node_group.agent_workloads.
# Hubble stays disabled — that's Phase 10d's shadow-rollout tooling, not this phase.

resource "helm_release" "cilium" {
  name       = "cilium"
  repository = "https://helm.cilium.io/"
  chart      = "cilium"
  version    = "1.16.5"
  # Deliberately NOT kube-system: that namespace's Fargate profile has no
  # label selector, so EKS's Fargate admission webhook force-sets
  # schedulerName=fargate-scheduler on every pod created there — including
  # cilium-operator (a Deployment, so unlike the agent DaemonSet it isn't
  # structurally exempt from Fargate matching). Fargate then rejects it
  # outright for hostNetwork, and no toleration/affinity on the pod can
  # override a scheduler assignment made by the webhook. A namespace with no
  # Fargate profile sidesteps the webhook entirely; tolerations/affinity then
  # do their normal job against the default scheduler.
  namespace        = "cilium-system"
  create_namespace = true

  set {
    name  = "ipam.mode"
    value = "eni"
  }
  set {
    name  = "eni.enabled"
    value = "true"
  }
  set {
    name  = "egressMasqueradeInterfaces"
    value = "eth0"
  }
  set {
    name  = "routingMode"
    value = "native"
  }
  # Phase 10d: Hubble flow-log burn-in — this is what lets a namespace be
  # observed for real egress needs before its AgentIsolationPolicy flips
  # Observe -> Enforce, and what Phase 10f's cross-layer conformance job diffs
  # against the app-layer's own audit denials. Relay only (no UI): the
  # conformance job is the only Hubble client this repo ships.
  set {
    name  = "hubble.enabled"
    value = "true"
  }
  set {
    name  = "hubble.relay.enabled"
    value = "true"
  }
  set {
    name  = "hubble.metrics.enabled"
    value = "{drop,flow}"
  }
  set {
    name  = "hubble.metrics.enableOpenMetrics"
    value = "true"
  }

  # hubble-relay is a Deployment, not the agent DaemonSet — it needs its own
  # toleration for the same taint. Learned live 2026-09-11: cilium-system has
  # no Fargate profile (cilium-operator's hostNetwork requirement, see the
  # comment above), and the only real EC2 node is tainted
  # perchguard.io/agent-workload — without this, hubble-relay has nowhere to
  # schedule ("0/5 nodes are available", both Fargate and the EC2 node rejected).
  set {
    name  = "hubble.relay.tolerations[0].key"
    value = "perchguard.io/agent-workload"
  }
  set {
    name  = "hubble.relay.tolerations[0].operator"
    value = "Equal"
  }
  set {
    name  = "hubble.relay.tolerations[0].value"
    value = "true"
    type  = "string"
  }
  set {
    name  = "hubble.relay.tolerations[0].effect"
    value = "NoSchedule"
  }

  set {
    name  = "affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key"
    value = "perchguard/workload-class"
  }
  set {
    name  = "affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator"
    value = "In"
  }
  set {
    name  = "affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values[0]"
    value = "isolated-agent"
  }

  set {
    name  = "tolerations[0].key"
    value = "perchguard.io/agent-workload"
  }
  set {
    name  = "tolerations[0].operator"
    value = "Equal"
  }
  set {
    name  = "tolerations[0].value"
    value = "true"
    type  = "string"
  }
  set {
    name  = "tolerations[0].effect"
    value = "NoSchedule"
  }

  # cilium-operator runs hostNetwork: true, which Fargate rejects outright
  # ("fields not supported: HostNetwork"). It has no relation to the agent
  # DaemonSet's affinity/tolerations above (separate values path in the
  # chart), so without these it's stuck Pending forever — nowhere to run.
  # replicas=1 because the operator's default HA pod anti-affinity can't be
  # satisfied by the single-node agent-workloads group (lab budget, see
  # variables.tf agent_node_desired_size).
  set {
    name  = "operator.replicas"
    value = "1"
  }
  set {
    name  = "operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key"
    value = "perchguard/workload-class"
  }
  set {
    name  = "operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator"
    value = "In"
  }
  set {
    name  = "operator.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values[0]"
    value = "isolated-agent"
  }
  set {
    name  = "operator.tolerations[0].key"
    value = "perchguard.io/agent-workload"
  }
  set {
    name  = "operator.tolerations[0].operator"
    value = "Equal"
  }
  set {
    name  = "operator.tolerations[0].value"
    value = "true"
    type  = "string"
  }
  set {
    name  = "operator.tolerations[0].effect"
    value = "NoSchedule"
  }

  # cilium-envoy is yet another DaemonSet with its own separate values path
  # (envoy.*, not inherited from the top-level affinity/tolerations above).
  # Without this it targets every node — including the cluster's Fargate
  # virtual nodes it can never run on — and `helm_release`'s default wait
  # counts those in the DaemonSet's desiredNumberScheduled, so it waits for
  # pods that can never schedule and times out even though the real pod (on
  # the one EC2 node) is healthy.
  set {
    name  = "envoy.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].key"
    value = "perchguard/workload-class"
  }
  set {
    name  = "envoy.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].operator"
    value = "In"
  }
  set {
    name  = "envoy.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values[0]"
    value = "isolated-agent"
  }
  set {
    name  = "envoy.tolerations[0].key"
    value = "perchguard.io/agent-workload"
  }
  set {
    name  = "envoy.tolerations[0].operator"
    value = "Equal"
  }
  set {
    name  = "envoy.tolerations[0].value"
    value = "true"
    type  = "string"
  }
  set {
    name  = "envoy.tolerations[0].effect"
    value = "NoSchedule"
  }

  depends_on = [
    aws_eks_node_group.agent_workloads,
    null_resource.aws_node_exclude_agent_nodes,
  ]
}

# ─── AgentIsolationPolicy CRD ─────────────────────────────────────────────────
# Reuses the CRD YAML already committed for k3s (controller-gen output,
# pkg/operator/api/v1alpha1) rather than duplicating it here.

resource "null_resource" "agent_isolation_policy_crd" {
  triggers = {
    cluster_name = aws_eks_cluster.perchguard.name
    crd_file_sha = filesha256("${path.module}/../k3s/agent-isolation-policy-crd.yaml")
  }

  provisioner "local-exec" {
    environment = {
      KUBECONFIG = local_file.kubeconfig.filename
    }
    command = <<-EOT
      kubectl apply -f ${path.module}/../k3s/agent-isolation-policy-crd.yaml
      kubectl wait --for=condition=Established crd/agentisolationpolicies.security.perchguard.io --timeout=60s
    EOT
  }

  depends_on = [null_resource.coredns_fargate_patch, local_file.kubeconfig]
}
