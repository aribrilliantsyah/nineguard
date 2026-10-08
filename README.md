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
| **Token Quotas & Rate Limiting** | Batasi anggaran token per API key (harian, mingguan, bulanan, lifetime) dengan soft post-facto gating, respons standar HTTP 429, header `Retry-After`, dan notifikasi waktu reset (WIB & UTC). |
| **Token Burn Velocity & Presets** | Pantau laju konsumsi historis (24 jam, rata-rata 7 hari, 30 hari) per key untuk membantu penentuan batas kuota dengan tombol preset instan (1.5x / 2x daily avg). |
| **Plugin Pipeline & Token Savers** | Hemat token dan modifikasi request otomatis via built-in plugins (Headroom, Ponytail, Caveman) atau layanan HTTP eksternal dengan urutan pipeline global dan hierarki override (Global $\rightarrow$ Model Group $\rightarrow$ API Key). |
| **Secret Leak Guardrail (SecretGuard)** | Cegah kebocoran API token (`sk-`, `ghp-`, `AKIA`), private key, dan environment credential ke LLM upstream dengan inspeksi regex (pilihan mode Block HTTP 403, Redact in-flight, atau Audit mode). |
| **Traffic Spike Alerts** | Tandai request raksasa yang melewati ambang batas (`heavy_token_threshold`) dengan badge peringatan ⚠️ di Traffic Explorer dan log audit tanpa memutus request. |
| **Multimodal Image Logging** | Deteksi dan lacak request yang menyertakan input gambar/vision dalam riwayat percakapan beserta badge `🖼️ N img` dan filter eksplorasi. |
| **Payload Inspector & Request Replay** | Inspeksi payload body request dan response (limit 512KB, auto-purge 7 hari), salin perintah cURL 1-klik, dan replay request langsung dari dashboard. |
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

#### Opsi A: Menggunakan Image Resmi (GHCR)
```bash
# Tarik image terbaru atau versi spesifik
docker pull ghcr.io/darktama/nineguard:latest

# Jalankan container
docker run -d \
  --name nineguard \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v nineguard-data:/data \
  ghcr.io/darktama/nineguard:latest

# Cek versi yang terpasang
docker exec nineguard /app/nineguard --version
```

#### Opsi B: Build Sendiri dengan Versioning
```bash
# 1. Build image Docker dengan git tag & commit otomatis
make build-docker
# atau via script: ./scripts/build.sh docker

# 2. Jalankan container (bind ke localhost saja)
docker run -d \
  --name nineguard \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v nineguard-data:/data \
  nineguard:latest
```

#### Docker Compose (NineGuard + 9router)

```bash
# 1. Siapkan secret 9router
cp .env.example .env
# isi NINEROUTER_* di .env (generate: openssl rand -hex 32)

# 2. Jalankan stack
docker compose up -d --build

# 3. Akses dari mesin lokal via SSH tunnel
ssh -L 8080:localhost:8080 -L 20128:localhost:20128 user@VM_IP
```

* NineGuard: `http://localhost:8080/` (buat akun admin pada kunjungan pertama).
* 9router: `http://localhost:20128/dashboard` — buat API key di sini.
* Daftarkan 9router di NineGuard (**Gateway → Providers**) dengan URL `http://9router:20128/v1` dan API key 9router.
* Kedua port hanya bind ke `127.0.0.1`. Untuk akses publik, gunakan reverse proxy dengan TLS (Caddy/nginx).

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

## Model Groups (Template Akses Model)

**Model Group** adalah template daftar model yang diizinkan. Alih-alih mengatur whitelist model satu per satu di setiap API key, buat satu grup (misal *Claude*, *Model Murah*, *Tim Frontend*) lalu tautkan ke banyak key sekaligus. Setiap agent/pengguna yang memakai key tersebut hanya bisa mengakses model di dalam grupnya.

### Mode Akses API Key

Setiap API key memiliki tepat **satu** mode akses (`model_access_mode`):

| Mode | Sumber Daftar Model | Kegunaan |
|---|---|---|
| `all` | Semua model aktif di firewall global | Admin / key internal tepercaya |
| `group` | Gabungan (*union*) model dari grup yang ditautkan | Template akses yang dipakai bersama banyak key |
| `custom` | Whitelist manual pada key itu sendiri | Pengecualian khusus satu key |

Mode `group` dan `custom` tidak bisa dicampur dalam satu key.

### Contoh Alur via Dashboard

1. Masuk ke **Gateway $\rightarrow$ Models**, klik **`Fetch from Providers`** agar daftar model tersedia.
2. Buka tab **Model Groups**, klik **`+ Create Model Group`**.
3. Isi nama (misal `Claude`), deskripsi, lalu pilih model dari daftar atau ketik ID/pola model (misal `9router/*`). Lihat **Aturan Pola Model** di bawah.
4. Masuk ke **Gateway $\rightarrow$ Endpoints & Keys**, klik **`+ Generate New Key`** (atau edit key yang sudah ada).
5. Pada **Allowed Models**, pilih mode **Model Groups** dan centang satu atau beberapa grup.
6. Simpan. Key langsung terbatas pada model di grup tersebut.

### Perilaku Penting

* **Multi-grup = gabungan.** Key yang ditautkan ke grup `Claude` dan `GPT` mendapat semua model dari kedua grup (duplikat otomatis dihapus).
* **Sinkronisasi langsung.** Mengubah isi grup langsung berlaku ke semua key yang tertaut — tanpa generate ulang key dan tanpa restart server.
* **Grup kosong = tolak semua.** Key mode `group` yang grupnya tidak berisi model akan ditolak (HTTP 403) untuk setiap model. (Berbeda dengan mode `custom` kosong, yang dianggap mengizinkan semua.)
* **Grup terlindungi dari penghapusan.** Grup yang masih ditautkan ke API key tidak bisa dihapus; pesan error menyebutkan key yang memakainya. Lepaskan tautan dari key terlebih dahulu.
* **`GET /v1/models` ikut terfilter.** Agent hanya melihat model yang diizinkan untuk key-nya, sehingga dropdown model di IDE otomatis sesuai grup.
* **Firewall global tetap berlaku.** Model yang ada di grup tetapi di-disable di **Gateway $\rightarrow$ Models** tetap ditolak (`model_disabled`).

### Aturan Pola Model

Anggota grup bisa berupa ID model persis atau pola:

| Pola | Cocok Dengan | Contoh |
|---|---|---|
| `*` atau `all` | Semua model | — |
| `prefix/*` | Semua model dari satu provider/prefix | `openrouter/*` |
| `*.suffix` | Model dengan akhiran tertentu | `*.flash` |
| ID persis | Model itu saja (tidak case-sensitive) | `openrouter/anthropic/claude-3.5-sonnet` |

Pola di tengah nama seperti `9router/claude-*` **tidak didukung**. Gunakan `prefix/*` atau daftar ID model lengkap.

Prefix provider bersifat fleksibel: anggota `gpt-4o` juga cocok dengan request `openrouter/gpt-4o`, dan sebaliknya anggota `openrouter/gpt-4o` cocok dengan request `gpt-4o`.

### Respons Saat Ditolak

Request ke model di luar grup mendapat HTTP 403 dengan format kompatibel OpenAI:

```json
{
  "error": {
    "message": "Model 'openrouter/gpt-4o' is not allowed for API key 'Cursor IDE'.",
    "type": "permission_error",
    "param": "model",
    "code": "model_not_allowed"
  }
}
```

Penolakan ini juga tercatat di **Traffic Explorer** dengan status 403.

### Contoh via REST API

Management API memakai sesi cookie dashboard. Login terlebih dahulu dan simpan cookie:

```bash
# 1. Login
curl -c cookies.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"PASSWORD_ANDA"}'

# 2. Buat model group
curl -b cookies.txt -X POST http://localhost:8080/api/v1/model-groups \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Claude",
    "description": "Akses model Claude via 9router",
    "models": ["9router/*", "openrouter/anthropic/claude-3.5-sonnet"]
  }'
# Respons berisi "id" grup — gunakan pada langkah berikutnya.

# 3. Buat API key yang ditautkan ke grup
curl -b cookies.txt -X POST http://localhost:8080/api/v1/keys \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Cursor IDE",
    "model_access_mode": "group",
    "model_group_ids": ["ID_GRUP"]
  }'
# Respons berisi key sk-ng-... (hanya ditampilkan sekali).

# 4. Perbarui isi grup (langsung berlaku ke semua key tertaut)
curl -b cookies.txt -X PUT http://localhost:8080/api/v1/model-groups/ID_GRUP \
  -H "Content-Type: application/json" \
  -d '{"name":"Claude","description":"","models":["9router/*"]}'
```

> **Catatan:** `PUT /api/v1/model-groups/{id}` mengganti seluruh daftar model (bukan menambahkan). Kirim daftar lengkap setiap kali memperbarui.

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
* `GET /api/v1/keys` — Daftar NineGuard API key yang aktif beserta allowed models dan kuota.
* `POST /api/v1/keys` — Menerbitkan NineGuard API key baru dengan konfigurasi allowed models dan batas kuota.
* `PUT /api/v1/keys/{id}` — Mengubah nama, daftar model yang diizinkan, dan kuota untuk key tertentu.
* `POST /api/v1/keys/{id}/toggle` — Mengaktifkan / menonaktifkan key.
* `DELETE /api/v1/keys/{id}` — Mencabut / menghapus key.
* `GET /api/v1/keys/{id}/velocity` — Mengambil metrik laju konsumsi token historis per key (24 jam, rata-rata 7 hari, 30 hari).
* `GET /api/v1/models` — Daftar model terdaftar dan status firewall.
* `POST /api/v1/models/toggle` — Mengubah status aktif/blokir model.
* `POST /api/v1/models/sync` — Mengambil model terbaru dari semua provider aktif.
* `DELETE /api/v1/models/{id...}` — Menghapus model tertentu dari database NineGuard.
* `GET /api/v1/model-groups` — Daftar model groups yang tersedia.
* `POST /api/v1/model-groups` — Membuat model group baru.
* `GET /api/v1/model-groups/{id}` — Detail anggota model dalam suatu group.
* `PUT /api/v1/model-groups/{id}` — Memperbarui nama, deskripsi, dan anggota model group.
* `DELETE /api/v1/model-groups/{id}` — Menghapus model group.
* `GET /api/v1/traffic` — Log traffic riwayat per request (mendukung filter key, model, IP, status, has_images).
* `GET /api/v1/traffic/volume` — Data time-series histogram volume request & token.
* `GET /api/v1/traffic/stats` — Metrik statistik ringkas & volume series untuk Dashboard.
* `GET /api/v1/traffic/report` — Laporan breakdown penggunaan token per key & model dengan custom date range.
* `GET /api/v1/traffic/export` — Ekspor log traffic ke CSV/JSON.
* `GET /api/v1/traffic/{id}/payload` — Mengambil body payload request & response untuk inspeksi dan replay cURL.
* `GET /api/v1/settings/traffic` — Mengambil pengaturan threshold lonjakan token dan status perekaman payload.
* `POST /api/v1/settings/traffic` — Memperbarui threshold lonjakan token (`heavy_token_threshold`) dan mode perekaman payload.
* `GET /api/v1/plugins` — Daftar seluruh plugin terdaftar (built-in & HTTP) beserta urutan pipeline.
* `POST /api/v1/plugins` — Mendaftarkan plugin HTTP baru.
* `PUT /api/v1/plugins/{id}` — Memperbarui konfigurasi default plugin.
* `DELETE /api/v1/plugins/{id}` — Menghapus plugin HTTP.
* `POST /api/v1/plugins/{id}/test` — Menguji koneksi probe ke endpoint plugin HTTP.
* `PUT /api/v1/plugins/order` — Mengatur ulang urutan eksekusi pipeline plugin.
* `GET /api/v1/plugins/bindings` — Mengambil seluruh binding plugin pada scope tertentu (global, group, key).
* `PUT /api/v1/plugins/{id}/bindings` — Menyimpan atau menghapus override binding plugin (on, off, inherit).
* `GET /api/v1/plugins/resolve` — Pratinjau plugin efektif dan hierarki penentu untuk kombinasi key dan model.
* `GET /api/v1/plugins/warnings` — Mengambil peringatan tumpang-tindih plugin token saving.
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
* [Glossary & Istilah Domain](GLOSSARY.md)
* [Architecture Decision Records (ADR)](docs/adr/)

---

## Reporting Issues & Feedback

Menemukan kendala atau punya ide untuk menyempurnakan NineGuard? Kami menyambut laporan bug, ide fitur, serta masukan!

* **[Report a Bug](https://github.com/DarkTama/nineguard/issues/new?template=bug_report.yml)**: Gunakan form ini jika menemukan bug, error, atau anomali gateway.
* **[Request a Feature](https://github.com/DarkTama/nineguard/issues/new?template=feature_request.yml)**: Usulkan ide baru, optimasi routing, atau peningkatan UI/UX.
* **[Submit Feedback](https://github.com/DarkTama/nineguard/issues/new?template=feedback.yml)**: Berikan saran, kesan, atau kritik seputar pengalaman penggunaan NineGuard.
* **[GitHub Discussions](https://github.com/DarkTama/nineguard/discussions)**: Diskusi umum, tanya-jawab konfigurasi, dan interaksi komunitas.

> **Privacy Notice**: Saat melaporkan masalah atau melampirkan log server / screenshot, **jangan pernah mencantumkan master API key provider, NineGuard API key, password, atau credential sensitif**.

---

## Author & Kontributor

### Author
* **Ari Ardiansyah**
  * GitHub: [@aribrilliantsyah](https://github.com/aribrilliantsyah)
  * Email: [ariadiansyah.study@gmail.com](mailto:ariadiansyah.study@gmail.com)

### Kontributor
* **Ekatama Ilham Prayoga**
  * GitHub: [@DarkTama](https://github.com/DarkTama)
  * Email: [ekatamailhamprayoga@gmail.com](mailto:ekatamailhamprayoga@gmail.com)

<br/>

<a href="https://github.com/DarkTama/nineguard/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=DarkTama/nineguard" alt="Contributors" />
</a>

---

## Lisensi

Didistribusikan di bawah lisensi [MIT](LICENSE).
