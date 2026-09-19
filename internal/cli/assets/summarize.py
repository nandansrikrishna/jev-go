"""Summarize the latest result per ticket, including resumed runs."""
import json
import sys
from collections import Counter

rows = {}
with open(sys.argv[1], encoding="utf-8") as stream:
    for line in stream:
        row = json.loads(line)
        rows[row["id"]] = row
success = [row for row in rows.values() if "answers" in row]
print(json.dumps({
    "tickets": len(rows),
    "failed": len(rows) - len(success),
    "issues": dict(Counter(row["answers"]["issue"]["choice"] for row in success)),
    "most_relevant": [{"id": row["id"], "probability": row["answers"]["bug_relevance"]["noul"]}
                      for row in sorted(success, key=lambda row: row["answers"]["bug_relevance"]["noul"], reverse=True)[:5]],
}, indent=2))
