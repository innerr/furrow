use rand::Rng;
use std::time::Duration;

pub const WRITE_SIZES: &[usize] = &[
    64,
    128,
    256,
    512,
    1024,
    2 * 1024,
    4 * 1024,
    8 * 1024,
    16 * 1024,
    32 * 1024,
    64 * 1024,
    128 * 1024,
    256 * 1024,
    512 * 1024,
    1024 * 1024,
    2 * 1024 * 1024,
    4 * 1024 * 1024,
    8 * 1024 * 1024,
    16 * 1024 * 1024,
];

pub const TYPICAL_SIZES: &[usize] = &[64, 1024, 4096, 65536, 1048576];

pub fn generate_data(size: usize) -> Vec<u8> {
    let mut rng = rand::thread_rng();
    (0..size).map(|_| rng.gen()).collect()
}

pub fn temp_wal_dir() -> tempfile::TempDir {
    tempfile::tempdir().expect("Failed to create temp dir")
}

pub struct LatencyStats {
    pub min: Duration,
    pub max: Duration,
    pub mean: Duration,
    pub p50: Duration,
    pub p90: Duration,
    pub p99: Duration,
    pub p999: Duration,
}

impl LatencyStats {
    pub fn from_samples(samples: &mut Vec<Duration>) -> Self {
        if samples.is_empty() {
            return Self {
                min: Duration::ZERO,
                max: Duration::ZERO,
                mean: Duration::ZERO,
                p50: Duration::ZERO,
                p90: Duration::ZERO,
                p99: Duration::ZERO,
                p999: Duration::ZERO,
            };
        }

        samples.sort();

        let min = samples[0];
        let max = samples[samples.len() - 1];
        let mean = samples.iter().sum::<Duration>() / samples.len() as u32;
        let p50 = samples[(samples.len() as f64 * 0.50) as usize];
        let p90 = samples[(samples.len() as f64 * 0.90) as usize];
        let p99 = samples[(samples.len() as f64 * 0.99) as usize];
        let p999 = samples[(samples.len() as f64 * 0.999) as usize];

        Self {
            min,
            max,
            mean,
            p50,
            p90,
            p99,
            p999,
        }
    }
}

#[macro_export]
macro_rules! bench_write {
    ($group:ident, $name:expr, $wal:expr, $data:expr) => {
        $group.bench_function($name, |b| {
            b.to_async(tokio::runtime::Runtime::new().unwrap())
                .iter(|| async {
                    $wal.write(&$data).await.unwrap();
                })
        });
    };
}
