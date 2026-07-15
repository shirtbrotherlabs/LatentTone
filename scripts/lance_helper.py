#!/usr/bin/env python3
# Copyright (C) 2026 martinsah
# SPDX-License-Identifier: GPL-3.0-only
# Author: martinsah
# Date: 2026-07-15
"""
Thin LanceDB helper for LatentTone (local on-disk DB).
Commands:
  upsert <db_path> <table>   — stdin JSON lines: {"track_id":1,"vector":[...]}
  search <db_path> <table> <k> — stdin JSON: {"vector":[...],"exclude_track_id":N}
                                 → stdout JSON [{"track_id":..,"score":..},...]
  dump <db_path> <table> <limit> <offset> [preview]
                                 → stdout JSON dump (vectors truncated to preview floats)
"""
from __future__ import annotations

import json
import sys


def _table_names(db) -> list[str]:
    if hasattr(db, "list_tables"):
        names = db.list_tables()
        if hasattr(names, "tables"):
            return list(names.tables)
        return list(names)
    return list(db.table_names())



def _vector_dim(tbl) -> int | None:
    """Return FixedSizeList vector width if schema exposes it."""
    try:
        schema = tbl.schema
        for field in schema:
            name = getattr(field, "name", "")
            if name != "vector":
                continue
            typ = field.type
            # pyarrow FixedSizeListType
            if hasattr(typ, "list_size"):
                return int(typ.list_size)
    except Exception:
        pass
    try:
        # Fallback: peek one row
        rows = tbl.head(1).to_pandas()
        if len(rows) and "vector" in rows.columns:
            v = rows.iloc[0]["vector"]
            return len(v)
    except Exception:
        pass
    return None


def upsert(db_path: str, table: str) -> None:
    import lancedb

    db = lancedb.connect(db_path)
    rows = []
    dim = None
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        obj = json.loads(line)
        vec = [float(x) for x in obj["vector"]]
        if dim is None:
            dim = len(vec)
        elif len(vec) != dim:
            raise SystemExit(f"vector dim mismatch: {len(vec)} != {dim}")
        rows.append({"track_id": int(obj["track_id"]), "vector": vec})
    if not rows:
        return
    if table in _table_names(db):
        tbl = db.open_table(table)
        existing_dim = _vector_dim(tbl)
        if existing_dim is not None and existing_dim != dim:
            # Extractor set grew/shrunk compiled dim — recreate table (ADR-004 / PHASE_2).
            print(
                f"lance: recreating table {table} dim {existing_dim} -> {dim}",
                file=sys.stderr,
            )
            db.drop_table(table)
            db.create_table(table, data=rows, mode="overwrite")
            return
        ids = [r["track_id"] for r in rows]
        try:
            tbl.delete(f"track_id in ({','.join(str(i) for i in ids)})")
        except Exception:
            pass
        tbl.add(rows)
    else:
        db.create_table(table, data=rows, mode="overwrite")


def search(db_path: str, table: str, k: int) -> None:
    import lancedb

    db = lancedb.connect(db_path)
    if table not in _table_names(db):
        print("[]")
        return
    tbl = db.open_table(table)
    body = json.load(sys.stdin)
    vec = [float(x) for x in body["vector"]]
    exclude = int(body.get("exclude_track_id", -1))
    # Cosine distance preferred when available (L2-normalized vectors → 1 - cosine_sim)
    try:
        q = tbl.search(vec).metric("cosine").limit(max(k + 8, k))
    except Exception:
        q = tbl.search(vec).limit(max(k + 8, k))
    res = q.to_list()
    out = []
    for r in res:
        tid = int(r["track_id"])
        if tid == exclude:
            continue
        dist = float(r.get("_distance", r.get("distance", 0.0)))
        # cosine metric distance is usually 1 - sim; L2 uses raw distance
        score = max(0.0, 1.0 - dist)
        out.append({"track_id": tid, "score": score})
        if len(out) >= k:
            break
    print(json.dumps(out))


def dump(db_path: str, table: str, limit: int, offset: int, preview: int) -> None:
    import lancedb

    if limit <= 0:
        limit = 100
    if offset < 0:
        offset = 0
    if preview <= 0:
        preview = 8

    db = lancedb.connect(db_path)
    tables = _table_names(db)
    if table not in tables:
        print(
            json.dumps(
                {
                    "db_path": db_path,
                    "table": table,
                    "tables": tables,
                    "count": 0,
                    "offset": offset,
                    "limit": limit,
                    "preview": preview,
                    "rows": [],
                    "error": f"table {table!r} not found",
                }
            )
        )
        return

    tbl = db.open_table(table)
    try:
        count = int(tbl.count_rows())
    except Exception:
        count = -1

    # to_pandas / to_arrow then slice is version-stable enough for dumps
    try:
        arrow = tbl.to_arrow()
        total = arrow.num_rows if count < 0 else count
        end = min(offset + limit, arrow.num_rows)
        batch = arrow.slice(offset, max(0, end - offset)) if offset < arrow.num_rows else arrow.slice(0, 0)
        records = batch.to_pylist()
    except Exception as exc:
        print(
            json.dumps(
                {
                    "db_path": db_path,
                    "table": table,
                    "tables": tables,
                    "count": count,
                    "offset": offset,
                    "limit": limit,
                    "preview": preview,
                    "rows": [],
                    "error": str(exc),
                }
            )
        )
        return

    rows = []
    for r in records:
        tid = int(r.get("track_id", 0))
        vec = r.get("vector") or []
        if hasattr(vec, "tolist"):
            vec = vec.tolist()
        vec = [float(x) for x in vec]
        rows.append(
            {
                "track_id": tid,
                "vector_dim": len(vec),
                "vector_preview": vec[:preview],
                "vector_tail": vec[-min(preview, len(vec)) :] if len(vec) > preview else [],
            }
        )

    print(
        json.dumps(
            {
                "db_path": db_path,
                "table": table,
                "tables": tables,
                "count": count if count >= 0 else total,
                "offset": offset,
                "limit": limit,
                "preview": preview,
                "rows": rows,
            }
        )
    )


def main() -> None:
    if len(sys.argv) < 2:
        raise SystemExit("usage: lance_helper.py upsert|search|dump ...")
    cmd = sys.argv[1]
    if cmd == "upsert":
        upsert(sys.argv[2], sys.argv[3])
    elif cmd == "search":
        search(sys.argv[2], sys.argv[3], int(sys.argv[4]))
    elif cmd == "dump":
        preview = int(sys.argv[6]) if len(sys.argv) > 6 else 8
        dump(sys.argv[2], sys.argv[3], int(sys.argv[4]), int(sys.argv[5]), preview)
    else:
        raise SystemExit(f"unknown command {cmd}")


if __name__ == "__main__":
    main()
