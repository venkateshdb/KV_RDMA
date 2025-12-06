# KV-RDMA

A high-performance Key-Value store implementation leveraging Remote Direct Memory Access (RDMA) for low latency and high throughput.

## Overview

This project implements a key-value store that uses RDMA verbs to bypass the kernel and copy data directly between application memory of the client and server. This results in significantly lower latency and higher throughput compared to traditional TCP/IP-based solutions.

## Benchmark Results

The following table shows the performance comparison between TCP and RDMA implementations under various configurations.

| Type | Servers | Clients | Batch Size | Workers | Throughput (ops/s) | Latency (us) | Server CPU (%) | Net BW (MB/s) |
|------|---------|---------|------------|---------|-------------------|--------------|----------------|---------------|
| TCP | 1 | 1 | 1 | 16 | 55,559 | 285.75 | 5.74 | 0 |
| TCP | 2 | 2 | 1 | 16 | 106,413 | 298.26 | 41.42 | 49.41 |
| TCP | 4 | 4 | 1 | 16 | 208,589 | 304.31 | 36.63 | 96.92 |
| TCP | 1 | 1 | 4 | 16 | 191,867 | 330.70 | 45.18 | 58.93 |
| TCP | 2 | 2 | 4 | 16 | 540,295 | 235.26 | 52.37 | 189.77 |
| TCP | 4 | 4 | 4 | 16 | 867,185 | 293.25 | 50.54 | 341.38 |
| TCP | 1 | 1 | 16 | 16 | 451,901 | 562.04 | 41.67 | 128.79 |
| TCP | 2 | 2 | 16 | 16 | 614,029 | 828.04 | 40.47 | 175.95 |
| TCP | 4 | 4 | 16 | 16 | 829,000 | 1227.01 | 38.57 | 254.28 |
| TCP | 1 | 1 | 64 | 16 | 720,030 | 1413.45 | 39.15 | 196.77 |
| TCP | 2 | 2 | 64 | 16 | 1,444,090 | 1410.04 | 44.23 | 400.72 |
| TCP | 4 | 4 | 64 | 16 | 2,521,500 | 1614.35 | 39.56 | 719.1 |
| RDMA | 1 | 1 | 1 | 16 | 23 | 6.09 | 89.54 | 0 |
| RDMA | 2 | 2 | 1 | 16 | 3,269,680 | 9.47 | 88.25 | 0 |
| RDMA | 4 | 4 | 1 | 16 | 6,190,190 | 9.96 | 85.39 | 0.01 |
| RDMA | 1 | 1 | 4 | 16 | 3,086,270 | 5.05 | 89.33 | 0 |
| RDMA | 2 | 2 | 4 | 16 | 1,862,993 | 8.42 | 88.86 | 0 |
| RDMA | 4 | 4 | 4 | 16 | 6,348,330 | 9.87 | 86.07 | 0.01 |
| RDMA | 1 | 1 | 16 | 16 | 3,301,910 | 4.75 | 90.19 | 0 |
| RDMA | 2 | 2 | 16 | 16 | 3,930,150 | 8.02 | 87.73 | 0 |
| RDMA | 4 | 4 | 16 | 16 | 4,781,473 | 9.84 | 85.68 | 0.01 |
| RDMA | 1 | 1 | 64 | 16 | 2,922,360 | 5.38 | 90.00 | 0 |
| RDMA | 2 | 2 | 64 | 16 | 4,037,010 | 7.82 | 88.52 | 0 |
| RDMA | 4 | 4 | 64 | 16 | 6,407,940 | 9.81 | 84.77 | 0.01 |

**Note:** Throughput is in operations per second. Latency is in microseconds.

## Resources

- [Presentation Slides](KV_RDMA/RDMA Based KV Store.pdf)

## Setup

### Prerequisites

- **Hardware**: RDMA-capable network interface (e.g., Mellanox ConnectX). We used m510 instances on cloudlab.

### Installation

The project includes a script to automate the installation of dependencies (Go and RDMA libraries) across a cluster.

1.  **Run the installation script:**

    ```bash
    ./install_lib.sh
    ```

    This script will:
    - Install Go 1.25.0.
    - Install required RDMA libraries (`libibverbs-dev`, `rdma-core`, `ibverbs-utils`).
    - If running on `node0` of a CloudLab cluster, it will automatically propagate the installation to other nodes.

2.  **Verify Installation:**
    Ensure `go` is in your path and RDMA devices are visible:

    ```bash
    go version
    ibv_devinfo
    ```

## Running Benchmarks

### Comprehensive Benchmark

To reproduce the full benchmark results (TCP vs RDMA across different scales and batch sizes), run the comprehensive benchmark script:

```bash
./run_comprehensive_bench.sh
```

This script will:
1. Build the project.
2. Run TCP and RDMA benchmarks with varying batch sizes (1, 4, 16, 64) and cluster sizes (1x1, 2x2, 4x4).
3. Output the results to `benchmark_results.csv`.

### RDMA Benchmark Only

To run only the RDMA benchmark:

```bash
./run_rdma_cluster.sh [server_count] [client_count]
```

Example:
```bash
./run_rdma_cluster.sh 2 2
```

### TCP Baseline Only

To run only the TCP baseline benchmark:

```bash
./run_cluster.sh [server_count] [client_count]
```

Example:
```bash
./run_cluster.sh 2 2
```
