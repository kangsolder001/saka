# Daftar Masalah dan Utang Teknis (Tier 1)

> Dibuat setelah Fase 0-7 selesai. Semua kode Tier 1 sudah ada, lint bersih, dan seluruh suite unit + integrasi hijau. Dokumen ini mencatat **semua yang belum beres, belum terverifikasi, atau sengaja dikompromikan**, supaya tidak hilang di antara sesi.
>
> Status: `Terbuka` (butuh tindakan), `Diterima` (sengaja, dengan alasan), `Backlog` (masuk Tier 2).
> Rujukan `§` adalah bagian di [spec.md](spec.md). Kolom "Prioritas": **P1** blokir rilis, **P2** sebaiknya sebelum rilis, **P3** boleh menyusul.

---

## A. Tindakan yang hanya bisa dilakukan pemilik repo

| ID | Masalah | Dampak | Tindakan | Prioritas | Status |
|---|---|---|---|---|---|
| A1 | **Akun sandbox Midtrans belum ada.** Integrasi Midtrans ditulis dari dokumentasi publik dan hanya diuji dengan server HTTP palsu dan signature yang dihitung di luar kode. Tidak pernah memanggil API sungguhan. | Checkout, webhook, dan `GetPaymentStatus` belum terbukti jalan di gateway sungguhan. Kriteria §12 Billing pertama ("payment link valid di sandbox") tetap tidak tercentang. | Daftar akun sandbox, isi `billing.provider = "midtrans"` dan `billing.midtrans.server_key` (`SB-...`) di `config.json`, jalankan `go test ./internal/billing/payment/midtrans -run TestSandbox -v`, lalu verifikasi enam asumsi di §8.7 (lihat B1). Centang kriteria itu. | **P1** | Terbuka |
| A2 | **Provider email produksi belum dikonfirmasi.** Resend direkomendasikan dan diimplementasikan (`internal/mail/resend_mailer.go`), tetapi API-nya tidak pernah dipanggil. | Tanpa akun, kunci API, dan DNS pengirim, email (verifikasi, reset password, undangan, invoice) tidak terkirim di production. Production menolak driver `log`. | Putuskan provider (atau tukar: cukup satu implementasi `Mailer` baru, langkah di `docs/email.md`), buat akun dan kunci, pasang SPF/DKIM/DMARC, kirim satu email uji sungguhan. Nilai DNS di `docs/email.md` berbentuk umum; salin yang persis dari dashboard provider. | **P1** | Terbuka |
| A3 | **Toolchain Go lokal 1.26.6 punya 12 advisori pustaka standar** (`net/http`, `crypto/tls`, `crypto/x509`, `html/template`, `net/textproto`, `os`), diperbaiki di Go 1.26.9 / 1.26.10. | Binary yang dibangun dengan toolchain ini ikut rentan. Bukan cacat kode proyek. | Bangun dan deploy dengan Go >= 1.26.10, jalankan ulang `govulncheck` sebelum rilis. `go.mod` sengaja tidak dipaku ke toolchain tertentu. Job `govulncheck` di CI bersifat advisory. | **P1** | Terbuka |
| A4 | **Migrasi 00006-00010 belum diterapkan ke database dev `saka`** milikmu (hanya diuji lewat testcontainers dan database sekali pakai). | `make run` di dev gagal konek sampai migrasi jalan: `database.app_role` default `saka_app` dan role itu dibuat migrasi `00007`. | `make migrate`, lalu `make seed` bila perlu akun demo. Migrasi `00007` membuat role dan butuh hak `CREATEROLE` (user compose dev adalah superuser, jadi aman di dev). Di production, sediakan role dan hak itu lewat infrastruktur. `make seed` belum pernah dijalankan ke database dev sungguhan (hanya diuji di PostgreSQL container). | P2 | Terbuka |
| A5 | **Item todo yang masih terbuka** karena A1: "Daftar akun sandbox Midtrans" (Fase 4) dan "Spike Midtrans sandbox" (Fase 6). | Checklist Fase 4 dan 6 belum 100%. | Selesai bersama A1. | P2 | Terbuka |

---

## B. Belum terverifikasi

| ID | Hal | Catatan |
|---|---|---|
| B1 | **Asumsi API Midtrans** (spec §8.7): (1) `POST {snap}/snap/v1/transactions` dengan Basic auth `ServerKey:` menjawab `201` berisi `token` dan `redirect_url`; (2) `expiry` menerima `start_time` berformat `2006-01-02 15:04:05 -0700`, `unit: "minutes"`, dan durasi sampai 14 hari (20160 menit) tanpa batas atas; (3) field notifikasi `transaction_id`, `transaction_status`, `status_code`, `order_id`, `gross_amount` (string `"99000.00"`), `fraud_status`, `signature_key`, `currency` (default IDR bila hilang), dan signature dihitung atas teks persis seperti dikirim; (4) `GET {api}/v2/{order_id}/status` memakai auth yang sama, menjawab bentuk yang sama dengan notifikasi, dan `status_code "404"` untuk order yang belum dibayar; (5) notifikasi dikirim ulang bila tidak dijawab 2xx; (6) `settlement` menyusul `capture` untuk kartu. | **Risiko terbesar:** bila (2) salah, checkout plan ber-trial 14 hari gagal. Midtrans tidak punya ID event unik, jadi ID event diturunkan dari `transaction_id:transaction_status:status_code:fraud_status`; bila gateway mengirim dua notifikasi berbeda dengan empat nilai itu sama, yang kedua dianggap duplikat. |
| B2 | **API Resend** (kode error mana yang diperlakukan sementara/permanen, `Idempotency-Key`) belum pernah dipanggil. Klasifikasi saat ini: jaringan, 5xx, 429, 408, 409, 401, 403 diulang; 4xx lain permanen. | Diverifikasi bersama A2. |
| B3 | **Pengiriman email sungguhan** (SMTP ke Mailpit teruji; provider produksi tidak). | Lihat A2. |
| B4 | **Perilaku saat Redis mati di production.** Rate limit gagal tertutup (500) kecuali webhook yang gagal terbuka; cache user jatuh ke DB. Teruji di test, tetapi tidak pernah dicoba pada infrastruktur nyata. | Uji saat staging ada. |
| B5 | **Beban nyata.** `docs/loadtest.md` diukur di mesin dev 2 inti dengan API, PostgreSQL, dan Redis di satu mesin (login ~50 req/s, p95 225 ms; webhook ~900 req/s). | Bukan angka kapasitas production. Argon2id mendominasi login/register; sesuaikan biaya hash dan ukuran instans. |

---

## C. Keputusan implementasi yang menyimpang dari spec awal (perlu konfirmasi pemilik)

| ID | Keputusan | Alasan | Konsekuensi jika ingin diubah |
|---|---|---|---|
| C1 | **Keanggotaan dari undangan diberikan setelah email terbukti** (verifikasi email atau konfirmasi ganti email), bukan saat registrasi. Mendaftar dengan email yang diundang tidak membuat organisasi baru. Menerima undangan lewat tautan juga butuh email terverifikasi. (§7.6) | Jika dijoin saat mendaftar, siapa pun bisa mendaftar dengan email korban (belum terverifikasi) dan masuk ke organisasi yang mengundangnya. | Kalimat §7.4 ("setelah daftar ... otomatis diterima") dipenuhi secara fungsional tetapi lebih lambat satu langkah. Mengikuti spec secara harfiah membuka celah di atas. |
| C2 | `subscriptions.current_period_end` **boleh NULL** (NULL = tidak berakhir, plan Free); spec §4.8 awal tidak menandainya nullable. | Subscription Free tidak punya akhir periode; cron memfilter `IS NOT NULL`. | Alternatif (tanggal jauh di depan) lebih berantakan dan membuat cron mengeksekusi baris Free. |
| C3 | **Trial.** Checkout plan berbayar ber-trial dari org Free yang belum pernah trial langsung membuat status `trialing`; invoice pertama jatuh tempo di akhir trial; membayar lebih awal langsung `active` dan **menghanguskan sisa trial**. Trial hanya sekali (`trial_ends_at IS NOT NULL`). Checkout ulang untuk plan yang sama memakai ulang invoice terbuka. (§8.7) | Spec hanya bilang "trial dipicu saat memilih plan berbayar". | Mengubah model trial menyentuh state machine, checkout, dan cron `expire`. |
| C4 | **Downgrade ke `free` lewat `change-plan` ditolak** (pakai `cancel`). Downgrade ke plan berbayar lain dicatat di `pending_plan_id` dan berlaku lewat invoice perpanjangan plan itu yang diterbitkan `billing.renewals`. | Menghindari dua jalur menuju Free. | UX downgrade-ke-Free harus lewat tombol batalkan. |
| C5 | **Perpanjangan yang dibayar sebelum periode berakhir** hanya dicatat; langganan berpindah ke periode itu saat periode berjalan habis. `cancel_at_period_end` menang atas perpanjangan yang sudah dibayar, dan **tidak ada refund** di Tier 1. | Tanpa proration (§13). | Pengguna yang membatalkan setelah membayar perpanjangan kehilangan uang itu; perlu kebijakan refund (D4). |
| C6 | **Kasus tepi webhook dijawab `200`** dengan catatan di `payment_events.error` (dan `processed_at` terisi), lalu dicatat di log level error untuk ditinjau manual: nominal/mata uang tidak cocok, pembayaran untuk invoice `void`/`expired`, invoice tidak dikenal, provider tidak cocok, perpanjangan dibayar setelah langganan berakhir, refund/chargeback. Hanya kegagalan pemrosesan yang `500`. | Gateway tidak perlu mengirim ulang kasus yang memang tak bisa diproses otomatis. | **Tidak ada alarm otomatis**: catatan itu hanya ditemukan bila seseorang memantau log atau tabel (lihat D9). |
| C7 | **Kode error baru `PAYMENT_PROVIDER_ERROR` (502)** untuk kegagalan gateway saat checkout; ditambahkan ke spec §3. Tanda tangan `apperr.PlanLimitReached` dan `PlanFeatureUnavailable` berubah (membawa detail upgrade). | Kebutuhan klien untuk membedakan galat gateway dari galat server. | Klien harus menangani kode baru. |
| C8 | **`Entitlements` menerima `q db.DBTX`** (spec §9.1 awal tanpa itu), supaya pengecekan limit ikut transaksi pemanggil. `CheckLimit` dengan `delta <= 0` tidak pernah ditolak; limit tak tercantum di plan dianggap 0 (gagal tertutup); limit tanpa counter terdaftar adalah error pemrograman. | Konsistensi transaksi dan keamanan default. | `Usage` melaporkan semua counter terdaftar dengan `max=0` bila plan tidak mencantumkannya. |
| C9 | **Mengirim ulang undangan yang sudah kedaluwarsa memakai satu kursi** (limit `members`), karena undangan kedaluwarsa tidak lagi dihitung; menyegarkan undangan yang masih hidup tidak memakai kursi. | Konsisten dengan "anggota + undangan aktif". | - |
| C10 | **Batas rate limit endpoint anonim** ditetapkan konstanta di `internal/app/ratelimits.go` (login 60/menit/IP, webhook 600/menit/IP, tautan undangan 60/menit/IP, `POST /orgs` 10/jam/user, dst. §5.4). | Angka konservatif tanpa data nyata. | Perlu disetel; belum bisa lewat config (D8). |

---

## D. Utang teknis dan batasan yang diketahui

### Billing

| ID | Masalah | Dampak | Prioritas | Status |
|---|---|---|---|---|
| D1 | **Panggilan ke gateway terjadi di dalam transaksi DB** (checkout menahan kunci baris subscription dan satu koneksi selama panggilan, timeout klien Midtrans default 15 detik). Bila gateway gagal, tidak ada yang tersimpan. | Gateway lambat menahan koneksi pool dan kunci org. Pada beban tinggi bisa menghabiskan pool. | P2 | Backlog (invoice dua fase / draft) |
| D2 | **Tautan pembayaran tidak bisa dibatalkan di gateway** (`PaymentProvider` tidak punya `Cancel`). Pembayaran masuk untuk invoice yang sudah `void` hanya ditandai untuk ditinjau (C6). | Pengguna bisa membayar tautan lama; uang masuk tanpa efek sampai ditangani manual. | P2 | Backlog |
| D3 | **Tidak ada retry/backoff** panggilan keluar ke gateway selain timeout klien. | Gangguan singkat gateway langsung menggagalkan checkout. | P3 | Backlog |
| D4 | **Tidak ada penanganan refund/chargeback otomatis.** | Ditandai manual (C6). | P3 | Backlog |
| D5 | **Akses bisa melewati akhir periode sampai satu interval job** (default 1 jam, lebih lama bila worker mati): `active -> past_due` dan trial `-> expired` baru terjadi saat job `billing.expire` jalan. Akhir masa tenggang eksak (dibandingkan dengan jam). Pengingat harian bisa tiba hingga sehari setelah tanda H-3/H-1. `GrantsAccess` sengaja tidak diubah. | Sedikit akses gratis di akhir periode; pengingat telat. | P3 | Diterima |
| D6 | Status invoice **`draft` ada di skema tetapi tidak pernah disimpan.** Mock `payment.Provider` dibuat mockery tetapi tidak dipakai (provider fake mencakup semua alur). | Kode mati kecil. | P3 | Diterima |
| D7 | **Scheduler satu loop** (bukan satu goroutine per job) agar hanya memakai satu koneksi tambahan. Job yang menemukan baris terkunci job lain melewatinya dan mengambilnya pada jalan berikutnya. | Job yang lambat menunda job lain pada tick yang sama. | P3 | Diterima |

### Keamanan dan operasi

| ID | Masalah | Dampak | Prioritas | Status |
|---|---|---|---|---|
| D8 | Batas rate limit (C10) adalah konstanta, belum bisa disetel lewat config. | Perlu rebuild untuk menyesuaikan. | P3 | Backlog |
| D9 | **Tidak ada alarm/metrik.** Kasus manual webhook (C6), job outbox `failed`, dan job billing gagal hanya ada di log JSON dan tabel; belum ada metrik, tracing terdistribusi, atau alert. | Masalah uang bisa tidak terlihat. | **P2** | Backlog |
| D10 | **`outbox_jobs` menyimpan payload email apa adanya**, termasuk tautan dengan token mentah (berumur 1-24 jam, undangan 7 hari). Baris `done`/`failed` dan `payment_events` lama tidak pernah dibersihkan. | Akses ke database = akses ke token yang belum kedaluwarsa; tabel tumbuh terus. | P2 | Backlog |
| D11 | **`LogMailer`** (driver `log`, hanya dev, ditolak di production) mencatat isi email di log level debug. | Jangan naikkan ke debug bila log dibagikan. | P3 | Diterima |
| D12 | **Kredensial DB default (`saka:saka`) tidak ditolak di production** oleh konfigurasi (begitu juga Redis tanpa password). | Tanggung jawab operator; dicatat di README. | P2 | Backlog |
| D13 | **JWT tanpa `iss`/`aud`.** | Aman selama satu penerbit dan satu penerima; perlu ditambah bila ada layanan penerima kedua. | P3 | Backlog |
| D14 | **Cookie refresh dan subdomain:** `SameSite=Strict` adalah batas site, bukan origin; subdomain yang dikuasai penyerang di domain yang sama dapat memicu `POST /auth/refresh`. Tanpa token CSRF terpisah. | Hanya merotasi token korban dan hasilnya tak terbaca lintas origin; risiko rendah. | P3 | Diterima |
| D15 | **`/healthz` dan `/readyz` tidak dibatasi** (`/readyz` mem-ping DB dan Redis). | Batasi di reverse proxy bila terbuka ke internet. | P3 | Diterima |
| D16 | **`golang.org/x/crypto` GO-2026-5932** belum punya versi perbaikan; simbolnya tidak dipanggil dari kode kita (hanya `argon2`). | Dipantau lewat `govulncheck`. | P3 | Diterima |
| D17 | **Log panic memuat nilai dan stack apa adanya** (hanya ke log, tidak ke klien). | Kode proyek tidak memanggil `panic` dengan data pengguna. | P3 | Diterima |
| D18 | Rate limit webhook **gagal terbuka** bila Redis mati (menjatuhkan notifikasi pembayaran lebih mahal daripada lonjakan yang ditolak signature). | Disengaja; satu-satunya endpoint yang begitu. | P3 | Diterima |

### Cakupan test dan tooling

| ID | Masalah | Dampak | Prioritas | Status |
|---|---|---|---|---|
| D19 | **Handler billing tidak punya unit test jalur gagal** (mis. `Usage` mengembalikan error -> 500). Jalur itu hanya tertutup lewat e2e. | Regresi di pemetaan error handler tidak tertangkap cepat. | P3 | Terbuka |
| D20 | **Suite integrasi lambat:** `internal/app` ~150-160 detik, `internal/billing` ~110 detik (testcontainers + Argon2 ringan + banyak skenario waktu). | CI dan siklus lokal panjang. | P3 | Diterima |
| D21 | `TestGuard_JoiningWaitsForTheOrganizationLock` menunggu 400 ms untuk membuktikan penerimaan undangan **menunggu** kunci. Pada mesin sangat lambat, tes bisa lulus palsu (bukan gagal palsu). | Kecil. | P3 | Diterima |
| D22 | Job `govulncheck` di CI bersifat **advisory** (tidak menggagalkan build). | Kerentanan tidak memblokir merge. | P3 | Diterima |
| D23 | **Rute pertama yang digerbang fitur belum ada.** `audit_log` dan `export_csv` hanya kode fitur di data plan dan dites lewat rute uji (`app.Options.TenantRoutes`); tidak ada rute produksi memakai `RequireFeature`. | Penggunaan nyata menunggu modul domain pertama (`docs/panduan-modul-baru.md`). | P3 | Diterima |
| D24 | **Avatar dan logo organisasi hanya URL** (upload file di luar Tier 1). | Pengguna harus meng-host gambarnya sendiri. | P3 | Backlog |

---

## E. Riwayat kualitas proses yang perlu diingat

- Perintah `make lint ... | tail && git commit` tanpa `set -o pipefail` meloloskan commit meski lint gagal; dua commit Fase 4 sempat begitu dan diperbaiki dengan amend sebelum digabung. Gunakan `set -o pipefail` atau jalankan lint terpisah sebelum commit.
- Di worktree, `make lint` bisa kehabisan waktu karena harus mengompilasi `golangci-lint`; salin `bin/golangci-lint` dari checkout utama (gitignored).
- Fase 5 sempat menaruh sembilan butir §9.4 di bawah tabel §14; sudah dikembalikan ke §9.4 oleh Fase 7.
- Fase 6 menggabung beberapa tugas todo dalam satu commit (checkout + invoice, expire + apply_scheduled, reminders + reconcile); centangnya benar di `docs/todo.md`.

---

## Ringkasan prioritas

**P1 (sebelum rilis):** A1 (sandbox Midtrans dan verifikasi asumsi B1), A2 (provider email + DNS), A3 (Go >= 1.26.10).
**P2 (sebaiknya sebelum rilis):** A4 (migrasi dev), D1 (gateway di dalam transaksi), D2 (batalkan tautan pembayaran), D9 (alarm dan metrik), D10 (pembersihan outbox/payment_events), D12 (tolak kredensial default di production).
**Sisanya** boleh menyusul (P3) atau sudah diterima dengan alasan.
