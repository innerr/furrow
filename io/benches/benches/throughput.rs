use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use furrow_io_wal::{SyncMode, Wal, WalConfig};
use std::time::Duration;
use wal_bench::{generate_data, temp_wal_dir, TYPICAL_SIZES, WRITE_SIZES};

fn throughput_by_size(c: &mut Criterion) {
    let mut group = c.benchmark_group("throughput_by_size");

    for &size in WRITE_SIZES.iter() {
        group.throughput(Throughput::Bytes(size as u64));

        let data = generate_data(size);

        group.bench_with_input(
            BenchmarkId::new("batch_sync", size),
            &data,
            |b, data| {
                b.to_async(tokio::runtime::Runtime::new().unwrap())
                    .iter(|| async {
                        let dir = temp_wal_dir();
                        let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Batch {
                            bytes: 4 * 1024 * 1024,
                            time: Duration::from_millis(100),
                        });
                        let wal = Wal::open(config).await.unwrap();
                        wal.write(data).await.unwrap();
                        wal.close().await.unwrap();
                    });
            },
        );
    }

    group.finish();
}

fn throughput_by_sync_mode(c: &mut Criterion) {
    let mut group = c.benchmark_group("throughput_sync_mode");

    let size = 4096;
    group.throughput(Throughput::Bytes(size as u64));
    let data = generate_data(size);

    let modes = vec![
        ("always", SyncMode::Always),
        (
            "batch_4mb",
            SyncMode::Batch {
                bytes: 4 * 1024 * 1024,
                time: Duration::from_millis(100),
            },
        ),
        (
            "batch_1mb",
            SyncMode::Batch {
                bytes: 1 * 1024 * 1024,
                time: Duration::from_millis(100),
            },
        ),
        ("never", SyncMode::Never),
    ];

    for (name, mode) in modes {
        group.bench_with_input(BenchmarkId::new(name, size), &mode, |b, mode| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter_with_setup(
                    || {
                        let dir = temp_wal_dir();
                        let config = WalConfig::new(dir.path()).sync_mode(mode.clone());
                        let data = data.clone();
                        (dir, config, data)
                    },
                    |(_dir, config, data)| async move {
                        let wal = Wal::open(config).await.unwrap();
                        wal.write(&data).await.unwrap();
                        wal.close().await.unwrap();
                    },
                );
        });
    }

    group.finish();
}

fn sustained_throughput(c: &mut Criterion) {
    let mut group = c.benchmark_group("sustained_throughput");

    let total_bytes = 100 * 1024 * 1024;

    for &record_size in TYPICAL_SIZES.iter() {
        let records = total_bytes / record_size;
        group.throughput(Throughput::Bytes(total_bytes as u64));

        group.bench_function(format!("{}b_records", record_size), |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter(|| async {
                    let dir = temp_wal_dir();
                    let config = WalConfig::new(dir.path()).sync_mode(SyncMode::Batch {
                        bytes: 4 * 1024 * 1024,
                        time: Duration::from_millis(100),
                    });
                    let wal = Wal::open(config).await.unwrap();

                    let data = generate_data(record_size);
                    for _ in 0..records {
                        wal.write(&data).await.unwrap();
                    }

                    wal.close().await.unwrap();
                });
        });
    }

    group.finish();
}

criterion_group!(
    benches,
    throughput_by_size,
    throughput_by_sync_mode,
    sustained_throughput,
);
criterion_main!(benches);
