# Panduan Deployment Saka Web Frontend (SPA)

Dokumen ini berisi panduan komprehensif untuk membangun, mengonfigurasi, dan men-deploy frontend web Saka (SvelteKit Single Page Application) ke lingkungan produksi.

---

## 1. Arsitektur Deployment

Frontend Saka dibangun sebagai **Single Page Application (SPA) statis** menggunakan `@sveltejs/adapter-static`. Seluruh berkas keluaran berada di direktori `web/build/` dan disajikan oleh web server/reverse proxy seperti Nginx atau Caddy.

Ada dua pola arsitektur deployment yang didukung:

### Opsi A: Same-Origin (Sangat Disarankan)

Frontend dan Backend API berjalan di bawah satu domain yang sama:
- **Frontend SPA:** `https://example.com/`
- **Backend Go API:** `https://example.com/api/` (diteruskan via reverse proxy ke backend Go lokal, mis. `http://127.0.0.1:8080`)

**Keunggulan:**
- Tidak memerlukan konfigurasi CORS browser (`app.cors_allowed_origins` di backend bisa dibiarkan kosong `[]`).
- Mengurangi latency preflight request `OPTIONS`.
- Mengeliminasi masalah keamanan third-party cookie/partitioned storage.

### Opsi B: Subdomain Terpisah

Frontend dan Backend berada di domain berbeda:
- **Frontend SPA:** `https://app.example.com`
- **Backend Go API:** `https://api.example.com`

**Persyaratan tambahan:**
1. Frontend harus di-build dengan variabel `PUBLIC_API_BASE_URL`:
   ```bash
   PUBLIC_API_BASE_URL=https://api.example.com/api/v1 pnpm build
   ```
2. Backend `config.json` wajib mendaftarkan origin frontend:
   ```json
   {
     "app": {
       "frontend_url": "https://app.example.com",
       "cors_allowed_origins": ["https://app.example.com"]
     }
   }
   ```

---

## 2. Langkah Build Produksi

### Prasyarat
- Node.js 22 LTS atau lebih baru
- `pnpm` (direkomendasikan v10+)

### Perintah Build

```bash
cd web

# 1. Pasang dependensi secara deterministik
pnpm install --frozen-lockfile

# 2. Validasi tipe, linting, dan pengujian sebelum build
pnpm check
pnpm lint
pnpm test

# 3. Jalankan build produksi
pnpm build
```

Berkas hasil build berada di direktori `web/build/` dengan struktur:
```
web/build/
├── index.html           # SPA entry point
├── favicon.svg          # Favicon vektor
├── _app/
│   ├── immutable/
│   │   ├── assets/      # CSS & font statis inter/dm-sans
│   │   ├── chunks/      # JS chunks
│   │   ├── entry/
│   │   └── nodes/
```

### Verifikasi Build Produksi

Pastikan artefak build aman dan tidak memuat modul rahasia/simulasi dev:
```bash
# 1. Pastikan index.html terbuat
test -f build/index.html && echo "Build OK"

# 2. Verifikasi TIDAK ADA rahasia simulasi dev di berkas build
! grep -r "fake-webhook-secret" build/ && echo "Security check passed: No dev simulation secrets found."
```

---

## 3. Konfigurasi Reverse Proxy

### Nginx

Simpan konfigurasi berikut pada `/etc/nginx/sites-available/saka.conf` (sesuaikan domain dan path root):

```nginx
# HTTP -> HTTPS redirect
server {
    listen 80;
    listen [::]:80;
    server_name example.com;
    return 301 https://$host$request_uri;
}

# HTTPS server
server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name example.com;

    # SSL Certificates
    ssl_certificate /etc/letsencrypt/live/example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;
    ssl_session_cache shared:SSL:10m;
    ssl_session_timeout 1d;

    # Root direktori hasil build SPA
    root /var/www/saka-web/build;
    index index.html;

    # Security Headers
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains; preload" always;
    add_header Permissions-Policy "camera=(), microphone=(), geolocation=()" always;

    # Content Security Policy (CSP)
    # Sesuaikan connect-src bila menggunakan endpoint API atau analytics eksternal
    add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https://api.midtrans.com https://app.midtrans.com; frame-src https://app.midtrans.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self';" always;

    # Kompresi Gzip
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_proxied any;
    gzip_types text/plain text/css text/xml application/json application/javascript application/xml+rss application/atom+xml image/svg+xml;

    # Aset statis immutable SvelteKit (cache selamanya karena file berkode hash)
    location /_app/immutable/ {
        expires 1y;
        add_header Cache-Control "public, max-age=31536000, immutable";
        try_files $uri =404;
    }

    # Proxy untuk backend Go API
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;

        # Header forwarding standar
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Timeout proxy
        proxy_connect_timeout 10s;
        proxy_read_timeout 30s;
        proxy_send_timeout 30s;
    }

    # Fallback SPA: semua rute aplikasi diteruskan ke index.html
    location / {
        try_files $uri $uri/ /index.html;
        # Jangan cache index.html agar rilis versi baru langsung terdeteksi
        add_header Cache-Control "no-cache, no-store, must-revalidate";
    }
}
```

### Caddy (Caddyfile)

Caddy menyediakan HTTPS otomatis dan sintaks konfigurasi yang sangat ringkas:

```caddy
example.com {
    # Root direktori hasil build SPA
    root * /var/www/saka-web/build

    # Security Headers
    header {
        X-Content-Type-Options "nosniff"
        X-Frame-Options "DENY"
        Referrer-Policy "strict-origin-when-cross-origin"
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        Permissions-Policy "camera=(), microphone=(), geolocation=()"
        Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https://api.midtrans.com https://app.midtrans.com; frame-src https://app.midtrans.com; frame-ancestors 'none'; base-uri 'self'; form-action 'self';"
    }

    # Kompresi zstd & gzip
    encode zstd gzip

    # Cache immutable untuk aset SvelteKit
    @immutable path /_app/immutable/*
    header @immutable Cache-Control "public, max-age=31536000, immutable"

    # Reverse proxy ke API Go
    handle_path /api/* {
        reverse_proxy localhost:8080 {
            header_up Host {host}
            header_up X-Real-IP {remote_host}
            header_up X-Forwarded-For {remote_host}
            header_up X-Forwarded-Proto {scheme}
        }
    }

    # SPA Fallback ke index.html
    handle {
        try_files {path} {path}/ /index.html
        header Cache-Control "no-cache, no-store, must-revalidate"
        file_server
    }
}
```

---

## 4. Analisis Header Keamanan & CSP

| Direktif CSP / Header | Nilai | Penjelasan Keamanan |
|---|---|---|
| `default-src` | `'self'` | Membatasi pemuatan aset hanya dari origin domain sendiri secara default. |
| `script-src` | `'self'` | Mencegah eksekusi script eksternal atau injection XSS (tidak memakai `'unsafe-eval'`). |
| `style-src` | `'self' 'unsafe-inline'` | Mengizinkan CSS internal Tailwind/shadcn variabel inline, mencegah style sheet luar asing. |
| `img-src` | `'self' data: https:` | Mengizinkan avatar SVG base64 (`data:`) dan avatar URL pengguna berbasis HTTPS aman. |
| `font-src` | `'self'` | Font Inter dan DM Sans disajikan mandiri lokal dari bundel tanpa CDN Google Fonts pihak ketiga. |
| `connect-src` | `'self' https://api.midtrans.com https://app.midtrans.com` | Mengizinkan request fetch/XHR ke API Saka dan gateway pembayaran Midtrans. |
| `frame-src` | `https://app.midtrans.com` | Memungkinkan rendering iframe Snap Midtrans saat transaksi jika menggunakan mode popup. |
| `frame-ancestors` | `'none'` | **Anti-Clickjacking**: Mencegah seluruh halaman Saka disematkan ke dalam `<iframe>` di situs lain. |
| `X-Content-Type-Options` | `nosniff` | Mencegah MIME sniffing oleh browser lama terhadap konten file. |
| `X-Frame-Options` | `DENY` | Proteksi tambahan kompatibilitas lama untuk clickjacking. |
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains; preload` | Memaksa koneksi HTTPS selama 1 tahun penuh. |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Mencegah path internal bocor ke situs eksternal saat navigasi link keluar. |

---

## 5. Konfigurasi Midtrans Payment Return URL

Saat invoice dibayarkan melalui Snap Midtrans atau dialihkan ke payment page gateway bank/e-wallet, pengguna akan dikembalikan ke URL frontend Saka.

### Pengaturan di Dashboard Midtrans

1. Masuk ke **Midtrans Merchant Administration Portal (MAP)**:
   - Sandbox: `https://dashboard.sandbox.midtrans.com/`
   - Production: `https://dashboard.midtrans.com/`
2. Buka menu **Settings** > **Snap Preferences** > **Payment Redirection**.
3. Atur tiga field redirect URL berikut ke rute return frontend:
   - **Finish Redirect URL:** `<frontend_url>/billing/return`
   - **Unfinish Redirect URL:** `<frontend_url>/billing/return`
   - **Error Redirect URL:** `<frontend_url>/billing/return`

*Contoh di produksi:*
`https://example.com/billing/return`

### Cara Kerja Halaman `/billing/return`
1. Sebelum pengalihan ke pembayaran, frontend menyimpan ID invoice dan ID organisasi ke dalam `sessionStorage` (`saka.pendingPayment`).
2. Setelah pengguna kembali dari halaman gateway bank, halaman `/billing/return` membaca status invoice secara berkala (*exponential backoff polling*).
3. Saat webhook Midtrans diterima backend dan invoice beralih ke status `paid`, polling berhenti dan menampilkan ucapan sukses dengan tombol navigasi kembali ke dasbor billing.
4. Jika waktu tunggu habis (*timeout* 30 detik), pengguna tetap diberikan konfirmasi bahwa pembayaran sedang diproses di latar belakang dan email konfirmasi akan dikirimkan saat dana terverifikasi.
