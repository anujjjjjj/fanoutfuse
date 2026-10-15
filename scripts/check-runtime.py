#!/usr/bin/env python3
"""Probe the built executable's guard latency and idle daemon resources."""
import json
import os
import statistics
import subprocess
import sys
import tempfile
import time

binary = os.path.abspath(sys.argv[1] if len(sys.argv) > 1 else "./fanoutfuse")
with tempfile.TemporaryDirectory(prefix="fanoutfuse-runtime-") as sandbox:
    env = dict(os.environ, HOME=sandbox, FANOUTFUSE_HOME=sandbox + "/state",
               CLAUDE_CONFIG_DIR=sandbox + "/profile")
    subprocess.run([binary, "replay", "fixtures/69332.yaml"], env=env,
                   check=True, stdout=subprocess.DEVNULL)
    durations = []
    for _ in range(150):
        start = time.perf_counter_ns()
        result = subprocess.run([binary, "guard"], env=env,
                                input=b'{"session_id":"69332","tool_name":"Agent"}',
                                check=True, stdout=subprocess.PIPE)
        durations.append((time.perf_counter_ns() - start) / 1e6)
        assert json.loads(result.stdout)["hookSpecificOutput"]["permissionDecision"] == "deny"
    p99 = sorted(durations)[148]
    print(f"guard executable median={statistics.median(durations):.3f}ms p99={p99:.3f}ms")
    assert p99 < 5, "guard p99 exceeded 5ms on this host"
    daemon = subprocess.Popen([binary, "run"], env=env, stdout=subprocess.DEVNULL,
                              stderr=subprocess.PIPE)
    try:
        time.sleep(3)
        assert daemon.poll() is None, "daemon exited early"
        usage = subprocess.check_output(["ps", "-p", str(daemon.pid), "-o", "rss=,pcpu="]).decode().split()
        rss, cpu = int(usage[0]), float(usage[1])
        print(f"idle daemon RSS={rss} KiB CPU={cpu}% (short sample)")
        assert rss < 30 * 1024, "idle RSS exceeded 30 MiB"
        assert cpu < 1, "idle CPU exceeded 1%"
    finally:
        daemon.terminate()
        daemon.wait(timeout=10)
