<div align="center">

<img src="build/appicon.svg" width="128" alt="NineGuard">

# NineGuard

**Universal OpenAI-Compatible Gateway, Model Firewall & Multi-Provider Router**

NineGuard adalah reverse proxy dan firewall cerdas berperforma tinggi untuk mengamankan, mengelompokkan, dan melacak penggunaan model LLM dari berbagai provider penyedia (*OpenAI-Compatible*) tanpa mengubah kode aplikasi client.

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=flat&logo=docker&logoColor=white)](https://www.docker.com)
[![Storage](https://img.shields.io/badge/Storage-SQLite%20Pure%20Go-7952B3?style=flat)](https://modernc.org/sqlite)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

</div>

---

## Gambaran Umum (Overview)

Dalam ekosistem AI engineering, tim dan developer sering menggunakan berbagai model LLM dari beragam penyedia OpenAI-compatible (seperti 9router, OpenRouter, vLLM, Ollama lokal, LiteLLM, dsb.). Namun, muncul beberapa tantangan:

1. **Kebocoran Model & Kuota** — Model yang ingin di-nonaktifkan sering kali tetap bisa diakses jika client mengetahui atau mengetik nama modelnya secara manual.
2. **Ketiadaan Kontrol API Key Client** — Client / agent langsung menggunakan master key provider upstream, sehingga sulit melacak siapa developer atau agent yang menghabiskan token.
3. **Fragmentasi Multi-Provider** — Mengarahkan agent ke beberapa provider berbeda membutuhkan perubahan konfigurasi berulang kali.

**NineGuard menyelesaikan masalah tersebut sebagai Single Gateway:**
* **Gerbang Autentikasi Mandiri (*Strict Gatekeeper*)** — Client (Cursor, Cline, Pi, script) mengakses NineGuard menggunakan API key NineGuard (`sk-ng-...`). Tanpa key yang sah, request langsung ditolak dengan HTTP 401.
* **Multi-Provider Routing dengan Prefix** — Daftarkan berbagai provider upstream dengan prefix kustom (misal `dak`, `local`, `openrouter`). NineGuard menggabungkan model menjadi `dak/ag/gemini-3.8-flash`, secara otomatis memotong prefix saat meneruskan ke upstream provider yang sesuai.
* **Firewall Model Permanen** — Mencegat request sebelum masuk ke upstream. Model yang di-disable langsung ditolak seketika dengan HTTP 403 Forbidden.
* **Audit & Pelacakan Token Akurat** — Mencatat request, nama key client, nama model, prompt tokens, completion tokens, latency, dan IP client secara detail ke SQLite.
* **Streaming Transparan Tanpa Latensi Tambahan** — Response Server-Sent Events (SSE) di-*flush* secara instan ke client.
* **Single Binary Murni Tanpa Ketergantungan** — Dibangun dengan Go murni (`CGO_ENABLED=0`) dan frontend Vanilla JS yang di-embed langsung ke binary.

---

## Arsitektur: Bagaimana NineGuard Bekerja?

NineGuard bertindak sebagai gateway tunggal antara AI Coding Agents dan Upstream Providers:

```text
┌─────────────────────────────────────────────────────────────┐
│  AI Coding Agent (Cursor / Cline / Continue / Pi / Python)  │
└──────────────────────────────┬──────────────────────────────┘
                               │  Base URL: http://localhost:8080/v1
                               │  Header: Authorization: Bearer sk-ng-...
                               ▼
┌─────────────────────────────────────────────────────────────┐
│  NineGuard Gateway (:8080)                                  │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 1. Gatekeeper: Validasi NineGuard Client API Key      │  │
│  │    • Tidak Valid? ──► Tolak (HTTP 401 Unauthorized)   │  │
│  │ 2. Firewall: Cek Status Model di Database             │  │
│  │    • Disabled?    ──► Tolak (HTTP 403 Forbidden)      │  │
│  │ 3. Prefix Router: Identifikasi Provider Berdasarkan   │  │
│  │    Prefix Model (contoh: dak/... atau local/...)      │  │
│  │ 4. Telemetry: Rekam Log, Latensi, & Token Usage       │  │
│  └───────────────────────────────────────────────────────┘  │
└──────────────────────────────┬──────────────────────────────┘
                               │  Injeksi Upstream Master API Key
                               ▼
┌─────────────────────────────────────────────────────────────┐
│  Upstream OpenAI-Compatible Providers                       │
│  ├── Provider A (dak):   http://localhost:20128             │
│  ├── Provider B (local): http://localhost:11434             │
│  └── Provider C:         https://api.openai.com/v1          │
└─────────────────────────────────────────────────────────────┘
```

---

## Fitur Utama

| Fitur | Penjelasan |
|---|---|
| **Multi-Provider Routing** | Daftarkan rute upstream OpenAI-compatible dan kelompokkan model dengan prefix (e.g. `dak/model-id`). |
| **Client Key Management** | Terbitkan dan cabut API key NineGuard (`sk-ng-...`) untuk melacak konsumsi tiap agent. |
| **Model Firewall** | Aktifkan/nonaktifkan model secara instan dengan HTTP 403 Forbidden. |
| **Auto & Manual Model Fetch** | Ambil daftar model otomatis dari semua provider aktif dan tampilkan secara terpusat. |
| **Interactive Trend Line Chart** | Visualisasi throughput request, volume token, dan latensi respons dengan grafik interaktif. |
| **Usage Reports & Breakdown** | Audit "siapa saja pemakai tokennya" dengan filter periode preset (Today, 7D, 30D, Month, Last Month) dan Custom Date Range. |
| **Traffic Explorer** | Log real-time berdensitas tinggi dengan filter nama key client, status HTTP, dan live tail. |
| **Agent Setup Guides** | Panduan integrasi siap salin untuk Cursor IDE, Cline, Continue.dev, Pi Agent, Python, Node.js, dan cURL. |

---

## Instalasi & Menjalankan

### Cara 1: Menggunakan Binary Go

```bash
# 1. Masuk ke direktori NineGuard
cd 9router-extended

# 2. Build single binary
go build -o nineguard cmd/nineguard/main.go

# 3. Jalankan NineGuard
./nineguard
```

Buka browser di **`http://localhost:8080/`**. Pada kunjungan pertama, buat akun administrator Anda.

### Cara 2: Menggunakan Docker

```bash
# 1. Build image Docker
docker build -t nineguard:latest .

# 2. Jalankan container
docker run -d \
  --name nineguard \
  -p 8080:8080 \
  -v nineguard-data:/data \
  nineguard:latest
```

---

## Konfigurasi Lingkungan (Environment Variables)

File `.env` otomatis dibaca saat startup:

| Variabel Lingkungan | Nilai Bawaan | Keterangan |
|---|---|---|
| `NINEGUARD_PORT` | `8080` | Port listen server NineGuard |
| `NINEGUARD_ROUTER_TARGET` | `http://localhost:20128` | Target route default untuk provider awal |
| `NINEGUARD_ROUTER_API_KEY` | `""` | Master API key upstream awal (bisa diatur langsung via UI) |
| `NINEGUARD_AUTH_ENABLED` | `true` | Proteksi login dashboard (`true`/`false`) |
| `NINEGUARD_DB_FILE` | `./data/nineguard.db` | Lokasi file database SQLite lokal NineGuard |

---

## Panduan Pengaturan Cepat

### 1. Daftarkan Upstream Provider
Masuk ke menu **Gateway $\rightarrow$ Providers**:
1. Klik **Add Provider**.
2. Masukkan nama provider (contoh: `DAK Upstream`), prefix (contoh: `dak`), URL target route (contoh: `http://localhost:20128`), dan Upstream API Key.
3. Klik tombol **Test Probe** untuk memverifikasi koneksi.

### 2. Generate NineGuard API Key untuk Agent
Masuk ke menu **Gateway $\rightarrow$ Endpoints & Keys**:
1. Klik **+ Generate NineGuard API Key**.
2. Beri nama (misal: `Cursor IDE` atau `Cline Mac`).
3. Salin token yang dihasilkan (`sk-ng-...`).

### 3. Konfigurasikan pada AI Agent / IDE
Atur pengaturan OpenAI-compatible pada agent Anda:
* **Base URL:** `http://localhost:8080/v1`
* **API Key:** `sk-ng-...` (key NineGuard yang baru dibuat)
* **Model ID:** Pilih model dengan prefix (misal: `dak/ag/gemini-3.8-flash` atau model default).

---

## REST API Endpoint

### OpenAI-Compatible Proxy (Memerlukan NineGuard API Key)
* `GET /v1/models` — Daftar agregat seluruh model dari semua provider aktif.
* `POST /v1/chat/completions` — Chat completions (mendukung SSE streaming & tracking token).
* `POST /v1/*` — Proxy transparan untuk endpoint OpenAI standar lainnya.

### NineGuard Management API (Dashboard)
* `GET /healthz` — Health check status probe.
* `GET /api/v1/providers` — Daftar provider upstream yang terdaftar.
* `POST /api/v1/providers` — Menambahkan provider baru dengan prefix kustom.
* `PUT /api/v1/providers/{id}` — Memperbarui data provider.
* `DELETE /api/v1/providers/{id}` — Menghapus provider (otomatis menghapus model terkait di NineGuard).
* `POST /api/v1/providers/test` — Menguji koneksi probe ke provider upstream.
* `GET /api/v1/keys` — Daftar NineGuard API key yang aktif.
* `POST /api/v1/keys` — Menerbitkan NineGuard API key baru.
* `POST /api/v1/keys/{id}/toggle` — Mengaktifkan / menonaktifkan key.
* `DELETE /api/v1/keys/{id}` — Mencabut / menghapus key.
* `GET /api/v1/models` — Daftar model terdaftar dan status firewall.
* `POST /api/v1/models/toggle` — Mengubah status aktif/blokir model.
* `POST /api/v1/models/sync` — Mengambil model terbaru dari semua provider aktif.
* `DELETE /api/v1/models/{id...}` — Menghapus model tertentu dari database NineGuard.
* `GET /api/v1/traffic` — Log traffic riwayat per request.
* `GET /api/v1/traffic/stats` — Metrik statistik ringkas & volume series untuk Dashboard.
* `GET /api/v1/traffic/report` — Laporan breakdown penggunaan token per key & model dengan custom date range.

---

## Dokumentasi Terkait
* [Arsitektur & Alur Request](docs/ARCHITECTURE.md)
* [Panduan Multi-Provider & Prefix Routing](docs/PROVIDERS.md)
* [Panduan Menghubungkan Agent & IDE](docs/AGENTS_SETUP.md)

---

## Author & Kontributor

* **Author:** Ari Ardiansyah
* **GitHub:** [@aribrilliantsyah](https://github.com/aribrilliantsyah)
* **Email:** [ariadiansyah.study@gmail.com](mailto:ariadiansyah.study@gmail.com)

---

## Lisensi

Didistribusikan di bawah lisensi [MIT](LICENSE).
