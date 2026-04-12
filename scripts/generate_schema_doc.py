#!/usr/bin/env python3
"""schema doc 生成。doc-tools パッケージを呼び出す。"""
import sys
from pathlib import Path

from doc_tools import generate_schema_doc

generate_schema_doc.run(
    Path("db/schema.sql"),
    Path("docs/DATA_DESIGN.md"),
    do_add_markers="--add-markers" in sys.argv,
)
