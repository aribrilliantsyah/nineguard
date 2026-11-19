# Panduan Manajemen Upstream Providers

NineGuard mendukung **Custom URL** untuk server kompatibel OpenAI REST API v1 maupun Anthropic Messages API (misalnya `https://api.anthropic.com`), serta akun **Claude** dan **OpenAI/Codex** melalui OAuth engine CLIProxyAPI tertanam. Tidak perlu menjalankan service CLIProxyAPI terpisah. Provider Custom URL diteruskan apa adanya tanpa translasi protokol: client `/v1/messages` hanya bisa memakai upstream yang melayani Anthropic Messages.

---

## 1. Menambahkan Upstream Provider Baru

Buka antarmuka NineGuard pada browser di menu **Gateway $\rightarrow$ Providers**:

1. Klik tombol **+ Add Provider**.
2. Isi formulir konfigurasi:
   * **Provider Name:** Nama penyedia (contoh: `OpenRouter Cloud`, `Local Ollama`, `vLLM Server`).
   * **Model Routing Prefix:** Prefix pembeda (contoh: `openrouter`, `local`, `ollama`).
     * *Catatan:* Biarkan kosong jika provider ini dijadikan root tanpa prefix.
   * **Route / Target URL:** Endpoint dasar HTTP provider:
     * 9router: `http://localhost:20128`
     * Ollama: `http://localhost:11434`
     * vLLM / Local AI: `http://localhost:8000`
     * OpenRouter: `https://openrouter.ai/api/v1`
     * Anthropic API: `https://api.anthropic.com`
   * **Upstream API Key:** Master API key yang dibutuhkan provider tersebut. NineGuard mengirimnya sebagai `Authorization: Bearer` sekaligus `x-api-key`, untuk request proxy, **Check connection**, dan sinkronisasi model; ke `api.anthropic.com` juga ditambahkan `anthropic-version: 2023-06-01`.
   * **Set as Default Provider:** Centang opsi ini jika provider ini ingin dijadikan tujuan cadangan (*fallback*) untuk request tanpa prefix.
3. Klik **Add Provider** (atau **Save Changes** jika mengedit provider yang sudah ada).
4. Setelah provider tersimpan, klik **Check connection** untuk memvalidasi endpoint custom. Untuk provider OAuth, gunakan **Connect account**.

---

## 2. Bagaimana Prefix Routing Bekerja?

Misalkan Anda memiliki 2 provider terdaftar:
1. **Provider 1:** Prefix `openrouter`, Route `https://openrouter.ai/api/v1`
2. **Provider 2:** Prefix `local`, Route `http://localhost:11434`

### Contoh Panggilan Model dari Client:
* Jika client memanggil model: **`openrouter/anthropic/claude-3.5-sonnet`**
  * NineGuard mendeteksi prefix `openrouter/`.
  * NineGuard memotong prefix menjadi `anthropic/claude-3.5-sonnet`.
  * NineGuard meneruskan request ke `https://openrouter.ai/api/v1` dengan master key Provider 1.
* Jika client memanggil model: **`local/llama3:8b`**
  * NineGuard mendeteksi prefix `local/`.
  * NineGuard memotong prefix menjadi `llama3:8b`.
  * NineGuard meneruskan request ke `http://localhost:11434` dengan master key Provider 2.

---

## 3. Sinkronisasi Model (Manual & Otomatis)

* **Otomatis:** Setiap kali NineGuard dinyalakan dan setiap 30 menit, NineGuard memanggil endpoint `/v1/models` ke seluruh provider aktif di background dan menyinkronkan daftar model.
* **Manual:** Pada menu **Gateway $\rightarrow$ Models**, klik tombol **Fetch from Providers** untuk memperbarui daftar model secara instan.
* **Pembersihan Otomatis:** Jika suatu provider dihapus dari daftar, seluruh model yang terkait dengan prefix provider tersebut akan otomatis dihapus dari database NineGuard. Begitu pula model-model tanpa provider akan otomatis dibersihkan saat sinkronisasi berjalan.

## 4. Menghubungkan akun OAuth

1. Masuk sebagai administrator, buka **Providers → Add provider**.
2. Pilih **Claude — OAuth** atau **OpenAI / Codex — OAuth**, isi nama dan prefix, lalu simpan. Codex OAuth bukan API key OpenAI Platform; model tersedia mengikuti hak akses akun.
3. Klik **Connect account**, lalu **Open authorization** bila tab login tidak terbuka. Selesaikan persetujuan di situs provider.
4. Jika NineGuard berjalan pada server remote dan browser berhenti di alamat localhost yang gagal dibuka, salin **URL lengkap** dari address bar ke kolom callback, lalu klik **Finish login**. URL tersebut berisi kode sekali pakai; jangan dibagikan atau disimpan di log.
5. Setelah akun terhubung, sinkronkan model di halaman Models. Gunakan prefix yang ditampilkan NineGuard pada client. Untuk Claude, protokol Messages direkomendasikan.

Tindakan akun: **Refresh token**, **Login again**, **Disable/Enable**, dan **Disconnect**. Disconnect menghapus kredensial lokal; pencabutan grant pada provider tidak selalu otomatis. Token yang kedaluwarsa atau dicabut mungkin memerlukan login ulang. Ketentuan provider tetap berlaku; tidak ada jaminan anti-ban.

Kredensial tersimpan terenkripsi di SQLite. Backup database bersama master key vault (`data/.master.key` atau `NINEGUARD_ENCRYPTION_KEY` yang digunakan). Jangan membagikan file database atau master key. Dashboard tidak menyediakan ekspor access/refresh token.

## 5. Ikon provider

Form Add/Edit menyediakan Auto, ikon bawaan (9router, OmniRoute, NineGuard, Claude, Anthropic, OpenAI, OpenRouter, Ollama, Gemini), Generic, atau Custom image URL. URL hanya menerima HTTP(S); browser memuat gambar tanpa referrer dan memakai fallback bila gambar gagal. Host gambar eksternal tetap dapat melihat IP browser.

Brand marks diambil dari Simple Icons (CC0; OpenAI v15.0.0, lainnya v16.35.0), 9router dan OmniRoute (MIT). Logo tetap milik pemegang mereknya. Engine menggunakan [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) v8.0.43 (MIT), dipin untuk kompatibilitas.


## 6. Limit provider langsung SDK

Provider OAuth Claude/Codex otomatis memakai **SDK account telemetry**; jangan memilih tracker 9router. Claude dapat melaporkan jendela 5 jam/7 hari dan reset; Codex memakai window yang dikirim upstream. Nilainya persentase, bukan jumlah token absolut.

Data berasal dari header respons normal. Tombol **Refresh** hanya membaca snapshot SDK terbaru tanpa inferensi, biaya prompt, atau refresh token. Jika header belum tersedia, UI menampilkan **Waiting for upstream telemetry**, bukan unlimited/0%. Snapshot lebih dari 5 menit atau melewati reset ditandai **Stale snapshot**; reset tidak diasumsikan mengosongkan penggunaan.

Dashboard admin menampilkan per akun. Portal dan `/quota` hanya menerima ringkasan tanpa email/ID akun: **most constrained observed account**, bukan total kapasitas seluruh akun. Data parsial diberi label incomplete; akun lain mungkin masih memiliki kapasitas. Limit ini tidak ditambahkan ke budget key NineGuard dan tidak mengubah enforcement token client.


## 7. Provider Anthropic API (`api.anthropic.com`)

API key Anthropic tidak menyediakan endpoint kuota, jadi NineGuard tidak melacak kapasitas upstream untuk route `api.anthropic.com`. Pilihan **Capacity tracker** disembunyikan di form dan selalu disimpan `disabled`; tracker yang terlanjur di-set juga diabaikan. Halaman **Quotas** dan daftar provider menampilkan tautan ke [Claude Console → Usage](https://platform.claude.com/settings/usage); cek pemakaian, biaya, dan rate limit di console akun masing-masing. Budget key NineGuard (Key/Model Quota) tetap berlaku seperti biasa.
