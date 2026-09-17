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
* **Multi-Provider Routing dengan Prefix** — Daftarkan berbagai provider upstream dengan prefix kustom (misal `openrouter`, `local`, `ollama`). NineGuard menggabungkan model menjadi `openrouter/claude-3.5-sonnet`, secara otomatis memotong prefix saat meneruskan ke upstream provider yang sesuai.
* **Per-Key Model Access Control** — Tentukan model apa saja yang diizinkan untuk masing-masing API key (whitelist kustom atau wildcard `*`). Request model di luar daftar izin ditolak seketika (HTTP 403).
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
│  │ 2. Firewall & Access Control: Cek Izin Model Per-Key  │  │
│  │    • Model Tidak Diizinkan? ──► Tolak (403 Forbidden) │  │
│  │ 3. Prefix Router: Identifikasi Provider Berdasarkan   │  │
│  │    Prefix Model (contoh: openrouter/... atau local/..)│  │
│  │ 4. Telemetry: Rekam Log, Latensi, & Token Usage       │  │
│  └───────────────────────────────────────────────────────┘  │
└──────────────────────────────┬──────────────────────────────┘
                               │  Injeksi Upstream Master API Key
                               ▼
┌─────────────────────────────────────────────────────────────┐
│  Upstream OpenAI-Compatible Providers                       │
│  ├── Provider A (openrouter): https://openrouter.ai/api/v1  │
│  ├── Provider B (local):      http://localhost:11434        │
│  └── Provider C:              https://api.openai.com/v1     │
└─────────────────────────────────────────────────────────────┘
```

---

## Fitur Utama

| Fitur | Penjelasan |
|---|---|
| **Multi-Provider Routing** | Daftarkan rute upstream OpenAI-compatible dan kelompokkan model dengan prefix (e.g. `openrouter/model-id`). |
| **Client Key Management** | Terbitkan dan cabut API key NineGuard (`sk-ng-...`) untuk melacak konsumsi tiap agent. |
| **Per-Key Model Access Control** | Atur hak akses model per API key (akses global `*`, Model Groups dinamis, atau custom whitelist). |
| **Model Groups** | Kelompokkan model ke grup reusable (e.g. GPT Ecosystem, Claude) dengan sinkronisasi dinamis ke API key. |
| **Model Firewall** | Aktifkan/nonaktifkan model secara instan dengan HTTP 403 Forbidden. |
| **Auto & Manual Model Fetch** | Ambil daftar model otomatis dari semua provider aktif dan tampilkan secara terpusat. |
| **Interactive Trend Line Chart** | Visualisasi throughput request, volume token, dan latensi respons dengan grafik interaktif. |
| **Usage Reports & Breakdown** | Audit "siapa saja pemakai tokennya" dengan filter periode preset (Today, 7D, 30D, Month, Last Month) dan Custom Date Range. |
| **Traffic Explorer** | Log real-time berdensitas tinggi dengan filter nama key client, status HTTP, dan live tail. |
| **System Log & Audit Explorer** | Monitoring log gateway internal, rotasi SQLite, filter sumber komponen, dan audit keamanan RBAC. |
| **Interactive TUI & System Tray** | Antarmuka terminal interaktif dan icon tray Windows/macOS/Linux untuk kemudahan monitoring & akses cepat ke dashboard. |
| **Agent Setup Guides** | Panduan integrasi siap salin untuk Cursor IDE, Cline, Continue.dev, Pi Agent, Python, Node.js, dan cURL. |

---

## Instalasi & Menjalankan

### Cara 1: Menggunakan Binary Go (Linux & macOS)

NineGuard mendukung 2 kondisi arsitektur target:
1. **Desktop Environment / Window Manager (Linux GUI, macOS):**
   - Mendukung integrasi **System Tray** di area notifikasi desktop (KDE Plasma, GNOME, XFCE, i3, Sway, Hyprland, dll.).
   - Menu TUI menampilkan opsi: **`Hide to Tray (Background)`**.
   - Menggunakan CGO untuk berinteraksi dengan display server / StatusNotifierItem.
2. **Linux Server / Headless Environment:**
   - Single binary **Pure Go** tanpa dependensi library GUI / CGO (`CGO_ENABLED=0`, `-tags server`).
   - Tidak memerlukan system tray; berfokus pada **Background Daemon Process** yang terpisah dari sesi terminal/SSH.
   - Menu TUI otomatis beradaptasi menampilkan opsi: **`Run in Background (Daemon)`**.
   - Jika dijalankan dengan flag `-t / --tray` di server, NineGuard otomatis *fallback* aman ke mode background daemon tanpa crash.

```bash
# 1. Masuk ke direktori NineGuard
cd nineguard

# 2. Build sesuai kebutuhan:
# Opsi A: Deteksi otomatis (jika ada Desktop Environment/WM -> Desktop, jika server -> Server)
make build

# Opsi B: Build khusus Desktop (dengan System Tray)
make build-desktop

# Opsi C: Build khusus Linux Server (Headless Daemon, pure Go tanpa CGO/GTK)
make build-server

# Opsi D: Build semua target sekaligus (nineguard, nineguard-server, nineguard.exe)
make build-all

# 3. Jalankan NineGuard
./nineguard
```

#### Mode Eksekusi CLI & Background (Linux / Server)

| Perintah | Mode | Keterangan |
|---|---|---|
| `./nineguard` | **Interactive TUI** | Menampilkan antarmuka interaktif: browser shortcut, live log, dan background process. Menu otomatis menyesuaikan: `Hide to Tray (Background)` di Desktop GUI atau `Run in Background (Daemon)` di Server. |
| `./nineguard -d` / `--daemon` | **Headless Daemon** | Berjalan di latar belakang tanpa TUI (cocok untuk systemd, Docker, atau server background). |
| `./nineguard -t` / `--tray` | **System Tray / Daemon** | Berjalan di system tray jika ada GUI session; otomatis *fallback* ke daemon mode yang stabil jika di server. |
| `./nineguard -l` / `--logs` | **Live Stream Logs** | Menjalankan server dan langsung menampilkan live log request HTTP di terminal. |
| `./nineguard -p 9090` | **Custom Port** | Menjalankan server pada port tertentu (default: `8080`). |

Buka browser di **`http://localhost:8080/`**. Pada kunjungan pertama, buat akun administrator Anda.

### Cara 2: Build & Menjalankan di Windows

NineGuard dirancang sebagai single-binary mandiri dengan SQLite murni (`modernc.org/sqlite`). Anda **tidak memerlukan compiler C (CGO / MinGW / GCC)** untuk mengompilasinya di Windows.

#### 1. Build Langsung di Windows (PowerShell / Command Prompt)

Pastikan Go 1.22+ sudah terpasang di sistem Windows Anda:

```powershell
# 1. Masuk ke direktori NineGuard
cd nineguard

# 2. Build binary executable
go build -o nineguard.exe cmd/nineguard/main.go

# 3. Jalankan NineGuard (Interactive TUI Menu)
.\nineguard.exe
```

> **Tip (Background / System Tray Mode Tanpa Jendela Terminal):**
> Jika Anda ingin menjalankan NineGuard di latar belakang dan langsung masuk ke System Tray tanpa memunculkan jendela console (Command Prompt) hitam, gunakan flag linker `-H=windowsgui`:
> ```powershell
> go build -ldflags="-H=windowsgui" -o nineguard.exe cmd/nineguard/main.go
> .\nineguard.exe -t
> ```

#### 2. Cross-Compile untuk Windows dari Linux / macOS

Anda dapat mengompilasi binary `.exe` untuk Windows langsung dari Linux atau macOS:

```bash
# Console / Interactive TUI mode
GOOS=windows GOARCH=amd64 go build -o nineguard.exe cmd/nineguard/main.go

# GUI / System Tray mode (tanpa popup jendela console)
GOOS=windows GOARCH=amd64 go build -ldflags="-H=windowsgui" -o nineguard.exe cmd/nineguard/main.go
```

#### 3. Mode Eksekusi CLI di Windows

| Perintah | Mode | Keterangan |
|---|---|---|
| `.\nineguard.exe` | **Interactive TUI** | Menampilkan antarmuka terminal interaktif dengan ringkasan status, pintasan browser, dan live log. |
| `.\nineguard.exe -t` / `--tray` | **System Tray** | Berjalan di tray taskbar Windows. Klik ikon NineGuard untuk membuka dashboard atau keluar. |
| `.\nineguard.exe -l` / `--logs` | **Live Logs** | Menjalankan server dan langsung streaming log HTTP/traffic di terminal. |
| `.\nineguard.exe -d` / `--daemon` | **Headless / Service** | Mode daemon tanpa UI terminal (cocok untuk Task Scheduler atau Windows Service). |
| `.\nineguard.exe -p 9090` | **Custom Port** | Mengubah port listen server HTTP (default: `8080`). |

### Cara 3: Menggunakan Docker

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

File `.env` otomatis dibaca saat startup (hanya port, auth, dan path database yang dibutuhkan di env):

| Variabel Lingkungan | Nilai Bawaan | Keterangan |
|---|---|---|
| `NINEGUARD_PORT` | `8080` | Port listen server NineGuard |
| `NINEGUARD_AUTH_ENABLED` | `true` | Proteksi login dashboard (`true`/`false`) |
| `NINEGUARD_DB_FILE` | `./data/nineguard.db` | Lokasi file database SQLite lokal NineGuard |
| `NINEGUARD_AUTH_FILE` | `./data/auth.json` | Lokasi file fallback autentikasi (opsional) |

*Catatan: Upstream provider dikelola langsung secara dinamis melalui UI Dashboard (menu Providers) dan tersimpan di database lokal, sehingga tidak perlu mengatur target upstream di `.env`.*

---

## Panduan Pengaturan Cepat

> **Penting Mengenai Navigasi Dashboard & Gateway:**
> Tombol untuk membuat API key, mendaftarkan provider, maupun mengelola model **tidak berada di halaman Dashboard (`#/dashboard`)**. Halaman **Dashboard** difungsikan murni untuk observabilitas dan monitoring (metrik KPI, grafik volume request, breakdown token, dan inspeksi error). Seluruh konfigurasi sistem berada di menu kelompok **Gateway** pada sidebar:
> * Registrasi Provider: **Gateway $\rightarrow$ Providers**
> * Generate & Kelola API Key: **Gateway $\rightarrow$ Endpoints & Keys**
> * Firewall & Sinkronisasi Model: **Gateway $\rightarrow$ Models**
> * Telemetri & Laporan: **Gateway $\rightarrow$ Traffic Explorer**, **Log Explorer**, & **Usage Reports**

---

### 1. Daftarkan Upstream Provider
Masuk ke menu **Gateway $\rightarrow$ Providers**:
1. Klik tombol **`+ Add Provider`** (terletak di kanan atas halaman atau pada kartu kosong jika belum ada provider).
2. Isi formulir modal dialog:
   * **Provider Name:** Nama penyedia (contoh: `OpenRouter Cloud` atau `Local Ollama`).
   * **Model Routing Prefix:** Prefix rute model (contoh: `openrouter` atau `local`). Kosongkan jika ingin dijadikan root tanpa prefix.
   * **Route / Target URL:** Endpoint URL OpenAI-compatible (contoh: `https://openrouter.ai/api/v1` atau `http://localhost:11434`).
   * **Upstream API Key:** Master API key upstream (kosongkan jika provider lokal tidak membutuhkan key).
   * **Set as Default Provider:** Centang opsi ini jika ingin menjadikannya rute fallback untuk request model tanpa prefix.
3. Klik tombol **`Add Provider`** di pojok kanan bawah modal untuk menyimpan.
4. Setelah provider berhasil ditambahkan dan kartu provider muncul di daftar, klik tombol **`Test Probe`** pada kartu tersebut untuk memverifikasi koneksi. Jika berhasil, status akan menampilkan tanda centang hijau *Connected (HTTP 200 OK)* beserta latensi dan jumlah model.

---

### 2. Generate NineGuard API Key untuk Agent
Masuk ke menu **Gateway $\rightarrow$ Endpoints & Keys** *(bukan di halaman Dashboard)*:
1. Pada kartu **NineGuard Client API Keys**, klik tombol **`+ Generate New Key`** di kanan atas kartu.
2. Pada modal **Generate New NineGuard API Key**:
   * **Key Name / Description:** Beri nama identifikasi agent/developer (misal: `Cursor IDE`, `Cline Mac`, atau `Pi Agent`). Nama ini akan dicatat di log traffic dan laporan penggunaan token.
   * **Allowed Models (Access Control):** Tentukan hak akses model dengan memilih salah satu dari 3 mode:
     * **All Models (Global):** Memberikan akses ke semua model yang aktif di firewall global. Model baru yang ditambahkan di masa depan otomatis langsung bisa diakses.
     * **Model Groups:** Menautkan API key ke satu atau beberapa grup model (misal: grup *OpenAI Models* atau *Claude*). Bersifat *dynamic sync* sehingga pembaruan isi grup langsung berlaku ke key ini.
     * **Custom Selection:** Memilih model secara spesifik dari checklist yang tersedia, atau mengetik wildcard/ID model kustom (contoh: `openrouter/*`).
3. Klik tombol **`Generate Key`** di pojok kanan bawah modal.
4. Pop-up dialog **NineGuard API Key Generated** akan muncul menampilkan key baru (`sk-ng-...`).
5. Klik tombol **`Copy Key`** untuk menyalin token ke clipboard.
   > **Catatan Keamanan:** Token lengkap hanya ditampilkan sekali saat dibuat. Simpan key ini di tempat aman karena NineGuard hanya menyimpan hash key di database.

---

### 3. Sinkronisasi Model & Pengujian (Opsional tapi Direkomendasikan)
* **Ambil Daftar Model Terbaru:**
  Masuk ke menu **Gateway $\rightarrow$ Models**, lalu klik tombol **`Fetch from Providers`** di kanan atas untuk menyinkronkan seluruh model dari upstream provider yang aktif. Anda juga bisa mengelompokkan model di tab **Model Groups** dengan tombol **`+ Create Model Group`**.
* **Live Endpoint Tester di Browser:**
  Masuk ke menu **Gateway $\rightarrow$ Endpoints & Keys**, lalu gulir ke bagian **Live Endpoint Tester** di bawah. Pilih model target, pilih API key NineGuard yang baru dibuat, ketik prompt uji coba, dan klik **Send Test Request** untuk memastikan respons mengalir lancar sebelum diterapkan ke IDE / AI Agent.

---

### 4. Konfigurasikan pada AI Agent / IDE
Atur pengaturan OpenAI-compatible pada agent atau IDE Anda:
* **Base URL:** `http://localhost:8080/v1`
* **API Key:** `sk-ng-...` (NineGuard Client API Key yang baru disalin pada Langkah 2)
* **Model ID:** Pilih model yang telah diberi prefix provider (misal: `openrouter/anthropic/claude-3.5-sonnet` atau model lokal `local/llama3:8b`).

---

## REST API Endpoint

### OpenAI-Compatible Proxy (Memerlukan NineGuard API Key)
* `GET /v1/models` — Daftar agregat seluruh model dari semua provider aktif.
* `POST /v1/chat/completions` — Chat completions (mendukung SSE streaming & tracking token).
* `POST /v1/*` — Proxy transparan untuk endpoint OpenAI standar lainnya.

### NineGuard Management API (Dashboard & Admin)
* `GET /healthz` — Health check status probe.
* `GET /api/v1/providers` — Daftar provider upstream yang terdaftar.
* `POST /api/v1/providers` — Menambahkan provider baru dengan prefix kustom.
* `PUT /api/v1/providers/{id}` — Memperbarui data provider.
* `POST /api/v1/providers/{id}/toggle` — Mengaktifkan / menonaktifkan provider.
* `POST /api/v1/providers/{id}/default` — Menjadikan provider sebagai fallback default.
* `DELETE /api/v1/providers/{id}` — Menghapus provider (otomatis menghapus model terkait di NineGuard).
* `POST /api/v1/providers/test` — Menguji koneksi probe ke provider upstream.
* `GET /api/v1/keys` — Daftar NineGuard API key yang aktif beserta allowed models.
* `POST /api/v1/keys` — Menerbitkan NineGuard API key baru dengan konfigurasi allowed models.
* `PUT /api/v1/keys/{id}` — Mengubah nama dan daftar model yang diizinkan untuk key tertentu.
* `POST /api/v1/keys/{id}/toggle` — Mengaktifkan / menonaktifkan key.
* `DELETE /api/v1/keys/{id}` — Mencabut / menghapus key.
* `GET /api/v1/models` — Daftar model terdaftar dan status firewall.
* `POST /api/v1/models/toggle` — Mengubah status aktif/blokir model.
* `POST /api/v1/models/sync` — Mengambil model terbaru dari semua provider aktif.
* `DELETE /api/v1/models/{id...}` — Menghapus model tertentu dari database NineGuard.
* `GET /api/v1/model-groups` — Daftar model groups yang tersedia.
* `POST /api/v1/model-groups` — Membuat model group baru.
* `GET /api/v1/model-groups/{id}` — Detail anggota model dalam suatu group.
* `PUT /api/v1/model-groups/{id}` — Memperbarui nama, deskripsi, dan anggota model group.
* `DELETE /api/v1/model-groups/{id}` — Menghapus model group.
* `GET /api/v1/traffic` — Log traffic riwayat per request (mendukung filter key, model, IP, status).
* `GET /api/v1/traffic/volume` — Data time-series histogram volume request & token.
* `GET /api/v1/traffic/stats` — Metrik statistik ringkas & volume series untuk Dashboard.
* `GET /api/v1/traffic/report` — Laporan breakdown penggunaan token per key & model dengan custom date range.
* `GET /api/v1/traffic/export` — Ekspor log traffic ke CSV/JSON.
* `GET /api/v1/logs` — Log sistem internal & security audit log.
* `GET /api/v1/logs/volume` — Volume histogram log sistem per severity.
* `GET /api/v1/logs/sources` — Daftar sumber log sistem yang aktif.
* `GET /api/v1/logs/export` — Ekspor log sistem ke format JSON/NDJSON.
* `GET /api/v1/auth/status` — Status autentikasi dan ketersediaan setup awal.
* `POST /api/v1/auth/setup` — Inisialisasi akun administrator pertama.
* `POST /api/v1/auth/login` — Autentikasi sesi dashboard.
* `POST /api/v1/auth/logout` — Mengakhiri sesi dashboard.
* `GET /api/v1/profile` — Profil pengguna saat ini.
* `PATCH /api/v1/profile` — Memperbarui profil pengguna.
* `POST /api/v1/profile/password` — Mengubah password akun saat ini.
* `POST /api/v1/profile/recovery` — Mengatur pertanyaan keamanan password recovery.
* `GET /api/v1/users` — Daftar akun pengguna dashboard (khusus admin).
* `POST /api/v1/users` — Membuat akun pengguna operator/admin baru.
* `DELETE /api/v1/users/{id}` — Menghapus akun pengguna dashboard.

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
