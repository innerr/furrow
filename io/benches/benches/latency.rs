use criterion::{criterion_group, criterion_main, Criterion, Throughput};
use furrow_io_wal::{SyncMode, Wal, WalConfig};
use std::time::{Duration, Instant};
use wal_bench::{generate_data, temp_wal_dir, LatencyStats, TYPICAL_SIZES};

fn latency_distribution(c: &mut Criterion) {
    let mut group = c.benchmark_group("latency");
    group.sample_size(1000);

    for &size in TYPICAL_SIZES.iter() {
        group.throughput(Throughput::Bytes(size as u64));

        group.bench_function(format!("latency_{}b", size), |b| {
            let rt = tokio::runtime::Runtime::new().unwrap();
            let data = generate_data(size);

            b.to_async(&rt).iter_with_setup(
                || {
                    let dir = temp_wal_dir();
                    let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Batch {
                        bytes: 4 * 1024 * 1024,
                        time: Duration::from_millis(100),
                    });
                    let data = data.clone();
                    (dir, config, data)
                },
                |(_dir, config, data)| async move {
                    let wal = Wal::open(config).await.unwrap();

                    let mut latencies = Vec::with_capacity(100);
                    for _ in 0..100 {
                        let start = Instant::now();
                        wal.write(&data).await.unwrap();
                        latencies.push(start.elapsed());
                    }

                    wal.close().await.unwrap();

                    LatencyStats::from_samples(&mut latencies)
                },
            );
        });
    }

    group.finish();
}

fn latency_by_sync_mode(c: &mut Criterion) {
    let mut group = c.benchmark_group("latency_sync_mode");
    group.sample_size(100);

    let size = 4096;
    group.throughput(Throughput::Bytes(size as u64));

    let modes = vec![
        ("always", SyncMode::Always),
        (
            "batch_4mb",
            SyncMode::Batch {
                bytes: 4 * 1024 * 1024,
                time: Duration::from_millis(100),
            },
        ),
        ("never", SyncMode::Never),
    ];

    for (name, mode) in modes {
        group.bench_function(format!("{}_{}b", name, size), |b| {
            let rt = tokio::runtime::Runtime::new().unwrap();
            let data = generate_data(size);

            b.to_async(&rt).iter_with_setup(
                || {
                    let dir = temp_wal_dir();
                    let config = WalConfig::new(dir.path()).sync_mode(mode.clone());
                    let data = data.clone();
                    (dir, config, data)
                },
                |(_dir, config, data)| async move {
                    let wal = Wal::open(config).await.unwrap();

                    let start = Instant::now();
                    wal.write(&data).await.unwrap();
                    let latency = start.elapsed();

                    wal.close().await.unwrap();

                    latency
                },
            );
        });
    }

    group.finish();
}

criterion_group!(benches, latency_distribution, latency_by_sync_mode);
criterion_main!(benches);
