# Demo end-to-end

Skenario lengkap dengan `curl` terhadap API lokal: **daftar, verifikasi email, undang anggota, mentok di limit Free, upgrade (checkout + pembayaran), lalu langganan tidak diperpanjang sampai kembali ke Free tanpa data yang hilang**. Skenario yang sama berjalan otomatis sebagai satu test cerita: `TestDemoScenario_RegisterInviteHitTheLimitUpgradeThenLapseBackToFree` (`internal/app/demo_scenario_integration_test.go`), dengan jam palsu menggantikan menunggu berminggu-minggu.

## Persiapan

Butuh `curl`, `jq`, `openssl` dan Docker. Pakai database dev **kosong** atau yang boleh dikotori (demo membuat akun dan invoice).

```bash
make up config migrate      # PostgreSQL, Redis, Mailpit; config.json memakai billing.provider "fake" dan mail.driver "smtp"
make run                    # terminal 1: API di http://localhost:8080
make run-worker             # terminal 2: worker (email lewat outbox, job billing)
```

Provider pembayaran `fake` (default `config.example.json`) tidak memanggil gateway apa pun: ia membuat tautan bayar palsu dan menerima webhook yang **ditandatangani HMAC-SHA256 dengan rahasia publik `fake-webhook-secret`**. Itulah cara mensimulasikan "pelanggan sudah membayar" lewat HTTP di dev: kita mengirim sendiri webhook bertanda tangan itu (Langkah 5). Provider ini ditolak konfigurasi di production.

Email keluar lewat worker ke Mailpit (UI http://localhost:8025). Fungsi bantu berikut (dipakai seterusnya) mengambil token dari email terbaru ke sebuah alamat:

```bash
API=http://localhost:8080/api/v1
MAILPIT=http://localhost:8025

mail_token() {  # mail_token ALAMAT KATA_SUBJEK: token dalam tautan email terbaru ke ALAMAT yang subjeknya memuat KATA_SUBJEK
  for _ in $(seq 1 20); do   # worker mengirim lewat outbox, jadi tunggu sebentar
    id=$(curl -s -G "$MAILPIT/api/v1/search" --data-urlencode "query=to:$1 subject:$2" | jq -r '.messages[0].ID')
    [ "$id" != "null" ] && [ -n "$id" ] && break
    sleep 1
  done
  curl -s "$MAILPIT/api/v1/message/$id" | jq -r '.Text' | grep -o 'token=[A-Za-z0-9_-]*' | head -1 | cut -d= -f2
}

register() {  # register EMAIL NAMA ORGANISASI
  curl -s -X POST "$API/auth/register" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"correct-horse\",\"name\":\"$2\",\"organization_name\":\"$3\"}" | jq -r '.user.id'
}

verify() {  # verify EMAIL: buka tautan verifikasi dari email
  curl -s -X POST "$API/auth/verify-email" -H 'Content-Type: application/json' \
    -d "{\"token\":\"$(mail_token "$1" Verifikasi)\"}" | jq -r '.email_verified'
}

login() {  # login EMAIL -> access token (15 menit; panggil lagi bila kedaluwarsa)
  curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"correct-horse\",\"token_delivery\":\"body\"}" | jq -r '.access_token'
}

api() {  # api TOKEN METODE PATH [BODY]
  curl -s -X "$2" "$API$3" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' ${4:+-d "$4"}
}
```

## Langkah 1: Ann mendaftar dan mendapat organisasi di plan Free

```bash
register ann@example.com Ann Acme
ANN=$(login ann@example.com)
ORG=$(api $ANN GET /orgs | jq -r '.items[0].id')
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription.effective_plan'      # free
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription.usage.members'       # {"used":1,"max":3}
```

Ann belum memverifikasi email, jadi mengundang ditolak (`403 EMAIL_NOT_VERIFIED`):

```bash
api $ANN POST /orgs/$ORG/invitations '{"email":"bob@example.com","role":"member"}' | jq -r '.error.code'
verify ann@example.com                                                            # true
ANN=$(login ann@example.com)
```

## Langkah 2: undang Bob (belum punya akun) dan Carol (sudah punya)

```bash
api $ANN POST /orgs/$ORG/invitations '{"email":"bob@example.com","role":"member"}' | jq -r '.invitation.email'
TOKEN_BOB=$(mail_token bob@example.com mengundang)
curl -s "$API/invitations/$TOKEN_BOB" | jq -r '.invitation.account_exists'         # false -> frontend mengarahkan ke registrasi
register bob@example.com Bob Bobsorg
verify bob@example.com                                                            # membuktikan alamat = menerima undangan

register carol@example.com Carol Carolco
verify carol@example.com
api $ANN POST /orgs/$ORG/invitations '{"email":"carol@example.com","role":"member"}' > /dev/null
TOKEN_CAROL=$(mail_token carol@example.com mengundang)
CAROL=$(login carol@example.com)
curl -s -X POST "$API/invitations/$TOKEN_CAROL/accept" -H "Authorization: Bearer $CAROL" | jq -r '.organization.role'   # member
api $ANN GET /orgs/$ORG/members | jq -r '.items | length'                          # 3: Ann, Bob, Carol
```

Catatan: undangan ke email yang sudah punya akun memakai `accept` (login dan email terverifikasi); undangan ke email baru otomatis diterima begitu pemilik alamat itu memverifikasinya.

## Langkah 3: mentok di limit Free

```bash
api $ANN POST /orgs/$ORG/invitations '{"email":"dave@example.com","role":"member"}' | jq -r '.error | "\(.code) \(.details)"'
# PLAN_LIMIT_REACHED {"limit":"members","max":3,"plan":"free","upgrade_to":"pro","used":3}
```

Limit menghitung anggota ditambah undangan yang masih aktif, dan respons memberi tahu plan yang cukup (`upgrade_to`).

## Langkah 4: upgrade ke Pro (checkout)

```bash
CHECKOUT=$(api $ANN POST /orgs/$ORG/subscription/checkout '{"plan":"pro"}')
echo "$CHECKOUT" | jq -r '.trial_started'                                          # true: trial 14 hari, tanpa bayar
INV=$(echo "$CHECKOUT" | jq -r '.invoice.number')                                   # mis. INV-2026-000001
echo "$CHECKOUT" | jq -r '.invoice.payment_url'                                      # tautan gateway (palsu: https://pay.fake.test/...)
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription | "\(.status) \(.effective_plan)"'   # trialing pro
api $ANN POST /orgs/$ORG/invitations '{"email":"dave@example.com","role":"member"}' | jq -r '.invitation.email'  # sekarang muat (limit Pro: 10)
register dave@example.com Dave Dave
verify dave@example.com
```

## Langkah 5: pelanggan membayar (webhook bertanda tangan)

Gateway sungguhan mengirim webhook sendiri. Dengan provider `fake`, kirim yang sama dengan tanda tangan HMAC-SHA256 atas body (hanya webhook terverifikasi yang boleh mengubah status bayar; redirect klien tidak pernah dipercaya):

```bash
pay_invoice() {  # pay_invoice NOMOR_INVOICE JUMLAH [EVENT_ID]
  body="{\"event_id\":\"${3:-evt-$1}\",\"invoice_number\":\"$1\",\"provider_ref\":\"txn-$1\",\"state\":\"paid\",\"amount\":$2,\"currency\":\"IDR\"}"
  sig=$(printf '%s' "$body" | openssl dgst -sha256 -hmac fake-webhook-secret | awk '{print $NF}')
  curl -s -X POST "$API/webhooks/fake" -H 'Content-Type: application/json' -H "X-Fake-Signature: $sig" -d "$body" | jq -r '.status'
}

pay_invoice "$INV" 99000                                                          # ok
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription | "\(.status) \(.current_period_end)"'  # active, satu bulan dari sekarang
api $ANN GET /orgs/$ORG/invoices | jq -r '.items[0].status'                          # paid
pay_invoice "$INV" 99000                                                          # kirim ulang: tetap ok, tidak diproses dua kali
```

Mencoba webhook dengan tanda tangan salah menghasilkan `401` dan tidak ada yang tersimpan.

## Langkah 6: waktu berlalu, langganan tidak dibayar

Jam tidak bisa dimajukan di dev sungguhan, jadi kita **menggeser data langganan ke masa lalu** lalu membiarkan worker menjalankan job billing (job berjalan saat worker start dan tiap jam; restart worker supaya langsung jalan). Di test otomatis yang digeser adalah jam palsu, bukan data.

```bash
sql() { docker compose exec -T postgres psql -U saka -d saka -c "$1"; }

# Periode berbayar berakhir 1 jam lalu. Job renewals menerbitkan invoice perpanjangan, job expire memindahkan ke past_due.
sql "UPDATE subscriptions SET current_period_start = now() - interval '31 days', current_period_end = now() - interval '1 hour' WHERE organization_id = '$ORG'"
```

Restart worker (`Ctrl+C` di terminal 2, lalu `make run-worker`), kemudian:

```bash
ANN=$(login ann@example.com)
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription | "\(.status) \(.effective_plan) \(.grace_until)"'   # past_due pro <+3 hari>
api $ANN GET /orgs/$ORG/invoices | jq -r '.items[0] | "\(.kind) \(.status)"'                                        # renewal open
```

`past_due` masih memberi akses Pro sampai `grace_until` (3 hari). Geser lagi supaya masa tenggang habis, restart worker:

```bash
sql "UPDATE subscriptions SET current_period_end = now() - interval '5 days', grace_until = now() - interval '1 hour' WHERE organization_id = '$ORG'"
```

```bash
ANN=$(login ann@example.com)
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription | "\(.status) \(.effective_plan) \(.usage.members)"'   # expired free {"used":4,"max":3}
api $ANN POST /orgs/$ORG/invitations '{"email":"erin@example.com","role":"member"}' | jq -r '.error.code'            # PLAN_LIMIT_REACHED (Free lagi)
api $ANN GET /orgs/$ORG/members | jq -r '.items | length'                                                           # 4: tidak ada anggota yang dihapus
```

Email yang terkirim sepanjang cerita (lihat di Mailpit): verifikasi, undangan, `invoice_created`, `payment_succeeded`, `subscription_past_due`, `subscription_expired`.

## Langkah 7: kembali berlangganan

Organisasi `expired` bisa berlangganan lagi lewat checkout (tanpa trial kedua):

```bash
CHECKOUT=$(api $ANN POST /orgs/$ORG/subscription/checkout '{"plan":"pro"}')
echo "$CHECKOUT" | jq -r '.trial_started'                                           # false
pay_invoice "$(echo "$CHECKOUT" | jq -r '.invoice.number')" 99000
api $ANN GET /orgs/$ORG/subscription | jq -r '.subscription | "\(.status) \(.effective_plan)"'   # active pro
```

## Membersihkan

Database dev tidak otomatis dibersihkan. Hapus akun demo dengan `DELETE /me` (butuh password) atau buang database dev: `make down` lalu `docker volume rm` volume Postgres proyek ini, atau jalankan `make migrate-down` berulang. Email demo dapat dihapus dari UI Mailpit.
