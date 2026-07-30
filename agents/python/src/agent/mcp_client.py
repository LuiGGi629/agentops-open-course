"""Consume the AgentOps Agent MCP server as a client toolset (Chapter 3.3).

``McpToolset`` launches the MCP server (here over stdio) and adapts its tools into ADK tools an
agent can call — no code change to the agent beyond adding the toolset. This is the seam the
gateway later slots into (Ch. 5.2): point the toolset at the gateway instead of the raw server.
"""

from __future__ import annotations

import sys

from google.adk.tools.mcp_tool.mcp_session_manager import StdioConnectionParams, StreamableHTTPConnectionParams
from google.adk.tools.mcp_tool.mcp_toolset import McpToolset
from mcp import StdioServerParameters

from .config import settings

# The exact read tools this agent will accept from an MCP server, in the order the server
# registers them. This is a least-privilege allowlist, not documentation: a tool's *name,
# description, and schema* reach the model as instruction text at connection time, so a
# compromised or swapped server can inject prose simply by registering a new tool. Pinning the
# set means a renamed or added tool is never offered to the model at all. `skills.py` applies
# the same rule to its toolset; `scripts/check-infra.sh` asserts the gateway allowlist matches.
# --8<-- [start:mcp-read-tools]
MCP_READ_TOOL_NAMES = (
    "list_incidents",
    "get_incident",
    "get_service_status",
    "search_service_logs",
    "get_runbook",
    "search_runbooks",
)
# --8<-- [end:mcp-read-tools]


# --8<-- [start:ops-mcp-toolset]
def ops_mcp_toolset(url: str | None = None) -> McpToolset:
    """Return the toolset over local stdio or a gateway streamable-HTTP URL.

    Both transports carry the course's explicit deadlines (Chapter 4.5): a hung
    MCP server or gateway then fails a tool call fast instead of hanging a turn.
    ``tool_filter`` pins which tools may be offered, so a server cannot widen the
    agent's surface — or reach the model with new description text — by adding one.
    """
    endpoint = url or settings.mcp_url
    if endpoint:
        # A secured gateway route (Ch. 5.5) authenticates the caller by bearer
        # token; the default local route needs no header.
        headers = {"Authorization": f"Bearer {settings.mcp_token.get_secret_value()}"} if settings.mcp_token else None
        return McpToolset(
            connection_params=StreamableHTTPConnectionParams(
                url=endpoint,
                headers=headers,
                timeout=settings.tool_timeout_s,
                sse_read_timeout=settings.tool_timeout_s,
            ),
            tool_filter=list(MCP_READ_TOOL_NAMES),
        )
    return McpToolset(
        connection_params=StdioConnectionParams(
            # Use the current interpreter so it works inside the project's virtualenv.
            server_params=StdioServerParameters(command=sys.executable, args=["-m", "agent.mcp_server"]),
            timeout=settings.tool_timeout_s,
        ),
        tool_filter=list(MCP_READ_TOOL_NAMES),
    )


# --8<-- [end:ops-mcp-toolset]
