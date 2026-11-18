"""Generate the command index from Cobra's English help."""
import os
from pathlib import Path
import re
import subprocess
import sys

binary = sys.argv[1]
rows = []
def walk(parts):
    text = subprocess.check_output([binary, *parts, "--help"], text=True,
                                   env={**os.environ, "SINGFORGE_LANG": "en"})
    if "[command]" in text.partition("Flags:")[0]:
        for name in re.findall(r"^  ([a-z][a-z0-9-]*) +[A-Z]",text,re.M):
            if name not in {"completion", "help"}:
                walk([*parts,name])
        return
    flags = re.search(r"^Flags:\n(.*?)(?:\n\n|\Z)", text, re.S | re.M)
    names = re.findall(r"--([a-z][a-z-]+)", flags[1]) if flags else []
    rows.append("| `singforge " + " ".join(parts) + "` | " + text.splitlines()[0] + " | " +
                ", ".join("`--"+n+"`" for n in names if n!="help") + " |")

walk([])
Path("docs/cli.md").write_text("# CLI reference\n\nGenerated from Cobra. Append `--help` to any command "
    "for flag types and defaults. Global options are `--workspace`, `--core`, `--json`, "
    "`--dry-run` and `--yes`. Shell completions are available through `singforge completion`.\n\n"
    "| Command | Purpose | Local options |\n|---|---|---|\n"+"\n".join(rows)+"\n")
