"""Scenario text for the LangGraph fraud-escalation graph (Phase 11).

Same claim (CLM-9985, $2.1M product liability, in litigation — already in the mock
MCP server's claim list) as the hand-wired 2-hop demo
(deployments/insurance-agent-python/tasks.py's FRAUD_ESCALATION_TASK), extended
into a real 5-role diamond: claims_intake fans out to fraud_investigator +
compliance_reviewer concurrently, both join at senior_approver, which hands off to
notifier.
"""

INTAKE_TASK = (
    "Call read_policy for claim CLM-9985, then call search_claims for 'Berger' to check "
    "history. This is a $2.1M product liability claim under litigation. Summarise why it "
    "needs deeper review."
)

INVESTIGATOR_TASK_TEMPLATE = (
    "You are a fraud investigator reviewing claim CLM-9985. Intake summary: "
    "{intake_summary}\n\n"
    "Use search_claims and run_sql to investigate for fraud indicators, then call "
    "write_report with filename='investigation_CLM-9985.txt' summarising your findings."
)

COMPLIANCE_TASK_TEMPLATE = (
    "You are a compliance reviewer checking claim CLM-9985 against policy terms. Intake "
    "summary: {intake_summary}\n\n"
    "Use read_policy and search_claims to check compliance, then call write_report with "
    "filename='compliance_CLM-9985.txt' summarising whether the claim's handling so far "
    "meets policy requirements."
)

APPROVAL_TASK_TEMPLATE = (
    "You are the senior approver making the final call on claim CLM-9985.\n\n"
    "Investigator findings: {investigator_findings}\n\n"
    "Compliance findings: {compliance_findings}\n\n"
    "Call write_report with filename='decision_CLM-9985.txt' stating your approve/hold "
    "decision and the reasoning, combining both inputs."
)

NOTIFY_TASK_TEMPLATE = (
    "The senior approver's decision on claim CLM-9985 is: {decision}\n\n"
    "Call send_notification with destination='claims-team@internal' and a message "
    "summarising this decision for the claims team."
)
