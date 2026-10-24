# Arsitektur NineGuard

Dokumen ini menjelaskan rancangan sistem, alur transmisi paket, dan komponen inti dari **NineGuard**.

---

## 1. Konsep Desain

NineGuard dirancang berdasarkan prinsip:
* **Single Gateway for All Models:** AI Coding Agent hanya perlu mengarah ke satu endpoint (`http://localhost:8080/v1`).
* **Strict Client Authentication:** Hanya agen atau developer yang memiliki API key resmi dari NineGuard (`sk-ng-...`) yang diizinkan mengakses model.
* **Prefix-Based Upstream Routing:** Model dari berbagai penyedia dikelompokkan dengan prefix (misalnya `openrouter/model-id`, `local/model-id`).
* **Zero Overhead Streaming:** Response Server-Sent Events (SSE) dialirkan tanpa buffering berlebih.

---

## 2. Alur Request Masuk (Request Lifecycle)

```text
[1. Client Agent]
       │
       │ HTTP POST /v1/chat/completions (atau /v1/messages, /v1/responses)
       │ Header: Authorization: Bearer sk-ng-...
       ▼
[2. NineGuard Security Interceptor]
       │
       ├── Deteksi Format API (chat, messages, responses, other)
       │
       ├── Cek Validitas NineGuard API Key
       │     └── Jika Invalid / Disabled ──► 401 Unauthorized
       │
       ├── Cek Key Quota (hanya mode "key", total semua model)
       │     └── Jika limit tercapai ───────► 429 Too Many Requests
       │
       ├── Cek Izin Model Per-Key (Allowed Models)
       │     └── Jika Model Tidak Diizinkan ──► 403 Forbidden
       │
       ├── Cek Status Model Firewall Global
       │     └── Jika Disabled ─────────────► 403 Forbidden
       │
       ├── Prefix Stripping & Provider Matching
       │     └── "openrouter/anthropic/claude-3.5-sonnet"
       │           ├── Prefix "openrouter" ──► Provider OpenRouter
       │           └── Model "anthropic/claude-3.5-sonnet"
       │
       ├── Cek Model Quota (hanya mode "model", pakai Model ID hasil resolve)
       │     └── Jika limit tercapai ───────► 429 model_quota_exceeded
       │
       ├── Pipeline Plugin (hanya format chat)
       │
       ├── Injeksi Stream Usage Core (ADR 0011, format chat streaming)
       │     └── Tambah stream_options.include_usage: true jika belum ada
       │
       ├── Rewrite Request Header:
       │     └── Authorization: Bearer <Master_API_Key_Provider>
       ▼
[3. Upstream Provider]
       │
       │ Eksekusi Inferensi LLM
       ▼
[4. Response Streaming & Usage Diagnostics Ingestion]
       ├── Event-Buffered SSE Relay:
       │     ├── Filter chunk usage-only sintetis hasil injeksi (tidak dikirim ke client)
       │     └── Stream event SSE tetap di-flush ke client secara real-time
       └── Asynchronous Diagnostics Logging:
             ├── Ekstraksi usage normalisasi (input, output, cache read/write, reasoning)
             ├── Verifikasi Thinking Delivered & deteksi Model Fallback (throttled warning log)
             └── Simpan ke SQLite traffic_logs
```

---

## 3. Komponen Inti

1. **`internal/proxy`:**
   * Mengelola multiplexing permintaan HTTP, parsing JSON request, dan streaming SSE.
   * Menghitung penggunaan token (Prompt & Completion Tokens) secara transparan.
2. **`internal/providers`:**
   * Mengelola registrasi provider upstream (Route, Master Key, Prefix, Default Fallback).
   * Menjalankan agregasi model multi-provider untuk endpoint `GET /v1/models`.
3. **`internal/keys`:**
   * Mengelola siklus hidup API key client NineGuard (`sk-ng-...`).
   * Melakukan verifikasi token cepat berbasis memory cache.
4. **`internal/models`:**
   * Bertindak sebagai firewall model. Model yang dinonaktifkan langsung dicegat sebelum menyentuh upstream.
5. **`internal/traffic`:**
   * Mengagregasi metrik analitik: data time-series, p95 latensi, token mix, dan laporan penggunaan per key.
   * Menghitung jendela kuota (`CalcQuotaWindow`) dari Quota Reset Schedule milik key. Lihat bagian 3.1.

### 3.1 Jendela Kuota & Quota Reset Schedule

Kuota (Key Quota maupun Model Quota) dihitung dari token yang terpakai di dalam **jendela** berjalan. Kapan jendela itu reset ditentukan oleh **Quota Reset Schedule**: timezone (nama IANA), jam (HH:MM), hari dalam minggu (untuk `weekly`), dan tanggal (untuk `monthly`). Lihat ADR 0010.

**Jadwal yang berlaku** untuk sebuah key dicari berurutan:
1. jadwal milik key itu sendiri (override),
2. jadwal global (diatur di halaman Quotas, tombol *Reset schedule*),
3. bawaan lama: 00:00 UTC, Senin untuk weekly, tanggal 1 untuk monthly.

Jadwal global kosong secara bawaan, jadi key yang sudah ada tidak berubah perilakunya sampai admin mengaturnya.

**Cara jendela dihitung.** Tidak ada penghitung atau cron reset. Awal jendela dihitung ulang setiap pengecekan dari jadwal dan jam sekarang, lalu token dijumlahkan dari `traffic_logs` sejak awal jendela itu (`SELECT SUM(total_tokens) ... WHERE timestamp >= awal_jendela`). Awal dan akhir tiap jendela berasal dari satu fungsi yang sama, sehingga jendela berurutan tidak pernah tumpang tindih atau berlubang. Waktu dibentuk dari tanggal lokal (`time.Date`), bukan penambahan 24 jam, jadi tidak bergeser satu jam saat daylight saving berganti. Tanggal 29-31 di bulan yang lebih pendek berarti hari terakhir bulan itu.

**Mengubah jadwal tidak menghitung ulang pemakaian.** Saat jadwal efektif sebuah key berubah (edit jadwal key, edit jadwal global, atau menghapus override), jendela yang sedang berjalan disimpan sebagai **Transitional Window**: awalnya tetap, dan berakhir di reset pertama dari jadwal baru. Setelah itu jendela kembali dihitung normal. Satu jendela itu bisa lebih pendek atau lebih panjang dari biasanya. Ini disimpan di kolom `api_keys.quota_window_pins` (JSON per periode, berisi `start` dan `until`) dan kedaluwarsa sendiri, tanpa job pembersih. Contoh: reset bulanan tanggal 1 diganti ke tanggal 20 pada 17 Okt, maka jendela menjadi 1 Okt sampai 20 Okt, lalu 20 Okt sampai 20 Nov. Pemakaian 4,1M tetap 4,1M, sehingga key yang sedang diblokir tidak terbuka dan key yang aman tidak mendadak terblokir.

**Penyimpanan:** `api_keys.quota_reset_schedule` (JSON, NULL berarti mengikuti jadwal global), `api_keys.quota_window_pins` (JSON), dan baris `settings` bernama `quota_reset_schedule` untuk jadwal global.

**Pesan 429** menampilkan jam reset dalam timezone jadwal efektif key (mis. `at 14:00 WIB`), atau `UTC` bila memakai bawaan. `Retry-After` dalam detik sampai reset berikutnya, atau sampai akhir Transitional Window.

### 3.2 Telemetri Diagnostik Penggunaan (Usage Diagnostics)

NineGuard mencatat telemetri diagnostik lengkap pada setiap request:
1. **Format API (`api_format`):** Mengklasifikasikan request ke dalam `chat`, `messages` (Anthropic), `responses`, atau `other`.
2. **Prompt Caching (`cache_read_tokens`, `cache_write_tokens`):** Membaca token cache read dari OpenAI `prompt_tokens_details.cached_tokens` atau Anthropic `cache_read_input_tokens`, serta cache write dari `cache_creation_input_tokens` / `cache_write_tokens`.
3. **Reasoning & Extended Thinking:**
   * `thinking_requested`: Menandai apakah client meminta penalaran (via `reasoning_effort`, objek `reasoning`, objek `thinking`, atau nama model berakhiran `:thinking`).
   * `thinking_delivered`: Memverifikasi apakah upstream benar-benar menghasilkan penalaran (ada delta `reasoning_content`, blok `thinking`, atau `reasoning_tokens > 0`).
   * `reasoning_tokens`: Jumlah token penalaran yang dikonsumsi model.
4. **Deteksi Silent Model Fallback (`model_fallback`, `served_model`):** Membandingkan model yang diminta (setelah normalisasi keluarga model) dengan model aktual yang dilaporkan oleh response upstream. Jika terjadi penurunan model (misal Opus di-fallback ke Sonnet oleh upstream aggregator), request ditandai dengan badge peringatan dan memicu throttled warning log (`slog.Warn`) maksimal satu kali per 10 menit untuk kombinasi key dan model yang sama.
5. **Estimasi Penggunaan (`usage_estimated`):** Jika response stream atau non-stream 2xx tidak mengembalikan blok `usage` dari upstream, NineGuard menggunakan estimasi fallback yang ditandai secara eksplisit dengan simbol `≈` agar tidak disalahartikan sebagai angka pasti.

### 3.3 Estimasi Biaya (Estimated Cost)

NineGuard menghitung estimasi biaya ekuivalen harga katalog resmi (list-price) per request secara write-time (ADR 0012):
1. **Penyimpanan Write-Time (`cost_usd`):** Setiap request sukses (2xx) yang selesai dihitung biayanya sekali pada saat pencatatan dan disimpan di kolom `traffic_logs.cost_usd` dalam satuan USD (`REAL`). Request dengan estimasi token (`usage_estimated = 1`), model yang belum memiliki harga, total token 0, atau perintah sistem internal (`nineguard/system`) menyimpan nilai `NULL` (bukan 0).
2. **Katalog Harga & Prioritas Resolusi:**
   * **Override Admin (`model_price_overrides`):** Prioritas tertinggi, dicocokkan secara eksak per Model ID.
   * **Daftar Harga Publik LiteLLM (`model_prices`):** Dicocokkan berdasarkan normalisasi keluarga model (`NormalizeFamily`). Diimpor dari dataset publik LiteLLM dengan filter mode `chat` dan `responses`, diskalakan ke satuan per 1 juta token (1M tokens).
   * **Snapshot Bawaan (Embedded):** Snapshot JSON harga keluarga model populer dibenamkan (`go:embed`) di dalam binary NineGuard untuk inisialisasi awal tanpa internet.
3. **Konversi Multi-Mata Uang (Display Currency):**
   * Nilai di database selalu disimpan dalam USD. Konversi mata uang (misal ke IDR, EUR, JPY) dihitung saat penayangan menggunakan kurs efektif (kurs manual jika diisi, atau kurs otomatis dari Frankfurter API / ECB yang disinkronkan harian).
4. **Hitung Ulang Riwayat (Re-price):**
   * Admin dapat memicu perhitungan ulang riwayat `cost_usd` pada rentang waktu tertentu via `POST /api/v1/costs/reprice`. Request dengan token estimasi tidak akan disentuh.
5. **Visibilitas Biaya Per-Key (`cost_visibility`):**
   * Visibilitas biaya pada endpoint non-admin (`/usage`, `/v1/usage`) dapat diatur per-key (`show`, `hide`, atau mengikuti pengaturan global `cost_visible_to_keys`).

### 3.4 Ringkasan Penggunaan (Usage Summary)

NineGuard menyediakan metrik ringkasan penggunaan komprehensif per API key melalui perintah chat `/usage`, endpoint agent `GET /v1/usage`, dan panel admin:
1. **Perintah Chat & Endpoint Agent:**
   * `/usage [7d|30d|all]`: Merender ringkasan berformat Markdown yang merangkum jumlah request, tingkat error, sesi inferensi, total token (input, output, cache hit %), estimasi biaya (jika diizinkan), model favorit, jam sibuk (*peak hour*), status *thinking* & *fallback*, tabel distribusi model, perbandingan buku sastra, serta penggunaan gateway upstream induk.
   * Catatan perubahan perilaku: `/usage <provider>` tidak lagi memfilter laporan kuota; gunakan `/quota <provider>` untuk memfilter kuota per provider.
   * `GET /v1/usage?range=7d|30d|all`: Mengembalikan data terstruktur JSON yang diotentikasi oleh API key client. Jika visibilitas biaya dinonaktifkan (`ShowCost = false`), field `cost` dan `upstream_usage` dihilangkan.
2. **Inferensi Sesi (`session_gap_minutes`):**
   * Sesi dikelompokkan secara otomatis menggunakan *window function* SQLite (`LAG()`). Sebuah sesi baru dihitung pada request pertama dan setiap request yang memiliki jeda waktu lebih besar dari ambang batas (bawaan 30 menit).
3. **Cascaded Upstream Usage (ADR 0013):**
   * Untuk provider aktif bertipe `nineguard`, gateway memanggil `GET /v1/usage` ke gateway induk secara terisolasi dengan cache 5 menit. Penggunaan upstream disajikan murni sebagai metrik kapasitas agregat bersama, tidak pernah dicampur ke dalam metrik individu pengguna.
4. **Perbandingan Token Sastra (Book Comparison):**
   * Total token dibandingkan secara proporsional dengan karya sastra dunia (misal *Animal Farm*, *The Little Prince*, *The Hobbit*, *War and Peace*, hingga *English Wikipedia*).

### 3.5 Portal Mandiri Pemilik Key (Developer Portal `/portal`)

Untuk klien agent yang membajak perintah slash lokal (seperti Claude Desktop) atau bagi pengembang yang ingin memantau kuota tanpa akses admin, NineGuard menyediakan portal web mandiri di `/portal`:
1. **Otentikasi Berbasis API Key:** Pengguna masuk hanya menggunakan NineGuard API Key (`sk-ng-...`) yang disimpan di `localStorage` peramban lokal. Tidak memerlukan akun atau kata sandi admin.
2. **Isolasi Penuh:** Seluruh data diambil via endpoint key-scoped (`GET /v1/quota`, `GET /v1/usage`, `GET /v1/models`). Gateway menjamin pemegang key hanya dapat melihat kuota, sisa token, jadwal reset, estimasi biaya (jika diizinkan), heatmap aktivitas harian, dan model yang diizinkan untuk key miliknya sendiri. Data admin (log sistem, master key provider di vault, traffic explorer global) tetap terisolasi sepenuhnya.

---

## 4. Keamanan & Isolasi

* Master API Key dari upstream provider disimpan secara aman di SQLite lokal (`data/nineguard.db`) dan tidak pernah dikirim ke client agent.
* Client agent hanya berinteraksi menggunakan key `sk-ng-...`. Jika sebuah laptop atau environment terkompromi, key client tersebut dapat dicabut seketika tanpa perlu mengganti master key di provider upstream.
