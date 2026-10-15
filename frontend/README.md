# Frontend (Svelte 5 + TypeScript + Vite)

The frontend for **no-bs-transfer** is a modern, reactive single-page application built using **Svelte 5**, **TypeScript**, and **Tailwind CSS v4**, communicating directly with the Go backend via **Wails v2** bindings.

---

## Architecture & Tech Stack

- **Framework:** [Svelte 5](https://svelte.dev/) utilizing modern runes (`$state`, `$derived`, `$effect`).
- **Styling:** [Tailwind CSS v4](https://tailwindcss.com/) with full responsive design and dynamic dark/light theme switching.
- **Icons:** [`@lucide/svelte`](https://lucide.dev/) for clean, accessible UI iconography.
- **QR Codes:** [`qrcode`](https://www.npmjs.com/package/qrcode) for high-contrast, scalable QR generation for instant mobile transfers.
- **Internationalization (i18n):** Native reactive translation store (`src/i18n/`) supporting English (`en`), Spanish (`es`), and automatic OS locale detection (`system`).
- **Backend Bridge:** [Wails v2](https://wails.io/) JavaScript runtime and Go binding generation located in `wailsjs/`.

---

## Directory Structure

```text
frontend/
├── public/                 # Static assets (favicons, app icons)
├── src/
│   ├── assets/             # Bundled fonts and images
│   ├── i18n/               # Internationalization engine and locale dictionaries
│   │   ├── index.svelte.ts # Reactive translation store & system locale resolver
│   │   ├── types.ts        # Type-safe translation key definitions
│   │   └── locales/        # Translation dictionaries (en.ts, es.ts)
│   ├── App.svelte          # Main application component & state machine
│   ├── main.ts             # Svelte application entrypoint
│   ├── style.css           # Tailwind CSS imports and global styling
│   └── vite-env.d.ts       # Vite TypeScript declarations
├── wailsjs/                # Auto-generated Go bindings and Wails runtime
├── package.json            # Node dependencies and build scripts
├── svelte.config.js        # Svelte compiler configuration
├── tsconfig.json           # TypeScript configuration
└── vite.config.ts          # Vite bundler configuration
```

---

## Available Scripts

From the `frontend` folder (or automatically managed by Wails during `wails dev` / `wails build`):

- **Start Vite Dev Server:**
  ```bash
  npm run dev
  ```
- **Build Frontend Assets:**
  ```bash
  npm run build
  ```
- **Type Checking:**
  ```bash
  npm run check
  ```
- **Preview Production Build:**
  ```bash
  npm run preview
  ```

---

## Key Features & UI Workflows

1. **Send Mode (File Sharing):**
   - Drag-and-drop or native file picker file staging.
   - Interactive reordering and individual file removal.
   - Live share management: dynamically add new files or remove files during an active tunnel without dropping existing connections.
   - Per-guest transfer metrics: progress bar, transfer rate, downloaded bytes, and operating system icon.
   - Modal QR code view for direct mobile downloads.

2. **Receive Mode (File Ingestion):**
   - Starts a secure upload receiver and generates a mobile-optimized upload web interface.
   - Live incoming upload tracker showing guest upload speeds, progress, and completed files.
   - Direct shortcut to open the designated downloads destination folder.

3. **Network Mode & Settings:**
   - Quick toggle between **Internet + LAN** (Cloudflare Tunnel + Local IP) and **LAN Only** (strictly local Wi-Fi, fully offline).
   - Custom Cloudflare Tunnel Token and Domain configuration.
   - Configurable fixed listening port and downloads folder selector.
