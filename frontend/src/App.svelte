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
    SaveConfig,
    GetNetworkMode,
    SetNetworkMode,
    GetCloudflaredStatus,
    DownloadCloudflared,
    CancelCloudflaredDownload,
    StartReceive,
    StopReceive,
    GetDownloadDir,
    SetDownloadDir,
    SelectDownloadDir,
    OpenDownloadDir,
    OpenFile,
    ShowFileInFolder
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
    EthernetPort,
    Eye,
    EyeOff,
    CornerDownLeft,
    ChevronUp,
    ChevronDown,
    Download,
    ShieldCheck,
    Folder,
    FolderOpen,
    FolderDown,
    ExternalLink
  } from '@lucide/svelte';

  interface ReceivedFile {
    id: string;
    name: string;
    size: string;
    rawBytes: number;
    savedPath: string;
    completedAt: string;
  }

  interface ReceiveProgress {
    id: string;
    fileName: string;
    bytesRecv: number;
    totalBytes: number;
    speed: number;
    progress: number;
    status: string;
    error?: string;
  }

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
    networkMode: 'both',
    useCustomDomain: false,
    customDomain: '',
    tunnelToken: '',
    fixedPort: 56789,
    savePortable: false,
    downloadDir: ''
  });
  let defaultDownloadDir = $state('');
  let showToken = $state(false);

  // App Mode State: 'send' (default) vs 'receive'
  let activeMode = $state<'send' | 'receive'>('send');

  // Receive State
  let isReceiving = $state(false);
  let isStartingReceive = $state(false);
  let isStoppingReceive = $state(false);
  let receiveResult = $state<main.ReceiveResult | null>(null);
  let receiveQrSvg = $state('');
  let receiveQrDataUrl = $state('');
  let receiveQrTarget = $state<'internet' | 'local'>('internet');
  let receivedFiles = $state<ReceivedFile[]>([]);
  let activeUploads = $state<Record<string, ReceiveProgress>>({});
  let itemWarnings = $state<Record<string, string>>({});
  let itemWarningTimers: Record<string, any> = {};

  // Files State (Send mode)
  let files = $state<transfer.SharedFile[]>([]);
  let isDragging = $state(false);
  let isSharing = $state(false);
  let isStarting = $state(false);
  let isAddingMore = $state(false);
  let isStopping = $state(false);
  let shareResult = $state<main.MultiShareResult | null>(null);
  let shareLinkTarget = $state<'internet' | 'local'>('internet');
  let isIndividualLinksExpanded = $state(false);

  // QR Modal State
  let qrModal = $state<{
    show: boolean;
    title: string;
    internetUrl: string;
    localUrl: string;
    activeTarget: 'internet' | 'local';
    qrSvg: string;
    qrDataUrl: string;
    canToggle: boolean;
  }>({
    show: false,
    title: '',
    internetUrl: '',
    localUrl: '',
    activeTarget: 'internet',
    qrSvg: '',
    qrDataUrl: '',
    canToggle: false
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

  // Cloudflared Component State
  let cloudflaredStatus = $state<main.CloudflaredStatus>({
    installed: false,
    path: ''
  });
  let isDownloadingCloudflared = $state(false);
  let cloudflaredDownloadProgress = $state({
    show: false,
    downloaded: 0,
    total: 0,
    percent: 0,
    speed: 0
  });

  async function refreshCloudflaredStatus() {
    try {
      const res = await GetCloudflaredStatus();
      if (res) {
        cloudflaredStatus = res;
      }
    } catch (e) {
      console.error('Error al consultar estado de cloudflared:', e);
    }
  }

  async function handleStartCloudflaredDownload() {
    isDownloadingCloudflared = true;
    errorMessage = '';
    try {
      await DownloadCloudflared();
      await refreshCloudflaredStatus();
    } catch (err: any) {
      errorMessage = err?.message || 'Error al descargar cloudflared';
    } finally {
      isDownloadingCloudflared = false;
      cloudflaredDownloadProgress.show = false;
    }
  }

  async function handleCancelCloudflaredDownload() {
    try {
      await CancelCloudflaredDownload();
    } catch (err) {
      console.error(err);
    }
    cloudflaredDownloadProgress.show = false;
    isDownloadingCloudflared = false;
    isStarting = false;
  }

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
    // Strip subpath, query parameters and hash fragments
    const splitIdx = str.search(/[\/\?\#]/);
    if (splitIdx !== -1) {
      str = str.substring(0, splitIdx);
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

  function validatePort(clampMin = true) {
    let p = Number(configState.fixedPort);
    if (isNaN(p)) {
      if (clampMin) configState.fixedPort = 56789;
    } else if (p > 65535) {
      configState.fixedPort = 65535;
    } else if (clampMin && p < 1024) {
      configState.fixedPort = 56789;
    } else if (!isNaN(p)) {
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
          networkMode: loaded.networkMode === 'lan_only' ? 'lan_only' : 'both',
          useCustomDomain: loaded.useCustomDomain || false,
          customDomain: cleanDomainInput(loaded.customDomain || ''),
          tunnelToken: loaded.tunnelToken || '',
          fixedPort: loaded.fixedPort || 56789,
          savePortable: loaded.savePortable || false,
          downloadDir: loaded.downloadDir || ''
        };
        defaultDownloadDir = loaded.downloadDir || '';
        lastSavedConfigJSON = JSON.stringify({
          networkMode: configState.networkMode,
          useCustomDomain: configState.useCustomDomain,
          customDomain: configState.customDomain,
          tunnelToken: configState.tunnelToken,
          fixedPort: configState.fixedPort,
          savePortable: configState.savePortable,
          downloadDir: configState.downloadDir
        });
      }
    } catch (err: any) {
      console.error('Error al cargar configuración:', err);
    }
  }

  async function handleNetworkModeChange(mode: 'both' | 'lan_only') {
    if (configState.networkMode === mode) return;
    configState.networkMode = mode;
    try {
      await SetNetworkMode(mode);
      if (lastSavedConfigJSON) {
        try {
          const parsed = JSON.parse(lastSavedConfigJSON);
          parsed.networkMode = mode;
          lastSavedConfigJSON = JSON.stringify(parsed);
        } catch {}
      }
    } catch (err: any) {
      console.error('Error al cambiar modo de red:', err);
    }
  }

  let autoSaveTimer: any = null;

  async function performSave() {
    if (autoSaveTimer) {
      clearTimeout(autoSaveTimer);
      autoSaveTimer = null;
    }
    validatePort(false);
    const portVal = Number(configState.fixedPort);
    const toSave = {
      ...configState,
      networkMode: configState.networkMode === 'lan_only' ? 'lan_only' : 'both',
      fixedPort: isNaN(portVal) || portVal < 1024 ? 56789 : Math.round(portVal),
      customDomain: cleanDomainInput(configState.customDomain),
      downloadDir: configState.downloadDir || ''
    };
    const currentJSON = JSON.stringify({
      networkMode: toSave.networkMode,
      useCustomDomain: toSave.useCustomDomain,
      customDomain: toSave.customDomain,
      tunnelToken: toSave.tunnelToken,
      fixedPort: toSave.fixedPort,
      savePortable: toSave.savePortable,
      downloadDir: toSave.downloadDir
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
      isIndividualLinksExpanded = false;
    });

    EventsOn('receive_file_progress', (p: ReceiveProgress) => {
      activeUploads[p.id] = p;
      if (p.status === 'completed' || p.status === 'error') {
        setTimeout(() => {
          const copy = { ...activeUploads };
          delete copy[p.id];
          activeUploads = copy;
        }, 1200);
      }
    });

    EventsOn('receive_file_completed', (f: ReceivedFile) => {
      receivedFiles = [f, ...receivedFiles.filter(item => item.id !== f.id)];
    });

    EventsOn('receive_stopped', () => {
      isReceiving = false;
      receiveResult = null;
      receiveQrSvg = '';
      receiveQrDataUrl = '';
    });

    refreshCloudflaredStatus();

    EventsOn('cloudflared_download_progress', (p: { downloaded: number; total: number; percent: number; speed: number; done: boolean; error?: string }) => {
      if (p.error) {
        if (!p.error.toLowerCase().includes('cancel')) {
          errorMessage = 'Fallo al descargar componente de túnel: ' + p.error;
        }
        cloudflaredDownloadProgress.show = false;
        isDownloadingCloudflared = false;
        return;
      }
      if (p.done) {
        cloudflaredDownloadProgress.percent = 100;
        cloudflaredDownloadProgress.downloaded = p.downloaded;
        cloudflaredDownloadProgress.total = p.total;
        setTimeout(() => {
          cloudflaredDownloadProgress.show = false;
          isDownloadingCloudflared = false;
          refreshCloudflaredStatus();
        }, 400);
        return;
      }
      isDownloadingCloudflared = true;
      cloudflaredDownloadProgress = {
        show: true,
        downloaded: p.downloaded,
        total: p.total,
        percent: Math.min(100, Math.max(0, p.percent)),
        speed: p.speed
      };
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
    EventsOff('receive_file_progress');
    EventsOff('receive_file_completed');
    EventsOff('receive_stopped');
    EventsOff('cloudflared_download_progress');
    window.removeEventListener('dragover', handleGlobalDragOver);
    window.removeEventListener('dragenter', handleGlobalDragOver);
    window.removeEventListener('drop', handleGlobalDrop);
    Object.values(itemWarningTimers).forEach(t => clearTimeout(t));
    itemWarningTimers = {};
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

  function handleDragReorder(targetId: string) {
    if (!draggedFileId || targetId === draggedFileId) return;

    const fromIndex = files.findIndex(f => f.id === draggedFileId);
    const toIndex = files.findIndex(f => f.id === targetId);

    if (fromIndex !== -1 && toIndex !== -1 && fromIndex !== toIndex) {
      const reordered = [...files];
      const [moved] = reordered.splice(fromIndex, 1);
      reordered.splice(toIndex, 0, moved);
      files = reordered;
    }
  }

  function handleItemDragEnter(e: DragEvent, targetId: string) {
    if (!draggedFileId) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    handleDragReorder(targetId);
  }

  function handleItemDragOver(e: DragEvent, targetId: string) {
    if (!draggedFileId) return;
    e.preventDefault();
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move';
    }
    handleDragReorder(targetId);
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

    if (configState.networkMode === 'both' && configState.useCustomDomain) {
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
      shareLinkTarget = result.networkMode === 'lan_only' ? 'local' : 'internet';
      isIndividualLinksExpanded = false;
    } catch (err: any) {
      errorMessage = err?.message || 'Error al iniciar la transferencia';
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
      shareLinkTarget = 'internet';
      isIndividualLinksExpanded = false;
      stats = { totalSpeed: 0, totalBytesSent: 0, activeGuests: 0, transfers: [] };
    } catch (err: any) {
      errorMessage = err?.message || 'Error al detener la sesión';
    } finally {
      isStopping = false;
    }
  }

  function getActiveReceiveUrl(): string {
    if (!receiveResult) return '';
    if (receiveResult.networkMode === 'lan_only' || receiveQrTarget === 'local') {
      return receiveResult.localUploadUrl || receiveResult.uploadUrl;
    }
    return receiveResult.uploadUrl || receiveResult.localUploadUrl;
  }

  async function toggleReceiveQrTarget() {
    if (!receiveResult || receiveResult.networkMode === 'lan_only') return;
    receiveQrTarget = receiveQrTarget === 'internet' ? 'local' : 'internet';
    await generateReceiveQR(getActiveReceiveUrl());
  }

  async function generateReceiveQR(url: string) {
    if (!url) return;
    try {
      const rawSvg = await QRCode.toString(url, {
        type: 'svg',
        margin: 1.5,
        color: {
          dark: '#000000',
          light: '#00000000'
        }
      });
      receiveQrSvg = rawSvg
        .replace(/stroke="#000000"/g, 'stroke="currentColor"')
        .replace('<svg ', '<svg class="w-full h-full block" ');
    } catch (e) {
      console.error('Error generating receive QR:', e);
    }
  }

  async function handleStartReceive() {
    errorMessage = '';
    if (configState.networkMode === 'both' && configState.useCustomDomain) {
      if (!configState.customDomain.trim() || !configState.tunnelToken.trim()) {
        errorMessage = 'Has activado dominio propio pero falta completar el dominio o el token en Configuración.';
        showConfig = true;
        return;
      }
    }

    isStartingReceive = true;
    try {
      const res = await StartReceive();
      if (res) {
        receiveResult = res;
        isReceiving = true;
        receiveQrTarget = res.networkMode === 'lan_only' ? 'local' : 'internet';
        await generateReceiveQR(getActiveReceiveUrl());
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al iniciar el receptor de archivos';
      isReceiving = false;
    } finally {
      isStartingReceive = false;
    }
  }

  async function handleStopReceive() {
    isStoppingReceive = true;
    errorMessage = '';
    try {
      await StopReceive();
      isReceiving = false;
      receiveResult = null;
      receiveQrSvg = '';
      receiveQrDataUrl = '';
      receiveQrTarget = 'internet';
    } catch (err: any) {
      errorMessage = err?.message || 'Error al detener la recepción';
    } finally {
      isStoppingReceive = false;
    }
  }

  async function switchMode(mode: 'send' | 'receive') {
    if (activeMode === mode || isStarting || isStartingReceive || isStopping || isStoppingReceive) return;

    if (mode === 'receive') {
      if (isSharing) {
        await handleStopShare();
      }
      activeMode = 'receive';
      if (!isReceiving && !isStartingReceive) {
        await handleStartReceive();
      }
    } else {
      if (isReceiving) {
        await handleStopReceive();
      }
      activeMode = 'send';
    }
  }

  async function handleSelectDownloadDir() {
    errorMessage = '';
    try {
      const selected = await SelectDownloadDir();
      if (selected) {
        configState.downloadDir = selected;
        if (receiveResult) {
          receiveResult.downloadDir = selected;
        }
        await performSave();
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al seleccionar carpeta de descargas';
    }
  }

  async function handleOpenDownloadDir() {
    try {
      await OpenDownloadDir();
    } catch (err: any) {
      errorMessage = err?.message || 'Error al abrir la carpeta de descargas';
    }
  }

  function setItemWarning(fileId: string, msg: string) {
    itemWarnings[fileId] = msg;
    if (itemWarningTimers[fileId]) {
      clearTimeout(itemWarningTimers[fileId]);
    }
    itemWarningTimers[fileId] = setTimeout(() => {
      const copy = { ...itemWarnings };
      delete copy[fileId];
      itemWarnings = copy;
      delete itemWarningTimers[fileId];
    }, 3500);
  }

  async function handleOpenFile(rf: ReceivedFile) {
    try {
      await OpenFile(rf.savedPath);
    } catch (err: any) {
      setItemWarning(rf.id, 'El archivo fue movido o eliminado');
    }
  }

  async function handleShowFileInFolder(rf: ReceivedFile) {
    try {
      await ShowFileInFolder(rf.savedPath);
    } catch (err: any) {
      setItemWarning(rf.id, 'El archivo fue movido o eliminado');
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
        const remainingIDs = new Set(updatedResult.files.map(f => f.id));
        files = files.filter(f => remainingIDs.has(f.id));
      }
    } catch (err: any) {
      errorMessage = err?.message || 'Error al eliminar el archivo de la sesión';
    } finally {
      isDeleting = false;
    }
  }

  let copyFeedbackTimers: Record<string, any> = {};

  async function copyText(key: string, text: string) {
    if (!text) return;
    await CopyToClipboard(text);
    copiedMap[key] = true;
    if (copyFeedbackTimers[key]) {
      clearTimeout(copyFeedbackTimers[key]);
    }
    copyFeedbackTimers[key] = setTimeout(() => {
      copiedMap[key] = false;
      delete copyFeedbackTimers[key];
    }, 2000);
  }

  async function openQR(title: string, internetUrl: string, localUrl: string = '') {
    const isLanOnly = (configState.networkMode === 'lan_only') || (shareResult?.networkMode === 'lan_only') || (receiveResult?.networkMode === 'lan_only');
    const isBothMode = (shareResult?.networkMode === 'both') || (receiveResult?.networkMode === 'both') || (configState.networkMode === 'both');
    const hasInternet = Boolean(internetUrl && internetUrl.trim() !== '');
    const hasLocal = Boolean(localUrl && localUrl.trim() !== '');
    const canToggle = !isLanOnly && isBothMode && hasInternet && hasLocal && (internetUrl !== localUrl);

    let activeTarget: 'internet' | 'local' = 'internet';
    if (!hasInternet || isLanOnly) {
      activeTarget = 'local';
    } else if (shareLinkTarget === 'local' && hasLocal) {
      activeTarget = 'local';
    }

    const currentUrl = activeTarget === 'local' ? (localUrl || internetUrl) : (internetUrl || localUrl);
    try {
      const rawSvg = await QRCode.toString(currentUrl, {
        type: 'svg',
        margin: 1.5,
        color: {
          dark: '#000000',
          light: '#00000000'
        }
      });
      const qrSvg = rawSvg
        .replace(/stroke="#000000"/g, 'stroke="currentColor"')
        .replace('<svg ', '<svg class="w-full h-full block" ');

      qrModal = {
        show: true,
        title,
        internetUrl,
        localUrl,
        activeTarget,
        qrSvg,
        qrDataUrl: '',
        canToggle
      };
    } catch (err) {
      errorMessage = 'Error al generar el código QR';
    }
  }

  async function toggleModalQrTarget() {
    if (!qrModal.canToggle || !qrModal.internetUrl || !qrModal.localUrl) return;
    const nextTarget: 'internet' | 'local' = qrModal.activeTarget === 'internet' ? 'local' : 'internet';
    const nextUrl = nextTarget === 'local' ? qrModal.localUrl : qrModal.internetUrl;
    try {
      const rawSvg = await QRCode.toString(nextUrl, {
        type: 'svg',
        margin: 1.5,
        color: {
          dark: '#000000',
          light: '#00000000'
        }
      });
      const nextQrSvg = rawSvg
        .replace(/stroke="#000000"/g, 'stroke="currentColor"')
        .replace('<svg ', '<svg class="w-full h-full block" ');

      qrModal.activeTarget = nextTarget;
      qrModal.qrSvg = nextQrSvg;
    } catch (err) {
      console.error('Error al alternar código QR:', err);
    }
  }

  function getActiveFileUrl(file: main.FileShareItem): string {
    if (!file) return '';
    if (shareResult?.networkMode === 'lan_only' || shareLinkTarget === 'local') {
      return file.localUrl || file.directUrl;
    }
    return file.directUrl || file.localUrl;
  }

  function getActiveBundleUrl(): string {
    if (!shareResult) return '';
    if (shareResult.networkMode === 'lan_only' || shareLinkTarget === 'local') {
      return shareResult.localBundleUrl || shareResult.bundleUrl;
    }
    return shareResult.bundleUrl || shareResult.localBundleUrl;
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
    return url.replace(/^https?:\/\//i, '').replace(/\/+$/, '');
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
        <span class="text-xs px-2 py-0.5 rounded-md font-mono {isReceiving || isSharing ? 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border border-emerald-500/20' : 'bg-zinc-100 dark:bg-zinc-900 text-zinc-500 dark:text-zinc-400 border border-zinc-200 dark:border-zinc-800'}">
          {#if isReceiving || isSharing}
            {configState.networkMode === 'lan_only' ? 'red local activa' : 'túnel activo'}
          {:else}
            {configState.networkMode === 'lan_only' ? 'red local inactiva' : 'túnel inactivo'}
          {/if}
        </span>
      </div>

      <!-- Action buttons: Network Mode Toggle, Settings & Theme Toggle -->
      <div class="flex items-center gap-1.5">
        <!-- Network Mode Toggle in Top-Right Corner -->
        <div class="inline-flex p-0.5 bg-zinc-200/70 dark:bg-zinc-800/70 rounded-lg border border-zinc-200 dark:border-zinc-700/60 shadow-2xs text-xs">
          <button
            type="button"
            onclick={() => handleNetworkModeChange('lan_only')}
            disabled={isSharing || isReceiving || isStarting || isStartingReceive || isStopping || isStoppingReceive}
            class="px-2 py-1 rounded-md flex items-center gap-1.5 font-medium transition-all duration-150 cursor-pointer disabled:opacity-50 disabled:cursor-default {configState.networkMode === 'lan_only' ? 'bg-white dark:bg-zinc-900 text-emerald-600 dark:text-emerald-400 shadow-xs' : 'text-zinc-500 enabled:hover:text-zinc-800 dark:enabled:hover:text-zinc-200'}"
          >
            <EthernetPort class="w-3.5 h-3.5" />
            <span class="hidden sm:inline">Red local</span>
          </button>
          <button
            type="button"
            onclick={() => handleNetworkModeChange('both')}
            disabled={isSharing || isReceiving || isStarting || isStartingReceive || isStopping || isStoppingReceive}
            class="px-2 py-1 rounded-md flex items-center gap-1.5 font-medium transition-all duration-150 cursor-pointer disabled:opacity-50 disabled:cursor-default {configState.networkMode === 'both' ? 'bg-white dark:bg-zinc-900 text-indigo-600 dark:text-indigo-400 shadow-xs' : 'text-zinc-500 enabled:hover:text-zinc-800 dark:enabled:hover:text-zinc-200'}"
          >
            <Globe class="w-3.5 h-3.5" />
            <span class="hidden sm:inline">Internet</span>
          </button>
        </div>

        <div class="h-4 w-px bg-zinc-200 dark:bg-zinc-800 mx-0.5"></div>

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

    <!-- Mode Selector: Inmediatamente debajo de la barra superior -->
    {#if !showConfig}
      <div class="shrink-0 flex items-center justify-center pt-2.5 pb-1">
        <div class="inline-flex p-1 bg-zinc-200/80 dark:bg-zinc-800/80 rounded-xl border border-zinc-200/80 dark:border-zinc-700/60 shadow-xs">
          <button
            type="button"
            onclick={() => switchMode('send')}
            disabled={isStarting || isStartingReceive || isStopping || isStoppingReceive}
            class="px-4 py-1.5 rounded-lg text-xs font-semibold transition-all duration-150 flex items-center gap-1.5 cursor-pointer disabled:cursor-pointer {activeMode === 'send' ? 'bg-white dark:bg-zinc-900 text-zinc-900 dark:text-zinc-100 shadow-sm' : 'text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200'}"
          >
            <Upload class="w-3.5 h-3.5" />
            <span>Enviar</span>
          </button>
          <button
            type="button"
            onclick={() => switchMode('receive')}
            disabled={isStarting || isStartingReceive || isStopping || isStoppingReceive}
            class="px-4 py-1.5 rounded-lg text-xs font-semibold transition-all duration-150 flex items-center gap-1.5 cursor-pointer disabled:cursor-pointer {activeMode === 'receive' ? 'bg-white dark:bg-zinc-900 text-zinc-900 dark:text-zinc-100 shadow-sm' : 'text-zinc-500 hover:text-zinc-800 dark:hover:text-zinc-200'}"
          >
            <Download class="w-3.5 h-3.5" />
            <span>Recibir</span>
          </button>
        </div>
      </div>
    {/if}

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
            {#if isSharing || isReceiving}
              <div class="p-3 rounded-lg bg-amber-50 dark:bg-amber-950/30 border border-amber-200 dark:border-amber-900/50 flex items-center gap-2 text-xs text-amber-700 dark:text-amber-400 shrink-0">
                <AlertCircle class="w-4 h-4 shrink-0" />
                <span>Hay un túnel activo. Cualquier cambio guardado se aplicará en la próxima sesión.</span>
              </div>
            {/if}

            <!-- Section 1: Componente de Túnel (Cloudflared) -->
            <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-5 flex flex-col gap-3 shrink-0 transition-colors">
              <div class="flex items-start justify-between gap-4">
                <div class="flex flex-col gap-1 flex-1">
                  <div class="flex items-center gap-2">
                    <ShieldCheck class="w-4 h-4 text-zinc-600 dark:text-zinc-400" />
                    <h3 class="text-xs font-semibold tracking-wider text-zinc-900 dark:text-zinc-100">
                      Componente de Túnel (Cloudflared)
                    </h3>
                  </div>
                  <p class="text-xs text-zinc-500 dark:text-zinc-400 leading-relaxed">
                    Motor responsable de generar los enlaces públicos y cifrados de Cloudflare.
                  </p>
                </div>

                <!-- Status Badge -->
                <div class="shrink-0">
                  {#if cloudflaredStatus.installed}
                    <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-emerald-50 dark:bg-emerald-950/40 text-emerald-600 dark:text-emerald-400 border border-emerald-200 dark:border-emerald-900/60 font-mono">
                      <span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                      Instalado
                    </span>
                  {:else}
                    <span class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium bg-amber-50 dark:bg-amber-950/40 text-amber-600 dark:text-amber-400 border border-amber-200 dark:border-amber-900/60 font-mono">
                      <span class="w-1.5 h-1.5 rounded-full bg-amber-500"></span>
                      No detectado
                    </span>
                  {/if}
                </div>
              </div>

              {#if cloudflaredStatus.installed}
                <div class="mt-1 p-2.5 rounded-lg bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 text-[11px] font-mono text-zinc-600 dark:text-zinc-400 break-all flex items-center justify-between gap-2">
                  <span class="truncate">{cloudflaredStatus.path}</span>
                  <button
                    type="button"
                    onclick={() => copyText('cloudflared_path', cloudflaredStatus.path)}
                    class="p-1 text-zinc-500 hover:text-zinc-700 dark:text-zinc-400 dark:hover:text-zinc-200 transition-colors cursor-pointer shrink-0"
                    title="Copiar ruta"
                  >
                    {#if copiedMap['cloudflared_path']}
                      <Check class="w-3.5 h-3.5 text-zinc-800 dark:text-zinc-100" />
                    {:else}
                      <Copy class="w-3.5 h-3.5" />
                    {/if}
                  </button>
                </div>
              {:else}
                <div class="mt-1 flex items-center justify-between gap-3 p-3 rounded-lg bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800">
                  <span class="text-xs text-zinc-500 dark:text-zinc-400">
                    Se descargará de forma automática al iniciar el primer túnel.
                  </span>
                  <button
                    type="button"
                    disabled={isDownloadingCloudflared}
                    onclick={handleStartCloudflaredDownload}
                    class="px-3 py-1.5 bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 rounded-lg text-xs font-medium transition-colors flex items-center gap-1.5 cursor-pointer disabled:opacity-50 shrink-0"
                  >
                    <Download class="w-3.5 h-3.5" />
                    <span>Descargar ahora</span>
                  </button>
                </div>
              {/if}
            </div>

            <!-- Section 2: Carpeta de Descargas -->
            <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-5 flex flex-col gap-3 shrink-0 transition-colors">
              <div class="flex items-start justify-between gap-4">
                <div class="flex flex-col gap-1 flex-1">
                  <div class="flex items-center gap-2">
                    <FolderDown class="w-4 h-4 text-zinc-600 dark:text-zinc-400" />
                    <h3 class="text-xs font-semibold tracking-wider text-zinc-900 dark:text-zinc-100">
                      Carpeta de Descargas
                    </h3>
                  </div>
                  <p class="text-xs text-zinc-500 dark:text-zinc-400 leading-relaxed">
                    Ubicación en tu equipo donde se guardarán los archivos recibidos en el modo recibir.
                  </p>
                </div>
              </div>

              <div class="mt-1 flex items-center justify-between gap-2.5 p-2.5 rounded-lg bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800">
                <div class="flex items-center gap-2 min-w-0 flex-1">
                  <Folder class="w-4 h-4 text-zinc-400 shrink-0" />
                  <span class="text-[11px] leading-none font-mono text-zinc-700 dark:text-zinc-300 truncate" title={configState.downloadDir}>
                    {configState.downloadDir || 'Descargas (por defecto)'}
                  </span>
                </div>

                <div class="flex items-center gap-1.5 shrink-0">
                  <button
                    type="button"
                    onclick={handleSelectDownloadDir}
                    class="px-2.5 py-1.5 bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 rounded-lg text-xs font-medium transition-colors flex items-center gap-1 cursor-pointer"
                    title="Elegir otra carpeta de descargas"
                  >
                    <FolderOpen class="w-3.5 h-3.5" />
                    <span>Cambiar</span>
                  </button>

                  <button
                    type="button"
                    onclick={handleOpenDownloadDir}
                    class="p-1.5 border border-zinc-200 dark:border-zinc-700 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-300 rounded-lg text-xs transition-colors cursor-pointer"
                    title="Abrir carpeta en el explorador"
                  >
                    <ExternalLink class="w-3.5 h-3.5" />
                  </button>
                </div>
              </div>
            </div>

            <!-- Section 3: Custom Domain -->
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
      {:else if activeMode === 'send' && !isSharing}
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
              class="w-36 justify-center rounded-lg bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 text-xs font-medium transition-colors cursor-pointer flex items-center gap-2 disabled:opacity-80 shadow-xs {files.length === 0 ? 'invisible pointer-events-none' : ''}"
              style="padding-top: clamp(0.4rem, 1.2vh, 0.625rem); padding-bottom: clamp(0.4rem, 1.2vh, 0.625rem);"
            >
              {#if isStarting}
                <span class="animate-spin h-3.5 w-3.5 border-2 border-white/30 border-t-white dark:border-zinc-900/30 dark:border-t-zinc-900 rounded-full shrink-0"></span>
                <span>Conectando...</span>
              {:else}
                <CornerDownLeft class="w-4 h-4" />
                <span>Compartir</span>
              {/if}
            </button>
          </div>
        </div>
      {:else if activeMode === 'send' && isSharing}
        <!-- Active Sharing Dashboard -->
        <div class="flex-1 flex flex-col gap-6 min-h-0 overflow-y-auto scrollbar-gutter-both">
          <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-4 flex flex-col gap-4 shrink-0 shadow-xs">
            <div class="flex items-center justify-between gap-3 pb-3 border-b border-zinc-100 dark:border-zinc-800/60">
              <!-- Status text -->
              <div class="flex items-center gap-2">
                <span class="h-2 w-2 rounded-full bg-emerald-500 animate-pulse"></span>
                <span class="text-xs font-medium text-zinc-700 dark:text-zinc-300">
                  Compartiendo {shareResult?.files.length} {shareResult?.files.length === 1 ? 'archivo' : 'archivos'}
                </span>
              </div>

              <!-- Action buttons: Agregar & Detener -->
              <div class="flex items-center gap-2 shrink-0">
                <!-- Add More Files Button -->
                <button
                  onclick={handleAddMoreToActive}
                  disabled={isAddingMore || isStopping}
                  class="px-2.5 py-1 rounded-md text-xs font-medium text-zinc-700 dark:text-zinc-300 hover:bg-zinc-100 dark:hover:bg-zinc-800 border border-zinc-200 dark:border-zinc-700 transition-colors cursor-pointer flex items-center gap-1 disabled:opacity-50 shrink-0"
                  title="Agregar más archivos sin reiniciar el túnel"
                >
                  {#if isAddingMore}
                    <span class="animate-spin h-3 w-3 border-2 border-zinc-300 border-t-zinc-800 dark:border-zinc-600 dark:border-t-zinc-200 rounded-full shrink-0"></span>
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
                    <span class="animate-spin h-3 w-3 border-2 border-red-200 border-t-red-600 dark:border-red-900/60 dark:border-t-red-400 rounded-full shrink-0"></span>
                    Deteniendo...
                  {:else}
                    Detener
                  {/if}
                </button>
              </div>
            </div>

            <!-- Single File Mode: Directly show the direct link, no redundant tabs -->
            {#if (shareResult?.files.length ?? 0) === 1}
              {@const singleFile = shareResult!.files[0]}
              <div class="flex flex-col gap-2">
                <div class="flex items-center justify-between gap-2.5 flex-wrap">
                  <div class="flex items-baseline text-xs text-zinc-500 dark:text-zinc-400 min-w-0 gap-1.5">
                    <span class="shrink-0">Enlace directo para descargar</span>
                    <strong class="text-zinc-800 dark:text-zinc-200 font-medium truncate min-w-0">{singleFile.name}</strong>
                    <span class="shrink-0 font-mono text-zinc-400 text-xs">({singleFile.size}):</span>
                  </div>
                  {#if shareResult?.networkMode === 'both' && (shareResult?.bundleUrl || singleFile.directUrl)}
                    <div class="flex items-center p-0.5 bg-zinc-100 dark:bg-zinc-800/80 rounded-lg text-xs font-medium shrink-0 ml-auto sm:ml-0">
                      <button
                        onclick={() => shareLinkTarget = 'local'}
                        class="px-2 py-0.5 rounded-md transition-colors cursor-pointer flex items-center gap-1 {shareLinkTarget === 'local' ? 'bg-white dark:bg-zinc-900 text-emerald-600 dark:text-emerald-400 shadow-xs' : 'text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200'}"
                        title="Mostrar enlace y QR de Red Local"
                      >
                        <EthernetPort class="w-3 h-3" />
                        <span>Red Local</span>
                      </button>
                      <button
                        onclick={() => shareLinkTarget = 'internet'}
                        class="px-2 py-0.5 rounded-md transition-colors cursor-pointer flex items-center gap-1 {shareLinkTarget === 'internet' ? 'bg-white dark:bg-zinc-900 text-indigo-600 dark:text-indigo-400 shadow-xs' : 'text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200'}"
                        title="Mostrar enlace y QR de Internet"
                      >
                        <Globe class="w-3 h-3" />
                        <span>Internet</span>
                      </button>
                    </div>
                  {:else if shareResult?.networkMode === 'lan_only'}
                    <span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-emerald-50 dark:bg-emerald-950/30 text-emerald-600 dark:text-emerald-400 border border-emerald-200 dark:border-emerald-800/50 flex items-center gap-1 shrink-0 ml-auto sm:ml-0">
                      <EthernetPort class="w-3 h-3" />
                      <span>Red Local</span>
                    </span>
                  {/if}
                </div>
                <div class="flex items-center gap-2">
                  <input
                    type="text"
                    readonly
                    value={formatDisplayUrl(getActiveFileUrl(singleFile))}
                    class="flex-1 bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-lg px-3 py-2 text-xs font-mono text-zinc-800 dark:text-zinc-200 outline-none select-all"
                  />
                  <button
                    onclick={() => copyText(singleFile.id, getActiveFileUrl(singleFile))}
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
                    onclick={() => openQR(singleFile.name, singleFile.directUrl, singleFile.localUrl)}
                    class="p-2 border border-zinc-200 dark:border-zinc-800 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 rounded-lg transition-colors cursor-pointer shrink-0"
                    title="Código QR"
                  >
                    <QrCode class="w-4 h-4" />
                  </button>
                </div>
              </div>
            {:else}
              <!-- Multi-File: First General Bundle Link, then Individual Links -->
              <div class="flex flex-col gap-4">
                <!-- Subsection 1: General Bundle Link -->
                <div class="flex flex-col gap-2">
                  <div class="flex items-center justify-between gap-2.5 flex-wrap">
                    <span class="text-xs text-zinc-500 dark:text-zinc-400">
                      Enlace general para descargar todos los archivos:
                    </span>
                    {#if shareResult?.networkMode === 'both' && (shareResult?.bundleUrl || (shareResult?.files && shareResult.files[0]?.directUrl))}
                      <div class="flex items-center p-0.5 bg-zinc-100 dark:bg-zinc-800/80 rounded-lg text-xs font-medium shrink-0 ml-auto sm:ml-0">
                        <button
                          onclick={() => shareLinkTarget = 'local'}
                          class="px-2 py-0.5 rounded-md transition-colors cursor-pointer flex items-center gap-1 {shareLinkTarget === 'local' ? 'bg-white dark:bg-zinc-900 text-emerald-600 dark:text-emerald-400 shadow-xs' : 'text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200'}"
                          title="Mostrar enlaces y QR de Red Local"
                        >
                          <EthernetPort class="w-3 h-3" />
                          <span>Red Local</span>
                        </button>
                        <button
                          onclick={() => shareLinkTarget = 'internet'}
                          class="px-2 py-0.5 rounded-md transition-colors cursor-pointer flex items-center gap-1 {shareLinkTarget === 'internet' ? 'bg-white dark:bg-zinc-900 text-indigo-600 dark:text-indigo-400 shadow-xs' : 'text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-200'}"
                          title="Mostrar enlaces y QR de Internet"
                        >
                          <Globe class="w-3 h-3" />
                          <span>Internet</span>
                        </button>
                      </div>
                    {:else if shareResult?.networkMode === 'lan_only'}
                      <span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-emerald-50 dark:bg-emerald-950/30 text-emerald-600 dark:text-emerald-400 border border-emerald-200 dark:border-emerald-800/50 flex items-center gap-1 shrink-0 ml-auto sm:ml-0">
                        <EthernetPort class="w-3 h-3" />
                        <span>Red Local</span>
                      </span>
                    {/if}
                  </div>
                  <div class="border border-zinc-200 dark:border-zinc-800 rounded-lg overflow-hidden bg-zinc-50/50 dark:bg-zinc-950/50">
                    <div class="p-3 flex items-center justify-between gap-3 text-xs">
                      <div class="min-w-0 flex-1">
                        <div class="flex items-baseline gap-1.5 min-w-0 mb-0.5">
                          <span class="font-medium text-zinc-900 dark:text-zinc-100 truncate min-w-0">Todos los archivos</span>
                          <span class="font-normal font-mono text-zinc-400 text-xs shrink-0">({totalBundleSize})</span>
                        </div>
                        <div class="font-mono text-zinc-400 text-[11px] truncate">{formatDisplayUrl(getActiveBundleUrl())}</div>
                      </div>
                      <div class="flex items-center gap-1.5 shrink-0">
                        <button
                          onclick={() => copyText('bundle', getActiveBundleUrl())}
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
                          onclick={() => openQR('Todos los archivos', shareResult?.bundleUrl || '', shareResult?.localBundleUrl || '')}
                          class="p-1.5 border border-zinc-200 dark:border-zinc-700 hover:bg-zinc-200 dark:hover:bg-zinc-800 text-zinc-600 dark:text-zinc-400 rounded transition-colors cursor-pointer"
                          title="Código QR"
                        >
                          <QrCode class="w-3.5 h-3.5" />
                        </button>
                        <!-- Spacer matching the remove button in individual file items to eliminate layout shifts -->
                        <button
                          type="button"
                          tabindex="-1"
                          disabled
                          class="p-1 rounded shrink-0 invisible pointer-events-none select-none"
                          aria-hidden="true"
                        >
                          <X class="w-4 h-4" />
                        </button>
                      </div>
                    </div>
                  </div>
                </div>

                <!-- Subsection 2: Individual Links per File (Collapsible) -->
                <div class="flex flex-col">
                  <button
                    type="button"
                    onclick={() => isIndividualLinksExpanded = !isIndividualLinksExpanded}
                    class="flex items-center justify-between w-full text-xs text-zinc-500 hover:text-zinc-800 dark:text-zinc-400 dark:hover:text-zinc-200 transition-colors cursor-pointer group text-left select-none py-1"
                    aria-expanded={isIndividualLinksExpanded}
                  >
                    <div class="flex items-baseline gap-1.5">
                      <span>Enlaces directos por archivo</span>
                      <span class="text-xs text-zinc-400 dark:text-zinc-500">({shareResult?.files.length ?? 0})</span>
                    </div>
                    <div class="flex items-center gap-1 text-zinc-400 group-hover:text-zinc-600 dark:group-hover:text-zinc-300 transition-colors">
                      <span class="text-[11px] font-normal">{isIndividualLinksExpanded ? 'Ocultar' : 'Mostrar'}</span>
                      <ChevronDown class="w-3.5 h-3.5 transform transition-transform duration-250 ease-out {isIndividualLinksExpanded ? 'rotate-180' : 'rotate-0'}" />
                    </div>
                  </button>

                  <!-- Collapsible Content with smooth CSS Grid transition -->
                  <div
                    class="grid"
                    style="grid-template-rows: {isIndividualLinksExpanded ? '1fr' : '0fr'}; transition: grid-template-rows 250ms cubic-bezier(0.4, 0, 0.2, 1);"
                  >
                    <div class="min-h-0 overflow-hidden" inert={!isIndividualLinksExpanded ? true : undefined}>
                      <div class="pt-2">
                        <div
                          class="border border-zinc-200 dark:border-zinc-800 rounded-lg divide-y divide-zinc-100 dark:divide-zinc-800/60 overflow-hidden max-h-[220px] overflow-y-auto bg-zinc-50/50 dark:bg-zinc-950/50"
                          style="scrollbar-gutter: auto;"
                        >
                          {#each (shareResult?.files || []) as f}
                            <div class="p-3 flex items-center justify-between gap-3 text-xs">
                              <div class="min-w-0 flex-1">
                                <div class="flex items-baseline gap-1.5 min-w-0 mb-0.5">
                                  <span class="font-medium text-zinc-900 dark:text-zinc-100 truncate min-w-0">{f.name}</span>
                                  <span class="font-normal font-mono text-zinc-400 text-xs shrink-0">({f.size})</span>
                                </div>
                                <div class="font-mono text-zinc-400 text-[11px] truncate">{formatDisplayUrl(getActiveFileUrl(f))}</div>
                              </div>
                              <div class="flex items-center gap-1.5 shrink-0">
                                <button
                                  onclick={() => copyText(f.id, getActiveFileUrl(f))}
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
                                  onclick={() => openQR(f.name, f.directUrl, f.localUrl)}
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
                    </div>
                  </div>
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
      {:else}
        <!-- Receive Mode View -->
        <div
          class="w-full my-auto flex flex-col max-h-full min-h-0 shrink"
          style="gap: clamp(0.5rem, 1.8vh, 1rem);"
        >
          {#if isStartingReceive}
            <!-- Loading State: Initiating Tunnel -->
            <div
              class="w-full border-2 border-dashed border-zinc-300 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/30 text-center flex flex-col items-center justify-center min-h-0 max-h-full shrink"
              style="height: clamp(140px, 36vh, 260px); padding-top: clamp(0.5rem, 3vh, 2rem); padding-bottom: clamp(0.5rem, 3vh, 2rem); padding-left: 1.5rem; padding-right: 1.5rem;"
            >
              <div class="flex flex-col items-center justify-center max-w-sm shrink min-h-0">
                <div
                  class="rounded-full bg-zinc-100 dark:bg-zinc-800/80 text-zinc-600 dark:text-zinc-400 shrink-0 flex items-center justify-center"
                  style="padding: clamp(0.45rem, 1.8vh, 1rem); margin-bottom: clamp(0.35rem, 1.4vh, 0.75rem);"
                >
                  <span class="animate-spin h-7 w-7 sm:h-8 sm:w-8 border-2 border-zinc-300 border-t-zinc-900 dark:border-zinc-700 dark:border-t-zinc-100 rounded-full shrink-0"></span>
                </div>
                <p class="text-sm font-medium text-zinc-800 dark:text-zinc-200 text-center leading-snug" style="margin-bottom: clamp(0.2rem, 0.8vh, 0.35rem);">
                  Iniciando receptor de archivos...
                </p>
                <p class="text-xs text-zinc-500 dark:text-zinc-400 text-center leading-normal">
                  {#if configState.networkMode === 'lan_only'}
                    Iniciando receptor en la red local
                  {:else}
                    Generando enlace público y código QR con Cloudflare
                  {/if}
                </p>
              </div>
            </div>

            <!-- Action Bar: Reserved to match Send mode and eliminate any vertical shift -->
            <div class="shrink-0 flex justify-end">
              <button
                tabindex="-1"
                aria-hidden="true"
                class="w-36 justify-center rounded-lg bg-zinc-900 text-white text-xs font-medium flex items-center gap-2 invisible pointer-events-none"
                style="padding-top: clamp(0.4rem, 1.2vh, 0.625rem); padding-bottom: clamp(0.4rem, 1.2vh, 0.625rem);"
              >
                <CornerDownLeft class="w-4 h-4" />
                <span>Compartir</span>
              </button>
            </div>
          {:else if isReceiving && receiveResult}
            <!-- Active Receive Dashboard -->
            <div class="flex-1 flex flex-col gap-3 min-h-0 overflow-y-auto pr-0.5">
              <!-- Main QR & Link Card -->
              <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-4 flex flex-col gap-3.5 shrink-0 shadow-xs">
                <!-- Top Status Header & Stop Button -->
                <div class="flex items-center justify-between pb-2.5 border-b border-zinc-100 dark:border-zinc-800/60">
                  <div class="flex items-center gap-2">
                    <span class="h-2 w-2 rounded-full bg-emerald-500 animate-pulse"></span>
                    <span class="text-xs font-semibold text-zinc-800 dark:text-zinc-200">
                      Listo para recibir archivos
                    </span>
                  </div>

                  <button
                    onclick={handleStopReceive}
                    disabled={isStoppingReceive}
                    class="px-3 py-1 rounded-md text-xs font-medium text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-950/30 border border-red-200 dark:border-red-900/40 transition-colors cursor-pointer flex items-center gap-1 disabled:opacity-50 shrink-0"
                  >
                    {#if isStoppingReceive}
                      <span class="animate-spin h-3 w-3 border-2 border-red-200 border-t-red-600 dark:border-red-900/60 dark:border-t-red-400 rounded-full shrink-0"></span>
                      <span>Deteniendo...</span>
                    {:else}
                      <span>Detener</span>
                    {/if}
                  </button>
                </div>

                <!-- QR + URL Content -->
                <div class="flex flex-col sm:flex-row items-center gap-4">
                  <!-- QR Code Plate -->
                  <div class="shrink-0 flex flex-col items-center">
                    <button
                      type="button"
                      onclick={() => openQR('Cargar archivos', receiveResult?.uploadUrl || '', receiveResult?.localUploadUrl || '')}
                      class="bg-white dark:bg-zinc-950 p-2 rounded-xl border border-zinc-200 dark:border-zinc-800 shadow-xs cursor-pointer hover:border-zinc-400 dark:hover:border-zinc-600 transition-colors flex items-center justify-center text-zinc-950 dark:text-zinc-50"
                      title="Ampliar código QR"
                    >
                      {#if receiveQrSvg}
                        <div class="w-28 h-28 sm:w-32 sm:h-32 flex items-center justify-center">
                          {@html receiveQrSvg}
                        </div>
                      {/if}
                    </button>

                    <!-- Button to switch QR code if in 'both' mode -->
                    {#if receiveResult?.networkMode === 'both' && receiveResult?.uploadUrl}
                      <button
                        type="button"
                        onclick={toggleReceiveQrTarget}
                        class="mt-2 w-56 justify-center px-2.5 py-1 text-xs font-medium rounded-lg border border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 hover:bg-zinc-100 dark:hover:bg-zinc-900 text-zinc-700 dark:text-zinc-300 transition-colors flex items-center gap-1.5 cursor-pointer shadow-2xs whitespace-nowrap"
                        title={receiveQrTarget === 'internet' ? 'Cambiar a QR Red Local' : 'Cambiar a QR Internet'}
                      >
                        {#if receiveQrTarget === 'internet'}
                          <EthernetPort class="w-3.5 h-3.5 text-emerald-500" />
                          <span>Cambiar a QR Red Local</span>
                        {:else}
                          <Globe class="w-3.5 h-3.5 text-indigo-500" />
                          <span>Cambiar a QR Internet</span>
                        {/if}
                      </button>
                    {/if}
                  </div>

                  <!-- Details & Link -->
                  <div class="flex-1 flex flex-col gap-2.5 min-w-0 w-full">
                    <div class="flex flex-col gap-0.5">
                      <span class="text-xs font-medium text-zinc-800 dark:text-zinc-200">
                        Enlace para subir archivos:
                      </span>
                      <p class="text-[11px] text-zinc-500 dark:text-zinc-400">
                        Escanea el QR o abre este link en tu teléfono o navegador:
                      </p>
                    </div>

                    <div class="flex items-center gap-2">
                      <input
                        type="text"
                        readonly
                        value={formatDisplayUrl(getActiveReceiveUrl())}
                        class="flex-1 bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 rounded-lg px-3 py-1.5 text-xs font-mono text-zinc-800 dark:text-zinc-200 outline-none select-all min-w-0"
                      />
                      <button
                        onclick={() => copyText('receive_url', getActiveReceiveUrl())}
                        class="w-[5.5rem] justify-center py-1.5 bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 text-xs font-medium rounded-lg transition-colors cursor-pointer flex items-center gap-1.5 shrink-0"
                      >
                        {#if copiedMap['receive_url']}
                          <Check class="w-3.5 h-3.5" />
                          <span>Copiado</span>
                        {:else}
                          <Copy class="w-3.5 h-3.5" />
                          <span>Copiar</span>
                        {/if}
                      </button>
                    </div>

                    <!-- Destination Directory Info -->
                    <div class="flex items-center justify-between gap-2 p-2 rounded-lg bg-zinc-50 dark:bg-zinc-950 border border-zinc-200 dark:border-zinc-800 text-[11px]">
                      <div class="flex items-center gap-1.5 min-w-0 flex-1 text-zinc-600 dark:text-zinc-400">
                        <Folder class="w-3.5 h-3.5 shrink-0 text-zinc-400" />
                        <span class="truncate leading-none" title={receiveResult.downloadDir}>
                          Guardando en: <strong class="text-zinc-800 dark:text-zinc-200 font-mono font-medium">{receiveResult.downloadDir}</strong>
                        </span>
                      </div>
                      <div class="flex items-center shrink-0">
                        <button
                          type="button"
                          onclick={handleSelectDownloadDir}
                          class="px-2 py-0.5 rounded text-[11px] font-medium text-zinc-700 dark:text-zinc-300 hover:bg-zinc-200 dark:hover:bg-zinc-800 transition-colors cursor-pointer"
                          title="Cambiar carpeta de descarga"
                        >
                          Cambiar
                        </button>
                      </div>
                    </div>
                  </div>
                </div>
              </div>

              <!-- Active Uploads (in progress) -->
              {#if Object.keys(activeUploads).length > 0}
                <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 p-3.5 flex flex-col gap-2 shrink-0 shadow-xs">
                  <div class="text-xs font-semibold text-zinc-800 dark:text-zinc-200">
                    Recibiendo en este momento:
                  </div>
                  <div class="divide-y divide-zinc-100 dark:divide-zinc-800/60">
                    {#each Object.values(activeUploads) as upload (upload.id)}
                      <div class="py-1.5 flex flex-col gap-1">
                        <div class="flex items-center justify-between text-xs">
                          <span class="font-medium text-zinc-800 dark:text-zinc-200 truncate">{upload.fileName}</span>
                          <span class="text-xs font-mono text-emerald-600 dark:text-emerald-400">
                            {upload.progress.toFixed(0)}% · {formatSpeed(upload.speed)}
                          </span>
                        </div>
                        <div class="w-full h-1.5 bg-zinc-100 dark:bg-zinc-800 rounded-full overflow-hidden">
                          <div
                            class="h-full bg-emerald-500 rounded-full transition-all duration-150"
                            style="width: {upload.progress}%"
                          ></div>
                        </div>
                      </div>
                    {/each}
                  </div>
                </div>
              {/if}

              <!-- Received Files List -->
              <div class="border border-zinc-200 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/40 overflow-hidden flex flex-col shrink-0 shadow-xs">
                <div class="px-4 py-2.5 border-b border-zinc-200 dark:border-zinc-800 flex items-center justify-between text-xs">
                  <span class="font-medium text-zinc-500 dark:text-zinc-400">
                    Archivos recibidos en esta sesión ({receivedFiles.length})
                  </span>
                </div>

                {#if receivedFiles.length === 0}
                  <div class="py-8 text-center text-xs text-zinc-400 dark:text-zinc-500">
                    Aún no se han recibido archivos. Los archivos cargados desde el enlace se guardarán automáticamente en tu carpeta de descargas.
                  </div>
                {:else}
                  <div class="divide-y divide-zinc-100 dark:divide-zinc-800/60 max-h-[220px] overflow-y-auto">
                    {#each receivedFiles as rf (rf.id)}
                      <div class="px-3.5 py-2.5 flex flex-col gap-1.5 hover:bg-zinc-50/50 dark:hover:bg-zinc-800/30 transition-colors">
                        <div class="flex items-center justify-between gap-3 text-xs">
                          <div class="flex items-center gap-2.5 min-w-0 flex-1">
                            <div class="w-7 h-7 rounded-lg bg-emerald-50 dark:bg-emerald-950/40 border border-emerald-200 dark:border-emerald-900/40 flex items-center justify-center text-emerald-600 dark:text-emerald-400 shrink-0">
                              <Check class="w-3.5 h-3.5" />
                            </div>
                            <div class="flex flex-col min-w-0">
                              <span class="font-medium text-zinc-900 dark:text-zinc-100 truncate" title={rf.name}>{rf.name}</span>
                              <span class="text-[11px] font-mono text-zinc-400">{rf.size} · {rf.completedAt}</span>
                            </div>
                          </div>

                          <div class="flex items-center gap-1.5 shrink-0">
                            <button
                              type="button"
                              onclick={() => handleOpenFile(rf)}
                              class="px-2.5 py-1 bg-zinc-100 dark:bg-zinc-800 hover:bg-zinc-200 dark:hover:bg-zinc-700 text-zinc-800 dark:text-zinc-200 rounded text-xs font-medium transition-colors cursor-pointer"
                              title="Abrir archivo"
                            >
                              Abrir
                            </button>
                            <button
                              type="button"
                              onclick={() => handleShowFileInFolder(rf)}
                              class="p-1 text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200 rounded transition-colors cursor-pointer"
                              title="Mostrar en carpeta"
                            >
                              <FolderOpen class="w-3.5 h-3.5" />
                            </button>
                          </div>
                        </div>

                        {#if itemWarnings[rf.id]}
                          <div class="flex items-center gap-1.5 text-[11px] text-amber-600 dark:text-amber-400 bg-amber-50 dark:bg-amber-950/40 border border-amber-200 dark:border-amber-900/40 rounded px-2 py-1">
                            <AlertCircle class="w-3.5 h-3.5 shrink-0" />
                            <span>{itemWarnings[rf.id]}</span>
                          </div>
                        {/if}
                      </div>
                    {/each}
                  </div>
                {/if}
              </div>
            </div>
          {:else}
            <!-- Stopped / Inactive State -->
            <div
              class="w-full border-2 border-dashed border-zinc-300 dark:border-zinc-800 rounded-xl bg-white dark:bg-zinc-900/30 text-center flex flex-col items-center justify-center min-h-0 max-h-full shrink"
              style="height: clamp(140px, 36vh, 260px); padding-top: clamp(0.5rem, 3vh, 2rem); padding-bottom: clamp(0.5rem, 3vh, 2rem); padding-left: 1.5rem; padding-right: 1.5rem;"
            >
              <div class="flex flex-col items-center justify-center max-w-sm shrink min-h-0">
                <div
                  class="rounded-full bg-zinc-100 dark:bg-zinc-800/80 text-zinc-500 dark:text-zinc-400 shrink-0"
                  style="padding: clamp(0.45rem, 1.8vh, 1rem); margin-bottom: clamp(0.35rem, 1.4vh, 0.75rem);"
                >
                  <Download class="w-7 h-7 sm:w-8 sm:h-8" />
                </div>
                <p class="text-sm font-medium text-zinc-800 dark:text-zinc-200 text-center leading-snug" style="margin-bottom: clamp(0.2rem, 0.8vh, 0.35rem);">
                  Recepción inactiva
                </p>
                <p class="text-xs text-zinc-500 dark:text-zinc-400 text-center leading-normal">
                  Inicia el receptor para generar un enlace y código QR donde otros dispositivos puedan enviarte archivos.
                </p>
              </div>
            </div>

            <!-- Action Bar: Iniciar recepción button matching Send mode action bar -->
            <div class="shrink-0 flex justify-end">
              <button
                type="button"
                onclick={handleStartReceive}
                class="w-36 justify-center rounded-lg bg-zinc-900 hover:bg-zinc-800 dark:bg-zinc-100 dark:hover:bg-zinc-200 text-white dark:text-zinc-900 text-xs font-medium transition-colors cursor-pointer flex items-center gap-2 shadow-xs"
                style="padding-top: clamp(0.4rem, 1.2vh, 0.625rem); padding-bottom: clamp(0.4rem, 1.2vh, 0.625rem);"
              >
                <CornerDownLeft class="w-4 h-4" />
                <span>Iniciar</span>
              </button>
            </div>
          {/if}
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
            <div class="flex items-center gap-2 min-w-0 pr-2">
              <h3 class="text-xs sm:text-sm font-semibold truncate text-zinc-900 dark:text-zinc-100">{qrModal.title}</h3>
              {#if qrModal.activeTarget === 'local'}
                <span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-emerald-50 dark:bg-emerald-950/30 text-emerald-600 dark:text-emerald-400 border border-emerald-200 dark:border-emerald-800/50 flex items-center gap-1 shrink-0">
                  <EthernetPort class="w-3 h-3" />
                  <span>Red Local</span>
                </span>
              {:else}
                <span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-indigo-50 dark:bg-indigo-950/30 text-indigo-600 dark:text-indigo-400 border border-indigo-200 dark:border-indigo-800/50 flex items-center gap-1 shrink-0">
                  <Globe class="w-3 h-3" />
                  <span>Internet</span>
                </span>
              {/if}
            </div>
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
          <div class="bg-white dark:bg-zinc-950 p-4 flex flex-col items-center justify-center w-full flex-1 text-zinc-950 dark:text-zinc-50">
            <div
              style="height: clamp(160px, 60vh, 320px); width: clamp(160px, 60vh, 320px);"
              class="aspect-square select-none pointer-events-none flex items-center justify-center"
            >
              {#if qrModal.qrSvg}
                {@html qrModal.qrSvg}
              {:else if qrModal.qrDataUrl}
                <img
                  src={qrModal.qrDataUrl}
                  alt="QR Code"
                  class="w-full h-full object-contain"
                />
              {/if}
            </div>

            <!-- Toggle QR Target button if both exist -->
            {#if qrModal.canToggle}
              <button
                type="button"
                onclick={toggleModalQrTarget}
                class="mt-3 w-56 justify-center px-3 py-1.5 text-xs font-medium rounded-lg border border-zinc-200 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-900 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-700 dark:text-zinc-300 transition-colors flex items-center gap-1.5 cursor-pointer shadow-xs whitespace-nowrap shrink-0"
                title={qrModal.activeTarget === 'internet' ? 'Cambiar a QR Red Local' : 'Cambiar a QR Internet'}
              >
                {#if qrModal.activeTarget === 'internet'}
                  <EthernetPort class="w-3.5 h-3.5 text-emerald-500" />
                  <span>Cambiar a QR Red Local</span>
                {:else}
                  <Globe class="w-3.5 h-3.5 text-indigo-500" />
                  <span>Cambiar a QR Internet</span>
                {/if}
              </button>
            {/if}
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
                <span class="animate-spin h-3.5 w-3.5 border-2 border-white/30 border-t-white rounded-full shrink-0"></span>
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

    <!-- Cloudflared Download Modal Dialog -->
    {#if cloudflaredDownloadProgress.show}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Descargando Cloudflared"
        tabindex="-1"
        class="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center p-4 z-50 select-none overflow-hidden"
      >
        <div
          class="bg-white dark:bg-zinc-900 border border-zinc-200 dark:border-zinc-800 rounded-2xl shadow-2xl p-5 max-w-sm w-full flex flex-col gap-4 animate-in fade-in zoom-in-95 duration-150"
        >
          <!-- Header -->
          <div class="flex items-start gap-3.5">
            <div class="w-10 h-10 rounded-full bg-blue-50 dark:bg-blue-950/50 border border-blue-200 dark:border-blue-900/40 flex items-center justify-center text-blue-600 dark:text-blue-400 shrink-0">
              <Download class="w-5 h-5" />
            </div>
            <div class="min-w-0 flex-1">
              <h3 class="text-sm font-semibold text-zinc-900 dark:text-zinc-100 mb-1">
                Descargando Cloudflare Tunnel
              </h3>
              <p class="text-xs text-zinc-500 dark:text-zinc-400 leading-relaxed">
                Requerido para generar el túnel público. Solo se descarga la primera vez.
              </p>
            </div>
          </div>

          <!-- Progress Bar & Statistics -->
          <div class="flex flex-col gap-2 pt-1">
            <!-- Barra de progreso -->
            <div class="w-full bg-zinc-100 dark:bg-zinc-800 rounded-full h-3 overflow-hidden p-0.5 border border-zinc-200 dark:border-zinc-700">
              <div
                class="bg-emerald-500 dark:bg-emerald-400 h-full rounded-full transition-all duration-150 ease-out"
                style="width: {cloudflaredDownloadProgress.percent}%;"
              ></div>
            </div>

            <!-- Datos de progreso -->
            <div class="flex items-center justify-between text-xs text-zinc-500 dark:text-zinc-400 font-mono">
              <div class="flex items-center gap-1 font-medium text-zinc-700 dark:text-zinc-300">
                <span class="font-bold">{cloudflaredDownloadProgress.percent.toFixed(0)}%</span>
                {#if cloudflaredDownloadProgress.total > 0}
                  <span class="text-zinc-400 font-normal">({formatBytes(cloudflaredDownloadProgress.downloaded)} / {formatBytes(cloudflaredDownloadProgress.total)})</span>
                {:else}
                  <span class="text-zinc-400 font-normal">({formatBytes(cloudflaredDownloadProgress.downloaded)})</span>
                {/if}
              </div>
              <div>
                {#if cloudflaredDownloadProgress.speed > 0}
                  <span>{formatSpeed(cloudflaredDownloadProgress.speed)}</span>
                {/if}
              </div>
            </div>
          </div>

          <!-- Footer Buttons -->
          <div class="flex items-center justify-end gap-2 pt-1 border-t border-zinc-100 dark:border-zinc-800">
            <button
              onclick={handleCancelCloudflaredDownload}
              class="px-3.5 py-1.5 border border-zinc-200 dark:border-zinc-700 hover:bg-zinc-100 dark:hover:bg-zinc-800 text-zinc-700 dark:text-zinc-300 font-medium rounded-lg text-xs transition-colors cursor-pointer"
            >
              Cancelar
            </button>
          </div>
        </div>
      </div>
    {/if}
  </div>
</div>
