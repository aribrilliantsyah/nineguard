# Arsitektur NineGuard

Dokumen ini menjelaskan rancangan sistem, alur transmisi paket, dan komponen inti dari **NineGuard**.

---

## 1. Konsep Desain

NineGuard dirancang berdasarkan prinsip:
* **Single Gateway for All Models:** AI Coding Agent hanya perlu mengarah ke satu endpoint (`http://localhost:8080/v1`).
* **Strict Client Authentication:** Hanya agen atau developer yang memiliki API key resmi dari NineGuard (`sk-ng-...`) yang diizinkan mengakses model.
* **Prefix-Based Upstream Routing:** Model dari berbagai penyedia dikelompokkan dengan prefix (misalnya `dak/model-id`, `local/model-id`).
* **Zero Overhead Streaming:** Response Server-Sent Events (SSE) dialirkan tanpa buffering berlebih.

---

## 2. Alur Request Masuk (Request Lifecycle)

```text
[1. Client Agent]
       │
       │ HTTP POST /v1/chat/completions
       │ Header: Authorization: Bearer sk-ng-...
       ▼
[2. NineGuard Security Interceptor]
       │
       ├── Cek Validitas NineGuard API Key
       │     └── Jika Invalid / Disabled ──► 401 Unauthorized
       │
       ├── Cek Status Model Firewall
       │     └── Jika Disabled ─────────────► 403 Forbidden
       │
       ├── Prefix Stripping & Provider Matching
       │     └── "dak/ag/gemini-3.8-flash"
       │           ├── Prefix "dak" ──► Provider DAK Upstream
       │           └── Model "ag/gemini-3.8-flash"
       │
       ├── Rewrite Request Header:
       │     └── Authorization: Bearer <Master_API_Key_Provider>
       ▼
[3. Upstream Provider (OpenAI-Compatible)]
       │
       │ Eksekusi Inferensi LLM
       ▼
[4. Response Streaming & Trace Ingestion]
       ├── Stream chunk SSE langsung di-flush ke client (zero delay)
       └── Asynchronous Logging: simpan token, durasi, status ke SQLite
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

---

## 4. Keamanan & Isolasi

* Master API Key dari upstream provider disimpan secara aman di SQLite lokal (`data/nineguard.db`) dan tidak pernah dikirim ke client agent.
* Client agent hanya berinteraksi menggunakan key `sk-ng-...`. Jika sebuah laptop atau environment terkompromi, key client tersebut dapat dicabut seketika tanpa perlu mengganti master key di provider upstream.
