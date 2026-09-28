# no-bs-transfer

Una aplicación de escritorio ligera, ultra rápida y sin límites para transferir archivos locales a cualquier dispositivo a través de **Túneles de Cloudflare**.

---

## Características

- **Sin límite de tamaño:** Transmite archivos de 1 GB, 50 GB o varios TB. Memoria RAM constante $O(1)$ gracias a streaming directo disco-a-socket.
- **Túneles Cloudflare automáticos:** Genera enlaces seguros HTTPS (`https://*.trycloudflare.com`) sin abrir puertos en tu router ni registrar dominios.
- **Monitoreo en vivo de invitados:**
  - Lista de dispositivos conectados en tiempo real (iPhone, Android, Windows, Mac, Linux, curl).
  - Porcentaje de descarga (0% a 100%) y bytes transferidos.
  - Velocidad individual y velocidad total de subida en MB/s.
  - Estados: Descargando, Pausado, Completado.
- **Código QR instantáneo:** Escanea con la cámara de cualquier teléfono para descargar sin instalar ninguna app.
- **Reanudación de descargas:** Soporte nativo para cabeceras HTTP `Accept-Ranges: bytes` (permite pausar y reanudar descargas).
- **Consumo mínimo de recursos:** Pesa solo **~12 MB** y consume **~50 MB de RAM**.

---

## Stack Tecnológico

- **Backend:** Go (Golang 1.27)
- **GUI:** Wails v2 (WebView2 nativo de Windows)
- **Frontend:** Svelte 5 + TypeScript + Vite + Tailwind CSS + `@lucide/svelte` + `qrcode`
- **Túnel:** Cloudflare Tunnel CLI (`cloudflared`)

---

## Cómo ejecutar la aplicación

### Modo desarrollo (con recarga en caliente / Hot-Reload):
```powershell
wails dev
```

### Compilar para producción:
```powershell
wails build
```
