// GoLoadix frontend logic
const go = window.go?.main?.App;

let downloads = {};
let currentTab = 'downloads';
let settingsData = {};
let speedLimitEnabled = false;
let dropdownHideTimer = null;

// ---- Init ----
window.addEventListener('DOMContentLoaded', async () => {
  await loadSettings();
  await refreshDownloads();
  updateDiskSpace();
  setInterval(updateDiskSpace, 10000);

  // Listen for real-time download updates
  window.runtime?.EventsOn('downloads:update', (list) => {
    downloads = {};
    (list || []).forEach(d => { downloads[d.id] = d; });
    renderDownloads();
    updateFooter();
  });

  // Global connection speed dropdown — keep visible when mouse moves into it
  const dd = document.getElementById('conn-speed-dropdown');
  dd.addEventListener('mouseenter', () => { clearTimeout(dropdownHideTimer); });
  dd.addEventListener('mouseleave', scheduleHideDropdown);

  // Update connection track width on slider change
  const slider = document.getElementById('connections-slider');
  slider.addEventListener('input', () => {
    const pct = ((slider.value - 1) / 127) * 100;
    document.getElementById('conn-track').style.width = pct + '%';
  });
});

async function loadSettings() {
  try {
    settingsData = await go.GetSettings();
    document.getElementById('save-path-input').value = settingsData.defaultSavePath || '';
    document.getElementById('settings-save-path').value = settingsData.defaultSavePath || '';
    document.getElementById('settings-connections').value = String(settingsData.maxConnections || 16);
    document.getElementById('settings-max-downloads').value = String(settingsData.maxDownloads || 1);
    document.getElementById('settings-speed-limit').value = settingsData.speedLimitMBps || 25;
    speedLimitEnabled = !!settingsData.speedLimitEnabled;
    updateSpeedToggleUI();
  } catch (e) { console.error('loadSettings', e); }
}

async function refreshDownloads() {
  try {
    const list = await go.GetDownloads();
    downloads = {};
    (list || []).forEach(d => { downloads[d.id] = d; });
    renderDownloads();
    updateFooter();
  } catch (e) { console.error('refreshDownloads', e); }
}

// ---- Render ----
function renderDownloads() {
  const list = Object.values(downloads);
  const container = document.getElementById('download-list');
  const empty = document.getElementById('empty-state');

  let filtered = list;
  if (currentTab === 'downloads') {
    filtered = list.filter(d => d.status !== 'Completed');
    document.getElementById('page-title').textContent = 'Active Queue';
    const active = list.filter(d => d.status === 'Downloading').length;
    document.getElementById('page-subtitle').textContent =
      active > 0 ? `Managing ${active} active connection${active > 1 ? 's' : ''}` : 'No active downloads';
  } else if (currentTab === 'completed') {
    filtered = list.filter(d => d.status === 'Completed');
    document.getElementById('page-title').textContent = 'Completed';
    document.getElementById('page-subtitle').textContent = `${filtered.length} completed download${filtered.length !== 1 ? 's' : ''}`;
  } else {
    filtered = [];
    document.getElementById('page-title').textContent = 'Scheduled';
    document.getElementById('page-subtitle').textContent = 'No scheduled downloads';
  }

  empty.style.display = filtered.length === 0 ? 'flex' : 'none';

  const filteredIds = new Set(filtered.map(d => d.id));

  // Remove cards no longer in the filtered list
  container.querySelectorAll('.download-card').forEach(el => {
    if (!filteredIds.has(el.dataset.id)) el.remove();
  });

  // Add new cards or patch existing ones — never replace a card that already exists
  filtered.forEach(d => {
    const existing = container.querySelector(`.download-card[data-id="${d.id}"]`);
    if (existing) {
      patchCard(existing, d);
    } else {
      container.appendChild(buildCard(d));
    }
  });
}

// Update only the dynamic parts of a card that are already in the DOM
function patchCard(el, d) {
  const isActive = d.status === 'Downloading';
  const isPaused = d.status === 'Paused';
  const isCompleted = d.status === 'Completed';
  const progress = d.progress || 0;

  // Progress bar fill
  const fill = el.querySelector('.card-progress-fill');
  if (fill) fill.style.width = progress.toFixed(1) + '%';

  // Speed
  const speedEl = el.querySelector('.card-speed');
  if (speedEl) speedEl.textContent = isActive ? formatSpeed(d.speed) : '0 B/s';

  // ETA
  const etaEl = el.querySelector('.card-eta');
  if (etaEl) etaEl.textContent = d.eta || '--';

  // Downloaded / total
  const sizeEl = el.querySelector('.card-size');
  if (sizeEl) sizeEl.textContent = `${formatBytes(d.downloaded)} / ${formatBytes(d.totalSize)}`;

  // Connections badge
  const connEl = el.querySelector('.card-connections');
  if (connEl) connEl.textContent = `${d.activeConnections || 0}/${d.connections} conn.`;

  // If global dropdown is showing this card's data, refresh it live
  const ddEl = document.getElementById('conn-speed-dropdown');
  if (!ddEl.classList.contains('hidden') && ddEl.dataset.cardId === d.id) {
    document.getElementById('conn-speed-dropdown-list').innerHTML = buildConnSpeedList(d);
  }

  // Status badge — only rebuild when status changes
  if (el.dataset.status !== d.status) {
    el.dataset.status = d.status;
    const badgeEl = el.querySelector('.card-status-badge');
    if (badgeEl) badgeEl.innerHTML = buildStatusBadge(d);

    // Action buttons change on status transition
    const actionsEl = el.querySelector('.card-actions');
    if (actionsEl) actionsEl.innerHTML = buildActionButtons(d);

    // Opacity for paused
    el.classList.toggle('opacity-70', isPaused);

    // Progress bar style swap (gradient ↔ grey ↔ green)
    const track = el.querySelector('.card-progress-track');
    if (track) track.innerHTML = buildProgressFill(d);
  }
}

function buildConnSpeedList(d) {
  const speeds = d.connectionSpeeds;
  if (!speeds || speeds.length === 0) {
    return `<span class="text-xs text-on-surface-variant opacity-40">No data yet</span>`;
  }
  return speeds.map((s, i) => {
    const active = s > 0;
    return `<div class="flex justify-between items-center gap-4">
      <span class="text-[11px] text-on-surface-variant opacity-70">Conn ${i + 1}</span>
      <span class="text-[11px] font-medium tabular-nums ${active ? 'text-primary' : 'text-on-surface-variant opacity-40'}">${formatSpeed(s)}</span>
    </div>`;
  }).join('');
}

function buildStatusBadge(d) {
  switch (d.status) {
    case 'Downloading':
      return `<span class="bg-primary/10 text-primary px-3 py-1 rounded-full text-xs font-bold border border-primary/20 flex items-center gap-1"><div class="w-1.5 h-1.5 rounded-full bg-primary animate-pulse"></div>Downloading</span>`;
    case 'Paused':
      return `<span class="bg-tertiary/10 text-tertiary px-3 py-1 rounded-full text-xs font-bold border border-tertiary/20 flex items-center gap-1"><span class="material-symbols-outlined text-[12px]">pause</span>Paused</span>`;
    case 'Completed':
      return `<span class="bg-green-500/10 text-green-400 px-3 py-1 rounded-full text-xs font-bold border border-green-500/20 flex items-center gap-1"><span class="material-symbols-outlined text-[12px]" style="font-variation-settings:'FILL' 1;">check_circle</span>Completed</span>`;
    case 'Error':
      return `<span class="bg-error/10 text-error px-3 py-1 rounded-full text-xs font-bold border border-error/20 flex items-center gap-1"><span class="material-symbols-outlined text-[12px]">error</span>Error</span>`;
    default:
      return `<span class="bg-outline/10 text-on-surface-variant px-3 py-1 rounded-full text-xs font-bold border border-outline/20">Queued</span>`;
  }
}

function buildProgressFill(d) {
  const progress = (d.progress || 0).toFixed(1);
  if (d.status === 'Completed') {
    return `<div class="h-full bg-green-500/70 rounded-full card-progress-fill" style="width:100%"></div>`;
  } else if (d.status === 'Paused') {
    return `<div class="h-full bg-surface-variant rounded-full card-progress-fill" style="width:${progress}%"></div>`;
  }
  return `<div class="h-full bg-gradient-to-r from-primary to-primary-container rounded-full relative card-progress-fill" style="width:${progress}%; box-shadow:0 0 8px rgba(76,214,251,0.5);"><div class="absolute inset-0 bg-white/20"></div></div>`;
}

function buildActionButtons(d) {
  const safeId = d.id;
  const safePath = (d.savePath || '').replace(/\\/g, '\\\\');
  let html = '';
  if (d.status === 'Downloading') {
    html += `<button onclick="pauseDownload('${safeId}')" class="text-on-surface-variant hover:text-primary transition-colors p-1.5 rounded-lg hover:bg-surface-container-high" title="Pause"><span class="material-symbols-outlined text-xl">pause</span></button>`;
  } else if (d.status === 'Paused' || d.status === 'Error') {
    html += `<button onclick="resumeDownload('${safeId}')" class="text-on-surface-variant hover:text-primary transition-colors p-1.5 rounded-lg hover:bg-surface-container-high" title="Resume"><span class="material-symbols-outlined text-xl">play_arrow</span></button>`;
  }
  if (d.status === 'Completed') {
    html += `<button onclick="openFolder('${safePath}')" class="text-on-surface-variant hover:text-primary transition-colors p-1.5 rounded-lg hover:bg-surface-container-high" title="Open folder"><span class="material-symbols-outlined text-xl">folder_open</span></button>`;
  }
  html += `<button onclick="cancelDownload('${safeId}')" class="text-on-surface-variant hover:text-error transition-colors p-1.5 rounded-lg hover:bg-surface-container-high" title="Remove"><span class="material-symbols-outlined text-xl">delete</span></button>`;
  return html;
}

function buildCard(d) {
  const isActive = d.status === 'Downloading';
  const isPaused = d.status === 'Paused';
  const isCompleted = d.status === 'Completed';
  const icon = fileIcon(d.filename);
  const speedStr = isActive ? formatSpeed(d.speed) : '0 B/s';
  const cardBg = isCompleted
    ? 'bg-surface-container-lowest border-outline-variant/10'
    : 'bg-surface-container-low/60 backdrop-blur-[20px] border-outline-variant/15';

  const div = document.createElement('div');
  div.className = `download-card ${cardBg} rounded-xl p-6 border hover:bg-surface-container-high/60 transition-colors duration-200 relative overflow-hidden group ${isPaused ? 'opacity-70' : ''}`;
  div.dataset.id = d.id;
  div.dataset.status = d.status;
  div.innerHTML = `
    <div class="flex flex-col gap-4 relative z-10">
      <div class="flex justify-between items-start">
        <div class="flex items-center gap-4 min-w-0 flex-1">
          <div class="w-12 h-12 shrink-0 rounded-lg bg-surface-container-highest flex items-center justify-center ${isCompleted ? 'text-green-400' : 'text-primary'} border border-outline-variant/15 shadow-[0_8px_16px_rgba(0,0,0,0.2)]">
            <span class="material-symbols-outlined text-2xl">${icon}</span>
          </div>
          <div class="min-w-0 flex-1">
            <h3 class="text-lg font-bold text-on-surface tracking-tight truncate">${escapeHtml(d.filename)}</h3>
            <div class="flex items-center gap-3 text-xs text-on-surface-variant mt-1 font-label flex-wrap">
              <span class="card-size flex items-center gap-1"><span class="material-symbols-outlined text-[14px]">hard_drive</span>${formatBytes(d.downloaded)} / ${formatBytes(d.totalSize)}</span>
              <span class="text-outline-variant">•</span>
              <span class="card-speed flex items-center gap-1 text-primary"><span class="material-symbols-outlined text-[14px]">speed</span>${speedStr}</span>
              ${!isCompleted ? `<span class="text-outline-variant">•</span><span class="card-eta flex items-center gap-1"><span class="material-symbols-outlined text-[14px]">schedule</span>${d.eta || '--'}</span>` : ''}
              ${d.error ? `<span class="text-error ml-1">${escapeHtml(d.error)}</span>` : ''}
            </div>
          </div>
        </div>
        <div class="flex flex-col items-end gap-2 shrink-0 ml-4">
          <div class="card-status-badge">${buildStatusBadge(d)}</div>
          <span class="card-connections text-xs text-on-surface-variant font-medium bg-surface-container-highest px-2 py-0.5 rounded border border-outline-variant/15 cursor-default select-none">${d.activeConnections || 0}/${d.connections} conn.</span>
          <div class="card-actions flex items-center gap-1 mt-1">${buildActionButtons(d)}</div>
        </div>
      </div>
      <div class="card-progress-track w-full h-2 bg-surface-container-highest rounded-full overflow-hidden border border-outline-variant/15">${buildProgressFill(d)}</div>
    </div>
    ${isActive ? '<div class="absolute right-0 top-1/2 -translate-y-1/2 w-64 h-64 bg-primary/5 rounded-full blur-[60px] pointer-events-none group-hover:bg-primary/10 transition-colors"></div>' : ''}
  `;

  div.addEventListener('mouseenter', () => {
    clearTimeout(dropdownHideTimer);
    const current = downloads[div.dataset.id];
    if (current && current.status !== 'Completed') showConnDropdown(div, current);
  });
  div.addEventListener('mouseleave', scheduleHideDropdown);

  return div;
}

function updateFooter() {
  const list = Object.values(downloads);
  const active = list.filter(d => d.status === 'Downloading');
  const totalSpeed = active.reduce((s, d) => s + (d.speed || 0), 0);
  document.getElementById('active-count').textContent = `${active.length} Active Task${active.length !== 1 ? 's' : ''}`;
  document.getElementById('global-speed').textContent = formatSpeed(totalSpeed);
}

async function updateDiskSpace() {
  try {
    const space = await go.GetDiskSpace();
    document.getElementById('disk-space').textContent = space;
  } catch {}
}

// ---- Actions ----
async function startDownload() {
  const url = document.getElementById('url-input').value.trim();
  const filename = document.getElementById('filename-input').value.trim();
  const savePath = document.getElementById('save-path-input').value.trim();
  const connections = parseInt(document.getElementById('connections-slider').value);
  const errEl = document.getElementById('modal-error');
  errEl.classList.add('hidden');
  errEl.textContent = '';

  if (!url) { showModalError('Please enter a URL'); return; }

  try {
    await go.StartDownload({ url, filename, savePath, connections });
    closeModal();
    document.getElementById('url-input').value = '';
    document.getElementById('filename-input').value = '';
    if (currentTab !== 'downloads') showTab('downloads');
  } catch (e) {
    showModalError(e.message || String(e));
  }
}

function showModalError(msg) {
  const el = document.getElementById('modal-error');
  el.textContent = msg;
  el.classList.remove('hidden');
}

async function pauseDownload(id) {
  try { await go.PauseDownload(id); } catch (e) { console.error(e); }
}

async function resumeDownload(id) {
  try { await go.ResumeDownload(id); } catch (e) { console.error(e); }
}

async function cancelDownload(id) {
  try { await go.CancelDownload(id); } catch (e) { console.error(e); }
}

async function pauseAll() {
  const active = Object.values(downloads).filter(d => d.status === 'Downloading');
  for (const d of active) {
    try { await go.PauseDownload(d.id); } catch {}
  }
}

async function openFolder(path) {
  try { await go.OpenFolder(path); } catch {}
}

async function browsePath() {
  try {
    const dir = await go.BrowseFolder();
    if (dir) document.getElementById('save-path-input').value = dir;
  } catch {}
}

async function browseSettingsPath() {
  try {
    const dir = await go.BrowseFolder();
    if (dir) document.getElementById('settings-save-path').value = dir;
  } catch {}
}

async function saveSettings() {
  const s = {
    defaultSavePath: document.getElementById('settings-save-path').value,
    maxConnections: parseInt(document.getElementById('settings-connections').value),
    maxDownloads: parseInt(document.getElementById('settings-max-downloads').value) || 1,
    speedLimitEnabled: speedLimitEnabled,
    speedLimitMBps: parseFloat(document.getElementById('settings-speed-limit').value) || 0,
    theme: 'dark',
  };
  try {
    await go.SaveSettings(s);
    settingsData = s;
    document.getElementById('save-path-input').value = s.defaultSavePath;
    closeSettings();
  } catch (e) { alert('Failed to save settings: ' + e); }
}

// ---- Connection speed dropdown ----
function showConnDropdown(cardEl, d) {
  const dd = document.getElementById('conn-speed-dropdown');
  document.getElementById('conn-speed-dropdown-list').innerHTML = buildConnSpeedList(d);
  dd.dataset.cardId = d.id;
  const rect = cardEl.getBoundingClientRect();
  dd.style.left = rect.left + 'px';
  dd.style.top = (rect.bottom + 6) + 'px';
  dd.style.width = rect.width + 'px';
  dd.classList.remove('hidden');
}

function hideConnDropdown() {
  const dd = document.getElementById('conn-speed-dropdown');
  dd.classList.add('hidden');
  dd.dataset.cardId = '';
}

function scheduleHideDropdown() {
  dropdownHideTimer = setTimeout(hideConnDropdown, 120);
}

// ---- UI helpers ----
function showTab(tab) {
  currentTab = tab;
  document.querySelectorAll('nav a').forEach(a => {
    a.className = a.textContent.trim().toLowerCase() === tab
      ? 'tab-active pb-1 font-bold text-lg cursor-pointer'
      : 'tab-inactive hover:text-[#4cd6fb] transition-colors font-bold text-lg cursor-pointer';
  });
  renderDownloads();
}

function openModal() {
  document.getElementById('modal').classList.add('open');
  document.getElementById('url-input').focus();
}

function closeModal() {
  document.getElementById('modal').classList.remove('open');
  document.getElementById('modal-error').classList.add('hidden');
}

function openSettings() {
  document.getElementById('settings-panel').classList.add('open');
  document.getElementById('settings-save-path').value = settingsData.defaultSavePath || '';
  document.getElementById('settings-connections').value = String(settingsData.maxConnections || 16);
  document.getElementById('settings-max-downloads').value = String(settingsData.maxDownloads || 1);
  document.getElementById('settings-speed-limit').value = settingsData.speedLimitMBps || 25;
  speedLimitEnabled = !!settingsData.speedLimitEnabled;
  updateSpeedToggleUI();
}

function closeSettings() {
  document.getElementById('settings-panel').classList.remove('open');
}

function setSettingsTab(tab) {
  document.getElementById('settings-general').classList.toggle('hidden', tab !== 'general');
  document.getElementById('settings-network').classList.toggle('hidden', tab !== 'network');
  document.getElementById('tab-general').className = tab === 'general'
    ? 'w-full flex items-center gap-3 px-4 py-3 bg-surface-container-highest text-primary rounded-xl font-medium transition-colors'
    : 'w-full flex items-center gap-3 px-4 py-3 text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high rounded-xl font-medium transition-colors';
  document.getElementById('tab-network').className = tab === 'network'
    ? 'w-full flex items-center gap-3 px-4 py-3 bg-surface-container-highest text-primary rounded-xl font-medium transition-colors'
    : 'w-full flex items-center gap-3 px-4 py-3 text-on-surface-variant hover:text-on-surface hover:bg-surface-container-high rounded-xl font-medium transition-colors';
}

function toggleSpeedLimit() {
  speedLimitEnabled = !speedLimitEnabled;
  updateSpeedToggleUI();
}

function updateSpeedToggleUI() {
  const btn = document.getElementById('speed-toggle');
  const knob = document.getElementById('speed-toggle-knob');
  if (speedLimitEnabled) {
    btn.classList.remove('bg-surface-container-highest');
    btn.classList.add('bg-primary');
    knob.classList.remove('bg-on-surface-variant');
    knob.classList.add('bg-on-primary', 'translate-x-5');
    btn.setAttribute('aria-checked', 'true');
  } else {
    btn.classList.add('bg-surface-container-highest');
    btn.classList.remove('bg-primary');
    knob.classList.add('bg-on-surface-variant');
    knob.classList.remove('bg-on-primary', 'translate-x-5');
    btn.setAttribute('aria-checked', 'false');
  }
}

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') { closeModal(); closeSettings(); }
  if ((e.ctrlKey || e.metaKey) && e.key === 'n') { e.preventDefault(); openModal(); }
});

// ---- Utilities ----
function formatBytes(b) {
  if (!b || b <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(b) / Math.log(1024));
  return (b / Math.pow(1024, i)).toFixed(1) + ' ' + units[i];
}

function formatSpeed(bps) {
  if (!bps || bps <= 0) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  const i = Math.floor(Math.log(bps) / Math.log(1024));
  return (bps / Math.pow(1024, i)).toFixed(1) + ' ' + units[i];
}

function fileIcon(filename) {
  const ext = (filename || '').split('.').pop().toLowerCase();
  const map = {
    mp4: 'movie', mkv: 'movie', avi: 'movie', mov: 'movie', webm: 'movie',
    mp3: 'audio_file', flac: 'audio_file', wav: 'audio_file', aac: 'audio_file',
    zip: 'folder_zip', rar: 'folder_zip', '7z': 'folder_zip', tar: 'folder_zip', gz: 'folder_zip',
    pdf: 'picture_as_pdf',
    jpg: 'image', jpeg: 'image', png: 'image', gif: 'image', webp: 'image', svg: 'image',
    exe: 'terminal', msi: 'terminal', deb: 'terminal',
    iso: 'album', img: 'album',
  };
  return map[ext] || 'description';
}

function escapeHtml(str) {
  return String(str || '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
