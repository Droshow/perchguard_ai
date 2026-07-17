from perchguard import Action, Decision, GovernedAgentLoop, Session


class FakeBlock:
    def __init__(self, type_, **kw):
        self.type = type_
        for k, v in kw.items():
            setattr(self, k, v)


class FakeResponse:
    def __init__(self, stop_reason, content):
        self.stop_reason = stop_reason
        self.content = content


class FakeAnthropic:
    """Records messages.create calls; returns pre-programmed responses in order."""

    def __init__(self, responses):
        self._responses = list(responses)
        self.calls = []

        class _Messages:
            def create(inner_self, **kwargs):
                self.calls.append(kwargs)
                return self._responses.pop(0)

        self.messages = _Messages()


class FakeMCP:
    def __init__(self, output="tool ran fine"):
        self.output = output
        self.calls = []

    def call_tool(self, name, arguments):
        self.calls.append((name, arguments))
        return self.output


class FakePG:
    """Stands in for PerchGuardClient inside GovernedAgentLoop."""

    def __init__(self, intercept_decisions, output_decisions):
        self._intercept_decisions = list(intercept_decisions)
        self._output_decisions = list(output_decisions)
        self.registered = []
        self.evicted = []

    def register(self, **kwargs):
        session = Session(
            id="pg-test-session",
            agent_id=kwargs["agent_id"],
            agent_role=kwargs["agent_role"],
            manifest_version="0.1.0",
            token="pgat-fake",
            intent=kwargs["intent"],
            _client=self,
        )
        self.registered.append(kwargs)
        return session

    def evict(self, session, best_effort=False):
        self.evicted.append(session.id)

    def intercept(self, **kwargs):
        return self._intercept_decisions.pop(0)

    def validate_output(self, **kwargs):
        return self._output_decisions.pop(0)


def _make_loop(anthropic_responses, intercept_decisions=(), output_decisions=(), mcp_output="ok"):
    loop = GovernedAgentLoop(
        anthropic_client=FakeAnthropic(anthropic_responses),
        perchguard_url="http://localhost:8080",
        mcp_url="http://localhost:8090",
        model="claude-haiku-4-5-20251001",
        verbose=False,
    )
    loop.pg = FakePG(intercept_decisions, output_decisions)
    loop.mcp = FakeMCP(mcp_output)
    return loop


def test_run_allow_path_registers_and_evicts_session():
    tool_block = FakeBlock("tool_use", id="t1", name="read_file", input={"path": "a.txt"})
    responses = [
        FakeResponse("tool_use", [tool_block]),
        FakeResponse("end_turn", [FakeBlock("text", text="done")]),
    ]
    loop = _make_loop(
        responses,
        intercept_decisions=[Decision(action=Action.ALLOW, reason="ok")],
        output_decisions=[Decision(action=Action.ALLOW, reason="ok")],
        mcp_output="file contents",
    )

    result = loop.run(task="read a file", agent_id="test-agent", agent_role="developer_agent", tools=[])

    assert result == "done"
    assert loop.mcp.calls == [("read_file", {"path": "a.txt"})]
    assert len(loop.pg.registered) == 1
    assert loop.pg.evicted == ["pg-test-session"]


def test_run_deny_blocks_tool_execution():
    tool_block = FakeBlock("tool_use", id="t1", name="delete_all", input={})
    responses = [
        FakeResponse("tool_use", [tool_block]),
        FakeResponse("end_turn", [FakeBlock("text", text="stopped")]),
    ]
    loop = _make_loop(
        responses,
        intercept_decisions=[Decision(action=Action.DENY, reason="not authorized")],
    )

    result = loop.run(task="delete everything", agent_id="test-agent", agent_role="developer_agent", tools=[])

    assert result == "stopped"
    assert loop.mcp.calls == []  # tool never executed

    # the blocked message must have been spliced back into the conversation
    last_call_messages = loop.anthropic.calls[-1]["messages"]
    tool_results = last_call_messages[-1]["content"]
    assert "GOVERNANCE BLOCKED" in tool_results[0]["content"]


def test_run_reuses_caller_supplied_session_without_evicting():
    session = Session(
        id="pg-caller-owned",
        agent_id="test-agent",
        agent_role="developer_agent",
        manifest_version="0.1.0",
        token="pgat-fake",
        intent="task",
    )
    responses = [FakeResponse("end_turn", [FakeBlock("text", text="done")])]
    loop = _make_loop(responses)

    result = loop.run(
        task="task", agent_id="test-agent", agent_role="developer_agent", tools=[], session=session
    )

    assert result == "done"
    assert loop.pg.registered == []  # caller owns this session, SDK must not re-register
    assert loop.pg.evicted == []  # ...and must not evict it either


def test_run_hits_max_iterations_ceiling():
    tool_block = FakeBlock("tool_use", id="t1", name="loop_tool", input={})
    responses = [FakeResponse("tool_use", [tool_block]) for _ in range(3)]
    loop = _make_loop(
        responses,
        intercept_decisions=[Decision(action=Action.ALLOW, reason="ok")] * 3,
        output_decisions=[Decision(action=Action.ALLOW, reason="ok")] * 3,
    )
    loop.max_iterations = 3

    result = loop.run(task="loop forever", agent_id="test-agent", agent_role="developer_agent", tools=[])

    assert result == "(agent hit max_iterations safety ceiling)"
