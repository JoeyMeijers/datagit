# datagit

## Vision

**datagit** is an open-source CLI tool written in Go that brings
Git-like versioning, metadata management, data quality validation, and
lineage tracking to datasets.

Its goal is to make datasets reproducible, searchable, auditable, and
easy to manage in statistical and data science environments.

## Core Principles

-   Single static Go binary
-   Streaming-first architecture
-   Handles datasets from MBs to TBs
-   Local-first with optional cloud storage
-   Kubernetes-native
-   Plugin architecture
-   Cross-platform

## Tech Stack

-   Language: Go
-   CLI: Cobra
-   Storage: Content-addressable chunks
-   Metadata: SQLite
-   Compression: Zstandard
-   Hashing: SHA-256
-   Object Storage: S3, Azure Blob, MinIO, GCS
-   Kubernetes: client-go

## Repository Layout

``` text
.datagit/
    objects/
    commits/
    metadata/
    refs/
    config/
    index.db
```

## CLI

``` bash
datagit init
datagit add population.csv
datagit commit -m "Initial import"
datagit log
datagit diff v1 v2
datagit checkout v1
datagit status
datagit schema population.csv
datagit profile population.csv
datagit verify
datagit gc
datagit search income
datagit lineage dashboard.csv
```

## Features

### Dataset Versioning

-   Git-like commits
-   Content-addressable storage
-   Chunk deduplication
-   Rollback and checkout

### Metadata Catalog

Store: - Name - Description - Owner - Tags - Source - License - Format -
Size - Record count - Column count - Schema - Checksums - Commit history

### Schema Detection

Automatically detect: - Integer - Float - String - Boolean - Date -
Datetime

Show schema differences between versions.

### Dataset Diff

Detect: - Added rows - Removed rows - Modified rows - Added columns -
Removed columns - Datatype changes - Missing value changes - Duplicate
changes

### Data Profiling

For every dataset: - Row count - Column count - Missing values -
Duplicate rows - Distinct values - Min/Max - Mean - Median - Standard
deviation - Quartiles - Histograms

### Data Quality

Automatic validation: - Missing values - Duplicate keys - Invalid
datatypes - Empty columns - Constant columns - Outliers

Support configurable validation rules.

### Lineage

Track: - Input datasets - Output datasets - Scripts - Pipelines -
Kubernetes jobs

### Storage Engine

-   Chunk large files
-   Deduplicate chunks
-   Parallel hashing
-   Parallel compression
-   Streaming I/O

### Supported Formats

Initial: - CSV - TSV - Parquet

Future: - Arrow - Feather - JSON Lines - DuckDB - SPSS - SAS - Stata

### Kubernetes

Commands:

``` bash
datagit publish
datagit fetch
datagit run
```

Link every dataset version to Kubernetes jobs for full reproducibility.

### Plugin System

Allow external plugins for: - Profiling - Validation - File formats - AI
assistants

### AI Ready

Future commands:

``` bash
datagit explain dataset.csv
datagit suggest
```

## Non-functional Requirements

-   Clean Architecture
-   Dependency Injection
-   Extensive testing
-   High performance
-   Low memory usage
-   Streaming by default
-   Cross-platform
-   One executable
-   No external database required

## Long-term Vision

Become the central data catalog and version control system for
statistical organisations, combining: - Git - DVC - lakeFS - Apache
Iceberg

while remaining simple, lightweight and fast.
