#!/usr/bin/env python3
"""asyncapi-codegen 出力中の pass-through フィールドを json.RawMessage に置換する。

asyncapi.yaml で `type: object additionalProperties: true` と宣言した
battle 由来の生 JSON を中継するフィールドは、Go では map[string]interface{}
としてではなく json.RawMessage で持ちたい (downstream のバイト列をそのまま
WS に流すため)。asyncapi-codegen-tools が x-go-type 拡張を未対応のため、
本スクリプトで生成後に置換する。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

# (フィールド名, JSON タグ) のペアで identify する。
PASSTHROUGH_FIELDS: list[tuple[str, str]] = [
    ("Data", "data"),
    ("ActionData", "action_data"),
    ("State", "state"),
]


def main(path: str) -> None:
    p = Path(path)
    src = p.read_text(encoding="utf-8")

    # Add encoding/json import after package declaration if missing.
    if "encoding/json" not in src:
        src = src.replace(
            "package apigateway\n\n",
            "package apigateway\n\nimport (\n\t\"encoding/json\"\n)\n\n",
            1,
        )

    for field, tag in PASSTHROUGH_FIELDS:
        pattern = re.compile(
            rf"(\b{field}\s+)map\[string\]interface\{{\}}(\s+`json:\"{tag}\"`)"
        )
        src = pattern.sub(rf"\1json.RawMessage      \2", src)

    p.write_text(src, encoding="utf-8")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        print("usage: postprocess_asyncapi_gen.py <path>", file=sys.stderr)
        sys.exit(2)
    main(sys.argv[1])
