FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

# ANTHROPIC_API_KEY and PERCHGUARD_MCP_URL injected at runtime via docker-compose
CMD ["python", "agent.py"]
