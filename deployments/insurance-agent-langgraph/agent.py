#!/usr/bin/env python3
"""
PerchGuard Practice Lab — Phase 11: LangGraph multi-agent delegation

Real LangGraph-orchestrated delegation graph, every hop governed by PerchGuard.
LangGraph owns orchestration (fan-out to fraud_investigator + compliance_reviewer,
fan-in at senior_approver, hand-off to notifier); PerchGuard owns governance
(session lifecycle, delegation scope, and — critically — verifying each child's
asserted parent via that parent's own token, not trusting graph state). See
graph.py and sdk/python/perchguard/langgraph_adapter.py for the design.
"""

import os

import anthropic
from graph import ClaimReviewState, build_graph

PERCHGUARD_URL = os.getenv("PERCHGUARD_URL", "http://localhost:8080")
MCP_URL = os.getenv("MCP_URL", "http://localhost:8090")
MODEL = os.getenv("CLAUDE_MODEL", "claude-haiku-4-5-20251001")


def main():
    print(f"PerchGuard: {PERCHGUARD_URL}")
    print(f"MCP server: {MCP_URL}")
    print(f"Model:      {MODEL}")
    print()
    print("=" * 70)
    print("GRAPH: fraud_escalation_graph")
    print("       intake -> {investigator, compliance} -> approver -> notifier")
    print("-" * 70)

    anthropic_client = anthropic.Anthropic()
    graph = build_graph(anthropic_client)
    final_state: ClaimReviewState = graph.invoke(ClaimReviewState())

    print()
    print(f"INTAKE:       {final_state.get('intake_summary')}")
    print(f"INVESTIGATOR: {final_state.get('investigator_findings')}")
    print(f"COMPLIANCE:   {final_state.get('compliance_findings')}")
    print(f"APPROVAL:     {final_state.get('approval_decision')}")
    print(f"NOTIFICATION: {final_state.get('notification_result')}")


if __name__ == "__main__":
    main()
