#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_ROOT"

echo "=== WAL Performance Benchmark Runner ==="
echo ""

function run_benchmark() {
    local name=$1
    echo "Running $name benchmark..."
    cargo bench --bench "$name" -- --save-baseline main
    echo ""
}

case "$1" in
    throughput)
        run_benchmark throughput
        ;;
    latency)
        run_benchmark latency
        ;;
    sync_modes)
        run_benchmark sync_modes
        ;;
    features)
        echo "Running features benchmark (requires compression and encryption)..."
        cargo bench --bench features --features "furrow-io-wal/compression,furrow-io-wal/encryption" -- --save-baseline main
        ;;
    all)
        run_benchmark throughput
        run_benchmark latency
        run_benchmark sync_modes
        echo "Running features benchmark..."
        cargo bench --bench features --features "furrow-io-wal/compression,furrow-io-wal/encryption" -- --save-baseline main
        ;;
    compare)
        echo "Comparing with baseline 'main'..."
        cargo bench -- --baseline main
        ;;
    quick)
        echo "Quick test (throughput only, small sample size)..."
        cargo bench --bench throughput -- --sample-size 10
        ;;
    *)
        echo "Usage: $0 {throughput|latency|sync_modes|features|all|compare|quick}"
        echo ""
        echo "Commands:"
        echo "  throughput  - Run throughput benchmarks"
        echo "  latency     - Run latency benchmarks"
        echo "  sync_modes  - Run sync mode comparison"
        echo "  features    - Run feature overhead tests (compression/encryption)"
        echo "  all         - Run all benchmarks"
        echo "  compare     - Compare current results with baseline 'main'"
        echo "  quick       - Quick test with small sample size"
        exit 1
        ;;
esac

echo "=== Benchmark complete ==="
echo "Results available in: $PROJECT_ROOT/target/criterion/"
