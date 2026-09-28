<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import QRCode from 'qrcode';
  import {
    SelectFiles,
    LoadFilesFromPaths,
    StartShare,
    AddFilesToShare,
    RemoveFileFromShare,
    StopShare,
    CopyToClipboard,
    GetConfig,
    SaveConfig
  } from '../wailsjs/go/main/App';
  import { EventsOn, EventsOff, OnFileDrop, OnFileDropOff } from '../wailsjs/runtime/runtime';
  import type { transfer, main, config } from '../wailsjs/go/models';

  import {
    Upload,
    File,
    X,
    Copy,
    Check,
    QrCode,
    Sun,
    Moon,
    Laptop,
    Smartphone,
    Terminal,
    AlertCircle,
    Plus,
    Trash2,
    GripVertical,
    Settings,
    Globe,
    Eye,
    EyeOff,
    CornerDownLeft,
    ChevronUp,
    ChevronDown
  } from '@lucide/svelte';

  interface GuestTransfer {
    id: string;
    guestId: string;
    fileId: string;
    fileName: string;
    ip: string;
    device: string;
    userAgent: string;
    bytesSent: number;
    totalBytes: number;
    speed: number;
    progress: number;
    status: string;
    connectedAt: string;
    lastActive: string;
  }

  interface TransferStats {
    totalSpeed: number;
    totalBytesSent: number;
    activeGuests: number;
    transfers: GuestTransfer[];
  }

  // View state: show configuration view
  let showConfig = $state(false);
  let isCustomDomainExpanded = $state(false);

  // Theme State (Dark mode by default)
  let isDark = $state(true);

  // Configuration State
  let configState = $state<config.AppConfig>({
    useCustomDomain: false,
    customDomain: '',
    tunnelToken: '',
    fixedPort: 56789,
    savePortable: false
  });
  let showToken = $state(false);

  // Files State
  let files = $state<transfer.SharedFile[]>([]);
  let isDragging = $state(false);
  let isSharing = $state(false);
  let isStarting = $state(false);
  let isAddingMore = $state(false);
  let isStopping = $state(false);
  let shareResult = $state<main.MultiShareResult | null>(null);

  // Sharing Mode: 'individual' (link per file, default) vs 'bundle' (single link for all)
  let linkViewMode = $state<'bundle' | 'individual'>('individual');

  // QR Modal State
  let qrModal = $state<{ show: boolean; title: string; url: string; qrDataUrl: string }>({
    show: false,
    title: '',
    url: '',
    qrDataUrl: ''
  });

  // Delete Confirmation Modal State
  let deleteModal = $state<{ show: boolean; fileId: string; fileName: string }>({
    show: false,
    fileId: '',
    fileName: ''
  });
  let isDeleting = $state(false);

  // Copied feedback map: key -> boolean
  let copiedMap = $state<Record<string, boolean>>({});
  let errorMessage = $state('');

  // Live Stats
  let stats = $state<TransferStats>({
    totalSpeed: 0,
    totalBytesSent: 0,
    activeGuests: 0,
    transfers: []
  });

  // Toggle Theme
  function toggleTheme() {
    isDark = !isDark;
    applyTheme(isDark);
    if (qrModal.show && qrModal.url) {
      openQR(qrModal.title, qrModal.url);
    }
    try {
      localStorage.setItem('nbs_theme', isDark ? 'dark' : 'light');
    } catch {}
  }

  function applyTheme(dark: boolean) {
    if (dark) {
      document.documentElement.classList.add('dark');
    } else {
      document.documentElement.classList.remove('dark');
    }
  }

  function cleanDomainInput(val: string): string {
    if (!val) return '';
    let str = val.trim();
    // Remove protocol: https://, http://, https:/, http:/, https:, http:
    str = str.replace(/^https?:\/\/+/i, '');
    str = str.replace(/^https?:+/i, '');
    str = str.replace(/^\/+/, '');
    str = str.replace(/\/+$/, '');
    // Strip subpath after domain if present: domain.com/foo -> domain.com
    const slashIdx = str.indexOf('/');
    if (slashIdx !== -1) {
      str = str.substring(0, slashIdx);
    }
    return str.trim();
  }

  function handleDomainPaste(e: ClipboardEvent) {
    const text = e.clipboardData?.getData('text');
    if (text && (/^https?:\/\//i.test(text.trim()) || text.includes('/'))) {
      e.preventDefault();
      configState.customDomain = cleanDomainInput(text);
      performSave();
    }
  }

  let portRepeatTimer: any = null;
  let portIntervalTimer: any = null;

  function changePort(delta: number) {
    let p = Number(configState.fixedPort) || 56789;
    p += delta;
    if (p < 1024) p = 1024;
    if (p > 65535) p = 65535;
    configState.fixedPort = p;
  }

  function validatePort() {
    let p = Number(configState.fixedPort);
    if (isNaN(p) || p < 1024) {
      configState.fixedPort = 56789;
    } else if (p > 65535) {
      configState.fixedPort = 65535;
    } else {
      configState.fixedPort = Math.round(p);
    }
  }

  let isSteppingPort = false;

  function startPortStep(delta: number) {
    isSteppingPort = true;
    changePort(delta);
    if (portRepeatTimer) clearTimeout(portRepeatTimer);
    if (portIntervalTimer) clearInterval(portIntervalTimer);
    portRepeatTimer = setTimeout(() => {
      portIntervalTimer = setInterval(() => {
        changePort(delta);
      }, 75);
    }, 350);
  }

  function stopPortStep() {
    if (!isSteppingPort) return;
    isSteppingPort = false;
    if (portRepeatTimer) {
      clearTimeout(portRepeatTimer);
      portRepeatTimer = null;
    }
    if (portIntervalTimer) {
      clearInterval(portIntervalTimer);
      portIntervalTimer = null;
    }
    performSave();
  }

  let lastSavedConfigJSON = '';

  async function loadConfig() {
    try {
      const loaded = await GetConfig();
      if (loaded) {
        configState = {
          useCustomDomain: loaded.useCustomDomain || false,
          customDomain: cleanDomainInput(loaded.customDomain || ''),
          tunnelToken: loaded.tunnelToken || '',
          fixedPort: loaded.fixedPort || 56789,
          savePortable: loaded.savePortable || false
        };
        lastSavedConfigJSON = JSON.stringify({
          useCustomDomain: configState.useCustomDomain,
          customDomain: configState.customDomain,
          tunnelToken: configState.tunnelToken,
          fixedPort: configState.fixedPort,
          savePortable: configState.savePortable
        });
      }
    } catch (err: any) {
      console.error('Error al cargar configuración:', err);
    }
  }

  let autoSaveTimer: any = null;

  async function performSave() {
    if (autoSaveTimer) {
      clearTimeout(autoSaveTimer);
      autoSaveTimer = null;
    }
    validatePort();
    const toSave = {
      ...configState,
      customDomain: cleanDomainInput(configState.customDomain)
    };
    const currentJSON = JSON.stringify({
      useCustomDomain: toSave.useCustomDomain,
      customDomain: toSave.customDomain,
      tunnelToken: toSave.tunnelToken,
      fixedPort: toSave.fixedPort,
      savePortable: toSave.savePortable
    });
    if (currentJSON === lastSavedConfigJSON) {
      return;
    }

    errorMessage = '';
    try {
      await SaveConfig(toSave);
      lastSavedConfigJSON = currentJSON;
    } catch (err: any) {
      errorMessage = err?.message || 'Error al guardar la configuración';
    }
  }

  function triggerAutoSave(debounceMs = 500) {
    if (autoSaveTimer) {
      clearTimeout(autoSaveTimer);
    }
    autoSaveTimer = setTimeout(() => {
      performSave();
    }, debounceMs);
  }

  function handleGlobalDragOver(e: DragEvent) {
    e.preventDefault();
    if (draggedFileId !== null && e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
  }

  function handleGlobalDrop(e: DragEvent) {
    e.preventDefault();
    if (draggedFileId !== null) {
      if (!isDropCommitted && initialFiles.length > 0) {
        files = initialFiles;
      }
      draggedFileId = null;
      initialFiles = [];
      isDropCommitted = false;
    }
  }

  onMount(() => {
    // Restore theme preference or default to dark
    const saved = localStorage.getItem('nbs_theme');
    isDark = saved !== 'light';
    applyTheme(isDark);

    // Load initial config
    loadConfig();

    // Prevent default browser drag & drop with move dropEffect during reordering
    window.addEventListener('dragover', handleGlobalDragOver);
    window.addEventListener('dragenter', handleGlobalDragOver);
    window.addEventListener('drop', handleGlobalDrop);

    // Native file drop
    OnFileDrop(async (x: number, y: number, paths: string[]) => {
      isDragging = false;
      if (paths && paths.length > 0) {
        try {
          const loaded = await LoadFilesFromPaths(paths);
          if (loaded && loaded.length > 0) {
            if (isSharing) {
              await handleAddFilesToActiveShare(loaded);
            } else {
              addFiles(loaded);
            }
          }
        } catch (err: any) {
          errorMessage = err?.message || 'Error al procesar archivos arrastrados';
        }
      }
    }, false);

    EventsOn('transfer_update', (newStats: TransferStats) => {
      stats = newStats;
    });

    EventsOn('share_stopped', () => {
      isSharing = false;
      shareResult = null;
    });
  });

  onDestroy(() => {
    if (autoSaveTimer) {
      clearTimeout(autoSaveTimer);
      performSave();
    }
    stopPortStep();
    OnFileDropOff();
    EventsOff('transfer_update');
    EventsOff('share_stopped');
    window.removeEventListener('dragover', handleGlobalDragOver);
    window.removeEventListener('dragenter', handleGlobalDragOver);
    window.removeEventListener('drop', handleGlobalDrop);
  });

  function addFiles(newFiles: transfer.SharedFile[]) {
    const existingPaths = new Set(files.map(f => f.path));
    const toAdd = newFiles
      .filter(f => !existingPaths.has(f.path))
      .map(f => ({
        ...f,
        id: f.id || (typeof crypto !== 'undefined' && crypto.randomUUID ? crypto.randomUUID() : 'f_' + Math.random().toString(36).slice(2))
      }));
    files = [...files, ...toAdd];
  }

  function removeFile(fileId: string) {
    files = files.filter(f => f.id !== fileId);
  }

  function clearFiles() {
    files = [];
  }

  // Drag-and-drop live reordering for files in staging list
  let draggedFileId = $state<string | null>(null);
  let initialFiles = $state<transfer.SharedFile[]>([]);
  let isDropCommitted = false;

  function handleItemDragStart(e: DragEvent, fileId: string) {
    if (files.length <= 1) {
      e.preventDefault();
      return;
    }
    draggedFileId = fileId;
    initialFiles = [...files];
    isDropCommitted = false;
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.dropEffect = 'move';
      e.dataTransfer.setData('text/plain', fileId);
    }
  }

  function handleContainerDragOver(e: DragEvent) {
    if (!draggedFileId) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
  }

  function handleItemDragEnter(e: DragEvent, targetId: string) {
    if (!draggedFileId) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    if (targetId === draggedFileId) return;

    const fromIndex = files.findIndex(f => f.id === draggedFileId);
    const toIndex = files.findIndex(f => f.id === targetId);

    if (fromIndex !== -1 && toIndex !== -1 && fromIndex !== toIndex) {
      const reordered = [...files];
      const [moved] = reordered.splice(fromIndex, 1);
      reordered.splice(toIndex, 0, moved);
      files = reordered;
    }
  }

  function handleItemDragOver(e: DragEvent, targetId: string) {
    if (!draggedFileId) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    if (targetId === draggedFileId) return;

    const fromIndex = files.findIndex(f => f.id === draggedFileId);
    const toIndex = files.findIndex(f => f.id === targetId);

    if (fromIndex !== -1 && toIndex !== -1 && fromIndex !== toIndex) {
      const reordered = [...files];
      const [moved] = reordered.splice(fromIndex, 1);
      reordered.splice(toIndex, 0, moved);
      files = reordered;
    }
  }

  function handleListContainerDragOver(e: DragEvent) {
    if (!draggedFileId) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }

    const fromIndex = files.findIndex(f => f.id === draggedFileId);
    const lastIndex = files.length - 1;
    if (fromIndex !== -1 && fromIndex !== lastIndex) {
      const listElement = e.currentTarget as HTMLElement;
      const items = listElement.querySelectorAll('[role="listitem"]');
      if (items.length > 0) {
        const lastItemRect = items[items.length - 1].getBoundingClientRect();
        if (e.clientY > lastItemRect.bottom) {
          const reordered = [...files];
          const [moved] = reordered.splice(fromIndex, 1);
          reordered.splice(lastIndex, 0, moved);
          files = reordered;
        }
      }
    }
  }

  function handleItemDrop(e: DragEvent) {
    if (!draggedFileId) return;
    e.preventDefault();
    isDropCommitted = true;
    draggedFileId = null;
    initialFiles = [];
  }

  function handleItemDragEnd() {
    if (!isDropCommitted && initialFiles.length > 0) {
      files = initialFiles;
    }
    draggedFileId = null;
    initialFiles = [];
    isDropCommitted = false;
  }

  async function handleSelectFiles() {
    errorMessage = '';
    try {
      const selected = await SelectFiles();
      if (selected && selected.length > 0) {
        addFiles(selected);
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al seleccionar archivos';
    }
  }

  // Adding more files dynamically while tunnel is running
  async function handleAddMoreToActive() {
    errorMessage = '';
    try {
      const selected = await SelectFiles();
      if (selected && selected.length > 0) {
        await handleAddFilesToActiveShare(selected);
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al seleccionar archivos adicionales';
    }
  }

  async function handleAddFilesToActiveShare(newFiles: transfer.SharedFile[]) {
    if (!newFiles || newFiles.length === 0) return;
    const existingPaths = new Set(files.map(f => f.path));
    const toAdd = newFiles.filter(f => !existingPaths.has(f.path));
    if (toAdd.length === 0) return;

    isAddingMore = true;
    errorMessage = '';
    try {
      const updatedResult = await AddFilesToShare(toAdd);
      if (updatedResult) {
        shareResult = updatedResult;
        addFiles(toAdd);
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al añadir archivos a la sesión activa';
    } finally {
      isAddingMore = false;
    }
  }

  async function handleStartShare() {
    if (files.length === 0) return;
    errorMessage = '';

    if (configState.useCustomDomain) {
      if (!configState.customDomain.trim() || !configState.tunnelToken.trim()) {
        errorMessage = 'Has activado dominio propio pero falta completar el dominio o el token en Configuración.';
        showConfig = true;
        return;
      }
    }

    isStarting = true;

    try {
      const result = await StartShare(files);
      shareResult = result;
      isSharing = true;
      linkViewMode = 'individual';
    } catch (err: any) {
      errorMessage = err?.message || 'Error al iniciar el túnel de Cloudflare';
      isSharing = false;
    } finally {
      isStarting = false;
    }
  }

  async function handleStopShare() {
    isStopping = true;
    errorMessage = '';
    try {
      await StopShare();
      isSharing = false;
      shareResult = null;
      stats = { totalSpeed: 0, totalBytesSent: 0, activeGuests: 0, transfers: [] };
    } catch (err: any) {
      errorMessage = err?.message || 'Error al detener la sesión';
    } finally {
      isStopping = false;
    }
  }

  function promptRemoveFile(fileId: string, fileName: string) {
    deleteModal = {
      show: true,
      fileId,
      fileName
    };
  }

  async function handleConfirmDelete() {
    if (!deleteModal.fileId) return;
    isDeleting = true;
    errorMessage = '';
    const fileIdToRemove = deleteModal.fileId;
    try {
      const updatedResult = await RemoveFileFromShare(fileIdToRemove);
      deleteModal = { show: false, fileId: '', fileName: '' };

      if (!updatedResult || updatedResult.files.length === 0) {
        isSharing = false;
        shareResult = null;
        stats = { totalSpeed: 0, totalBytesSent: 0, activeGuests: 0, transfers: [] };
        files = [];
      } else {
        shareResult = updatedResult;
        const remainingNames = new Set(updatedResult.files.map(f => f.name));
        files = files.filter(f => remainingNames.has(f.name));
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al eliminar el archivo de la sesión';
    } finally {
      isDeleting = false;
    }
  }

  async function copyText(key: string, text: string) {
    if (!text) return;
    await CopyToClipboard(text);
    copiedMap[key] = true;
    setTimeout(() => {
      copiedMap[key] = false;
    }, 2000);
  }

  async function openQR(title: string, url: string) {
    try {
      const qrDataUrl = await QRCode.toDataURL(url, {
        width: 450,
        margin: 1.5,
        color: {
          dark: isDark ? '#ffffff' : '#09090b',
          light: isDark ? '#09090b' : '#ffffff'
        }
      });
      qrModal = { show: true, title, url, qrDataUrl };
    } catch (err) {
      errorMessage = 'Error al generar el código QR';
    }
  }

  function formatBytes(bytes: number): string {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function formatSpeed(bytesPerSec: number): string {
    return `${formatBytes(bytesPerSec)}/s`;
  }

  function formatDisplayUrl(url: string): string {
    if (!url) return '';
    return url.replace(/^https?:\/\//i, '');
  }

  let totalBundleSize = $derived.by(() => {
    if (shareResult?.totalSize) return shareResult.totalSize;
    const total = files.reduce((acc, f) => acc + (f.rawBytes || 0), 0);
    return formatBytes(total);
  });

  function getDeviceIcon(device: string) {
    const d = (device || '').toLowerCase();
    if (d.includes('iphone') || d.includes('android')) return Smartphone;
    if (d.includes('curl') || d.includes('wget')) return Terminal;
    return Laptop;
  }
</script>

<div class={isDark ? 'dark' : ''}>
  <div class="h-screen w-screen bg-zinc-50 dark:bg-zinc-950 text-zinc-900 dark:text-zinc-100 flex flex-col font-sans select-none antialiased overflow-hidden">
    <!-- Header -->
    <header class="shrink-0 border-b border-zinc-200 dark:border-zinc-800/80 px-4 py-2 flex items-center justify-between">
      <div class="flex items-center gap-2.5">
        <h1 class="font-semibold text-sm tracking-tight text-zinc-900 dark:text-zinc-100">
          no-bs-transfer
        </h1>
        <span class="text-xs px-2 py-0.5 rounded-md font-mono {isSharing ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20' : 'bg-zinc-100 dark:bg-zinc-900 text-zinc-500 dark:text-zinc-400 border border-zinc-200 dark:border-zinc-800'}">
          {isSharing ? 'túnel activo' : 'tunel inactivo'}
        </span>
      </div>

      <!-- Action buttons: Settings & Theme Toggle -->
      <div class="flex items-center gap-1">
        <!-- Settings Button -->
        <button
          onclick={() => showConfig = !showConfig}
          class="p-1.5 rounded-lg hover:bg-zinc-200 dark:hover:bg-zinc-800 cursor-pointer {showConfig ? 'bg-zinc-200 dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100' : 'text-zinc-600 dark:text-zinc-400'}"
          title="Configuración"
          aria-label="Configuración"
        >
          <Settings class="w-4 h-4" />
        </button>

        <!-- Theme Toggle -->
        <button
          onclick={toggleTheme}
          class="p-1.5 rounded-lg hover:bg-zinc-200 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 cursor-pointer"
          title="Cambiar tema (Claro / Oscuro)"
          aria-label="Cambiar tema"
        >
          {#if isDark}
            <Sun class="w-4 h-4" />
          {:else}
            <Moon class="w-4 h-4" />
          {/if}
        </button>
      </div>
    </header>

    <!-- Error Alert -->
    {#if errorMessage}
      <div class="shrink-0 mx-6 mt-4 p-3 rounded-lg bg-red-50 dark:bg-red-950/40 border border-red-200 dark:border-red-900/50 flex items-center justify-between text-xs text-red-700 dark:text-red-400">
        <div class="flex items-center gap-2">
          <AlertCircle class="w-4 h-4 shrink-0" />
          <span>{errorMessage}</span>
        </div>
        <button onclick={() => errorMessage = ''} class="font-medium hover:underline cursor-pointer">
          Descartar
        </button>
      </div>
    {/if}

    <!-- Main Container -->
    <main
      class="flex-1 px-4 py-2 max-w-4xl mx-auto w-full flex flex-col justify-center min-h-0 overflow-hidden"
    >
      {#if showConfig}
        <!-- Configuration View -->
        <div class="flex-1 max-w-2xl mx-auto w-full flex flex-col min-h-0 overflow-hidden">
          <!-- Config Header with Close Button (X) -->
          <div class="shrink-0 flex items-center justify-between pb-3 mb-4 border-b border-zinc-200 dark:border-zinc-800">
            <div class="flex flex-col gap-0.5">
              <h2 class="text-sm font-semibold text-zinc-900 dark:text-zinc-100 flex items-center gap-2">
                <Settings class="w-4 h-4 text-zinc-500" />
                Configuración
              </h2>
            </div>

            <!-- Close Cross Button to go back to transferencias -->
            <button
              type="button"
              onclick={() => { performSave(); showConfig = false; }}
              class="p-1.5 rounded-lg hover:bg-zinc-200 dark:hover:bg-zinc-800 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 transition-colors cursor-pointer"
              title="Volver a transferencias"
              aria-label="Cerrar configuración"
            >
              <X class="w-5 h-5" />
            </button>
          </div>

          <!-- Config Scrollable Body -->
          <div class="flex-1 overflow-y-auto pr-1 flex flex-col gap-5 min-h-0">
            {#if isSharing}
              <div class="p-3 rounded-lg bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-900/50 flex items-center gap-2 text-xs text-amber-700 dark:text-amber-400 shrink-0">
                <AlertCircle class="w-4 h-4 shrink-0" />
                <span>Hay un túnel activo. Cualquier cambio guardado se aplicará en la próxima sesión.</span>
              </div>
            {/if}

            <!-- Section 1: Custom Domain -->
            <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-5 flex flex-col gap-0 shrink-0 transition-colors">
              <div class="flex items-start justify-between gap-4">
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div
                  class="flex flex-col gap-1 cursor-pointer flex-1 select-none"
                  onclick={() => isCustomDomainExpanded = !isCustomDomainExpanded}
                >
                  <div class="flex items-center gap-2">
                    <Globe class="w-4 h-4 text-zinc-600 dark:text-zinc-400" />
                    <h3 class="text-xs font-semibold tracking-wider text-zinc-900 dark:text-zinc-100">
                      Dominio Propio
                    </h3>
                  </div>
                  <p class="text-xs text-zinc-500 dark:text-zinc-400 leading-relaxed">
                    Enruta las transferencias con tu propio dominio usando un Named Tunnel de Cloudflare Zero Trust.
                  </p>
                </div>

                <div class="flex items-center gap-2.5 shrink-0 mt-0.5">
                  <!-- Switch Toggle -->
                  <button
                    type="button"
                    role="switch"
                    aria-label="Usar dominio propio"
                    aria-checked={configState.useCustomDomain}
                    onclick={() => {
                      configState.useCustomDomain = !configState.useCustomDomain;
                      performSave();
                    }}
                    class="relative w-11 h-6 flex items-center rounded-full p-0.5 cursor-pointer shrink-0 {configState.useCustomDomain ? 'bg-zinc-900 dark:bg-zinc-100' : 'bg-zinc-300 dark:bg-zinc-700'}"
                    style="transition: background-color 220ms cubic-bezier(0.4, 0, 0.2, 1);"
                  >
                    <div
                      class="bg-white dark:bg-zinc-900 w-5 h-5 rounded-full shadow-sm"
                      style="transition: transform 220ms cubic-bezier(0.4, 0, 0.2, 1); transform: translateX({configState.useCustomDomain ? '1.25rem' : '0rem'});"
                    ></div>
                  </button>

                  <!-- Expand/Collapse Button -->
                  <button
                    type="button"
                    onclick={() => isCustomDomainExpanded = !isCustomDomainExpanded}
                    class="p-1 rounded-lg hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 transition-colors cursor-pointer"
                    title={isCustomDomainExpanded ? "Colapsar configuración" : "Expandir configuración"}
                    aria-label={isCustomDomainExpanded ? "Colapsar configuración" : "Expandir configuración"}
                    aria-expanded={isCustomDomainExpanded}
                  >
                    <ChevronDown class="w-4 h-4 transform transition-transform duration-200 {isCustomDomainExpanded ? 'rotate-180' : 'rotate-0'}" />
                  </button>
                </div>
              </div>

              <!-- Collapsible Content with seamless Grid animation -->
              <div
                class="grid"
                style="grid-template-rows: {isCustomDomainExpanded ? '1fr' : '0fr'}; transition: grid-template-rows 250ms cubic-bezier(0.4, 0, 0.2, 1);"
              >
                <div class="overflow-hidden" inert={!isCustomDomainExpanded ? true : undefined}>
                  <div
                    class="pt-4 mt-4 border-t border-zinc-100 dark:border-zinc-800/80 flex flex-col gap-4"
                    style="transition: opacity 220ms cubic-bezier(0.4, 0, 0.2, 1), filter 220ms cubic-bezier(0.4, 0, 0.2, 1); opacity: {configState.useCustomDomain ? '1' : '0.4'}; filter: {configState.useCustomDomain ? 'none' : 'grayscale(1)'}; pointer-events: {configState.useCustomDomain ? 'auto' : 'none'}; user-select: {configState.useCustomDomain ? 'auto' : 'none'};"
                  >

                  <!-- Dominio Público Input -->
                  <div class="flex flex-col gap-1.5">
                    <label class="text-xs font-medium text-zinc-700 dark:text-zinc-300" for="custom-domain-input">
                      Dominio público
                    </label>
                    <input
                      id="custom-domain-input"
                      type="text"
                      disabled={!configState.useCustomDomain}
                      bind:value={configState.customDomain}
                      onpaste={handleDomainPaste}
                      oninput={() => triggerAutoSave(500)}
                      onblur={() => {
                        configState.customDomain = cleanDomainInput(configState.customDomain);
                        performSave();
                      }}
                      placeholder="transfer.midominio.com"
                      class="bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-lg px-3 py-2 text-xs font-mono text-zinc-800 dark:text-zinc-200 focus:outline-none focus:border-zinc-400 dark:focus:border-zinc-600 disabled:cursor-not-allowed transition-colors"
                    />
                    <span class="text-[11px] text-zinc-500">
                      El dominio o subdominio asignado en Cloudflare Zero Trust (ej: <code class="font-mono">transfer.midominio.com</code>).
                    </span>
                  </div>

                  <!-- Tunnel Token Input -->
                  <div class="flex flex-col gap-1.5">
                    <label class="text-xs font-medium text-zinc-700 dark:text-zinc-300" for="tunnel-token-input">
                      Tunnel Token
                    </label>
                    <div class="relative flex items-center">
                      <input
                        id="tunnel-token-input"
                        type={showToken ? 'text' : 'password'}
                        disabled={!configState.useCustomDomain}
                        bind:value={configState.tunnelToken}
                        oninput={() => triggerAutoSave(500)}
                        onblur={() => performSave()}
                        placeholder="eyJhIjoi..."
                        class="w-full bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-lg pl-3 pr-10 py-2 text-xs font-mono text-zinc-800 dark:text-zinc-200 focus:outline-none focus:border-zinc-400 dark:focus:border-zinc-600 disabled:cursor-not-allowed transition-colors"
                      />
                      <button
                        type="button"
                        disabled={!configState.useCustomDomain}
                        onclick={() => showToken = !showToken}
                        class="absolute right-2.5 p-1 text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-300 cursor-pointer disabled:cursor-not-allowed"
                        title={showToken ? 'Ocultar token' : 'Mostrar token'}
                        aria-label={showToken ? 'Ocultar token' : 'Mostrar token'}
                      >
                        {#if showToken}
                          <EyeOff class="w-3.5 h-3.5" />
                        {:else}
                          <Eye class="w-3.5 h-3.5" />
                        {/if}
                      </button>
                    </div>
                    <span class="text-[11px] text-zinc-500">
                      Token provisto por Cloudflare al crear el túnel (<code class="font-mono">cloudflared tunnel run --token ...</code>).
                    </span>
                  </div>

                  <!-- Puerto Local Input -->
                  <div class="flex flex-col gap-1.5">
                    <label class="text-xs font-medium text-zinc-700 dark:text-zinc-300" for="fixed-port-input">
                      Puerto local del servidor
                    </label>
                    <div class="relative flex items-center w-36">
                      <input
                        id="fixed-port-input"
                        type="number"
                        min="1024"
                        max="65535"
                        disabled={!configState.useCustomDomain}
                        bind:value={configState.fixedPort}
                        oninput={() => triggerAutoSave(500)}
                        onblur={() => {
                          validatePort();
                          performSave();
                        }}
                        class="w-full bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-lg pl-3 pr-7 py-2 text-xs font-mono text-zinc-800 dark:text-zinc-200 focus:outline-none focus:border-zinc-400 dark:focus:border-zinc-600 disabled:cursor-not-allowed transition-colors [appearance:textfield] [&::-webkit-outer-spin-button]:appearance-none [&::-webkit-inner-spin-button]:appearance-none"
                      />
                      <div class="absolute right-1 inset-y-1 w-5 flex flex-col justify-center border-l border-zinc-200 dark:border-zinc-800 overflow-hidden">
                        <button
                          type="button"
                          tabindex="-1"
                          disabled={!configState.useCustomDomain}
                          onmousedown={() => startPortStep(1)}
                          onmouseup={stopPortStep}
                          onmouseleave={stopPortStep}
                          ontouchstart={() => startPortStep(1)}
                          ontouchend={stopPortStep}
                          class="flex-1 flex items-center justify-center text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-200/60 dark:hover:bg-zinc-800/60 transition-colors cursor-pointer select-none disabled:cursor-not-allowed"
                          title="Aumentar puerto"
                          aria-label="Aumentar puerto"
                        >
                          <ChevronUp class="w-3 h-3 stroke-[2.5]" />
                        </button>
                        <button
                          type="button"
                          tabindex="-1"
                          disabled={!configState.useCustomDomain}
                          onmousedown={() => startPortStep(-1)}
                          onmouseup={stopPortStep}
                          onmouseleave={stopPortStep}
                          ontouchstart={() => startPortStep(-1)}
                          ontouchend={stopPortStep}
                          class="flex-1 flex items-center justify-center text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 hover:bg-zinc-200/60 dark:hover:bg-zinc-800/60 transition-colors cursor-pointer select-none border-t border-zinc-100 dark:border-zinc-800/60 disabled:cursor-not-allowed"
                          title="Disminuir puerto"
                          aria-label="Disminuir puerto"
                        >
                          <ChevronDown class="w-3 h-3 stroke-[2.5]" />
                        </button>
                      </div>
                    </div>
                    <span class="text-[11px] text-zinc-500">
                      Puerto donde escucha la app. En Cloudflare debe apuntar a <code class="font-mono">http://localhost:{configState.fixedPort || 56789}</code>.
                    </span>
                  </div>
                </div>
              </div>
            </div>
          </div>


          </div>
        </div>
      {:else if !isSharing}
        <!-- Initial / Staging View: Layout accounts for action bar in both empty and populated states to eliminate layout shifts -->
        <div
          class="w-full my-auto flex flex-col max-h-full min-h-0 shrink"
          style="gap: clamp(0.5rem, 1.8vh, 1rem);"
        >
          {#if files.length === 0}
            <!-- Empty Drop Zone: 260px by default, fluidly shrinks down to 140px with scaling padding -->
            <div
              role="button"
              tabindex="0"
              onclick={handleSelectFiles}
              onkeydown={(e) => e.key === 'Enter' && handleSelectFiles()}
              ondragover={(e) => { e.preventDefault(); isDragging = true; }}
              ondragleave={() => isDragging = false}
              class="w-full border-2 border-dashed rounded-xl text-center cursor-pointer flex flex-col items-center justify-center min-h-0 max-h-full shrink
              {isDragging 
                ? 'border-zinc-500 dark:border-zinc-400 bg-zinc-100/60 dark:bg-zinc-900/60' 
                : 'border-zinc-300 dark:border-zinc-800 hover:border-zinc-400 dark:hover:border-zinc-700 bg-white dark:bg-zinc-900/30'}"
              style="height: clamp(140px, 36vh, 260px); padding-top: clamp(0.5rem, 3vh, 2rem); padding-bottom: clamp(0.5rem, 3vh, 2rem); padding-left: 1.5rem; padding-right: 1.5rem;"
            >
              <div class="flex flex-col items-center justify-center max-w-sm shrink min-h-0">
                <div
                  class="rounded-full bg-zinc-100 dark:bg-zinc-800/80 text-zinc-500 dark:text-zinc-400 shrink-0"
                  style="padding: clamp(0.45rem, 1.8vh, 1rem); margin-bottom: clamp(0.35rem, 1.4vh, 0.75rem);"
                >
                  <Upload class="w-7 h-7 sm:w-8 sm:h-8" />
                </div>
                <p class="text-sm font-medium text-zinc-800 dark:text-zinc-200 text-center leading-snug" style="margin-bottom: clamp(0.2rem, 0.8vh, 0.35rem);">
                  Arrastra archivos aquí o haz clic para seleccionar
                </p>
                <p class="text-xs text-zinc-500 dark:text-zinc-500 text-center leading-normal">
                  Puedes seleccionar varios archivos a la vez
                </p>
              </div>
            </div>
          {:else}
            <!-- Files List Card: Centered vertically, grows dynamically with items up to max window height, shrinks gracefully if window gets small -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
              ondragover={handleContainerDragOver}
              ondragenter={handleContainerDragOver}
              class="flex flex-col border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 overflow-hidden shadow-xs shrink min-h-0 max-h-full"
              style="min-height: clamp(140px, 36vh, 260px);"
            >
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div
                ondragover={handleContainerDragOver}
                ondragenter={handleContainerDragOver}
                class="shrink-0 px-4 border-b border-zinc-200 dark:border-zinc-800 flex items-center justify-between text-xs"
                style="padding-top: clamp(0.45rem, 1.4vh, 0.75rem); padding-bottom: clamp(0.45rem, 1.4vh, 0.75rem);"
              >
                <span class="font-medium text-zinc-500 dark:text-zinc-400">
                  {files.length} {files.length === 1 ? 'archivo preparado' : 'archivos preparados'}
                </span>
                <div class="flex items-center gap-3">
                  <button
                    onclick={handleSelectFiles}
                    class="text-zinc-600 dark:text-zinc-400 hover:text-zinc-900 dark:hover:text-zinc-100 flex items-center gap-1 font-medium cursor-pointer"
                  >
                    <Plus class="w-3.5 h-3.5" />
                    Agregar más
                  </button>
                  <button
                    onclick={clearFiles}
                    class="text-zinc-400 hover:text-red-500 transition-colors flex items-center gap-1 cursor-pointer"
                  >
                    <Trash2 class="w-3.5 h-3.5" />
                    Limpiar
                  </button>
                </div>
              </div>

              <!-- Scrollable files list: grows with items, scrolls only if max height is reached -->
              <div
                role="list"
                ondragover={handleListContainerDragOver}
                ondragenter={handleContainerDragOver}
                ondrop={handleItemDrop}
                class="flex-1 overflow-y-auto overflow-x-hidden min-h-0 outline-none"
                style="scrollbar-gutter: stable;"
              >
                {#each files as f (f.id)}
                  <div
                    role="listitem"
                    draggable={files.length > 1}
                    ondragstart={(e) => handleItemDragStart(e, f.id)}
                    ondragenter={(e) => handleItemDragEnter(e, f.id)}
                    ondragover={(e) => handleItemDragOver(e, f.id)}
                    ondrop={handleItemDrop}
                    ondragend={handleItemDragEnd}
                    class="pl-2.5 pr-4 flex items-center justify-between gap-3 text-sm select-none outline-none border-b border-zinc-100 dark:border-zinc-800/60 last:border-b-0
                      {draggedFileId !== null ? 'transition-none [&_*]:pointer-events-none' : 'transition-colors duration-150'}
                      {draggedFileId === f.id ? 'opacity-40 bg-zinc-100 dark:bg-zinc-800' : (draggedFileId === null ? 'hover:bg-zinc-50/50 dark:hover:bg-zinc-800/30' : '')}"
                    style="padding-top: clamp(0.4rem, 1.1vh, 0.65rem); padding-bottom: clamp(0.4rem, 1.1vh, 0.65rem);"
                  >
                    <div class="flex items-baseline gap-2 min-w-0 flex-1">
                      <!-- Drag Handle: compact, hidden but space preserved when only 1 file -->
                      <div
                        class="w-3.5 h-4 flex items-center justify-center shrink-0 self-center {files.length > 1 ? 'text-zinc-400 dark:text-zinc-600 hover:text-zinc-700 dark:hover:text-zinc-300 cursor-grab active:cursor-grabbing' : 'invisible pointer-events-none'}"
                        title={files.length > 1 ? "Arrastra para reordenar" : ""}
                      >
                        <GripVertical class="w-3.5 h-3.5" />
                      </div>

                      <File class="w-4 h-4 text-zinc-400 shrink-0 self-center" />
                      <span class="truncate min-w-0 font-medium text-zinc-800 dark:text-zinc-200">{f.name}</span>
                      <span class="text-xs font-mono text-zinc-400 shrink-0">{f.size}</span>
                    </div>

                    <button
                      onclick={() => removeFile(f.id)}
                      class="text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 p-1 rounded cursor-pointer shrink-0"
                      title="Quitar archivo"
                    >
                      <X class="w-4 h-4" />
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Action Bar: Reserved in both states to eliminate any vertical layout shift -->
          <div class="shrink-0 flex justify-end">
            <button
              onclick={handleStartShare}
              disabled={isStarting || files.length === 0}
              tabindex={files.length === 0 ? -1 : 0}
              aria-hidden={files.length === 0}
              class="w-36 justify-center rounded-lg bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 text-xs font-medium transition-colors cursor-pointer flex items-center gap-2 disabled:opacity-50 shadow-xs {files.length === 0 ? 'invisible pointer-events-none' : ''}"
              style="padding-top: clamp(0.4rem, 1.2vh, 0.625rem); padding-bottom: clamp(0.4rem, 1.2vh, 0.625rem);"
            >
              {#if isStarting}
                <span class="animate-spin h-3.5 w-3.5 border-2 border-current border-t-transparent rounded-full"></span>
                Conectando...
              {:else}
                <CornerDownLeft class="w-4 h-4" />
                Compartir
              {/if}
            </button>
          </div>
        </div>
      {:else}
        <!-- Active Sharing Dashboard -->
        <div class="flex-1 flex flex-col gap-6 min-h-0 overflow-y-auto scrollbar-gutter-both">
          <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-4 flex flex-col gap-4 shrink-0 shadow-xs">
            <div class="grid grid-cols-[1fr_auto] sm:grid-cols-[auto_1fr_auto] items-center gap-y-2.5 gap-x-3 pb-3 border-b border-zinc-100 dark:border-zinc-800/60">
              <!-- Status text -->
              <div class="flex items-center gap-2">
                <span class="h-2 w-2 rounded-full bg-emerald-500 animate-pulse"></span>
                <span class="text-xs font-medium text-zinc-700 dark:text-zinc-300">
                  Compartiendo {shareResult?.files.length} {shareResult?.files.length === 1 ? 'archivo' : 'archivos'}
                </span>
              </div>

              <!-- Action buttons: Agregar & Detener (always in line with status) -->
              <div class="flex items-center gap-2 justify-self-end sm:col-start-3">
                <!-- Add More Files Button -->
                <button
                  onclick={handleAddMoreToActive}
                  disabled={isAddingMore || isStopping}
                  class="px-2.5 py-1 rounded-md text-xs font-medium text-zinc-700 dark:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 transition-colors cursor-pointer flex items-center gap-1 disabled:opacity-50 shrink-0"
                  title="Agregar más archivos sin reiniciar el túnel"
                >
                  {#if isAddingMore}
                    <span class="animate-spin h-3 w-3 border-2 border-current border-t-transparent rounded-full shrink-0"></span>
                    <span class="hidden md:inline">Añadiendo...</span>
                    <span class="md:hidden">Añadiendo...</span>
                  {:else}
                    <Plus class="w-3.5 h-3.5 shrink-0" />
                    <span class="hidden md:inline">Agregar archivos</span>
                    <span class="md:hidden">Agregar</span>
                  {/if}
                </button>

                <button
                  onclick={handleStopShare}
                  disabled={isStopping}
                  class="px-3 py-1 rounded-md text-xs font-medium text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-950/30 border border-red-200 dark:border-red-900/40 transition-colors cursor-pointer flex items-center gap-1 disabled:opacity-50 shrink-0"
                >
                  {#if isStopping}
                    <span class="animate-spin h-3 w-3 border-2 border-current border-t-transparent rounded-full"></span>
                    Deteniendo...
                  {:else}
                    Detener
                  {/if}
                </button>
              </div>

              <!-- Show tabs ONLY if multiple files are shared: stays inline down to sm, drops to second row below sm -->
              {#if (shareResult?.files.length ?? 0) > 1}
                <div class="col-span-2 sm:col-span-1 sm:col-start-2 sm:row-start-1 justify-self-center flex items-center pt-0.5 sm:pt-0">
                  <div class="flex items-center p-0.5 bg-zinc-100 dark:bg-zinc-800/80 rounded-lg text-xs font-medium">
                    <button
                      onclick={() => linkViewMode = 'individual'}
                      class="px-3 py-1 rounded-md transition-colors cursor-pointer {linkViewMode === 'individual' ? 'bg-white dark:bg-zinc-900 text-zinc-900 dark:text-zinc-100 shadow-sm' : 'text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200'}"
                    >
                      Por archivo
                    </button>
                    <button
                      onclick={() => linkViewMode = 'bundle'}
                      class="px-3 py-1 rounded-md transition-colors cursor-pointer {linkViewMode === 'bundle' ? 'bg-white dark:bg-zinc-900 text-zinc-900 dark:text-zinc-100 shadow-sm' : 'text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200'}"
                    >
                      Enlace general
                    </button>
                  </div>
                </div>
              {/if}
            </div>

            <!-- Single File Mode: Directly show the direct link, no redundant tabs -->
            {#if (shareResult?.files.length ?? 0) === 1}
              {@const singleFile = shareResult!.files[0]}
              <div class="flex flex-col gap-2">
                <div class="flex items-baseline text-xs text-zinc-500 dark:text-zinc-400 min-w-0 gap-1.5">
                  <span class="shrink-0">Enlace directo para descargar</span>
                  <strong class="text-zinc-800 dark:text-zinc-200 font-medium truncate min-w-0">{singleFile.name}</strong>
                  <span class="shrink-0 font-mono text-zinc-400 text-xs">({singleFile.size}):</span>
                </div>
                <div class="flex items-center gap-2">
                  <input
                    type="text"
                    readonly
                    value={formatDisplayUrl(singleFile.directUrl)}
                    class="flex-1 bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-lg px-3 py-2 text-xs font-mono text-zinc-800 dark:text-zinc-200 outline-none select-all"
                  />
                  <button
                    onclick={() => copyText(singleFile.id, singleFile.directUrl)}
                    class="w-[5.75rem] justify-center py-2 bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 text-xs font-medium rounded-lg transition-colors cursor-pointer flex items-center gap-1.5 shrink-0"
                  >
                    {#if copiedMap[singleFile.id]}
                      <Check class="w-3.5 h-3.5" />
                      Copiado
                    {:else}
                      <Copy class="w-3.5 h-3.5" />
                      Copiar
                    {/if}
                  </button>
                  <button
                    onclick={() => openQR(singleFile.name, singleFile.directUrl)}
                    class="p-2 border border-zinc-200 dark:border-zinc-800 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 rounded-lg transition-colors cursor-pointer shrink-0"
                    title="Código QR"
                  >
                    <QrCode class="w-4 h-4" />
                  </button>
                </div>
              </div>
            {:else if linkViewMode === 'bundle'}
              <!-- Multi-File: Bundle Link -->
              <div class="flex flex-col gap-2">
                <div class="text-xs text-zinc-500 dark:text-zinc-400">
                  Enlace general para descargar todos los archivos:
                </div>
                <div class="border border-zinc-200 dark:border-zinc-800 rounded-lg overflow-hidden bg-zinc-50/50 dark:bg-zinc-950/50 overflow-y-auto scrollbar-gutter-both">
                  <div class="p-3 flex items-center justify-between gap-3 text-xs">
                    <div class="min-w-0 flex-1">
                      <div class="flex items-baseline gap-1.5 min-w-0 mb-0.5">
                        <span class="font-medium text-zinc-900 dark:text-zinc-100 truncate min-w-0">Todos los archivos</span>
                        <span class="font-normal font-mono text-zinc-400 text-xs shrink-0">({totalBundleSize})</span>
                      </div>
                      <div class="font-mono text-zinc-400 text-[11px] truncate">{formatDisplayUrl(shareResult?.bundleUrl || '')}</div>
                    </div>
                    <div class="flex items-center gap-1.5 shrink-0">
                      <button
                        onclick={() => copyText('bundle', shareResult?.bundleUrl || '')}
                        class="w-[5.25rem] justify-center py-1.5 bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-800 dark:text-zinc-200 font-medium rounded transition-colors cursor-pointer flex items-center gap-1 shrink-0"
                      >
                        {#if copiedMap['bundle']}
                          <Check class="w-3 h-3" />
                          Copiado
                        {:else}
                          <Copy class="w-3 h-3" />
                          Copiar
                        {/if}
                      </button>
                      <button
                        onclick={() => openQR('Todos los archivos', shareResult?.bundleUrl || '')}
                        class="p-1.5 border border-zinc-200 dark:border-zinc-700 hover:bg-zinc-200 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 rounded transition-colors cursor-pointer"
                        title="Código QR"
                      >
                        <QrCode class="w-3.5 h-3.5" />
                      </button>
                      <!-- Spacer matching the remove button in individual file items to eliminate layout shifts -->
                      <div class="p-1 shrink-0 invisible pointer-events-none select-none" aria-hidden="true">
                        <X class="w-4 h-4" />
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            {:else}
              <!-- Multi-File: Individual Links per File -->
              <div class="flex flex-col gap-2">
                <div class="text-xs text-zinc-500 dark:text-zinc-400">
                  Enlaces directos por archivo:
                </div>
                <div class="border border-zinc-200 dark:border-zinc-800 rounded-lg divide-y divide-zinc-100 dark:divide-zinc-800/60 overflow-hidden max-h-[220px] overflow-y-auto bg-zinc-50/50 dark:bg-zinc-950/50 scrollbar-gutter-both">
                  {#each (shareResult?.files || []) as f}
                    <div class="p-3 flex items-center justify-between gap-3 text-xs">
                      <div class="min-w-0 flex-1">
                        <div class="flex items-baseline gap-1.5 min-w-0 mb-0.5">
                          <span class="font-medium text-zinc-900 dark:text-zinc-100 truncate min-w-0">{f.name}</span>
                          <span class="font-normal font-mono text-zinc-400 text-xs shrink-0">({f.size})</span>
                        </div>
                        <div class="font-mono text-zinc-400 text-[11px] truncate">{formatDisplayUrl(f.directUrl)}</div>
                      </div>
                      <div class="flex items-center gap-1.5 shrink-0">
                        <button
                          onclick={() => copyText(f.id, f.directUrl)}
                          class="w-[5.25rem] justify-center py-1.5 bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-800 dark:text-zinc-200 font-medium rounded transition-colors cursor-pointer flex items-center gap-1 shrink-0"
                        >
                          {#if copiedMap[f.id]}
                            <Check class="w-3 h-3" />
                            Copiado
                          {:else}
                            <Copy class="w-3 h-3" />
                            Copiar
                          {/if}
                        </button>
                        <button
                          onclick={() => openQR(f.name, f.directUrl)}
                          class="p-1.5 border border-zinc-200 dark:border-zinc-700 hover:bg-zinc-200 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 rounded transition-colors cursor-pointer"
                          title="Código QR"
                        >
                          <QrCode class="w-3.5 h-3.5" />
                        </button>
                        <button
                          onclick={() => promptRemoveFile(f.id, f.name)}
                          class="text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 p-1 rounded cursor-pointer shrink-0"
                          title="Quitar archivo"
                        >
                          <X class="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                  {/each}
                </div>
              </div>
            {/if}
          </div>

          <!-- Transfers Table -->
          <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 overflow-hidden flex flex-col shrink-0 shadow-xs">
            <div class="px-4 py-3 border-b border-zinc-200 dark:border-zinc-800 flex items-baseline justify-between text-xs font-medium">
              <span class="text-zinc-500 dark:text-zinc-400">Descargas activas</span>
              <div class="flex items-baseline gap-2.5 text-zinc-700 dark:text-zinc-300 shrink-0">
                <div class="flex items-baseline gap-1.5">
                  <span class="text-zinc-400 dark:text-zinc-500 font-normal">Subida:</span>
                  <span class="font-mono font-medium tabular-nums min-w-[3.75rem]">{formatSpeed(stats.totalSpeed)}</span>
                </div>
                <span class="text-zinc-300 dark:text-zinc-700 select-none font-normal">·</span>
                <div class="flex items-baseline gap-1.5">
                  <span class="text-zinc-400 dark:text-zinc-500 font-normal">Total:</span>
                  <span class="font-mono font-medium tabular-nums min-w-[3.25rem]">{formatBytes(stats.totalBytesSent)}</span>
                </div>
              </div>
            </div>

            {#if stats.transfers.length === 0}
              <div class="py-12 text-center text-xs text-zinc-400 dark:text-zinc-500">
                Esperando a que algún invitado inicie la descarga...
              </div>
            {:else}
              <div class="divide-y divide-zinc-100 dark:divide-zinc-800/60">
                {#each stats.transfers as t (t.id)}
                  {@const DeviceIcon = getDeviceIcon(t.device)}
                  <div class="p-4 flex flex-col gap-2.5">
                    <div class="flex items-baseline justify-between gap-3 text-xs">
                      <div class="flex items-baseline gap-2 min-w-0 flex-1">
                        <DeviceIcon class="w-4 h-4 text-zinc-400 shrink-0 self-center" />
                        <span class="font-medium text-zinc-900 dark:text-zinc-100 truncate min-w-0 shrink" title={t.fileName}>{t.fileName}</span>
                        <span class="text-zinc-400 text-xs font-mono inline-flex items-baseline min-w-0 shrink-[4]">
                          <span class="shrink-0">(</span>
                          <span class="truncate min-w-0 shrink-[4]" title={t.device}>{t.device}</span>
                          <span class="shrink-0 mx-1">·</span>
                          <span class="truncate min-w-0 shrink" title={t.ip}>{t.ip}</span>
                          <span class="shrink-0">)</span>
                        </span>
                      </div>

                      <div class="flex items-baseline gap-2.5 shrink-0 text-xs font-mono">
                        {#if t.status === 'downloading'}
                          <span class="text-emerald-600 dark:text-emerald-400 font-medium tabular-nums w-16 text-right">{formatSpeed(t.speed)}</span>
                          <span class="text-zinc-500 tabular-nums w-8 text-right">{t.progress.toFixed(0)}%</span>
                        {:else if t.status === 'completed'}
                          <span class="text-emerald-500 font-medium font-sans text-right">Completado</span>
                        {:else if t.status === 'cancelled'}
                          <span class="text-red-500/90 dark:text-red-400 font-medium font-sans text-right">Cancelado</span>
                          <span class="text-zinc-400 text-[11px] tabular-nums w-8 text-right">{t.progress.toFixed(0)}%</span>
                        {:else}
                          <span class="text-amber-500/90 dark:text-amber-400 font-medium font-sans text-right">Pausado</span>
                          <span class="text-zinc-400 text-[11px] tabular-nums w-8 text-right">{t.progress.toFixed(0)}%</span>
                        {/if}
                      </div>
                    </div>

                    <!-- Clean thin progress bar -->
                    <div class="w-full h-1.5 bg-zinc-100 dark:bg-zinc-800 rounded-full overflow-hidden">
                      <div
                        class="h-full transition-all duration-300 rounded-full {
                          t.status === 'completed'
                            ? 'bg-emerald-500'
                            : t.status === 'cancelled'
                            ? 'bg-red-400/60 dark:bg-red-500/50'
                            : t.status === 'paused'
                            ? 'bg-amber-400/70 dark:bg-amber-500/60'
                            : 'bg-zinc-900 dark:bg-zinc-100'
                        }"
                        style="width: {t.progress}%"
                      ></div>
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </main>

    <!-- QR Code Modal Dialog -->
    {#if qrModal.show}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Código QR"
        tabindex="-1"
        onclick={(e) => {
          if (e.target === e.currentTarget) {
            qrModal.show = false;
          }
        }}
        onkeydown={(e) => e.key === 'Escape' && (qrModal.show = false)}
        class="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center p-3 sm:p-4 z-50 select-none overflow-hidden"
      >
        <div
          class="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-2xl shadow-2xl flex flex-col items-center shrink-0 max-w-[90vw] overflow-hidden"
        >
          <!-- Title header -->
          <div class="flex items-center justify-between w-full px-4 py-2 border-b border-zinc-100 dark:border-zinc-800/80 shrink-0 bg-zinc-50/60 dark:bg-zinc-900">
            <h3 class="text-xs sm:text-sm font-semibold truncate text-zinc-900 dark:text-zinc-100 pr-2">{qrModal.title}</h3>
            <button
              onclick={() => qrModal.show = false}
              class="text-zinc-400 hover:text-zinc-600 dark:hover:text-zinc-200 cursor-pointer p-1 rounded-md shrink-0 transition-colors"
              title="Cerrar"
              aria-label="Cerrar"
            >
              <X class="w-4 h-4" />
            </button>
          </div>

          <!-- Area below title with matching background and QR directly inside -->
          <div class="bg-white dark:bg-zinc-950 p-3 sm:p-4 flex items-center justify-center w-full flex-1">
            <img
              src={qrModal.qrDataUrl}
              alt="QR Code"
              style="height: clamp(160px, 60vh, 320px); width: clamp(160px, 60vh, 320px);"
              class="object-contain aspect-square select-none pointer-events-none"
            />
          </div>
        </div>
      </div>
    {/if}

    <!-- Delete File Confirmation Modal Dialog -->
    {#if deleteModal.show}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Confirmar eliminación"
        tabindex="-1"
        onclick={(e) => {
          if (e.target === e.currentTarget && !isDeleting) {
            deleteModal.show = false;
          }
        }}
        onkeydown={(e) => e.key === 'Escape' && !isDeleting && (deleteModal.show = false)}
        class="fixed inset-0 bg-black/50 backdrop-blur-sm flex items-center justify-center p-4 z-50 select-none overflow-hidden"
      >
        <div
          class="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-2xl shadow-2xl p-5 max-w-sm w-full flex flex-col gap-4"
        >
          <div class="flex items-start gap-3.5">
            <div class="w-10 h-10 rounded-full bg-red-100 dark:bg-red-950/50 flex items-center justify-center text-red-600 dark:text-red-400 shrink-0">
              <Trash2 class="w-5 h-5" />
            </div>
            <div class="min-w-0 flex-1">
              <h3 class="text-sm font-semibold text-zinc-900 dark:text-zinc-100 mb-1">
                ¿Eliminar archivo de la sesión?
              </h3>
              <p class="text-xs text-zinc-500 dark:text-zinc-400 leading-relaxed">
                El archivo <span class="font-medium text-zinc-800 dark:text-zinc-200 break-all">"{deleteModal.fileName}"</span> dejará de estar disponible para su descarga.
              </p>
            </div>
          </div>

          <div class="p-2.5 rounded-lg bg-red-50/70 dark:bg-red-950/30 border border-red-200/60 dark:border-red-900/30 flex items-start gap-2 text-[11px] text-red-600 dark:text-red-400">
            <AlertCircle class="w-4 h-4 shrink-0 mt-0.5" />
            <span>Si algún usuario está descargando este archivo, su descarga se cancelará.</span>
          </div>

          <div class="flex items-center justify-end gap-2 pt-1">
            <button
              onclick={() => deleteModal.show = false}
              disabled={isDeleting}
              class="px-3.5 py-1.5 border border-zinc-200 dark:border-zinc-700 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-700 dark:text-zinc-300 font-medium rounded-lg text-xs transition-colors cursor-pointer disabled:opacity-50"
            >
              Cancelar
            </button>
            <button
              onclick={handleConfirmDelete}
              disabled={isDeleting}
              class="px-3.5 py-1.5 bg-red-600 hover:bg-red-700 dark:bg-red-800 dark:hover:bg-red-700 text-white font-medium rounded-lg text-xs transition-colors cursor-pointer flex items-center gap-1.5 disabled:opacity-50"
            >
              {#if isDeleting}
                <span class="animate-spin h-3.5 w-3.5 border-2 border-white border-t-transparent rounded-full"></span>
                <span>Eliminando...</span>
              {:else}
                <Trash2 class="w-3.5 h-3.5" />
                <span>Eliminar</span>
              {/if}
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>
