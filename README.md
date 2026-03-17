# Furrow

An adaptive storage engine that optimizes itself in real-time.

## Goals

1. **Efficient Storage Implementation** - High-performance storage primitives designed for modern hardware.

2. **Real-time Parameter Tuning** - Automatically adjusts configuration to achieve optimal performance for any workload.

3. **Full Hardware Utilization** - Maximizes the use of all allocated hardware resources.

4. **Dynamic Resource Allocation** - Supports online adjustment of hardware resource quotas while maintaining optimal workload performance.

## Non-Goals

1. **Workload Pattern Detection** - Furrow does not analyze or classify workload patterns. This responsibility belongs to upstream consumers.

2. **Distributed Storage** - Furrow is a single-node storage engine. Distributed coordination and replication are out of scope.

## Why Rust

We chose Rust for two key reasons:

1. **No Garbage Collection** - GC languages introduce unpredictable latency spikes, making them unsuitable for high-performance storage systems where deterministic performance is critical.

2. **Best-in-class AI Agent Training Data** - Among non-GC languages (C, C++, Rust), Rust has the highest quality training corpus for AI agents. This enables better automated code generation, review, and maintenance—critical for a project designed to be AI-assisted throughout its lifecycle.

## License

Apache-2.0
