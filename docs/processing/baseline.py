#!/usr/bin/env python3
"""Run the frozen pre-refactor baseline without changing the working tree.

Uses .env only for OPENROUTER_* variables. --live explicitly permits billed API
calls and writes private source/card output outside the repository by default.
"""
import argparse
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("pdf", type=Path)
parser.add_argument("--live", action="store_true")
parser.add_argument("--output", type=Path, default=Path("/tmp/pomnibot-live-baseline.json"))
args = parser.parse_args()
repo = Path(__file__).resolve().parents[2]
env = os.environ.copy()
env["GOCACHE"] = env.get("GOCACHE", "/tmp/pomnibot-go-cache")
env["POMNIBOT_EVAL_PDF"] = str(args.pdf.resolve())
if args.live:
    for line in (repo / ".env").read_text().splitlines():
        key, sep, value = line.partition("=")
        if sep and key.startswith("OPENROUTER_"):
            env[key] = value.strip().strip('"').strip("'")
    env["POMNIBOT_LIVE_BASELINE"] = "1"
    env["POMNIBOT_BASELINE_OUTPUT"] = str(args.output.resolve())
with tempfile.TemporaryDirectory(prefix="pomnibot-baseline-") as tmp:
    archive = Path(tmp) / "source.tar"
    with archive.open("wb") as output:
        subprocess.run(["git", "archive", "8e898c27d507828eac1a71ecd4802a5206606afa"], cwd=repo, stdout=output, check=True)
    source = Path(tmp) / "source"
    source.mkdir()
    with tarfile.open(archive) as packed:
        packed.extractall(source, filter="data")
    kind = "live_baseline" if args.live else "frozen_reading"
    fixture = Path(__file__).with_name(kind + "_test.go.txt")
    destination = source / "backend/internal/generator" / (kind + "_test.go")
    destination.write_bytes(fixture.read_bytes())
    test = "TestLiveBaseline" if args.live else "TestFrozenReadingBaseline"
    subprocess.run(["go", "-C", str(source / "backend"), "test", "./internal/generator", "-run", "^" + test + "$", "-v", "-timeout", "30m"], env=env, check=True)
