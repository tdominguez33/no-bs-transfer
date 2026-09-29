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
- **Modo portable:** Puedes transformar la aplicación en completamente **portable** colocando un archivo vacío llamado `portable.txt` junto al ejecutable.
- **Consumo mínimo de recursos:** Pesa solo **~13 MB** y consume **~50 MB de RAM**.

---

## Stack Tecnológico

- **Backend:** Go (Golang)
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

---

## Flags para Desarrollo y Pruebas

Para facilitar el testing local y probar el flujo completo con el binario descargado (ignorando instalaciones previas en tu máquina), puedes utilizar el flag de desarrollo:

### Ignorar instalación local del sistema (`--ignore-local-install`)
Ignora cualquier instalación previa en el sistema (`PATH`, `Program Files`, `WinGet`, etc.) o junto al ejecutable, y busca **únicamente** en la carpeta de AppData (`%LOCALAPPDATA%\no-bs-transfer\bin\cloudflared.exe`):

* **Si no existe en AppData:** La app se comportará como en una PC limpia, permitiéndote probar la descarga automática y la barra de progreso desde cero.
* **Si ya se descargó:** Utilizará el binario ubicado en dicha carpeta.

#### Cómo usarlo:
* **Con variable de entorno en PowerShell:**
  ```powershell
  $env:NO_BS_IGNORE_LOCAL_INSTALL="1"; wails dev
  ```
* **O pasando el argumento CLI al ejecutable:**
  ```powershell
  .\build\bin\no-bs-transfer.exe --ignore-local-install
  ```
