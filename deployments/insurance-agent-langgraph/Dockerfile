FROM python:3.11-slim

WORKDIR /app

# Build context is the repo root (see docker-compose.yaml) so this can install
# the local perchguard SDK — sdk/python isn't reachable from this dir's own context.
COPY sdk/python /sdk/python
RUN pip install --no-cache-dir /sdk/python

COPY deployments/insurance-agent-langgraph/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY deployments/insurance-agent-langgraph/ .

# ANTHROPIC_API_KEY, PERCHGUARD_URL, MCP_URL injected at runtime via docker-compose env_file
CMD ["python", "agent.py"]
