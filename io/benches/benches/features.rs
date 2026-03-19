use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use furrow_io_wal::{compress, decompress, encrypt, decrypt, generate_key, Wal, WalConfig, SyncMode};
use std::time::Duration;
use wal_bench::{generate_data, temp_wal_dir, TYPICAL_SIZES};

fn compression_overhead(c: &mut Criterion) {
    let mut group = c.benchmark_group("compression_overhead");

    let data_sizes = vec![1024, 4096, 16384, 65536];

    for &size in data_sizes.iter() {
        group.throughput(Throughput::Bytes(size as u64));
        let data = generate_data(size);

    group.bench_with_input(BenchmarkId::new("compress", size), &data, |b, data| {
        b.iter(|| compress(data).unwrap());
    });

    let compressed = compress(&data).unwrap().unwrap_or(data.clone());
    group.bench_with_input(
        BenchmarkId::new("decompress", size),
        &compressed,
        |b, compressed| {
            b.iter(|| decompress(compressed).unwrap());
        },
    );
    }

    group.finish();
}

fn encryption_overhead(c: &mut Criterion) {
    let mut group = c.benchmark_group("encryption_overhead");

    let data_sizes = vec![1024, 4096, 16384, 65536];
    let key = generate_key();

    for &size in data_sizes.iter() {
        group.throughput(Throughput::Bytes(size as u64));
        let data = generate_data(size);

        group.bench_with_input(BenchmarkId::new("encrypt", size), &data, |b, data| {
            b.iter(|| encrypt(data, &key).unwrap());
        });

        let encrypted = encrypt(&data, &key).unwrap();
        group.bench_with_input(
            BenchmarkId::new("decrypt", size),
            &encrypted,
            |b, encrypted| {
                b.iter(|| decrypt(encrypted, &key).unwrap());
            },
        );
    }

    group.finish();
}

fn combined_overhead(c: &mut Criterion) {
    let mut group = c.benchmark_group("combined_overhead");

    let size = 4096;
    group.throughput(Throughput::Bytes(size as u64));
    let data = generate_data(size);

    let key = generate_key();

    group.bench_function("compress_then_encrypt", |b| {
        b.iter(|| {
            let compressed = compress(&data).unwrap().unwrap_or_else(|| data.clone());
            let encrypted = encrypt(&compressed, &key).unwrap();
            encrypted
        });
    });

    group.bench_function("decrypt_then_decompress", |b| {
        let compressed = compress(&data).unwrap().unwrap_or_else(|| data.clone());
        let encrypted = encrypt(&compressed, &key).unwrap();
        b.iter(|| {
            let decrypted = decrypt(&encrypted, &key).unwrap();
            let decompressed = decompress(&decrypted).unwrap();
            decompressed
        });
    });

    group.finish();
}

fn wal_with_features(c: &mut Criterion) {
    let mut group = c.benchmark_group("wal_with_features");

    let size = 4096;
    group.throughput(Throughput::Bytes(size as u64));
    let data = generate_data(size);

    let configs = vec![
        ("baseline", false, false),
        ("compression", true, false),
        ("encryption", false, true),
        ("both", true, true),
    ];

    for (name, use_compression, use_encryption) in configs {
        group.bench_function(name, |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter(|| async {
                    let dir = temp_wal_dir();

                    let mut config_builder = WalConfig::new(dir.path());
                    if use_compression {
                        config_builder = config_builder;
                    }
                    if use_encryption {
                        config_builder = config_builder;
                    }

                    let config = config_builder.sync_mode(SyncMode::Batch {
                        bytes: 4 * 1024 * 1024,
                        time: Duration::from_millis(100),
                    });

                    let wal = Wal::open(config).await.unwrap();
                    wal.write(&data).await.unwrap();
                    wal.close().await.unwrap();
                });
        });
    }

    group.finish();
}

criterion_group!(
    benches,
    compression_overhead,
    encryption_overhead,
    combined_overhead,
    wal_with_features,
);
criterion_main!(benches);
