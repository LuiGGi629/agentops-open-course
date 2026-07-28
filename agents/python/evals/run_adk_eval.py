"""Run ADK evaluation with an honest process exit status.

The locked ADK CLI prints metric failures in its summary but still exits successfully.
This wrapper preserves the live CLI output and turns that summary into the
process contract used by local tasks and scheduled CI.
"""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

_SUMMARY_HEADER = re.compile(r"(?m)^Eval Run Summary\s*$")
_SUMMARY = re.compile(
    r"(?m)^[^\s:\r\n][^:\r\n]*:\s*\r?\n"
    r"^[ \t]+Tests passed:\s*(\d+)\s*\r?\n"
    r"^[ \t]+Tests failed:\s*(\d+)\s*$"
)


def verdict(output: str, process_returncode: int, min_pass_rate: float = 1.0) -> tuple[int, str]:
    """Return a truthful exit code for ADK's process and aggregate case floor."""
    if process_returncode:
        return process_returncode, f"ADK evaluation process failed with exit code {process_returncode}."

    headers = list(_SUMMARY_HEADER.finditer(output))
    if not headers:
        return 2, "ADK evaluation produced no run summary."
    # Model text is untrusted and may contain summary-shaped lines. ADK emits its
    # authoritative run summary last, after every model response and tool event.
    summary_section = output[headers[-1].end() :]
    summaries = [(int(passed), int(failed)) for passed, failed in _SUMMARY.findall(summary_section)]
    if not summaries:
        return 2, "ADK evaluation produced no run summary."

    passed = sum(item[0] for item in summaries)
    failed = sum(item[1] for item in summaries)
    total = passed + failed
    if not total:
        return 2, "ADK evaluation ran zero cases."
    pass_rate = passed / total
    message = (
        f"ADK evaluation case pass rate: {passed}/{total} ({pass_rate:.0%}); "
        f"required aggregate floor: {min_pass_rate:.0%}."
    )
    return (0, f"{message} Floor met.") if pass_rate >= min_pass_rate else (1, f"{message} Floor missed.")


def pass_rate(value: str) -> float:
    """Parse a positive pass-rate floor no greater than one."""
    try:
        parsed = float(value)
    except ValueError as error:
        raise argparse.ArgumentTypeError("must be a number greater than 0 and at most 1") from error
    if not 0 < parsed <= 1:
        raise argparse.ArgumentTypeError("must be greater than 0 and at most 1")
    return parsed


def parse_args() -> argparse.Namespace:
    """Parse the eval set and optional shared ADK paths."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("eval_set", type=Path)
    parser.add_argument("--agent", type=Path, default=Path("src/agent"))
    parser.add_argument("--config", type=Path, default=Path("evals/test_config.json"))
    parser.add_argument(
        "--min-pass-rate",
        type=pass_rate,
        default=1.0,
        help="minimum aggregate fraction of strictly passing eval cases (default: 1.0)",
    )
    return parser.parse_args()


def main() -> None:  # pragma: no cover - the model-backed subprocess belongs to the scheduled lane
    """Stream ADK output, then enforce the reported evaluation verdict."""
    args = parse_args()
    command = [
        sys.executable,
        "-m",
        "google.adk.cli",
        "eval",
        str(args.agent),
        str(args.eval_set),
        "--config_file_path",
        str(args.config),
    ]
    process = subprocess.Popen(  # noqa: S603 - fixed executable and argv, never a shell
        command,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        bufsize=1,
    )
    if process.stdout is None:  # defensive: PIPE above guarantees a stream
        raise RuntimeError("ADK evaluation process has no output stream.")
    lines: list[str] = []
    for line in process.stdout:
        lines.append(line)
        print(line, end="", flush=True)  # noqa: T201 - preserve the evaluator's live CLI output

    returncode, message = verdict("".join(lines), process.wait(), args.min_pass_rate)
    print(message, file=sys.stderr if returncode else sys.stdout)
    raise SystemExit(returncode)


if __name__ == "__main__":
    main()
