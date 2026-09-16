# Panduan Menghubungkan AI Coding Agents

NineGuard mengekspos endpoint OpenAI-compatible standar pada:
```text
Base URL: http://localhost:8080/v1
```

Setiap agen **wajib menggunakan NineGuard Client API Key** (`sk-ng-...`) yang diterbitkan melalui menu **Endpoints & Keys**.

---

## 1. Cursor IDE

1. Buka **Cursor Settings** (ikon gear di kanan atas atau `Ctrl + ,` / `Cmd + ,`).
2. Masuk ke tab **Models**.
3. Cari bagian **OpenAI API Key**:
   * Aktifkan toggle **Override OpenAI Base URL**.
   * Isi **Base URL**: `http://localhost:8080/v1`
   * Masukkan **API Key**: `sk-ng-...` (key NineGuard Anda).
4. Di daftar model di bawahnya, tambahkan model yang Anda inginkan (misalnya `openrouter/anthropic/claude-3.5-sonnet` atau model default).

---

## 2. Cline / Roo Code (VS Code Extension)

1. Klik ikon **Cline** pada sidebar VS Code.
2. Buka menu **Settings** (ikon gear di dalam tab Cline).
3. Pada pilihan **API Provider**, pilih **OpenAI Compatible**.
4. Isi konfigurasi:
   * **Base URL:** `http://localhost:8080/v1`
   * **API Key:** `sk-ng-...`
   * **Model ID:** `openrouter/anthropic/claude-3.5-sonnet` (atau model aktif lainnya).
5. Klik **Save**.

---

## 3. Continue.dev

Edit file konfigurasi Continue di `~/.continue/config.json`:

```json
{
  "models": [
    {
      "title": "NineGuard Claude",
      "provider": "openai",
      "model": "openrouter/anthropic/claude-3.5-sonnet",
      "apiBase": "http://localhost:8080/v1",
      "apiKey": "sk-ng-YOUR_NINEGUARD_KEY"
    }
  ]
}
```

---

## 4. Pi Coding Agent Harness

Set environment variable sebelum menjalankan Pi:

```bash
export OPENAI_BASE_URL="http://localhost:8080/v1"
export OPENAI_API_KEY="sk-ng-YOUR_NINEGUARD_KEY"

# Jalankan sesi agent
pi
```

Atau jalankan dalam satu baris perintah:
```bash
OPENAI_BASE_URL="http://localhost:8080/v1" OPENAI_API_KEY="sk-ng-YOUR_NINEGUARD_KEY" pi
```

---

## 5. Python (Official OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="sk-ng-YOUR_NINEGUARD_KEY"
)

response = client.chat.completions.create(
    model="openrouter/anthropic/claude-3.5-sonnet",
    messages=[{"role": "user", "content": "Halo NineGuard!"}],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content
    if content:
        print(content, end="", flush=True)
print()
```

---

## 6. Node.js / TypeScript (Official OpenAI SDK)

```typescript
import OpenAI from 'openai';

const openai = new OpenAI({
  baseURL: 'http://localhost:8080/v1',
  apiKey: 'sk-ng-YOUR_NINEGUARD_KEY',
});

async function main() {
  const stream = await openai.chat.completions.create({
    model: 'openrouter/anthropic/claude-3.5-sonnet',
    messages: [{ role: 'user', content: 'Halo NineGuard!' }],
    stream: true,
  });

  for await (const chunk of stream) {
    process.stdout.write(chunk.choices[0]?.delta?.content || '');
  }
  console.log();
}

main();
```

---

## 7. cURL / Terminal Shell

```bash
curl -X POST "http://localhost:8080/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-ng-YOUR_NINEGUARD_KEY" \
  -d '{
    "model": "openrouter/anthropic/claude-3.5-sonnet",
    "messages": [{"role": "user", "content": "Ping test"}]
  }'
```
