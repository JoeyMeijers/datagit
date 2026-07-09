#!/usr/bin/env python3
"""Regenerates the Parquet golden files used by parquet_test.go.

Usage: python3 gen_fixtures.py   (requires pyarrow)

The fixtures are committed to the repository so Go tests run offline;
this script only needs re-running if the fixtures must change.
"""
import datetime

import pyarrow as pa
import pyarrow.parquet as pq

# typed.parquet: one column per supported logical type, 3 rows.
typed = pa.table({
    "id": pa.array([1, 2, 3], type=pa.int64()),
    "small": pa.array([10, 20, 30], type=pa.int32()),
    "score": pa.array([1.5, 2.5, 3.5], type=pa.float64()),
    "name": pa.array(["a", "b", "c"], type=pa.string()),
    "active": pa.array([True, False, True], type=pa.bool_()),
    "born": pa.array([datetime.date(2020, 1, i) for i in (1, 2, 3)], type=pa.date32()),
    "seen": pa.array([datetime.datetime(2024, 1, 1, 10, 0, i) for i in (1, 2, 3)],
                     type=pa.timestamp("ms")),
})
pq.write_table(typed, "typed.parquet")

# empty.parquet: schema but zero rows.
pq.write_table(typed.slice(0, 0), "empty.parquet")

# nested.parquet: a nested struct column alongside a scalar.
nested = pa.table({
    "id": pa.array([1, 2], type=pa.int64()),
    "point": pa.array([{"x": 1.0, "y": 2.0}, {"x": 3.0, "y": 4.0}],
                      type=pa.struct([("x", pa.float64()), ("y", pa.float64())])),
})
pq.write_table(nested, "nested.parquet")

print("fixtures written")
