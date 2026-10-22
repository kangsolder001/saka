# Email produksi: Resend, SPF, DKIM dan DMARC

> Status keputusan: **Resend adalah rekomendasi, bukan keputusan final pemilik.** Pengirim email dibuat di balik interface `mail.Mailer` (`internal/mail/mailer.go`), jadi provider bisa diganti tanpa menyentuh modul lain (lihat "Mengganti provider" di bawah). Driver `resend` ditulis terhadap dokumentasi API Resend dan diuji dengan server HTTP palsu; **belum pernah memanggil API Resend sungguhan** (butuh akun, API key, dan domain terverifikasi, yaitu tindakan pemilik).

## Kenapa Resend

API HTTP sederhana (satu `POST /emails`), tanpa SDK (cukup `net/http`), tier gratis cukup untuk email transaksional kecil, dan dashboard menampilkan status domain dan log pengiriman. Alternatif yang masuk akal: Brevo, Amazon SES (murah di volume besar, setup paling panjang), atau SMTP relay apa pun (driver `smtp` sudah ada).

## Konfigurasi

Hanya di `config.json` (kunci API tidak pernah dari env var, tidak pernah di repositori):

```json
{
  "mail": {
    "driver": "resend",
    "from": "Saka <noreply@mail.contoh.com>",
    "resend": {
      "api_key": "re_...",
      "base_url": "https://api.resend.com",
      "timeout": "15s"
    }
  }
}
```

| Kunci | Arti |
|---|---|
| `mail.driver` | `resend`, `smtp` atau `log`. Production **menolak** `log` (email akan terbuang) |
| `mail.from` | Pengirim. Domainnya harus yang diverifikasi di Resend (bagian di bawah) |
| `mail.resend.api_key` | Kunci API. Wajib bila driver `resend`; production menolak placeholder |
| `mail.resend.base_url` | Default `https://api.resend.com`; production mewajibkan `https` |
| `mail.resend.timeout` | Batas satu panggilan API (default 15 detik) |

Yang membaca konfigurasi ini hanya proses **worker** (`cmd/worker`): API tidak pernah mengirim email sendiri, ia hanya memasukkan job ke outbox (spec §10.1). Jadi `mail.resend` harus ada di `config.json` worker.

## Perilaku kegagalan (retry)

Worker outbox mencoba ulang dengan jadwal 1m, 5m, 30m, 2j, 6j (total 6 percobaan). Driver `resend` memberi tahu worker mana yang layak diulang:

| Jawaban Resend | Diperlakukan sebagai | Alasan |
|---|---|---|
| jaringan putus, timeout, DNS | dicoba ulang | pulih sendiri |
| `5xx`, `429`, `408`, `409` | dicoba ulang | Resend sibuk atau membatasi kita |
| `401`, `403` (kunci salah, domain belum terverifikasi) | dicoba ulang | masalah konfigurasi yang bisa diperbaiki operator selagi job masih di antrean |
| `400`, `404`, `405`, `422` dan 4xx lain | gagal permanen (`failed`, tanpa retry) | pesan itu sendiri ditolak (alamat tidak valid, parameter salah) |

Pengiriman bersifat *at-least-once* (spec §10.1): bila worker mati setelah Resend menerima tetapi sebelum job ditandai `done`, email bisa terkirim dua kali. Resend mendukung header `Idempotency-Key`; memakainya (kunci = ID job) menutup celah itu, dan dicatat di backlog Tier 2 karena butuh `Mailer.Send` menerima ID job.

Log tidak pernah memuat API key; pesan error Resend dipotong 200 karakter.

## Menyiapkan domain pengirim

Tujuannya agar Gmail, Outlook dan lainnya percaya bahwa email dari `@mail.contoh.com` memang dikirim oleh Resend atas nama Anda. Tiga mekanisme DNS yang saling melengkapi:

- **SPF**: daftar server yang boleh mengirim untuk domain itu.
- **DKIM**: tanda tangan kriptografis pada tiap email; penerima memverifikasinya dengan kunci publik di DNS Anda.
- **DMARC**: kebijakan apa yang dilakukan penerima bila SPF dan DKIM gagal sejajar dengan alamat `From`, plus laporan.

Langkah (di dashboard Resend, bagian *Domains*):

1. **Pilih subdomain khusus** untuk email transaksional, mis. `mail.contoh.com`, bukan domain utama. Reputasi pengiriman terpisah dari email karyawan, dan record-nya tidak bentrok dengan SPF yang sudah ada di domain utama.
2. **Tambah domain** (*Add Domain*), pilih region terdekat dengan server Anda. Resend menampilkan daftar record DNS **yang harus Anda salin persis seperti yang tampil di dashboard** (nilai di bawah hanya bentuk umumnya; nilai kunci DKIM unik per domain):

   | Jenis | Nama (host) | Isi (bentuk umum) | Fungsi |
   |---|---|---|---|
   | `TXT` | `resend._domainkey.mail` | `p=MIGfMA0GCSq...` (kunci publik DKIM, panjang) | **DKIM** |
   | `MX` | `send.mail` | `feedback-smtp.<region>.amazonses.com`, prioritas 10 | alamat bounce (*return-path*) |
   | `TXT` | `send.mail` | `v=spf1 include:amazonses.com ~all` | **SPF** untuk return-path |

   Resend mengirim lewat infrastruktur Amazon SES, karena itu SPF-nya merujuk `amazonses.com`. Record SPF berada pada subdomain return-path (`send`), jadi **tidak menyentuh SPF domain utama**; jangan menggabungkan dua record SPF dalam satu nama (hanya satu `v=spf1` per nama yang sah).
3. **Tambahkan record itu** di penyedia DNS Anda (Cloudflare, Route 53, registrar). Bila registrar otomatis menambahkan nama domain di akhir host, isi host tanpa sufiks domain.
4. Klik **Verify** di Resend dan tunggu (biasanya menit sampai beberapa jam). Status harus `Verified` untuk DKIM dan SPF.
5. **Tambahkan DMARC** (record tersendiri, tidak diminta Resend tetapi sangat dianjurkan; tanpanya Gmail dan Yahoo dapat menurunkan kiriman massal):

   | Jenis | Nama | Isi |
   |---|---|---|
   | `TXT` | `_dmarc.mail` | `v=DMARC1; p=none; rua=mailto:dmarc@contoh.com; adkim=s; aspf=s` |

   Mulai dengan `p=none` (hanya memantau, laporan ke `rua`). Setelah 2-4 minggu laporan menunjukkan semua kiriman sah lulus, naikkan ke `p=quarantine`, lalu `p=reject`. `adkim=s; aspf=s` (selaras ketat) cocok karena `From` memakai subdomain yang sama dengan DKIM.
6. **Isi `mail.from`** dengan alamat di domain terverifikasi itu: `"Saka <noreply@mail.contoh.com>"`. Mengirim dari domain yang belum terverifikasi akan dijawab `403` oleh Resend (dicoba ulang, lalu gagal setelah 6 percobaan).
7. **Buat API key** di Resend dengan izin *Sending access* terbatas ke domain itu (bukan *Full access*), simpan di `mail.resend.api_key`.

## Memeriksa sebelum go-live

- Kirim satu email lewat alur sungguhan (daftar akun dengan alamat Anda), lalu di Gmail pilih *Show original*: **SPF: PASS**, **DKIM: PASS**, **DMARC: PASS** dan `mailed-by`/`signed-by` berisi domain Anda.
- `dig +short TXT resend._domainkey.mail.contoh.com`, `dig +short MX send.mail.contoh.com`, `dig +short TXT _dmarc.mail.contoh.com` mengembalikan nilai yang Anda pasang.
- Pantau `outbox_jobs` dengan `status = 'failed'` dan log worker (`outbox job failed`): 403 berulang berarti domain belum terverifikasi atau kunci salah.
- Bila memakai satu subdomain untuk produk lain, pisahkan `mail.from` per lingkungan (staging memakai domain/kunci sendiri).

## Mengganti provider

1. Implementasikan `mail.Mailer` (`Send(ctx, Message) error`) di `internal/mail`, hormati deadline `ctx`.
2. Bedakan kegagalan permanen dari sementara: kembalikan error bertipe yang dikenali `JobHandler.Handle` (`internal/mail/service.go`: tambahkan pemeriksaannya di samping `IsPermanentSMTPError` dan `IsPermanentResendError`) atau bungkus dengan `outbox.Permanent`.
3. Tambahkan nama driver di `newMailer` (`cmd/worker/main.go`), di struct dan validasi `platform/config` (default, `config.example.json`, test).
4. Tes dengan `httptest` seperti `internal/mail/resend_mailer_test.go`. Test `TestMailer_*` (`internal/mail/outbox_only_test.go`) memastikan driver baru hanya dipakai worker.
