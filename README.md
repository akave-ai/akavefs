# AkaveFS

AkaveFS is a high-performance, POSIX-ish FUSE filesystem for S3-compatible object storage, written in Go.

AkaveFS is based on [GeeseFS](https://github.com/yandex-cloud/geesefs), with additional development focused on high-performance AI, data-intensive, and distributed storage workloads.

The project maintains compatibility with standard S3 APIs while allowing Akave to independently develop filesystem caching, performance optimizations, backend integrations, observability, and other features over time.

## Overview

AkaveFS allows you to mount an S3-compatible bucket as a local filesystem.

FUSE filesystems backed by object storage can suffer from performance limitations, particularly around:

* Small files
* Metadata operations
* Directory listings
* Random reads
* Multipart uploads
* High-concurrency workloads

AkaveFS builds on the architecture of GeeseFS, which addresses many of these problems through aggressive parallelism, asynchronous operations, metadata caching, read-ahead, multipart uploads, and local caching.

AkaveFS will extend this foundation with optimizations and integrations designed around Akave infrastructure and high-throughput AI workloads.

## Project Lineage

AkaveFS is derived from the open-source [GeeseFS](https://github.com/yandex-cloud/geesefs) project.

GeeseFS itself was originally derived from [Goofys](https://github.com/kahing/goofys).

We are grateful to the GeeseFS and Goofys contributors whose work provides the foundation for this project.

AkaveFS is maintained as an independent project so that Akave-specific functionality and performance improvements can evolve separately while still allowing relevant upstream GeeseFS changes to be incorporated.

## Key Features

AkaveFS inherits a number of capabilities from GeeseFS, including:

* Parallel read-ahead
* Parallel multipart uploads
* Random-read detection
* Asynchronous writes
* Asynchronous deletes
* Asynchronous renames
* Server-side copy operations
* Metadata caching
* Disk caching for reads and writes
* Fast recursive listings
* Partial writes
* Truncate support
* `fsync`
* Extended attributes
* Directory renames

Additional Akave-specific functionality will be added as the project develops.

## AI and Data Workloads

AkaveFS is intended to support workloads where large datasets stored in object storage need to be exposed through a familiar filesystem interface.

Example workloads include:

* AI model training
* AI inference
* Dataset ingestion
* Checkpoint loading
* Model distribution
* Data preprocessing
* GPU cluster storage
* Shared datasets
* Large sequential reads
* Highly parallel reads across many files

The goal is to provide high-throughput access to object storage without requiring applications to be rewritten around an object-storage API.

## Installation

### Build from source

```bash
git clone https://github.com/akave-ai/akavefs.git
cd akavefs
go build
```

FUSE or FUSE3 must also be installed on the host.

For Ubuntu/Debian:

```bash
sudo apt update
sudo apt install -y fuse3
```

## Usage

Configure S3 credentials:

```ini
[default]
aws_access_key_id = YOUR_ACCESS_KEY
aws_secret_access_key = YOUR_SECRET_KEY
```

Then mount a bucket:

```bash
./akavefs <bucket> <mountpoint>
```

For a custom S3-compatible endpoint:

```bash
./akavefs \
  --endpoint https://your-s3-endpoint.example.com \
  <bucket> \
  <mountpoint>
```

A bucket prefix can also be mounted:

```bash
./akavefs \
  --endpoint https://your-s3-endpoint.example.com \
  <bucket:prefix> \
  <mountpoint>
```

Credentials can also be provided using environment variables:

```bash
export AWS_ACCESS_KEY_ID="YOUR_ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="YOUR_SECRET_KEY"
```

## Akave Object Storage

AkaveFS can be used with Akave S3-compatible storage by specifying the appropriate Akave endpoint:

```bash
./akavefs \
  --endpoint https://<akave-endpoint> \
  <bucket> \
  <mountpoint>
```

Additional Akave-specific configuration and optimizations will be documented as they are introduced.

## Configuration

AkaveFS inherits many configuration and performance tuning options from GeeseFS.

Run:

```bash
./akavefs -h
```

to view the currently supported options.

Examples include configuration for:

* Memory limits
* Metadata cache
* Read-ahead
* Disk cache
* Multipart upload concurrency
* Flush concurrency
* S3 endpoints
* File and directory permissions
* Logging
* FUSE behavior

## Performance Tuning

For high-bandwidth environments, performance can generally be improved by increasing memory allocation and parallelism.

For example:

```bash
./akavefs \
  --no-checksum \
  --memory-limit 4000 \
  --max-flushers 32 \
  --max-parallel-parts 32 \
  --part-sizes 25 \
  <bucket> \
  <mountpoint>
```

Optimal settings depend on:

* Available network bandwidth
* Object sizes
* Number of concurrent readers
* Number of concurrent writers
* Available memory
* Local cache capacity
* S3 backend behavior

Akave-specific recommended configurations will be added as benchmarking progresses.

## POSIX Compatibility

AkaveFS provides a filesystem interface over object storage but should not be considered a completely POSIX-compliant filesystem.

The underlying object-storage model differs fundamentally from a local block filesystem.

Known limitations inherited from GeeseFS include:

* No hard links
* No filesystem locking
* Limited handling of deleted-but-open files
* Backend-dependent metadata behavior
* Restrictions around concurrent writes to the same object

Applications relying heavily on strict POSIX locking or multi-host concurrent modification of the same file should be tested carefully.

## Concurrent Access

AkaveFS inherits GeeseFS's caching model.

Multiple clients can safely access shared datasets for read-heavy workloads, but concurrent modification of the same file from multiple hosts requires additional coordination.

The AkaveFS roadmap includes further work around distributed workloads, cache consistency, and multi-client behavior.

## Troubleshooting

For debugging, AkaveFS can be started with detailed S3 and FUSE logging:

```bash
./akavefs \
  --debug_s3 \
  --debug_fuse \
  --log-file /tmp/akavefs.log \
  <bucket> \
  <mountpoint>
```

When reporting an issue, please include:

* AkaveFS version
* Operating system
* FUSE version
* S3 backend
* Mount options
* Relevant logs
* Reproduction steps

## Compatibility

AkaveFS is designed primarily for Akave and other S3-compatible object-storage systems.

Because the implementation originates from GeeseFS, it may also operate with storage systems including:

* Amazon S3
* Ceph RGW
* MinIO
* Backblaze B2
* OpenStack Swift-compatible environments
* Other S3-compatible services supporting multipart uploads and multipart server-side copy

Compatibility with individual providers may vary and should be tested before production use.

## Development

AkaveFS is maintained independently from GeeseFS.

The repository may periodically incorporate relevant improvements and fixes from upstream GeeseFS while also carrying Akave-specific functionality.

Upstream GeeseFS repository:

https://github.com/yandex-cloud/geesefs

## License

AkaveFS is licensed under the Apache License, Version 2.0.

This project contains code derived from GeeseFS, which is also licensed under the Apache License, Version 2.0.

See `LICENSE` and `AUTHORS` for additional information and attribution.

## Acknowledgements

AkaveFS builds upon the work of several open-source projects, including:

* [GeeseFS](https://github.com/yandex-cloud/geesefs)
* [Goofys](https://github.com/kahing/goofys)
* [jacobsa/fuse](https://github.com/jacobsa/fuse)
* [AWS SDK for Go](https://github.com/aws/aws-sdk-go)

We thank their maintainers and contributors for their work.
