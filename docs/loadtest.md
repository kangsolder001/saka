# Uji beban ringan

Alat: `cmd/loadtest` (logikanya di `internal/loadtest`, hanya pustaka standar Go ditambah paket fake gateway milik proyek untuk menandatangani webhook). Dijalankan terhadap API yang sedang berjalan:

```
make loadtest ARGS="-scenario all -c 8 -n 400"
# atau: go run ./cmd/loadtest -base http://localhost:8080 -scenario login -c 16 -n 800
```

| Flag | Arti |
|---|---|
| `-base` | URL API (default `http://localhost:8080`) |
| `-scenario` | `plans`, `register`, `login`, `webhook` atau `all` |
| `-c` | jumlah klien paralel |
| `-n` | jumlah request per skenario |
| `-users` | akun yang dibuat skenario `login` lalu dipakai login (default `-c`) |
| `-spoof-ip` | kirim `X-Forwarded-For` berbeda tiap request (default aktif) |

Skenario:

- `plans`: `GET /plans`, pembacaan DB paling murah, sebagai dasar tumpukan HTTP.
- `register`: `POST /auth/register` dengan email baru (Argon2id + membuat user, organisasi, subscription, job email).
- `login`: mendaftarkan `-users` akun lebih dulu, lalu `POST /auth/login` bergantian (Argon2id verifikasi + membuat sesi).
- `webhook`: `POST /webhooks/fake` bertanda tangan sah untuk invoice yang tidak ada (cek signature, tulis `payment_events`, dijawab 200).

Laporan per skenario: jumlah request, `failed` (error transport atau status selain yang diharapkan, termasuk 429), durasi, requests/detik, latensi rata-rata, p50, p95, p99, maksimum, dan histogram status.

## Menyiapkan target (jangan ke production, jangan ke database dev yang dipakai)

Alat ini membuat akun dan event webhook. Pakai database sekali pakai dan `billing.provider = "fake"`.

Karena batas laju per IP (spec §5.4), satu alamat akan langsung terkena 429 (register 10/jam, login 60/menit). Alat mengirim `X-Forwarded-For` unik per request supaya yang terukur adalah kerja server, bukan pembatasnya; agar header itu dipercaya, isi `app.trusted_proxies` dengan alamat asal alat (lokal: `127.0.0.1` dan `::1`). Tanpa itu (`-spoof-ip=false` atau proxy tak dipercaya) hasilnya adalah angka 429 yang sendirinya berguna untuk memeriksa batas laju.

Konfigurasi sementara yang dipakai untuk angka di bawah (di luar repositori, tidak pernah di-commit):

```json
{
  "app": {"http_addr": "127.0.0.1:18080", "log_level": "info", "trusted_proxies": ["127.0.0.1", "::1"]},
  "database": {"url": "postgres://saka:saka@localhost:5433/saka_phase7_lt1?sslmode=disable"},
  "auth": {"jwt_secret": "<string acak >= 32 karakter>"},
  "redis": {"addr": "localhost:6380", "key_prefix": "phase7lt"},
  "mail": {"driver": "log"},
  "billing": {"provider": "fake"}
}
```

Langkahnya: `CREATE DATABASE saka_phase7_lt1`, `go run ./cmd/migrate -config <file> up`, `go run ./cmd/api -config <file>`, jalankan alat, hentikan API, `DROP DATABASE`, lalu hapus key Redis berawalan `phase7lt:`.

## Hasil (23 Oktober 2026, satu mesin)

Mesin: Intel Core i3-10110U (2 inti fisik, 4 thread), RAM 7,5 GB, Linux. **API, PostgreSQL 18, Redis, Mailpit dan alat uji berjalan di mesin yang sama** (yang juga sedang dipakai untuk hal lain), jadi angka ini adalah perkiraan kasar dan pesimistis untuk server khusus; jangan dibandingkan lintas mesin. Argon2id memakai parameter default (`19 MiB`, 2 iterasi, 1 jalur). Log API level `info` (satu baris per request) ke berkas.

| Skenario | Klien | Request | req/detik | rata-rata | p50 | p95 | p99 | gagal |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| `plans` | 8 | 400 | 2.542 | 3,1 ms | 2,2 ms | 4,1 ms | 36 ms | 0 |
| `plans` | 32 | 5.000 | 4.463 | 7,1 ms | 6,8 ms | 11,9 ms | 15,1 ms | 0 |
| `webhook` | 8 | 400 | 873 | 9,1 ms | 8,8 ms | 12,0 ms | 16,3 ms | 0 |
| `webhook` | 32 | 3.000 | 938 | 34,0 ms | 32,8 ms | 43,2 ms | 47,3 ms | 0 |
| `login` | 1 | 100 | 20 | 49 ms | 49 ms | 52 ms | 54 ms | 0 |
| `login` | 4 | 400 | 53 | 75 ms | 72 ms | 97 ms | 109 ms | 0 |
| `login` | 8 | 400 | 52 | 152 ms | 148 ms | 225 ms | 265 ms | 0 |
| `login` | 16 | 800 | 54 | 298 ms | 293 ms | 433 ms | 528 ms | 0 |
| `register` | 8 | 400 | 49 | 162 ms | 159 ms | 244 ms | 270 ms | 0 |

Tidak ada request gagal dan tidak ada log level error selain pencatatan "event pembayaran perlu ditinjau" yang memang diharapkan untuk invoice fiktif milik skenario `webhook`.

## Yang terbaca dari angka

- **Argon2id mendominasi login dan registrasi, seperti yang diperkirakan.** Satu verifikasi memakan sekitar 49 ms CPU; throughput login datar di sekitar 50-55 req/detik begitu klien melebihi jumlah inti yang tersedia, dan tambahan klien hanya menambah antrean (latensi naik hampir linear: p50 72 ms pada 4 klien, 293 ms pada 16). Itu perilaku yang benar dari fungsi hash yang sengaja mahal, bukan kebocoran. Untuk menaikkan kapasitas: tambah inti atau instance API (Redis sudah dipakai bersama sehingga batas laju konsisten antar instance); jangan menurunkan parameter Argon2 demi angka. Catatan: batas laju per IP untuk login (60/menit) berada jauh di bawah kapasitas ini, jadi penyerang dari satu alamat tidak bisa menghabiskan CPU.
- **Registrasi** ~162 ms rata-rata pada 8 klien: satu hash Argon2id ditambah satu transaksi yang menulis user, organisasi, membership, subscription, token verifikasi dan job outbox.
- **Webhook** ~900 req/detik pada mesin ini: cek HMAC, dua transaksi singkat dan satu insert `payment_events`. Jauh di atas batas laju 600/menit per IP; gateway sungguhan tidak akan mendekatinya.
- **`plans`** menunjukkan batas tumpukan HTTP + Postgres + log: ~4.500 req/detik dengan log akses menyala. Tidak ada bottleneck yang jelas selain CPU bersama.
- **Belum diukur**: jalur terautentikasi dengan cache member (`GET /me`), `POST /orgs/{id}/invitations`, checkout ke gateway nyata (dibatasi latensi gateway; satu koneksi DB tertahan selama panggilan, lihat spec §8.7), dan beban jangka panjang. Tidak ada penalaan prematur yang dilakukan.
