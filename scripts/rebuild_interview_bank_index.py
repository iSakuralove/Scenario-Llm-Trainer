import json
import subprocess
import time
import urllib.request


def http(method, url, token=None, body=None, timeout=180):
    data = None if body is None else json.dumps(body, ensure_ascii=False).encode("utf-8")
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode())


def psql(sql: str) -> str:
    out = subprocess.check_output(
        [
            "docker",
            "exec",
            "teaching-mvp-postgres-1",
            "psql",
            "-U",
            "teaching",
            "-d",
            "teaching_mvp",
            "-t",
            "-A",
            "-c",
            sql,
        ],
        text=True,
        encoding="utf-8",
    )
    return out.strip()


def sync_ready_atoms() -> int:
    result = psql(
        """
WITH ready AS (
  SELECT a.id
  FROM interview_knowledge_atoms a
  JOIN interview_knowledge_vector_documents d ON d.atom_id = a.id
  WHERE a.vector_status = 'pending'
  GROUP BY a.id
)
UPDATE interview_knowledge_atoms a
SET vector_status = 'indexed',
    last_indexed_at = COALESCE(last_indexed_at, NOW()),
    updated_at = NOW()
FROM ready r
WHERE a.id = r.id;
"""
    )
    # command returns empty; count indexed after
    return int(psql("SELECT COUNT(*) FROM interview_knowledge_atoms WHERE vector_status='indexed'") or "0")


def main() -> None:
    login = http(
        "POST",
        "http://127.0.0.1:8080/api/v1/auth/login",
        body={"identifier": "admin", "password": "admin123"},
    )
    token = login["data"]["access_token"]
    print("login ok")
    print("synced indexed count", sync_ready_atoms())

    ids = [
        line
        for line in psql(
            """
SELECT a.id
FROM interview_knowledge_atoms a
WHERE a.vector_status = 'pending'
  AND NOT EXISTS (
    SELECT 1 FROM interview_knowledge_vector_documents d WHERE d.atom_id = a.id
  )
ORDER BY a.id
"""
        ).splitlines()
        if line.strip()
    ]
    print("pending_without_docs", len(ids))

    ok = 0
    fail = 0
    for n, atom_id in enumerate(ids, 1):
        try:
            resp = http(
                "POST",
                "http://127.0.0.1:8080/api/v1/admin/interview-bank/index/rebuild",
                token=token,
                body={"atom_ids": [atom_id], "limit": 1},
                timeout=180,
            )
            data = resp.get("data", resp)
            results = data.get("results") or []
            status = results[0].get("status") if results else "unknown"
            err = results[0].get("error") if results else ""
            if status == "indexed":
                ok += 1
            else:
                fail += 1
                print(f"[{n}/{len(ids)}] FAIL {atom_id}: {status} {err}")
                # docs may have been written before status update race
                sync_ready_atoms()
            if n % 10 == 0 or status == "indexed":
                print(f"[{n}/{len(ids)}] {status} ok={ok} fail={fail}")
        except Exception as e:
            fail += 1
            print(f"[{n}/{len(ids)}] ERR {atom_id}: {e}")
            sync_ready_atoms()
            time.sleep(1)
        time.sleep(0.2)

    print("final sync indexed", sync_ready_atoms())
    summary = http(
        "GET",
        "http://127.0.0.1:8080/api/v1/admin/interview-bank/summary",
        token=token,
    )
    print("summary", json.dumps(summary.get("data", summary), ensure_ascii=False))
    print(
        "vector_status",
        psql(
            "SELECT vector_status || '=' || COUNT(*) FROM interview_knowledge_atoms GROUP BY vector_status ORDER BY 1"
        ),
    )
    print(
        "docs",
        psql(
            "SELECT 'docs=' || COUNT(*) || ' atoms=' || COUNT(DISTINCT atom_id) FROM interview_knowledge_vector_documents"
        ),
    )
    print("done ok", ok, "fail", fail)


if __name__ == "__main__":
    main()
