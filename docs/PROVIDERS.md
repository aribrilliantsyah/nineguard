# Panduan Manajemen Upstream Providers

NineGuard mendukung integrasi dengan semua server dan platform yang kompatibel dengan protokol **OpenAI REST API v1**.

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
   * **Upstream API Key:** Master API key yang dibutuhkan provider tersebut.
   * **Set as Default Provider:** Centang opsi ini jika provider ini ingin dijadikan tujuan cadangan (*fallback*) untuk request tanpa prefix.
3. Klik **Save Changes**.
4. Klik tombol **Test Probe** untuk memvalidasi bahwa route dan API key berfungsi (status 200 OK).

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
