#!/usr/bin/env -S uv run --quiet --script
# /// script
# requires-python = ">=3.13"
# dependencies = [
#     "rich>=15.0.0",
#     "typer>=0.24.2",
# ]
# ///
"""Deterministic OpenAI-compatible upstream for platform-only load tests."""

from __future__ import annotations

import json
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Annotated, Any, ClassVar

import typer
from rich.console import Console

app = typer.Typer(add_completion=False, rich_markup_mode="rich")
err = Console(stderr=True)


#: The one sentence every layer asserts on, so a wrong upstream is obvious rather than plausible.
REPLY_TEXT = "Fake model response for platform latency measurement."


def _response(request: dict[str, Any]) -> dict[str, Any]:
    """Return the smallest Responses API reply accepted by OpenAI clients.

    The agent speaks only `/v1/responses` (ADK Go's `openaimodel` has no
    chat-completions code path), so this mirrors the fields a real Ollama
    `/v1/responses` reply carries: a completed `output` message with one
    `output_text` part, and the `usage` block token budgets read.
    """
    model = request.get("model")
    model_name = model if isinstance(model, str) else "agentops-fake"
    return {
        "id": "resp-agentops-fake",
        "object": "response",
        "created_at": 0,
        "completed_at": 0,
        "status": "completed",
        "model": model_name,
        "output": [
            {
                "id": "msg-agentops-fake",
                "type": "message",
                "status": "completed",
                "role": "assistant",
                "content": [{"type": "output_text", "text": REPLY_TEXT, "annotations": []}],
            }
        ],
        "error": None,
        "incomplete_details": None,
        "parallel_tool_calls": True,
        "tool_choice": "auto",
        "tools": [],
        "usage": {"input_tokens": 10, "output_tokens": 8, "total_tokens": 18},
    }


class FakeModelHandler(BaseHTTPRequestHandler):
    """Serve health and OpenAI-compatible Responses API requests."""

    server_version = "AgentOpsFakeModel/1.0"
    protocol_version = "HTTP/1.1"
    allowed_paths: ClassVar[set[str]] = {"/v1/responses"}

    def do_GET(self) -> None:
        if self.path != "/healthz":
            self.send_error(HTTPStatus.NOT_FOUND)
            return
        self._write_json(HTTPStatus.OK, {"status": "ok"})

    def do_POST(self) -> None:
        if self.path not in self.allowed_paths:
            self.send_error(HTTPStatus.NOT_FOUND)
            return

        try:
            length = int(self.headers.get("Content-Length", "0"))
            request = json.loads(self.rfile.read(length))
            if not isinstance(request, dict):
                raise ValueError("request body must be a JSON object")
            if request.get("stream") is True:
                raise ValueError("streaming is intentionally unsupported; keep AGENT_A2A_STREAMING=false")
        except (UnicodeDecodeError, json.JSONDecodeError, ValueError) as error:
            self._write_json(HTTPStatus.BAD_REQUEST, {"error": {"message": str(error)}})
            return

        self._write_json(HTTPStatus.OK, _response(request))

    def log_message(self, format: str, *args: object) -> None:  # noqa: A002 - stdlib override
        """Keep load-test output focused on k6 rather than per-request access logs."""

    def _write_json(self, status: HTTPStatus, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


@app.command()
def main(
    host: Annotated[str, typer.Option(help="Bind address.")] = "127.0.0.1",
    port: Annotated[int, typer.Option(help="Bind port.", min=1, max=65535)] = 11434,
) -> None:
    """Run the deterministic upstream on the Ollama-compatible host port."""
    try:
        with ThreadingHTTPServer((host, port), FakeModelHandler) as server:
            err.print(f"[dim]Fake model listening on http://{host}:{port}[/dim]")
            try:
                server.serve_forever()
            except KeyboardInterrupt:
                return
    except Exception:
        err.print_exception(show_locals=True)
        raise typer.Exit(code=1) from None


if __name__ == "__main__":
    app()
