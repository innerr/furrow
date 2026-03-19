use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use furrow_io_wal::{SyncMode, Wal, WalConfig};
use std::time::Duration;
use wal_bench::{generate_data, temp_wal_dir, TYPICAL_SIZES};

fn sync_mode_comprehensive(c: &mut Criterion) {
    let mut group = c.benchmark_group("sync_modes");

    let modes = vec![
        ("always", SyncMode::Always),
        (
            "batch_4mb_100ms",
            SyncMode::Batch {
                bytes: 4 * 1024 * 1024,
                time: Duration::from_millis(100),
            },
        ),
        (
            "batch_1mb_100ms",
            SyncMode::Batch {
                bytes: 1 * 1024 * 1024,
                time: Duration::from_millis(100),
            },
        ),
        (
            "batch_4mb_50ms",
            SyncMode::Batch {
                bytes: 4 * 1024 * 1024,
                time: Duration::from_millis(50),
            },
        ),
        ("never", SyncMode::Never),
    ];

    for &size in TYPICAL_SIZES.iter() {
        group.throughput(Throughput::Bytes(size as u64));
        let data = generate_data(size);

        for (mode_name, mode) in &modes {
            let data = data.clone();
            let mode = mode.clone();

            group.bench_with_input(
                BenchmarkId::new(*mode_name, size),
                &mode,
                |b, _mode| {
                    b.to_async(tokio::runtime::Runtime::new().unwrap())
                        .iter_with_setup(
                            || {
                                let dir = temp_wal_dir();
                                let config =
                                    WalConfig::new(dir.path()).sync_mode(mode.clone());
                                let data = data.clone();
                                (dir, config, data)
                            },
                            |(_dir, config, data)| async move {
                                let wal = Wal::open(config).await.unwrap();
                                wal.write(&data).await.unwrap();
                                wal.close().await.unwrap();
                            },
                        );
                },
            );
        }
    }

    group.finish();
}

fn sync_mode_batch_writes(c: &mut Criterion) {
    let mut group = c.benchmark_group("sync_batch_writes");

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

    let record_size = 4096;
    let batch_size = 1000;
    let total_bytes = (record_size * batch_size) as u64;

    group.throughput(Throughput::Bytes(total_bytes));

    for (name, mode) in modes {
        group.bench_function(name, |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter(|| async {
                    let dir = temp_wal_dir();
                    let config = WalConfig::new(dir.path()).sync_mode(mode.clone());
                    let wal = Wal::open(config).await.unwrap();

                    let data = generate_data(record_size);
                    for _ in 0..batch_size {
                        wal.write(&data).await.unwrap();
                    }

                    wal.close().await.unwrap();
                });
        });
    }

    group.finish();
}

criterion_group!(benches, sync_mode_comprehensive, sync_mode_batch_writes);
criterion_main!(benches);
