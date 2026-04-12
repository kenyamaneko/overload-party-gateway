#!/usr/bin/env python3
"""API doc 生成。doc-tools パッケージを呼び出す。"""
import sys
from pathlib import Path

from doc_tools import generate_api_docs

generate_api_docs.run(
    Path("data/models.yaml"),
    Path("docs/API_REFERENCE.md"),
    do_add_markers="--add-markers" in sys.argv,
)
