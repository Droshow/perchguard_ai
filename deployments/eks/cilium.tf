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
    command = <<-EOT
      aws eks update-kubeconfig --name ${var.cluster_name} --region ${var.aws_region}
      kubectl patch daemonset aws-node -n kube-system --type merge -p \
        '{"spec":{"template":{"spec":{"affinity":{"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"perchguard/workload-class","operator":"NotIn","values":["isolated-agent"]}]}]}}}}}}}'
    EOT
  }

  depends_on = [
    aws_eks_node_group.agent_workloads,
    null_resource.coredns_fargate_patch,
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
  namespace  = "kube-system"

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
  set {
    name  = "hubble.enabled"
    value = "false"
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
  }
  set {
    name  = "tolerations[0].effect"
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
    command = <<-EOT
      aws eks update-kubeconfig --name ${var.cluster_name} --region ${var.aws_region}
      kubectl apply -f ${path.module}/../k3s/agent-isolation-policy-crd.yaml
      kubectl wait --for=condition=Established crd/agentisolationpolicies.security.perchguard.io --timeout=60s
    EOT
  }

  depends_on = [null_resource.coredns_fargate_patch]
}
