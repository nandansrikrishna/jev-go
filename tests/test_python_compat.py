import json
from pathlib import Path
import pytest


def test_native_go_mcp_server():
    """Exercise the public Go command against a real MCP client session."""
    import asyncio
    import os
    from mcp import ClientSession, StdioServerParameters
    from mcp.client.stdio import stdio_client

    default_binary = Path(__file__).resolve().parents[1] / "bin" / ("jev.exe" if os.name == "nt" else "jev")
    binary = os.environ.get("JEV_TEST_BIN") or str(default_binary)
    if not os.path.isfile(binary):
        pytest.skip("Build/install the Go CLI or set JEV_TEST_BIN")

    async def check():
        params = StdioServerParameters(
            command=binary, args=["mcp"],
            env={**os.environ, "PATH": "", "JEV_PYTHON": "/does/not/exist"})
        async with stdio_client(params) as (read, write):
            async with ClientSession(read, write) as session:
                await session.initialize()
                tools = await session.list_tools()
                assert {tool.name for tool in tools.tools} == {
                    "evaluate", "evaluate_batch", "question_schema"}
                result = await session.call_tool("question_schema", {})
                assert not result.isError
    asyncio.run(check())


def test_go_schema_and_templates_match_python():
    from jev.core import QUESTION_ADAPTER
    from importlib.resources import files
    root = Path(__file__).resolve().parents[1]
    assets = root / "internal" / "cli" / "assets"
    if not assets.exists():
        pytest.skip("Go sources are not included in the Python distribution")
    assert json.loads((assets / "schema.json").read_text()) == QUESTION_ADAPTER.json_schema()
    for name in ("questions.json", "tickets.jsonl", "summarize.py"):
        assert (assets / name).read_bytes() == files("jev").joinpath("templates", name).read_bytes()
