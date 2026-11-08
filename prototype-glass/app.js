/* EchoSub · Liquid Glass 交互原型
   纯静态原型：所有数据、网络请求、字幕获取与翻译均为本地模拟。 */
'use strict';
(() => {
  const D = window.ECHO_DATA;
  const DESK_W = 1512, DESK_H = 945, MB_H = 28, DOCK_H = 80;
  const MAIN_MIN = { w: 560, h: 430 }, FLOAT_MIN = { w: 260, h: 96 }, LYR_MIN_W = 360;
  const SIDEBAR_DOCK_MIN = 960; // 主窗口宽度 ≥ 960 时，播放列表侧栏停靠；否则浮在内容上
  const PREV_SENT_GRACE = 1.5; // 当前句开始不到 1.5 秒时，“上一句”回到上一句
  const SEEK_JUMP = 2; // 大幅拖动阈值（秒）：超过时单句循环改到新位置的句子
  const AUTOSAVE_EVERY = 10;
  const TRANSLATE_BATCH = 4;
  const TRANSLATE_STEP_SECONDS = 1.3; // 模拟：每批翻译耗时

  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
  const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
  const fmt = (sec) => {
    if (sec == null || isNaN(sec)) return '–:––';
    sec = Math.max(0, Math.floor(sec));
    const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
    return h ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`;
  };
  const setHTML = (el, html) => { if (el && el._h !== html) { el._h = html; el.innerHTML = html; } };

  /* ---------- 常量 ---------- */
  const PRESETS = {
    deepseek: { name: 'DeepSeek', note: '默认', baseUrl: 'https://api.deepseek.com', model: 'deepseek-chat' },
    qwen: { name: '通义千问', note: 'DashScope 兼容模式', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: 'qwen3-max' },
    openai: { name: 'OpenAI', note: '', baseUrl: 'https://api.openai.com/v1', model: 'gpt-5-mini' },
  };
  const COLOR_PRESETS = [
    { id: 'classic', name: '经典白 / 黄', en: '#FFFFFF', zh: '#FFD54F', note: '默认' },
    { id: 'mono', name: '统一高对比白', en: '#FFFFFF', zh: '#FFFFFF' },
    { id: 'cyan', name: '柔和白 / 青', en: '#F7F7F7', zh: '#7FDBFF' },
    { id: 'warm', name: '暖白 / 橙', en: '#FFF8E7', zh: '#FFB74D' },
  ];
  const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5];
  const LOOP_MODES = ['off', 'list', 'one'];
  const LOOP_LABEL = { off: '不循环', list: '列表循环', one: '单个视频循环' };
  const LOOP_ICON = { off: 'norepeat', list: 'repeat', one: 'repeat1' };
  const MODES = [['en', '原文'], ['zh', '中文'], ['bi', '双语']];
  const MODE_LABEL = { en: '原文', zh: '中文', bi: '双语' };
  const SRC = { youtube: 'YouTube 直连', ytdlp: '本机 yt-dlp', supadata: 'Supadata' };
  const SRC_SHORT = { youtube: 'YouTube', ytdlp: 'yt-dlp', supadata: 'Supadata' };
  const YTDLP_PATH = '~/.local/bin/yt-dlp';
  const SHORTCUTS = [
    ['播放 / 暂停', ['空格'], '主窗口，输入框没有焦点时'],
    ['后退 / 前进 5 秒', ['←', '→'], '同上'],
    ['上一句 / 下一句', ['⌥←', '⌥→'], '同上'],
    ['重听本句', ['R'], '同上'],
    ['单句循环', ['L'], '同上'],
    ['窗口内全屏', ['F'], '同上（Esc 退出）'],
    ['搜索字幕', ['⌘F'], '主窗口'],
    ['设置', ['⌘,'], 'App 内任意窗口'],
    ['悬浮字幕', ['⇧⌘F'], 'App 内任意窗口'],
    ['桌面歌词', ['⇧⌘D'], 'App 内任意窗口'],
    ['锁定桌面歌词', ['⇧⌘L'], 'App 内任意窗口'],
    ['显示 / 隐藏字幕与列表区域', ['⌃⌘T'], 'App 内任意窗口'],
    ['播放列表', ['⌃⌘S'], 'App 内任意窗口'],
  ];

  /* ---------- 数据 ---------- */
  function sentencesOf(key, { translated = true, missing = [], upto = Infinity } = {}) {
    const miss = new Set(missing);
    return D[key].sentences.map(([s, e, en, zh], i) => ({ s, e, en, zhFull: zh, zh: translated && i < upto && !miss.has(i) ? zh : null }));
  }
  function V(o) {
    return Object.assign({
      id: '', key: null, title: null, channel: null, duration: null, progress: 0, status: 'pending',
      sentences: [], source: null, kind: null, model: null, errors: null, failBatches: [], fetch: null, tr: null,
      cover: null, glyph: null, outcome: null,
    }, o);
  }
  const SEED_FAIL_ERRORS = [
    { src: 'youtube', msg: '网页内嵌字幕轨读取失败；改用 Android 客户端接口后返回 HTTP 429（请求过于频繁）' },
    { src: 'ytdlp', msg: 'yt-dlp 退出码 1：ERROR: Unable to download video subtitles: HTTP Error 429' },
    { src: 'supadata', msg: '未配置 Supadata API Key，已跳过' },
  ];
  function seedLibrary() {
    const w = D.walking, m = D.market;
    const marketTranslating = sentencesOf('market', { upto: 12 });
    return [
      V({ id: 'Wq7aLk2Pn0s', key: 'walking', title: w.title, channel: w.channel, duration: w.duration, status: 'ready', sentences: sentencesOf('walking'), source: 'youtube', kind: '人工字幕', model: 'deepseek-chat', progress: 61.5, cover: ['#2fbf9b', '#1b5d8c'], glyph: 'WALK' }),
      V({ id: 'Nm4tKs9Qe1R', key: 'market', title: 'Street Food Diaries · Night Market #3', channel: m.channel, duration: m.duration, status: 'translating', sentences: marketTranslating, source: 'ytdlp', kind: '自动字幕', model: 'deepseek-chat', failBatches: [2], tr: { queue: marketTranslating.map((s, i) => (s.zh ? null : i)).filter((i) => i !== null), total: marketTranslating.length, done: 12, fill: false, batch: 0 }, cover: ['#ff8a3d', '#7b3fd1'], glyph: 'NIGHT' }),
      V({ id: 'Hb2xVc8Lm3T', key: 'market', title: 'Street Food Diaries · Night Market #2', channel: m.channel, duration: m.duration, status: 'ready', sentences: sentencesOf('market', { missing: [16, 17, 18, 19] }), source: 'youtube', kind: '自动字幕', model: 'deepseek-chat', progress: 138, cover: ['#f5576c', '#4b2a8a'], glyph: 'NIGHT' }),
      V({ id: 'pF6sYd1Wq9Z', status: 'pending', cover: ['#8e9eab', '#4b5563'] }),
      V({ id: 'dQw4w9WgXcQ', title: 'Rick Astley - Never Gonna Give You Up (Official Video)', channel: 'Rick Astley', status: 'failed', errors: SEED_FAIL_ERRORS, cover: ['#c0392b', '#2c3e50'] }),
      V({ id: 'jNQXAC9IVRw', title: 'Me at the zoo', channel: 'jawed', duration: 19, status: 'none', progress: 7, cover: ['#7f8c8d', '#2d3436'] }),
    ];
  }
  function seedHidden() {
    return [
      V({ id: 'Lr5uTq0Zx8B', title: 'Ten Minutes of Rain on a Tin Roof', channel: 'Quiet Rooms', duration: 600, status: 'none', cover: ['#5b7c99', '#22313f'], glyph: 'RAIN' }),
      V({ id: 'Kc8nWb3Vz1M', key: 'market', title: 'Street Food Diaries · Night Market #1', channel: D.market.channel, duration: D.market.duration, status: 'ready', sentences: sentencesOf('market'), source: 'youtube', kind: '人工字幕', model: 'deepseek-chat', progress: 226, cover: ['#f7b733', '#fc4a1a'], glyph: 'NIGHT' }),
    ];
  }
  function defaultFrames() {
    return {
      main: { x: 30, y: 44, w: 1096, h: 640 },
      settings: { x: 402, y: 104, w: 720, h: 580 },
      about: { x: 606, y: 240, w: 300, h: 290 },
      notes: { x: 1140, y: 196, w: 344, h: 470 },
      float: { x: 1156, y: 432, w: 322, h: 214 },
      lyrics: { x: 386, y: 858, w: 740 }, // y 为底边
    };
  }
  function initialState(prev) {
    return {
      theme: prev ? prev.theme : 'light',
      lib: seedLibrary(),
      hidden: seedHidden(),
      curId: 'Wq7aLk2Pn0s',
      t: 61.5, playing: false, speed: 1, loopIdx: null, loopMode: 'off',
      player: 'ok', playerCode: 150,
      mode: 'bi', follow: true, followPaused: false,
      q: '', filter: '',
      cfg: { baseUrl: PRESETS.deepseek.baseUrl, model: PRESETS.deepseek.model, key: 'sk-6f1c2b0e9d4a47c8b3e5a1f07d2c9e48' },
      draft: null, showKey: false, test: { state: 'idle' },
      supadata: '', supadataDraft: '', ytdlp: true,
      colors: { en: '#FFFFFF', zh: '#FFD54F' }, colorDraft: null,
      float: { open: true, font: 18, plate: true, alpha: 0.58, pin: true, spaces: true, follow: true, followPaused: false },
      lyr: { open: true, en: 27, zh: 18, align: 'center', alpha: 0.35, locked: false, progress: true },
      mainAlpha: 0.86,
      ui: { sidebar: true, sideAreas: true, fs: false, settingsOpen: false, settingsTab: 'translate', aboutOpen: false, mainVisible: true, mainMin: false, zoomed: null, sysfs: false, appHidden: false, notesHidden: false, quit: false, protoCollapsed: prev ? prev.ui.protoCollapsed : false },
      frames: prev ? prev.frames : defaultFrames(),
      z: ['notes', 'float', 'main', 'settings', 'about'],
      focus: 'main',
      dialog: null, menu: null, resizing: null,
      sim: prev ? Object.assign({}, prev.sim, { paused: false, holdTest: false, slow: 1 }) : { addOutcome: 'ok', testOutcome: 'ok', paused: false, clip: 'https://youtu.be/Tq9wEr4Ty6U?si=EchoSub', annot: false, actual: false, holdTest: false, slow: 1 },
      nextEp: 4,
    };
  }
  let S = initialState();
  window.__echo = () => S;

  /* ---------- 选择器 ---------- */
  const cur = () => S.lib.find((v) => v.id === S.curId) || null;
  const allVideos = () => S.lib.concat(S.hidden);
  const findVideo = (id) => allVideos().find((v) => v.id === id) || null;
  const vTitle = (v) => v.title || 'YouTube 视频';
  const missingCount = (v) => v.sentences.filter((s) => !s.zh).length;
  const translatedCount = (v) => v.sentences.filter((s) => s.zh).length;
  const durationOf = (v) => v.duration || (v.key ? D[v.key].duration : 0) || D.market.duration;
  const idxAt = (v, t) => {
    if (!v || !v.sentences.length) return -1;
    let lo = 0, hi = v.sentences.length - 1, ans = -1;
    while (lo <= hi) { const mid = (lo + hi) >> 1; if (v.sentences[mid].s <= t + 0.01) { ans = mid; lo = mid + 1; } else hi = mid - 1; }
    return ans;
  };
  const curIdx = () => idxAt(cur(), S.t);
  const appRunning = () => !S.ui.quit;
  const appVisible = () => !S.ui.quit && !S.ui.appHidden;
  const appActive = () => ['main', 'settings', 'about', 'float', 'lyrics'].includes(S.focus) && appVisible();
  const playerReady = () => S.player === 'ok';
  const mainShown = () => appVisible() && S.ui.mainVisible && !S.ui.mainMin;
  const zhOf = (s) => s.zh;
  function statusInfo(v) {
    if (!v) return { k: 'none', label: '' };
    switch (v.status) {
      case 'pending': return { k: 'gray', label: '待获取' };
      case 'fetching': return { k: 'blue', label: '获取中', spin: true };
      case 'translating': {
        const pct = v.tr && v.tr.fill ? Math.round((v.tr.done / v.tr.total) * 100) : Math.round((translatedCount(v) / v.sentences.length) * 100);
        return { k: 'violet', label: v.tr && v.tr.fill ? `补全中 ${pct}%` : `翻译中 ${pct}%`, pct };
      }
      case 'none': return { k: 'gray', label: '无字幕' };
      case 'failed': return { k: 'red', label: '失败' };
      case 'ready': {
        if (!v.sentences.length) return { k: 'gray', label: '字幕为空' };
        const miss = missingCount(v);
        if (miss === v.sentences.length) return { k: 'gray', label: '仅原文' };
        if (miss) return { k: 'orange', label: `待补全 ${miss}` };
        return { k: 'green', label: '双语' };
      }
    }
    return { k: 'gray', label: v.status };
  }

  /* ---------- 剪贴板 / 提示 ---------- */
  function copyText(text, msg) {
    S.sim.clip = text;
    try { navigator.clipboard && navigator.clipboard.writeText(text).catch(() => {}); } catch (_) { /* 原型：忽略 */ }
    toast(msg, 'copy');
  }
  let toastSeq = 0;
  function toast(msg, icon = 'check', kind = '') {
    const layer = $('#toast-layer');
    const id = `t${++toastSeq}`;
    const el = document.createElement('div');
    el.className = `toast lgw ${kind}`;
    el.id = id;
    el.innerHTML = `${ic(icon, 15)}<span>${msg}</span>`;
    layer.appendChild(el);
    while (layer.children.length > 3) layer.firstChild.remove();
    setTimeout(() => el.classList.add('out'), 2600);
    setTimeout(() => el.remove(), 3000);
  }

  /* ---------- 图形：App 图标 / 缩略图 ---------- */
  function appIcon(size = 64) {
    const u = `g${Math.random().toString(36).slice(2, 7)}`;
    return `<svg class="appicon" width="${size}" height="${size}" viewBox="0 0 64 64" aria-hidden="true">
      <defs><linearGradient id="${u}a" x1="0" y1="0" x2="1" y2="1"><stop offset="0" stop-color="#5b8cff"/><stop offset=".55" stop-color="#7a5cff"/><stop offset="1" stop-color="#ff7ab0"/></linearGradient>
      <linearGradient id="${u}b" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#fff" stop-opacity=".95"/><stop offset="1" stop-color="#fff" stop-opacity=".7"/></linearGradient></defs>
      <rect x="2" y="2" width="60" height="60" rx="14" fill="url(#${u}a)"/>
      <rect x="2.5" y="2.5" width="59" height="59" rx="13.5" fill="none" stroke="#fff" stroke-opacity=".45"/>
      <path d="M14 20a6 6 0 0 1 6-6h24a6 6 0 0 1 6 6v16a6 6 0 0 1-6 6H30l-9 7v-7h-1a6 6 0 0 1-6-6z" fill="url(#${u}b)" opacity=".96"/>
      <rect x="20" y="22" width="24" height="4" rx="2" fill="#4b4fd6"/>
      <rect x="20" y="30" width="15" height="4" rx="2" fill="#f2a516"/>
    </svg>`;
  }
  const thumbState = {}; // id -> 'ok' | 'bad'
  window.__thumbLoaded = (img) => {
    const id = img.dataset.id;
    if (img.naturalWidth <= 120) { thumbState[id] = 'bad'; img.remove(); } else { thumbState[id] = 'ok'; img.classList.add('ok'); }
  };
  window.__thumbFailed = (img) => { thumbState[img.dataset.id] = 'bad'; img.remove(); };
  function hashColors(id) {
    let h = 0; for (const c of id) h = (h * 31 + c.charCodeAt(0)) % 360;
    return [`hsl(${h} 70% 58%)`, `hsl(${(h + 70) % 360} 60% 32%)`];
  }
  function coverHTML(v, big) {
    const [c1, c2] = v.cover || hashColors(v.id);
    const st = thumbState[v.id];
    const img = st === 'bad' ? '' : `<img data-id="${v.id}" class="${st === 'ok' ? 'ok' : ''}" src="https://i.ytimg.com/vi/${v.id}/mqdefault.jpg" alt="" onload="__thumbLoaded(this)" onerror="__thumbFailed(this)">`;
    const glyph = v.glyph || (v.title ? v.title.replace(/[^A-Za-z]/g, '').slice(0, 4).toUpperCase() : '');
    return `<div class="cover ${big ? 'big' : ''}" style="--c1:${c1};--c2:${c2}"><div class="cover-art">${glyph ? `<span class="cover-glyph">${esc(glyph)}</span>` : ic('film', big ? 44 : 18)}</div>${img}</div>`;
  }
  function trafficLights(win) {
    return `<div class="tl" data-nodrag><button class="tl-b c" data-tl="close" data-w="${win}" title="关闭"></button><button class="tl-b m" data-tl="min" data-w="${win}" title="最小化" ${win === 'main' ? '' : 'disabled'}></button><button class="tl-b z" data-tl="zoom" data-w="${win}" title="缩放" ${win === 'main' ? '' : 'disabled'}></button></div>`;
  }
  function rz(win, dirs) { return dirs.map((d) => `<div class="rz rz-${d}" data-rz="${d}" data-w="${win}"></div>`).join(''); }
  function ht(cls, text, lines, style = '') {
    const t = esc(text);
    return `<div class="ht ${cls}" style="--lines:${lines};${style}"><span class="ht-h2" aria-hidden="true">${t}</span><span class="ht-h" aria-hidden="true">${t}</span><span class="ht-t">${t}</span></div>`;
  }
  function badge(v, small) {
    const s = statusInfo(v);
    if (!s.label) return '';
    const ring = s.pct != null ? `<i class="ring" style="--p:${s.pct}"></i>` : '';
    return `<span class="badge b-${s.k} ${small ? 'sm' : ''}">${s.spin ? '<i class="spin"></i>' : ring}${esc(s.label)}</span>`;
  }

  /* ---------- 链接识别 ---------- */
  const ID_RE = /^[A-Za-z0-9_-]{11}$/;
  function parseYouTubeId(raw) {
    const s = (raw || '').trim();
    if (!s) return null;
    if (ID_RE.test(s)) return s;
    let u;
    try { u = new URL(/^[a-z]+:\/\//i.test(s) ? s : `https://${s}`); } catch (_) { return null; }
    const host = u.hostname.toLowerCase().replace(/^(www|m|music)\./, '');
    if (host === 'youtu.be') { const id = u.pathname.split('/')[1]; return ID_RE.test(id || '') ? id : null; }
    if (host === 'youtube.com' || host === 'youtube-nocookie.com') {
      if (u.pathname === '/watch') { const id = u.searchParams.get('v'); return ID_RE.test(id || '') ? id : null; }
      const m = u.pathname.match(/^\/(shorts|embed|v|live)\/([A-Za-z0-9_-]{11})(?:[/?#]|$)/);
      return m ? m[2] : null;
    }
    return null;
  }

  /* =========================================================
     命令（按钮 / 菜单 / 快捷键共用，保证多入口一致）
     ========================================================= */
  function seek(t, { play = null, fromPlayer = false } = {}) {
    const v = cur(); if (!v) return;
    const dur = durationOf(v);
    const old = S.t;
    S.t = clamp(t, 0, Math.max(0, dur - 0.05));
    if (fromPlayer && S.loopIdx != null && Math.abs(S.t - old) > SEEK_JUMP) {
      const ni = idxAt(v, S.t);
      S.loopIdx = ni >= 0 ? ni : null;
    }
    if (play != null && playerReady()) S.playing = play;
    S.followPaused = false;
    S.float.followPaused = false;
    v.progress = S.t;
  }
  function needPlayer() {
    if (!cur()) return false;
    if (!playerReady()) { toast(S.player === 'error' ? `播放器错误 ${S.playerCode}，无法在应用内播放` : '播放器不可用', 'warn', 'warn'); return false; }
    return true;
  }
  function gotoSentence(i, play = true) {
    const v = cur(); if (!v || !v.sentences[i]) return;
    seek(v.sentences[i].s + 0.001, { play });
    if (S.loopIdx != null) S.loopIdx = i;
  }
  const CMD = {
    togglePlay() { if (!needPlayer()) return; S.playing = !S.playing; },
    play() { if (!needPlayer()) return; S.playing = true; },
    back5() { if (!needPlayer()) return; seek(S.t - 5, { fromPlayer: true }); },
    fwd5() { if (!needPlayer()) return; seek(S.t + 5, { fromPlayer: true }); },
    prev() {
      const v = cur(); if (!needPlayer() || !v.sentences.length) return;
      const i = curIdx();
      if (i < 0) return gotoSentence(0, S.playing);
      const target = S.t - v.sentences[i].s < PREV_SENT_GRACE ? Math.max(0, i - 1) : i;
      gotoSentence(target, S.playing || null);
      if (S.loopIdx != null) S.loopIdx = target;
    },
    next() {
      const v = cur(); if (!needPlayer() || !v.sentences.length) return;
      const target = Math.min(v.sentences.length - 1, curIdx() + 1);
      gotoSentence(target, S.playing || null);
      if (S.loopIdx != null) S.loopIdx = target;
    },
    replay() {
      const v = cur(); if (!needPlayer() || !v.sentences.length) return;
      gotoSentence(Math.max(0, curIdx()), true);
    },
    toggleLoop() {
      const v = cur(); if (!needPlayer() || !v.sentences.length) return;
      if (S.loopIdx != null) { S.loopIdx = null; toast('已关闭单句循环', 'loopSent'); return; }
      S.loopIdx = Math.max(0, curIdx());
      toast(`单句循环：第 ${S.loopIdx + 1} 句`, 'loopSent');
    },
    cycleSpeed() { if (!cur()) return; S.speed = SPEEDS[(SPEEDS.indexOf(S.speed) + 1) % SPEEDS.length]; },
    cycleLoopMode() { S.loopMode = LOOP_MODES[(LOOP_MODES.indexOf(S.loopMode) + 1) % LOOP_MODES.length]; toast(`视频循环：${LOOP_LABEL[S.loopMode]}`, LOOP_ICON[S.loopMode]); },
    toggleFs() { if (!cur() || !mainShown()) return; S.ui.fs = !S.ui.fs; },
    exitFs() { S.ui.fs = false; },
    openYT(el) { const id = (el && el.dataset && el.dataset.vid) || S.curId; if (id) toast(`已在浏览器中打开 youtube.com/watch?v=${esc(id)}`, 'external'); },
    toggleFloat() { if (!appRunning()) return; S.float.open = !S.float.open; },
    toggleLyrics() { if (!appRunning()) return; S.lyr.open = !S.lyr.open; },
    toggleLock() {
      if (!S.lyr.open) return;
      S.lyr.locked = !S.lyr.locked;
      if (S.lyr.locked) toast('桌面歌词已锁定，鼠标会穿透到下层。<br>解锁：菜单栏图标，或 ⇧⌘L', 'lock');
      else toast('桌面歌词已解锁', 'unlock');
    },
    toggleSideAreas() { S.ui.sideAreas = !S.ui.sideAreas; if (S.ui.sideAreas && !S.ui.sidebar && S.frames.main.w >= SIDEBAR_DOCK_MIN) S.ui.sidebar = true; showMain(); },
    togglePlaylist() { S.ui.sbAutoClosed = false; if (!S.ui.sideAreas) { S.ui.sideAreas = true; S.ui.sidebar = true; } else S.ui.sidebar = !S.ui.sidebar; showMain(); },
    openSettings() { if (!appRunning()) return; S.ui.settingsOpen = true; S.ui.appHidden = false; bringFront('settings'); S.focus = 'settings'; settingsDirty = true; },
    focusSearch() {
      if (!cur() || !cur().sentences.length) return;
      if (!S.ui.sideAreas) S.ui.sideAreas = true;
      render();
      const q = $('#qInput'); if (q) { q.focus(); q.select(); }
    },
    setMode(el) { S.mode = el.dataset.mode; },
    cycleMode() { const order = ['bi', 'en', 'zh']; S.mode = order[(order.indexOf(S.mode) + 1) % order.length]; },
    openMain() { S.ui.quit = false; showMain(); },
  };
  function showMain() { S.ui.appHidden = false; S.ui.mainVisible = true; S.ui.mainMin = false; bringFront('main'); S.focus = 'main'; }

  /* ---------- 视频库操作 ---------- */
  function selectVideo(id, { fromStart = false, autoplay = true } = {}) {
    const old = cur();
    if (old) old.progress = S.t;
    const v = S.lib.find((x) => x.id === id);
    if (!v) return;
    S.curId = id;
    S.t = fromStart ? 0 : Math.min(v.progress || 0, Math.max(0, durationOf(v) - 1));
    S.loopIdx = null; S.q = ''; S.followPaused = false; S.float.followPaused = false; S.ui.fs = false;
    const q = $('#qInput'); if (q) q.value = '';
    S.player = 'loading'; S.playing = false;
    setTimeout(() => { if (S.player === 'loading' && S.curId === id) { S.player = 'ok'; S.playing = autoplay; render(); } }, 650);
    if (v.status === 'pending') startFetch(v);
    else if (v.status === 'ready' && v.sentences.length && missingCount(v) && S.cfg.key) startTranslate(v, { fill: translatedCount(v) > 0 });
    pendingScrollToCurrent = true;
    if (S.frames.main.w < SIDEBAR_DOCK_MIN) S.ui.sidebar = false;
  }
  function addVideo(raw, source) {
    const id = parseYouTubeId(raw);
    if (!id) { addErr[source] = true; toast('无法识别这个 YouTube 链接。', 'warn', 'warn'); return false; }
    addErr[source] = false;
    const inLib = S.lib.find((v) => v.id === id);
    if (inLib) { selectVideo(id); toast(`已在列表中，已切换到「${esc(vTitle(inLib))}」`, 'list'); return true; }
    const hi = S.hidden.findIndex((v) => v.id === id);
    if (hi >= 0) {
      const [v] = S.hidden.splice(hi, 1); S.lib.unshift(v); selectVideo(id);
      toast(`已从“已隐藏”中恢复「${esc(vTitle(v))}」`, 'restore'); return true;
    }
    const v = V({ id, status: 'pending', outcome: S.sim.addOutcome });
    S.lib.unshift(v);
    selectVideo(id, { fromStart: true });
    toast('已添加，正在获取字幕…', 'download');
    return true;
  }
  function hideVideo(id) {
    const i = S.lib.findIndex((v) => v.id === id); if (i < 0) return;
    const [v] = S.lib.splice(i, 1);
    S.hidden.unshift(v);
    if (S.curId === id) leaveCurrent(i);
    toast(`已隐藏「${esc(vTitle(v))}」，字幕和翻译都已保留`, 'eyeOff');
  }
  function restoreVideo(id) {
    const i = S.hidden.findIndex((v) => v.id === id); if (i < 0) return;
    const [v] = S.hidden.splice(i, 1);
    S.lib.unshift(v);
    toast(`已恢复「${esc(vTitle(v))}」`, 'restore');
  }
  function deleteVideo(id) {
    const i = S.lib.findIndex((v) => v.id === id);
    const v = findVideo(id);
    S.lib = S.lib.filter((x) => x.id !== id); S.hidden = S.hidden.filter((x) => x.id !== id);
    if (S.curId === id) leaveCurrent(Math.max(0, i));
    toast(`已删除「${esc(v ? vTitle(v) : id)}」`, 'trash');
  }
  function leaveCurrent(i) {
    const next = S.lib[Math.min(i, S.lib.length - 1)];
    S.curId = null; S.playing = false; S.t = 0; S.loopIdx = null; S.ui.fs = false;
    if (next) selectVideo(next.id, { autoplay: false });
  }
  function clearLibrary() {
    const n = S.lib.length;
    S.lib = []; S.curId = null; S.playing = false; S.t = 0; S.loopIdx = null; S.ui.fs = false;
    toast(`已清空列表（${n} 个视频）`, 'trash');
  }

  /* ---------- 模拟：字幕获取 ---------- */
  function fetchPlan(outcome) {
    const yt = (r, msg, dur = 2.2) => ({ src: 'youtube', dur, result: r, msg });
    const ytdlpMissing = { src: 'ytdlp', dur: 0.7, result: 'fail', msg: `未检测到本机 yt-dlp（可运行 brew install yt-dlp 安装）` };
    const supa = (r, msg) => ({ src: 'supadata', dur: 1.2, result: r, msg });
    switch (outcome) {
      case 'fallback': return [
        yt('fail', '网页内嵌字幕轨为空；Android 客户端接口未返回英文轨', 2),
        S.ytdlp ? { src: 'ytdlp', dur: 2.4, result: 'ok' } : ytdlpMissing,
        ...(S.ytdlp ? [] : [S.supadata ? supa('ok') : supa('fail', '未配置 Supadata API Key，已跳过')]),
      ];
      case 'fail': return [
        yt('fail', '网页内嵌字幕轨读取失败；Android 客户端接口返回 HTTP 403', 2),
        S.ytdlp ? { src: 'ytdlp', dur: 2, result: 'fail', msg: 'yt-dlp 退出码 1：ERROR: Sign in to confirm you’re not a bot' } : ytdlpMissing,
        S.supadata ? supa('fail', 'Supadata 返回 HTTP 502：upstream timeout') : supa('fail', '未配置 Supadata API Key，已跳过'),
      ];
      case 'none': return [
        yt('none', '视频没有英文字幕轨（含自动字幕）', 2),
        S.ytdlp ? { src: 'ytdlp', dur: 1.6, result: 'none', msg: '没有可用的英文字幕' } : ytdlpMissing,
        S.supadata ? supa('none', '该视频没有现成字幕') : supa('fail', '未配置 Supadata API Key，已跳过'),
      ];
      case 'empty': return [yt('empty', null, 2.4)];
      default: return [yt('ok', null, 2.6)];
    }
  }
  function startFetch(v, outcome) {
    v.status = 'fetching';
    v.errors = null;
    v.fetch = { step: 0, t: 0, plan: fetchPlan(outcome || v.outcome || S.sim.addOutcome) };
    v.outcomeUsed = outcome || v.outcome || S.sim.addOutcome;
  }
  function stepFetch(dt) {
    for (const v of allVideos()) {
      if (v.status !== 'fetching' || !v.fetch) continue;
      const f = v.fetch, p = f.plan[f.step];
      f.t += dt;
      if (f.t < p.dur) continue;
      p.done = true; f.t = 0;
      if (p.result === 'ok' || p.result === 'empty') { finishFetch(v, p); continue; }
      f.step++;
      if (f.step >= f.plan.length) {
        const none = f.plan.some((x) => x.result === 'none');
        v.status = none ? 'none' : 'failed';
        v.errors = f.plan.map((x) => ({ src: x.src, msg: x.msg }));
        v.fetch = null;
        if (!v.title) { v.title = 'Untitled Clip'; v.channel = 'Unknown Channel'; }
        if (v.id === S.curId) toast(none ? '这个视频没有可用字幕' : '字幕获取失败：三个来源都没有成功', 'warn', 'warn');
      }
    }
  }
  function finishFetch(v, p) {
    const key = v.key || 'market';
    if (!v.title) { v.title = `Street Food Diaries · Night Market #${S.nextEp++}`; v.channel = D.market.channel; v.glyph = v.glyph || 'NIGHT'; }
    v.key = key; v.duration = D[key].duration; v.source = p.src; v.kind = p.src === 'youtube' ? '人工字幕' : '自动字幕';
    v.sentences = p.result === 'empty' ? [] : sentencesOf(key, { translated: false });
    v.status = 'ready'; v.fetch = null; v.model = null;
    v.failBatches = v.outcomeUsed === 'partial' ? [2, 6] : [];
    if (v.sentences.length && S.cfg.key) startTranslate(v);
    if (v.id === S.curId) {
      if (!v.sentences.length) toast('已获取字幕，但字幕轨为空', 'info');
      else toast(S.cfg.key ? `已获取 ${v.sentences.length} 句字幕（${SRC[p.src]}），开始翻译` : `已获取 ${v.sentences.length} 句字幕。未配置 API Key，仅显示原文`, 'check');
    }
  }
  /* ---------- 模拟：翻译 ---------- */
  function startTranslate(v, { fill = false } = {}) {
    if (!S.cfg.key) { v.status = 'ready'; return; }
    const queue = v.sentences.map((s, i) => (s.zh ? null : i)).filter((i) => i !== null);
    if (!queue.length) { v.status = 'ready'; return; }
    v.tr = { queue, total: queue.length, done: 0, fill, batch: 0 };
    v.status = 'translating'; v.model = S.cfg.model;
  }
  function stepTranslations() {
    for (const v of allVideos()) {
      if (v.status !== 'translating' || !v.tr) continue;
      const batch = v.tr.queue.splice(0, TRANSLATE_BATCH);
      const skip = !v.tr.fill && v.failBatches.includes(v.tr.batch);
      v.tr.batch++;
      if (!skip) batch.forEach((i) => { v.sentences[i].zh = v.sentences[i].zhFull; });
      v.tr.done += batch.length;
      if (!v.tr.queue.length) {
        v.status = 'ready'; v.tr = null;
        if (v.id === S.curId) {
          const miss = missingCount(v);
          toast(miss ? `翻译结束，${miss} 句待补全` : '已全部翻译', miss ? 'warn' : 'check', miss ? 'warn' : '');
        }
      }
    }
  }

  /* =========================================================
     DOM 构建
     ========================================================= */
  const desktop = $('#desktop');
  function mkWin(id, cls, html) {
    const el = document.createElement('section');
    el.id = `win-${id}`; el.className = cls; el.dataset.win = id; el.innerHTML = html;
    desktop.appendChild(el);
    return el;
  }
  function buildNotes() {
    mkWin('notes', 'win win-notes', `
      <div class="nt-bar" data-drag>${trafficLights('notes')}<span class="nt-title">备忘录</span></div>
      <div class="nt-body" id="ntBody">
        <h4>英语精听计划</h4>
        <p class="nt-date">今天 21:04</p>
        <ul>
          <li>每天 1 个视频，逐句跟读 20 分钟</li>
          <li>先看双语，再切“原文”盲听</li>
          <li>卡住的句子开单句循环 ×3</li>
          <li>记 5 个新表达：<i>soft fascination</i>, <i>loosen our thoughts</i>…</li>
        </ul>
        <p class="nt-tip">（这是模拟的“其他 App”。悬浮字幕和桌面歌词会盖在它上面；桌面歌词锁定后，点击会穿透到这里。）</p>
      </div>`);
  }
  function buildMain() {
    mkWin('main', 'win win-main', `
      <div class="mw" id="mw">
        <div class="mw-bg"></div>
        <aside class="mw-side lg" id="mwSide" data-note="窗口内玻璃（mygo glass 插件）· 只折射窗口自绘内容">
          <div class="side-top" data-drag>
            <div class="tl-side">${trafficLights('main')}</div>
            <span class="sp"></span>
            <button class="tbtn side-close" data-cmd="togglePlaylist" title="收起播放列表 ⌃⌘S">${ic('sidebar', 16)}</button>
          </div>
          <form class="add" data-add="side" autocomplete="off">
            <div class="add-field" id="addFieldSide">
              ${ic('link', 14)}
              <input id="addSide" data-addinput="side" placeholder="粘贴 YouTube 链接或视频 ID" spellcheck="false">
              <button type="button" class="mini" data-act="paste" data-src="side" title="从剪贴板粘贴">${ic('paste', 14)}</button>
            </div>
            <button type="submit" class="add-btn" title="添加（回车）">${ic('plus', 15)}</button>
          </form>
          <div class="add-hint" id="hintSide"></div>
          <div class="side-search">${ic('search', 13)}<input id="filterInput" placeholder="筛选标题或频道" spellcheck="false"><button class="clear" data-act="clearFilter" title="清除">${ic('close', 10)}</button></div>
          <div class="side-label"><span>视频库</span><span id="libCount"></span></div>
          <div class="lib-list" id="libList"></div>
          <div class="side-foot" id="sideFoot"></div>
        </aside>
        <div class="sb-scrim" data-cmd="togglePlaylist"></div>
        <div class="mw-main">
          <div class="mw-toolbar" data-drag id="mwToolbar"></div>
          <div class="mw-content" id="mwContent"></div>
        </div>
        <div class="sheet-layer" id="mainSheet"></div>
      </div>
      ${rz('main', ['n', 'e', 's', 'w', 'ne', 'nw', 'se', 'sw'])}`);
  }
  function playerLayoutHTML() {
    return `
      <div class="pcol" id="pcol">
        <div class="vstage" id="vstage" data-note="16:9 以外的留白：缩略图模糊成环境光（窗口自绘图片，不是视频画面）" data-note-pos="bl">
          <div class="ambient" id="ambient"></div>
          <div class="video" id="video"></div>
          <div class="fs-hint">窗口内全屏 · 按 Esc 或 F 退出</div>
        </div>
        <div class="focus" id="focus"></div>
        <div class="transport lg" id="transport">
          <div class="scrub-row">
            <span class="tm" id="tCur">0:00</span>
            <div class="scrub" id="scrub">
              <div class="scrub-track"><div class="scrub-ticks" id="ticks"></div><div class="scrub-loop" id="scrubLoop"></div><div class="scrub-fill" id="scrubFill"></div></div>
              <div class="scrub-knob" id="scrubKnob"></div>
              <div class="scrub-tip" id="scrubTip"></div>
            </div>
            <span class="tm" id="tDur">0:00</span>
          </div>
          <div class="ctl-row" id="ctlRow"></div>
        </div>
      </div>
      <section class="tp" id="tp">
        <div class="tp-head" id="tpHead">
          <div class="tp-row1">
            <h3>字幕</h3>
            <div class="tp-status" id="tpStatus"></div>
            <span class="sp"></span>
            <button class="tbtn sm" id="followBtn" data-act="toggleFollow" title="自动跟随当前句">${ic('follow', 15)}<span>跟随</span></button>
            <button class="tbtn sm" data-act="tpMore" title="更多">${ic('more', 15)}</button>
          </div>
          <div class="tp-row2">
            <div class="seg" id="modeSeg">${MODES.map(([k, l]) => `<button data-cmd="setMode" data-mode="${k}">${l}</button>`).join('')}</div>
            <div class="tp-search" id="tpSearch">${ic('search', 13)}<input id="qInput" placeholder="搜索英文或中文" spellcheck="false"><span class="q-count" id="qCount"></span><button class="clear" data-act="clearSearch" title="清除">${ic('close', 10)}</button></div>
          </div>
          <div class="tp-banner" id="tpBanner"></div>
        </div>
        <div class="tp-list" id="tpList"></div>
        <button class="tp-return lg" id="tpReturn" data-act="returnCurrent">${ic('follow', 13)} 回到当前句</button>
        <div class="tp-meta" id="tpMeta"></div>
      </section>`;
  }
  function heroHTML(kind) {
    if (kind === 'pick') {
      return `<div class="hero pick"><div class="hero-icon">${ic('list', 40)}</div><h1>选择一个视频开始精听</h1><p>从左侧视频库里选一个，或粘贴新的 YouTube 链接。</p><button class="btn primary" data-cmd="togglePlaylist">打开播放列表</button></div>`;
    }
    return `<div class="hero">
      <div class="hero-icon big">${appIcon(84)}</div>
      <h1>把 YouTube 视频变成双语精听材料</h1>
      <p>粘贴链接，EchoSub 自动获取英文字幕、用大模型翻译成中文，然后边播边看。</p>
      <form class="hero-add lg" data-add="hero" autocomplete="off">
        ${ic('link', 16)}
        <input id="addHero" data-addinput="hero" placeholder="粘贴 YouTube 链接或 11 位视频 ID" spellcheck="false">
        <button type="button" class="mini" data-act="paste" data-src="hero" title="从剪贴板粘贴">${ic('paste', 15)}</button>
        <button type="submit" class="btn primary">添加</button>
      </form>
      <div class="add-hint hero-hint" id="hintHero"></div>
      <div class="hero-formats">支持 <code>watch?v=</code> <code>youtu.be/</code> <code>shorts/</code> <code>embed/</code> <code>v/</code> <code>live/</code> 和 11 位视频 ID</div>
      <ol class="hero-steps">
        <li><b>${ic('download', 16)}</b><span>获取英文字幕</span><small>YouTube 直连 → yt-dlp → Supadata</small></li>
        <li><b>${ic('translate', 16)}</b><span>翻译成中文</span><small>OpenAI 兼容接口，逐批出现</small></li>
        <li><b>${ic('captions', 16)}</b><span>边播边看</span><small>悬浮字幕 · 桌面歌词</small></li>
      </ol>
    </div>`;
  }
  function buildSettings() {
    mkWin('settings', 'win win-settings', `
      <div class="set">
        <aside class="set-side lg">
          <div class="side-top" data-drag>${trafficLights('settings')}</div>
          <nav class="set-nav" id="setNav"></nav>
        </aside>
        <div class="set-main">
          <header class="set-head" data-drag id="setHead"></header>
          <div class="set-body" id="setBody"></div>
        </div>
      </div>`);
  }
  function buildAbout() {
    mkWin('about', 'win win-about', `
      <div class="ab" data-drag>${trafficLights('about')}
        <div class="ab-icon">${appIcon(88)}</div>
        <h3>EchoSub</h3>
        <p>版本 0.9.0（原型）</p>
        <p class="ab-desc">YouTube 双语字幕精听工具<br>Go + mygo 原生界面</p>
        <p class="ab-c">示例数据均为虚构内容</p>
      </div>`);
  }
  function buildOverlays() {
    mkWin('float', 'ovl ovl-float', `
      <div class="of-plate" data-note="窗口级 Liquid Glass（VibrancyGlassClear）· 系统绘制，可透出桌面与其他 App"></div>
      <div class="of-body" id="ofBody" data-drag></div>
      <div class="of-bar lgw-dark" id="ofBar"></div>
      <div class="of-size" id="ofSize"></div>
      ${rz('float', ['n', 'e', 's', 'w', 'ne', 'nw', 'se', 'sw'])}`);
    mkWin('lyrics', 'ovl ovl-lyrics', `
      <div class="ol-bar-wrap"><div class="ol-bar lgw-dark" id="olBar"></div></div>
      <div class="ol-plate" id="olPlate" data-drag data-note="文字可读性：在文字下方绘制一层模糊的暗色副本（不是文字阴影）"></div>
      ${rz('lyrics', ['e', 'w'])}`);
    // 悬浮字幕列表：用户手动滚动时暂停自动跟随
    $('#ofBody').addEventListener('wheel', () => {
      if ($('#ofList') && S.float.follow && !S.float.followPaused) { S.float.followPaused = true; renderFloat(); }
    }, { passive: true });
  }

  /* =========================================================
     渲染
     ========================================================= */
  let mwMode = null;
  let pendingScrollToCurrent = true;
  let lastIdx = -2;
  let settingsDirty = true;
  const addErr = { side: false, hero: false };

  function render() {
    document.body.className = `${S.theme} ${S.sim.annot ? 'annot' : ''}`;
    renderStage();
    renderMenubar();
    renderDock();
    renderMain();
    renderSettings();
    renderAbout();
    renderNotes();
    renderFloat();
    renderLyrics();
    renderMenu();
    renderProto();
    applyFrames();
    sync();
    lastIdx = curIdx();
  }

  function renderStage() {
    const st = $('#stage');
    st.classList.toggle('sysfs', S.ui.sysfs && mainShown());
  }

  /* ---------- 菜单栏 ---------- */
  function focusedApp() {
    if (S.focus === 'notes' && !S.ui.notesHidden) return 'notes';
    if (appActive()) return 'echosub';
    return 'finder';
  }
  function renderMenubar() {
    const app = focusedApp();
    const left = app === 'echosub'
      ? [['app', 'EchoSub', true], ['edit', '编辑'], ['play', '播放'], ['window', '窗口']]
      : app === 'notes' ? [['x', '备忘录', true], ['x', '文件'], ['x', '编辑'], ['x', '格式'], ['x', '显示'], ['x', '窗口']]
        : [['x', '访达', true], ['x', '文件'], ['x', '编辑'], ['x', '显示'], ['x', '前往'], ['x', '窗口']];
    const open = S.menu && S.menu.owner;
    const now = new Date();
    const wd = '日一二三四五六'[now.getDay()];
    const clock = `${now.getMonth() + 1}月${now.getDate()}日 周${wd} ${String(now.getHours()).padStart(2, '0')}:${String(now.getMinutes()).padStart(2, '0')}`;
    setHTML($('#menubar'), `
      <div class="mb-left">
        <span class="mb-item apple">${ic('apple', 15)}</span>
        ${left.map(([k, l, b]) => `<button class="mb-item ${b ? 'bold' : ''} ${open === `mb:${k}` ? 'open' : ''}" ${k === 'x' ? 'data-nomenu' : `data-mb="${k}"`}>${l}</button>`).join('')}
      </div>
      <div class="mb-right">
        ${appRunning() ? `<button class="mb-item status ${open === 'status' ? 'open' : ''} ${S.playing ? 'live' : ''}" data-mb="status" title="EchoSub" data-note="菜单栏图标（系统菜单）">${ic('bubble', 17)}</button>` : ''}
        <span class="mb-item">${ic('battery', 19)}</span>
        <span class="mb-item">${ic('wifi', 16)}</span>
        <span class="mb-item">${ic('search', 14)}</span>
        <span class="mb-item">${ic('controls', 16)}</span>
        <span class="mb-item clock">${clock}</span>
      </div>`);
  }
  function renderDock() {
    const apps = [
      ['finder', '访达', 'linear-gradient(180deg,#5ac8fa,#1e6ff0)', true],
      ['safari', 'Safari 浏览器', 'linear-gradient(180deg,#f2f4f8,#cfd6e2)', false],
      ['notes', '备忘录', 'linear-gradient(180deg,#fff7c2,#ffd84d)', true],
      ['echosub', 'EchoSub', '', appRunning()],
      ['settings', '系统设置', 'linear-gradient(180deg,#9aa3ad,#5d6670)', false],
    ];
    setHTML($('#dock'), `<div class="dock-inner lgw">${apps.map(([k, l, bg, run]) => `
      <button class="dock-app" data-dock="${k}" title="${l}">
        ${k === 'echosub' ? appIcon(50) : `<span class="dock-ic dock-${k}" style="background:${bg}">${k === 'finder' ? '<i></i>' : k === 'safari' ? ic('external', 26) : k === 'notes' ? '<i></i><i></i><i></i>' : ic('gear', 28)}</span>`}
        ${run ? '<span class="dock-dot"></span>' : ''}
      </button>`).join('')}
      ${S.ui.mainMin && appVisible() ? `<span class="dock-sep"></span><button class="dock-app dock-min" data-dock="restoreMain" title="EchoSub 主窗口（已最小化）"><span class="dock-thumb">${appIcon(26)}</span></button>` : ''}
    </div>`);
  }

  /* ---------- 主窗口 ---------- */
  function renderMain() {
    const el = $('#win-main');
    el.classList.toggle('hidden', !mainShown());
    if (!mainShown()) return;
    const v = cur();
    const mw = $('#mw');
    const docked = S.frames.main.w >= SIDEBAR_DOCK_MIN;
    mw.classList.toggle('sb-open', S.ui.sidebar && S.ui.sideAreas);
    mw.classList.toggle('sb-docked', docked);
    mw.classList.toggle('no-side', !S.ui.sideAreas);
    mw.classList.toggle('fs', S.ui.fs && !!v);
    mw.classList.toggle('empty', !S.lib.length);
    el.classList.toggle('active', S.focus === 'main');
    el.classList.toggle('zoomed', !!S.ui.zoomed);
    el.classList.toggle('sysfs', S.ui.sysfs);
    el.style.setProperty('--main-alpha', S.mainAlpha);

    setHTML($('#mwToolbar'), toolbarHTML());
    renderLibrary();

    const mode = !S.lib.length ? 'hero' : !v ? 'pick' : 'player';
    if (mode !== mwMode) {
      mwMode = mode;
      const c = $('#mwContent');
      c._h = null;
      setHTML(c, mode === 'player' ? playerLayoutHTML() : heroHTML(mode));
      c.className = `mw-content mode-${mode}`;
      if (mode === 'player') { bindPlayerLayout(); pendingScrollToCurrent = true; }
      $$('[data-addinput]').forEach((i) => updateAddHint(i));
    }
    if (mode === 'player') {
      renderPlayer(v);
      renderFocus(v);
      renderTransport(v);
      renderTranscript(v);
    }
    renderDialog();
  }
  function toolbarHTML() {
    const v = cur();
    const titleBlock = v
      ? `<b title="${esc(vTitle(v))}">${esc(vTitle(v))}</b><span>${esc(v.channel || '频道信息获取中…')}${v.duration ? ` · ${fmt(v.duration)}` : ''}</span>`
      : '<b>EchoSub</b><span>YouTube 双语字幕精听</span>';
    return `
      <div class="tl-bar">${trafficLights('main')}</div>
      <button class="tbtn tb-side ${S.ui.sidebar && S.ui.sideAreas ? 'on' : ''}" data-cmd="togglePlaylist" title="播放列表 ⌃⌘S">${ic('sidebar', 17)}</button>
      <div class="tb-title">${titleBlock}</div>
      <span class="tb-sp"></span>
      <div class="tgroup lg">
        <button class="tbtn ${S.float.open ? 'on' : ''}" data-cmd="toggleFloat" title="悬浮字幕 ⇧⌘F">${ic('captions', 17)}<span class="tlabel">悬浮字幕</span></button>
        <button class="tbtn ${S.lyr.open ? 'on' : ''}" data-cmd="toggleLyrics" title="桌面歌词 ⇧⌘D">${ic('lyrics', 17)}<span class="tlabel">桌面歌词</span></button>
        <button class="tbtn ${S.lyr.locked ? 'on locked' : ''}" data-cmd="toggleLock" ${S.lyr.open ? '' : 'disabled'} title="${S.lyr.locked ? '解锁' : '锁定'}桌面歌词 ⇧⌘L">${ic(S.lyr.locked ? 'lock' : 'unlock', 16)}</button>
      </div>
      <div class="tgroup lg">
        <button class="tbtn ${!S.ui.sideAreas ? 'on' : ''}" data-cmd="toggleSideAreas" title="${S.ui.sideAreas ? '隐藏' : '显示'}字幕与播放列表区域 ⌃⌘T">${ic('theater', 17)}</button>
        <button class="tbtn" data-cmd="openSettings" title="设置 ⌘,">${ic('gear', 17)}</button>
      </div>`;
  }
  function renderLibrary() {
    const f = S.filter.trim().toLowerCase();
    const list = f ? S.lib.filter((v) => `${vTitle(v)} ${v.channel || ''}`.toLowerCase().includes(f)) : S.lib;
    setHTML($('#libCount'), S.lib.length ? `${f ? `${list.length} / ` : ''}${S.lib.length}` : '');
    let html;
    if (!S.lib.length) {
      html = `<div class="empty-mini">${ic('film', 26)}<b>还没有视频</b><span>在上方粘贴 YouTube 链接，<br>字幕会自动获取并翻译。</span></div>`;
    } else if (!list.length) {
      html = `<div class="empty-mini">${ic('search', 24)}<b>没有匹配的视频</b><span>没有标题或频道包含“${esc(S.filter)}”的视频。</span><button class="btn sm" data-act="clearFilter">清除筛选</button></div>`;
    } else {
      html = list.map((v) => {
        const dur = v.duration ? fmt(v.duration) : '–:––';
        const pct = v.duration ? clamp((v.progress / v.duration) * 100, 0, 100) : 0;
        const sel = v.id === S.curId;
        return `<div class="lib-row ${sel ? 'sel' : ''}" data-vid="${v.id}">
          <div class="lib-thumb">${coverHTML(v)}<span class="dur">${dur}</span>${pct > 0 ? `<i class="prog" style="width:${pct}%"></i>` : ''}${sel && S.playing ? '<span class="eq"><i></i><i></i><i></i></span>' : ''}</div>
          <div class="lib-meta">
            <div class="lib-title ${v.title ? '' : 'ph'}">${esc(vTitle(v))}</div>
            <div class="lib-sub">${esc(v.channel || (v.status === 'fetching' ? '获取中…' : '—'))}</div>
            <div class="lib-badges">${badge(v, true)}</div>
          </div>
          <button class="lib-more" data-act="rowMenu" data-vid="${v.id}" title="更多操作">${ic('more', 14)}</button>
        </div>`;
      }).join('');
    }
    setHTML($('#libList'), html);
    setHTML($('#sideFoot'), `
      <button class="foot-btn" data-act="showHidden" id="hiddenBtn">${ic('eyeOff', 14)}<span>已隐藏</span>${S.hidden.length ? `<b>${S.hidden.length}</b>` : ''}</button>
      <span class="sp"></span>
      <button class="foot-btn danger" data-act="askClear" ${S.lib.length ? '' : 'disabled'} title="删除当前列表中的所有视频">${ic('trash', 14)}<span>清空列表</span></button>`);
  }
  function updateAddHint(input) {
    const src = input.dataset.addinput;
    const hint = src === 'hero' ? $('#hintHero') : $('#hintSide');
    const field = src === 'hero' ? input.closest('.hero-add') : $('#addFieldSide');
    if (!hint) return;
    const val = input.value.trim();
    field && field.classList.toggle('err', !!addErr[src]);
    if (addErr[src]) { setHTML(hint, `<span class="h-err">${ic('warn', 13)} 无法识别这个 YouTube 链接。</span>`); return; }
    if (!val) { setHTML(hint, src === 'hero' ? '' : '<span class="h-muted">支持 watch、youtu.be、shorts、embed、live 链接或 11 位 ID</span>'); return; }
    const id = parseYouTubeId(val);
    if (!id) { setHTML(hint, '<span class="h-muted">还没识别出视频 ID…</span>'); return; }
    const inLib = S.lib.find((v) => v.id === id), inHidden = S.hidden.find((v) => v.id === id);
    if (inLib) setHTML(hint, `<span class="h-info">${ic('list', 13)} 已在列表中：${esc(vTitle(inLib))} · 回车切换</span>`);
    else if (inHidden) setHTML(hint, `<span class="h-info">${ic('eyeOff', 13)} 这个视频已隐藏 · 添加后会自动恢复</span>`);
    else setHTML(hint, `<span class="h-ok">${ic('check', 13)} 识别到视频 ID <code>${esc(id)}</code> · 回车添加</span>`);
  }

  function renderPlayer(v) {
    const vs = $('#vstage');
    const c = v.cover || hashColors(v.id);
    const thumbOk = thumbState[v.id] === 'ok';
    $('#ambient').style.cssText = thumbOk
      ? `background-image:url(https://i.ytimg.com/vi/${v.id}/mqdefault.jpg)`
      : `background-image:linear-gradient(135deg, ${c[0]}, ${c[1]})`;
    let html;
    if (S.player === 'ok' || S.player === 'loading') {
      html = `<div class="yt ${S.playing ? 'playing' : ''}" data-act="videoClick" data-note="系统 WebView（YouTube iframe）· 玻璃无法折射或模糊这里的画面">
        <div class="yt-cover">${coverHTML(v, true)}</div>
        <div class="yt-shade"></div>
        <div class="yt-top"><span class="yt-avatar">${esc((v.channel || 'Y')[0])}</span><span class="yt-title">${esc(vTitle(v))}</span></div>
        ${S.player === 'loading' ? '<div class="yt-loading"><i class="spin spin-lg"></i></div>' : !S.playing ? `<div class="yt-bigplay">${ic('play', 26)}</div>` : ''}
        <div class="yt-logo"><i></i>YouTube</div>
      </div>`;
    } else if (S.player === 'error') {
      html = `<div class="native-card"><div class="nc-ic red">${ic('warn', 26)}</div><h4>无法在应用内播放</h4>
        <p>YouTube 播放器错误 <b>${S.playerCode}</b>：视频所有者不允许在其他网站或应用中嵌入播放。</p>
        <p class="sub">字幕仍可阅读、搜索和复制。</p>
        <div class="nc-btns"><button class="btn primary" data-cmd="openYT">${ic('external', 14)} 在 YouTube 打开</button><button class="btn" data-act="reloadPlayer">重试</button></div></div>`;
    } else {
      html = `<div class="native-card"><div class="nc-ic gray">${ic('film', 26)}</div><h4>播放器不可用</h4>
        <p>内嵌的 YouTube 播放器没有加载成功（WebView 初始化失败或网络不可达）。</p>
        <p class="sub">字幕仍可阅读、搜索和复制。</p>
        <div class="nc-btns"><button class="btn primary" data-act="reloadPlayer">${ic('refetch', 14)} 重新加载播放器</button><button class="btn" data-cmd="openYT">在 YouTube 打开</button></div></div>`;
    }
    setHTML($('#video'), html);
    vs.classList.toggle('native', S.player === 'error' || S.player === 'unavailable');
  }
  function sentenceLines(s, mode, opts = {}) {
    if (!s) return '';
    if (mode === 'en') return `<p class="en">${opts.hl ? opts.hl(s.en) : esc(s.en)}</p>`;
    if (mode === 'zh') return s.zh ? `<p class="zh main">${opts.hl ? opts.hl(s.zh) : esc(s.zh)}</p>` : `<p class="en">${opts.hl ? opts.hl(s.en) : esc(s.en)}</p>${opts.noTag ? '' : '<span class="tag">未翻译</span>'}`;
    const zh = s.zh ? `<p class="zh">${opts.hl ? opts.hl(s.zh) : esc(s.zh)}</p>` : (opts.placeholder || '');
    return `<p class="en">${opts.hl ? opts.hl(s.en) : esc(s.en)}</p>${zh}`;
  }
  function renderFocus(v) {
    const i = curIdx();
    const n = v.sentences.length;
    let html;
    if (!n) {
      const msg = { fetching: '正在获取字幕，视频可以先播放', pending: '等待获取字幕', failed: '字幕获取失败', none: '这个视频没有可用字幕', ready: '字幕为空' }[v.status] || '';
      html = `<div class="focus-empty">${v.status === 'fetching' ? '<i class="spin"></i>' : ic('captions', 16)}<span>${msg}</span></div>`;
    } else {
      const s = v.sentences[Math.max(0, i)];
      const ph = v.status === 'translating' ? '<p class="zh pending">翻译中…</p>' : '';
      const ctx = (c, cls) => (c ? `<p class="focus-ctx ${cls}" data-goto="${v.sentences.indexOf(c)}">${esc(S.mode === 'zh' && c.zh ? c.zh : c.en)}</p>` : `<p class="focus-ctx ${cls} empty"></p>`);
      html = `${ctx(v.sentences[i - 1], 'prev')}<div class="focus-main"><div class="focus-meta"><span>第 ${Math.max(1, i + 1)} / ${n} 句</span>${S.loopIdx != null ? `<span class="chip loop">${ic('loopSent', 12)} 单句循环中</span>` : ''}${i < 0 ? '<span class="chip">即将开始</span>' : ''}</div>
        <div class="focus-text ${i < 0 ? 'dim' : ''}">${sentenceLines(s, S.mode, { placeholder: ph, noTag: true })}</div>
        <div class="focus-prog"><i id="focusProg"></i></div></div>${ctx(v.sentences[i + 1] || (i < 0 ? v.sentences[1] : null), 'next')}`;
    }
    setHTML($('#focus'), html);
  }
  function renderTransport(v) {
    const n = v.sentences.length;
    const i = curIdx();
    const dis = !playerReady() ? 'disabled' : '';
    const sdis = !playerReady() || !n ? 'disabled' : '';
    setHTML($('#ctlRow'), `
      <div class="ctl-l"><span class="pos-chip" title="当前句位置">${n ? `第 <b>${Math.max(0, i + 1)}</b> / ${n} 句` : '暂无字幕'}</span></div>
      <div class="ctl-c">
        <button class="cbtn" data-cmd="replay" ${sdis} title="重听本句（R）">${ic('replay', 18)}</button>
        <button class="cbtn" data-cmd="prev" ${sdis} title="上一句（⌥←）">${ic('prevSent', 18)}</button>
        <button class="cbtn hide-xs" data-cmd="back5" ${dis} title="后退 5 秒（←）">${ic('back5', 19)}</button>
        <button class="cbtn play" data-cmd="togglePlay" ${dis} title="${S.playing ? '暂停' : '播放'}（空格）">${ic(S.playing ? 'pause' : 'play', 20)}</button>
        <button class="cbtn hide-xs" data-cmd="fwd5" ${dis} title="前进 5 秒（→）">${ic('fwd5', 19)}</button>
        <button class="cbtn" data-cmd="next" ${sdis} title="下一句（⌥→）">${ic('nextSent', 18)}</button>
        <button class="cbtn ${S.loopIdx != null ? 'on' : ''}" data-cmd="toggleLoop" ${sdis} title="单句循环（L）">${ic('loopSent', 18)}</button>
      </div>
      <div class="ctl-r">
        <button class="pill-btn" data-cmd="cycleSpeed" ${dis} title="播放速度（点击切换 0.5× → 1.5×）">${S.speed}×</button>
        <button class="cbtn sm ${S.loopMode !== 'off' ? 'on' : ''}" data-cmd="cycleLoopMode" title="视频循环：${LOOP_LABEL[S.loopMode]}（点击切换）">${ic(LOOP_ICON[S.loopMode], 16)}</button>
        <button class="cbtn sm" data-cmd="toggleFs" title="${S.ui.fs ? '退出' : ''}窗口内全屏（F${S.ui.fs ? ' / Esc' : ''}）">${ic('expand', 16)}</button>
        <button class="cbtn sm hide-xs" data-cmd="openYT" title="在 YouTube 打开">${ic('external', 16)}</button>
      </div>`);
    const dur = durationOf(v);
    setHTML($('#ticks'), v.sentences.map((s) => `<i style="left:${(s.s / dur) * 100}%"></i>`).join(''));
    $('#tDur').textContent = fmt(dur);
    $('#transport').classList.toggle('disabled', !playerReady());
  }
  function hlFn(q) {
    if (!q) return null;
    const re = new RegExp(q.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'gi');
    return (t) => esc(t).replace(re, (m) => `<mark>${m}</mark>`);
  }
  function renderTranscript(v) {
    const n = v.sentences.length;
    const si = statusInfo(v);
    const miss = missingCount(v);
    // 状态
    let status = '';
    if (v.status === 'translating') {
      const tc = v.tr && v.tr.fill ? v.tr.done : translatedCount(v), tt = v.tr && v.tr.fill ? v.tr.total : n;
      status = `<span class="st st-violet"><i class="ring" style="--p:${si.pct}"></i>${v.tr && v.tr.fill ? '补全中' : '正在翻译'} ${tc} / ${tt} 句 · ${si.pct}%</span>`;
    } else if (v.status === 'ready' && n) {
      if (!S.cfg.key && miss) status = `<span class="st st-gray">${ic('info', 13)}仅原文</span>`;
      else if (miss === n) status = `<span class="st st-gray">${ic('info', 13)}仅原文</span>`;
      else if (miss) status = `<span class="st st-orange">${ic('warn', 13)}待补全 ${miss} 句</span>`;
      else status = `<span class="st st-green">${ic('checkcircle', 13)}已全部翻译</span>`;
    } else if (v.status === 'fetching') status = '<span class="st st-blue"><i class="spin"></i>获取字幕中</span>';
    else if (v.status === 'failed') status = `<span class="st st-red">${ic('warn', 13)}获取失败</span>`;
    else if (v.status === 'none') status = '<span class="st st-gray">无字幕</span>';
    else if (v.status === 'pending') status = '<span class="st st-gray">待获取</span>';
    else if (v.status === 'ready') status = '<span class="st st-gray">字幕为空</span>';
    setHTML($('#tpStatus'), status);
    // 横幅
    let banner = '';
    if (v.status === 'translating') {
      banner = `<div class="bn bn-progress"><div class="bar"><i style="width:${si.pct}%"></i></div><span>每批 ${TRANSLATE_BATCH} 句，译文会一句句出现 · ${esc(v.model || S.cfg.model)}</span></div>`;
    } else if (v.status === 'ready' && n && !S.cfg.key && miss) {
      banner = `<div class="bn bn-gray">${ic('key', 15)}<span>未配置翻译服务，当前只显示英文原文。</span><button class="btn sm primary" data-act="goSettings" data-tab="translate">去设置</button></div>`;
    } else if (v.status === 'ready' && n && miss && miss < n) {
      banner = `<div class="bn bn-orange">${ic('warn', 15)}<span>有 ${miss} 句在翻译时被跳过。</span><button class="btn sm" data-act="fillTranslate">${ic('translate', 13)} 补全翻译（${miss} 句）</button></div>`;
    }
    setHTML($('#tpBanner'), banner);
    $('#tp').classList.toggle('has-list', n > 0);
    // 元信息
    setHTML($('#tpMeta'), n ? `<span>${ic('download', 12)}${SRC_SHORT[v.source] || '—'}</span><span>${esc(v.kind || '')}</span><span>${n} 句</span>${translatedCount(v) ? `<span>${ic('translate', 12)}${esc(v.model || S.cfg.model)}</span>` : ''}` : '');
    // 列表 / 状态块
    let html;
    if (v.status === 'fetching' || v.status === 'pending') html = fetchCardHTML(v);
    else if (v.status === 'failed') html = failCardHTML(v);
    else if (v.status === 'none') {
      html = `<div class="state-card"><div class="sc-ic gray">${ic('captions', 26)}</div><h4>这个视频没有可用字幕</h4><p>三个来源都没有找到英文字幕（包括自动字幕）。</p>
        ${v.errors ? `<ul class="src-errs">${v.errors.map((e) => `<li><b>${SRC[e.src]}</b><span>${esc(e.msg || '')}</span></li>`).join('')}</ul>` : ''}
        <div class="sc-btns"><button class="btn" data-act="retryFetch">${ic('refetch', 14)} 重试</button><button class="btn" data-cmd="openYT">${ic('external', 14)} 在 YouTube 打开</button></div></div>`;
    } else if (!n) {
      html = `<div class="state-card"><div class="sc-ic gray">${ic('list', 26)}</div><h4>字幕为空</h4><p>获取到了字幕轨，但里面没有可显示的句子。</p><div class="sc-btns"><button class="btn" data-act="askRefetch">${ic('refetch', 14)} 重新获取字幕</button><button class="btn" data-cmd="openYT">${ic('external', 14)} 在 YouTube 打开</button></div></div>`;
    } else {
      const q = S.q.trim();
      const ql = q.toLowerCase();
      const idx = curIdx();
      const hl = hlFn(q);
      const rows = [];
      v.sentences.forEach((s, i) => {
        if (ql && !(s.en.toLowerCase().includes(ql) || (s.zh || '').toLowerCase().includes(ql))) return;
        const ph = S.mode === 'bi' && !s.zh
          ? (v.status === 'translating' ? '<p class="zh pending">翻译中…</p>' : S.cfg.key ? '<p class="zh missing">待补全</p>' : '')
          : '';
        rows.push(`<div class="row ${i === idx ? 'cur' : ''} ${i === S.loopIdx ? 'looping' : ''}" data-i="${i}">
          <span class="row-t">${fmt(s.s)}</span>
          <div class="row-tx">${sentenceLines(s, S.mode, { hl, placeholder: ph })}</div>
          <div class="row-act">
            <button data-row="play" title="从这里播放">${ic('play', 11)}</button>
            <button data-row="loop" class="${i === S.loopIdx ? 'on' : ''}" title="${i === S.loopIdx ? '关闭单句循环' : '单句循环这一句'}">${ic('loopSent', 13)}</button>
            <button data-row="copy" title="复制这一句（中英）">${ic('copy', 13)}</button>
          </div>
          ${i === S.loopIdx ? `<span class="row-loop">${ic('loopSent', 10)}循环</span>` : ''}
          <i class="row-prog"></i>
        </div>`);
      });
      setHTML($('#qCount'), q ? `${rows.length} 条` : '');
      html = rows.length ? rows.join('') : `<div class="state-card slim"><div class="sc-ic gray">${ic('search', 24)}</div><h4>没有找到结果</h4><p>没有句子包含“${esc(q)}”。试试英文原词或更短的中文。</p><button class="btn sm" data-act="clearSearch">清除搜索</button></div>`;
    }
    if (!n) setHTML($('#qCount'), '');
    setHTML($('#tpList'), html);
    $('#tpSearch').classList.toggle('active', !!S.q);
    $('#tpSearch').classList.toggle('disabled', !n);
    $('#qInput').disabled = !n;
    updateReturnBtn();
  }
  function fetchCardHTML(v) {
    const plan = v.fetch ? v.fetch.plan : [];
    const step = v.fetch ? v.fetch.step : -1;
    const rows = ['youtube', 'ytdlp', 'supadata'].map((src, i) => {
      const p = plan[i];
      let st = 'wait', note = i === 0 ? '读取网页字幕轨，失败时改用 Android 客户端接口' : i === 1 ? (S.ytdlp ? `已检测到 ${YTDLP_PATH}` : '未安装 · brew install yt-dlp') : S.supadata ? '已配置 API Key' : '未配置 API Key';
      if (p && i < step) { st = p.result === 'none' ? 'none' : 'fail'; note = p.msg; }
      else if (p && i === step) { st = 'run'; if (i === 0) note = v.fetch.t > p.dur / 2 ? '改用 YouTube Android 客户端接口…' : '读取网页内嵌字幕轨…'; else note = '正在尝试…'; }
      else if (!p && step >= 0 && i > step) { st = 'skip'; }
      const icon = st === 'run' ? '<i class="spin"></i>' : st === 'fail' ? ic('xcircle', 15) : st === 'none' ? ic('minus', 15) : `<span class="n">${i + 1}</span>`;
      return `<li class="fs-${st}"><span class="fs-ic">${icon}</span><div><b>${SRC[src]}</b><small>${esc(note)}</small></div></li>`;
    }).join('');
    return `<div class="state-card fetching"><h4>${v.status === 'pending' ? '等待获取字幕' : '正在获取字幕'}</h4>
      <p>按顺序尝试三个来源，前一个失败才用下一个。${playerReady() ? '视频可以先播放，字幕就绪后会自动出现。' : ''}</p>
      <ol class="fetch-steps">${rows}</ol>
      ${v.status === 'pending' ? '<div class="sc-btns"><button class="btn primary" data-act="retryFetch">开始获取</button></div>' : ''}</div>`;
  }
  function failCardHTML(v) {
    const errs = v.errors || [];
    return `<div class="state-card"><div class="sc-ic red">${ic('warn', 26)}</div><h4>字幕获取失败</h4><p>三个来源都没有成功：</p>
      <ul class="src-errs">${errs.map((e) => `<li><b>${SRC[e.src]}</b><span>${esc(e.msg || '')}</span></li>`).join('')}</ul>
      <div class="sc-btns"><button class="btn primary" data-act="retryFetch">${ic('refetch', 14)} 重试</button><button class="btn" data-cmd="openYT">${ic('external', 14)} 在 YouTube 打开</button><button class="btn" data-act="goSettings" data-tab="sources">字幕来源设置</button></div></div>`;
  }

  /* ---------- 确认框 ---------- */
  function renderDialog() {
    const d = S.dialog;
    let html = '';
    if (d) {
      const v = d.vid ? findVideo(d.vid) : null;
      let title, body, ok, icon = 'warn';
      if (d.kind === 'delete') {
        title = `删除「${esc(v ? vTitle(v) : '')}」？`;
        body = `<p>视频信息、英文原字幕和中文翻译${v && v.sentences.length ? `（${v.sentences.length} 句）` : ''}将被永久删除，无法恢复。</p><p class="alt">只想从列表收起？用“隐藏”，字幕和翻译都会保留。</p>`;
        ok = '删除';
      } else if (d.kind === 'clear') {
        title = '清空视频列表？';
        body = `<p>将删除列表中的 ${S.lib.length} 个视频，以及它们的字幕和翻译，无法恢复。</p>${S.hidden.length ? `<p class="alt">已隐藏的 ${S.hidden.length} 个视频不受影响。</p>` : ''}`;
        ok = '清空';
      } else {
        const tc = v ? translatedCount(v) : 0;
        title = '重新获取字幕？';
        body = `<p>会重新从字幕来源获取「${esc(v ? vTitle(v) : '')}」的英文字幕${tc ? `，当前的中文翻译（${tc} 句）会被替换并重新翻译` : ''}。</p>`;
        ok = '重新获取'; icon = 'refetch';
      }
      html = `<div class="sheet-scrim"></div>
        <div class="alert lgw" role="alertdialog">
          <div class="alert-icon">${appIcon(54)}<span class="alert-badge ${d.kind === 'refetch' ? 'blue' : ''}">${ic(icon, 14)}</span></div>
          <h3>${title}</h3>${body}
          <div class="alert-btns"><button class="abtn" data-dlg="cancel">取消</button><button class="abtn ${d.kind === 'refetch' ? 'primary' : 'danger'}" data-dlg="ok">${ok}</button></div>
        </div>`;
    }
    setHTML($('#mainSheet'), html);
    $('#mainSheet').classList.toggle('on', !!d);
  }
  function openDialog(kind, vid) { showMain(); S.dialog = { kind, vid }; closeMenu(); }
  function confirmDialog() {
    const d = S.dialog; S.dialog = null;
    if (!d) return;
    if (d.kind === 'delete') deleteVideo(d.vid);
    else if (d.kind === 'clear') clearLibrary();
    else if (d.kind === 'refetch') {
      const v = findVideo(d.vid); if (!v) return;
      v.sentences = []; v.tr = null; S.loopIdx = null;
      startFetch(v, 'ok');
      toast('正在重新获取字幕…', 'refetch');
    }
  }

  /* ---------- 设置 ---------- */
  const SET_TABS = [['translate', '翻译', 'translate'], ['sources', '字幕来源', 'download'], ['appearance', '外观', 'palette'], ['overlays', '悬浮窗', 'windows'], ['shortcuts', '快捷键', 'keyboard']];
  function renderSettings() {
    const el = $('#win-settings');
    const show = S.ui.settingsOpen && appVisible();
    el.classList.toggle('hidden', !show);
    el.classList.toggle('active', S.focus === 'settings');
    if (!show) return;
    setHTML($('#setNav'), SET_TABS.map(([k, l, i]) => `<button class="${S.ui.settingsTab === k ? 'on' : ''}" data-act="setTab" data-tab="${k}">${ic(i, 16)}<span>${l}</span></button>`).join(''));
    setHTML($('#setHead'), `<h2>${SET_TABS.find((t) => t[0] === S.ui.settingsTab)[1]}</h2>`);
    if (!S.draft) S.draft = Object.assign({}, S.cfg);
    if (!S.colorDraft) S.colorDraft = Object.assign({}, S.colors);
    if (settingsDirty) { settingsDirty = false; const b = $('#setBody'); b._h = null; setHTML(b, settingsBody()); }
    renderSettingsDynamic();
  }
  function settingsBody() {
    switch (S.ui.settingsTab) {
      case 'translate': return `
        <div class="grp-title">快速预设</div>
        <div class="presets" id="presets"></div>
        <div class="grp-title">服务配置 <small>任意 OpenAI 兼容接口（/chat/completions）</small></div>
        <div class="grp">
          <label class="frow"><span class="fl">服务地址</span><input class="tf" data-draft="baseUrl" spellcheck="false"></label>
          <label class="frow"><span class="fl">模型</span><input class="tf" data-draft="model" spellcheck="false"></label>
          <div class="frow"><span class="fl">API Key</span><div class="tf-wrap"><input class="tf mono" id="keyInput" data-draft="key" spellcheck="false" placeholder="sk-…"><button class="mini" data-act="toggleKey" id="keyEye" title="显示 / 隐藏"></button></div></div>
        </div>
        <div class="savebar" id="saveBar"></div>
        <div class="grp">
          <div class="frow"><span class="fl">测试连接</span><div class="test-zone"><button class="btn" data-act="testConn" id="testBtn">测试连接</button><div class="test-res" id="testRes"></div></div></div>
        </div>
        <p class="note">${ic('lock', 13)}<span>密钥以明文保存在本机 <code>~/Library/Application Support/EchoSub/credentials.json</code>，文件权限 600，与 Swift 版 EchoSub 共用。</span></p>
        <p class="note">${ic('info', 13)}<span>每批 4 句，附前后约 4 句上下文和视频标题，要求模型返回 JSON；Qwen3 系列会自动关闭“思考”。某批失败会稍后重试一次，仍失败就跳过并计入“待补全”。</span></p>`;
      case 'sources': return `
        <p class="lead">按顺序尝试，前一个失败才用下一个。公开视频不需要登录 YouTube。优先选英文字幕轨，并记录人工 / 自动字幕和来源。</p>
        <div class="src-list">
          <div class="src"><span class="src-n">1</span><div class="src-b"><div class="src-t">YouTube 直连 <span class="chip ok">${ic('check', 11)} 始终可用</span></div><p>内置，不需要配置。先读网页里内嵌的字幕轨；失败后换 YouTube Android 客户端接口再试。</p></div></div>
          <div class="src" id="srcYtdlp"></div>
          <div class="src"><span class="src-n">3</span><div class="src-b"><div class="src-t">Supadata <span id="supaChip"></span></div>
            <p>在线服务，可选。只读取已有字幕，不会触发 AI 转写费用。</p>
            <div class="cmd"><input class="tf mono" data-supa placeholder="Supadata API Key（可选）" spellcheck="false"><button class="btn sm" data-act="saveSupa" id="supaSave">存储</button></div></div></div>
        </div>`;
      case 'appearance': return `
        <div class="preview" id="preview" data-note="效果预览：与真实悬浮窗使用同一套配色、字号和透明度"></div>
        <div class="grp-title">字幕配色 <small>同时作用于悬浮字幕和桌面歌词</small></div>
        <div class="swatches" id="swatches"></div>
        <div class="grp">
          <div class="frow"><span class="fl">英文颜色</span><div class="hex"><i class="sw" id="swEn"></i><input class="tf mono" data-cdraft="en" maxlength="7" spellcheck="false"></div></div>
          <div class="frow"><span class="fl">中文颜色</span><div class="hex"><i class="sw" id="swZh"></i><input class="tf mono" data-cdraft="zh" maxlength="7" spellcheck="false"></div></div>
        </div>
        <div class="savebar" id="colorSave"></div>
        <div class="grp-title">字号</div>
        <div class="grp">
          ${sliderRow('悬浮字幕', 'float.font', 12, 36, 1, 'px')}
          ${sliderRow('桌面歌词 · 英文', 'lyr.en', 12, 48, 1, 'px')}
          ${sliderRow('桌面歌词 · 中文', 'lyr.zh', 12, 48, 1, 'px')}
        </div>
        <div class="grp-title">透明度 <small>背景不透明度</small></div>
        <div class="grp">
          ${sliderRow('悬浮字幕背景', 'float.alpha', 0, 100, 1, 'pct')}
          ${sliderRow('桌面歌词背景', 'lyr.alpha', 0, 100, 1, 'pct')}
          ${sliderRow('主窗口背景', 'mainAlpha', 0, 100, 1, 'pct')}
        </div>`;
      case 'overlays': return `
        <div class="grp-title">悬浮字幕</div>
        <div class="grp">
          ${switchRow('显示底板', 'float.plate', '关闭后背景完全透明，只剩文字')}
          ${switchRow('置顶', 'float.pin', '浮在其他 App 的窗口上面')}
          ${switchRow('在所有桌面空间显示', 'float.spaces', '切换 Spaces 时跟随')}
        </div>
        <div class="grp-title">桌面歌词</div>
        <div class="grp">
          <div class="frow"><span class="fl">对齐方式</span><div class="seg sm" data-segbind="lyr.align"><button data-set="lyr.align" data-val="center">${ic('alignCenter', 14)} 居中</button><button data-set="lyr.align" data-val="left">${ic('alignLeft', 14)} 左对齐</button></div></div>
          ${switchRow('显示句内进度', 'lyr.progress', '在歌词下方显示当前这一句播到了哪里')}
        </div>
        <p class="note">${ic('info', 13)}<span>两个悬浮窗目前固定使用深色外观，不跟随系统浅色 / 深色。桌面歌词始终可显示在全屏 App 上和所有桌面空间里。</span></p>`;
      case 'shortcuts': return `
        <table class="kbd-table"><thead><tr><th>操作</th><th>按键</th><th>生效范围</th></tr></thead><tbody>
        ${SHORTCUTS.map(([a, ks, sc]) => `<tr><td>${a}</td><td>${ks.map((k) => `<kbd>${k}</kbd>`).join(' ')}</td><td>${sc}</td></tr>`).join('')}
        </tbody></table>`;
    }
    return '';
  }
  function sliderRow(label, path, min, max, step, unit) {
    const scale = unit === 'pct' ? 100 : 1;
    return `<div class="frow"><span class="fl">${label}</span><div class="slider"><input type="range" min="${min}" max="${max}" step="${step}" data-bind="${path}" data-scale="${scale}"><output data-out="${path}" data-fmt="${unit}"></output></div></div>`;
  }
  function switchRow(label, path, desc) {
    return `<label class="frow sw-row"><span class="fl">${label}${desc ? `<small>${desc}</small>` : ''}</span><input type="checkbox" class="switch" data-bind="${path}"></label>`;
  }
  function renderSettingsDynamic() {
    const tab = S.ui.settingsTab;
    if (tab === 'translate') {
      const d = S.draft;
      setHTML($('#presets'), Object.entries(PRESETS).map(([k, p]) => {
        const on = d.baseUrl === p.baseUrl && d.model === p.model;
        return `<button class="preset ${on ? 'on' : ''}" data-act="preset" data-preset="${k}"><b>${p.name}${p.note === '默认' ? '<em>默认</em>' : ''}</b><span>${esc(p.model)}</span><small>${esc(p.baseUrl.replace('https://', ''))}</small></button>`;
      }).join(''));
      const dirty = d.baseUrl !== S.cfg.baseUrl || d.model !== S.cfg.model || d.key !== S.cfg.key;
      setHTML($('#saveBar'), dirty
        ? `<span>${ic('info', 13)} 有未存储的更改（文本类设置需确认后保存）</span><span class="sp"></span><button class="btn sm" data-act="revertDraft">还原</button><button class="btn sm primary" data-act="saveDraft">存储</button>`
        : `<span class="muted">${ic('check', 13)} ${S.cfg.key ? '已保存' : '尚未配置 API Key，翻译不可用'}</span>`);
      $('#saveBar').classList.toggle('dirty', dirty);
      const ki = $('#keyInput'); if (ki) ki.type = S.showKey ? 'text' : 'password';
      setHTML($('#keyEye'), ic(S.showKey ? 'eyeOff' : 'eye', 15));
      const t = S.test;
      let res = '';
      if (t.state === 'loading') res = `<span class="tr-load"><i class="spin"></i>正在用示例句请求 ${esc(t.model)}…</span>`;
      else if (t.state === 'ok') res = `<span class="tr-ok">${ic('checkcircle', 15)}<span><b>连接成功</b> · ${esc(t.model)} · ${t.ms} ms<small>“Walking helps me think.” → “散步能帮我思考。”</small></span></span>`;
      else if (t.state === 'fail') res = `<span class="tr-fail">${ic('xcircle', 15)}<span><b>连接失败</b> · ${esc(t.err)}</span></span>`;
      setHTML($('#testRes'), res);
      const tb = $('#testBtn'); if (tb) tb.disabled = t.state === 'loading';
    } else if (tab === 'sources') {
      setHTML($('#srcYtdlp'), `<span class="src-n">2</span><div class="src-b"><div class="src-t">本机 yt-dlp ${S.ytdlp ? `<span class="chip ok">${ic('check', 11)} 已检测到</span>` : '<span class="chip warn">未安装</span>'}</div>
          ${S.ytdlp ? `<p>自动检测到可执行文件：<code>${YTDLP_PATH}</code></p>` : `<p>没有找到 yt-dlp。安装后会自动检测：</p><div class="cmd"><code>brew install yt-dlp</code><button class="btn sm" data-act="copyBrew">${ic('copy', 12)} 复制</button><button class="btn sm" data-act="redetect">重新检测</button></div>`}</div>`);
      setHTML($('#supaChip'), S.supadata ? `<span class="chip ok">${ic('check', 11)} 已配置</span>` : '<span class="chip">未配置</span>');
      const ss = $('#supaSave'), dirty = S.supadataDraft !== S.supadata;
      if (ss) { ss.disabled = !dirty; ss.classList.toggle('primary', dirty); }
    } else if (tab === 'appearance') {
      const cd = S.colorDraft;
      setHTML($('#swatches'), COLOR_PRESETS.map((p) => `<button class="swatch ${S.colors.en.toUpperCase() === p.en && S.colors.zh.toUpperCase() === p.zh ? 'on' : ''}" data-act="colorPreset" data-cp="${p.id}"><span class="sw-demo"><b style="color:${p.en}">Aa</b><b style="color:${p.zh}">中文</b></span><span class="sw-name">${p.name}${p.note ? '<em>默认</em>' : ''}</span></button>`).join(''));
      const okHex = (h) => /^#[0-9a-f]{6}$/i.test(h);
      $('#swEn') && ($('#swEn').style.background = okHex(cd.en) ? cd.en : 'transparent');
      $('#swZh') && ($('#swZh').style.background = okHex(cd.zh) ? cd.zh : 'transparent');
      const dirty = cd.en.toUpperCase() !== S.colors.en.toUpperCase() || cd.zh.toUpperCase() !== S.colors.zh.toUpperCase();
      const valid = okHex(cd.en) && okHex(cd.zh);
      setHTML($('#colorSave'), dirty
        ? `<span>${valid ? `${ic('info', 13)} 颜色值确认后生效` : `<span class="h-err">${ic('warn', 13)} 请输入 #RRGGBB 格式的十六进制颜色</span>`}</span><span class="sp"></span><button class="btn sm" data-act="revertColor">还原</button><button class="btn sm primary" data-act="saveColor" ${valid ? '' : 'disabled'}>应用</button>`
        : '');
      $('#colorSave').classList.toggle('dirty', dirty);
      setHTML($('#preview'), previewHTML());
    }
  }
  function previewHTML() {
    const s = D.walking.sentences[13];
    const fa = S.float.plate ? S.float.alpha : 0, la = S.lyr.alpha;
    return `<div class="pv-wall"></div><div class="pv-app"><i></i><i></i><i></i></div>
      <div class="pv-float ${S.float.plate ? 'plate' : ''} ${fa < 0.15 ? 'halo' : ''}" style="--a:${fa}">
        ${ht('pv-en', s[2], 2, `color:${S.colors.en};font-size:${Math.round(S.float.font * 0.62)}px`)}
        ${ht('pv-zh', s[3], 1, `color:${S.colors.zh};font-size:${Math.round(S.float.font * 0.56)}px`)}
        <span class="pv-tag">悬浮字幕</span>
      </div>
      <div class="pv-lyr ${la < 0.15 ? 'halo' : ''} align-${S.lyr.align}" style="--a:${la}">
        ${ht('pv-en', s[2], 2, `color:${S.colors.en};font-size:${Math.round(S.lyr.en * 0.55)}px`)}
        ${ht('pv-zh', s[3], 1, `color:${S.colors.zh};font-size:${Math.round(S.lyr.zh * 0.6)}px`)}
        ${S.lyr.progress ? '<i class="pv-prog"><b></b></i>' : ''}
        <span class="pv-tag">桌面歌词</span>
      </div>`;
  }
  function runTest() {
    const d = S.draft;
    S.test = { state: 'loading', model: d.model || '（未填写模型）' };
    renderSettingsDynamic();
    if (S.sim.holdTest) return;
    setTimeout(() => {
      if (S.test.state !== 'loading') return;
      if (!d.key) S.test = { state: 'fail', model: d.model, err: '未填写 API Key' };
      else if (!/^https?:\/\/\S+$/.test(d.baseUrl)) S.test = { state: 'fail', model: d.model, err: '服务地址无效' };
      else if (S.sim.testOutcome === 'fail') S.test = { state: 'fail', model: d.model, err: 'HTTP 401 · Authentication Fails, Your api key is invalid' };
      else S.test = { state: 'ok', model: d.model, ms: 600 + Math.round(Math.random() * 700) };
      render();
    }, 1500);
  }
  function saveDraft() {
    const hadKey = !!S.cfg.key;
    S.cfg = Object.assign({}, S.draft);
    toast('翻译设置已存储', 'check');
    const v = cur();
    if (!hadKey && S.cfg.key && v && v.status === 'ready' && missingCount(v)) {
      startTranslate(v, { fill: translatedCount(v) > 0 });
      toast('已开始翻译当前视频', 'translate');
    }
  }

  function renderAbout() {
    const el = $('#win-about');
    el.classList.toggle('hidden', !(S.ui.aboutOpen && appVisible()));
    el.classList.toggle('active', S.focus === 'about');
  }
  function renderNotes() {
    const el = $('#win-notes');
    el.classList.toggle('hidden', S.ui.notesHidden);
    el.classList.toggle('active', S.focus === 'notes');
  }

  /* ---------- 悬浮字幕窗口 ---------- */
  function floatDensity(h) { return h < 160 ? 'one' : h <= 300 ? 'three' : 'list'; }
  const DENSITY_LABEL = { one: '单句', three: '上一句 / 当前 / 下一句', list: '滚动列表' };
  function overlayLines(s, sizeEn, sizeZh, linesEn, linesZh, extraCls = '') {
    if (!s) return '';
    const en = S.colors.en, zh = S.colors.zh;
    const showZh = S.mode !== 'en' && s.zh;
    const zhMain = S.mode === 'zh';
    if (zhMain) {
      return s.zh ? ht(`o-zh ${extraCls}`, s.zh, linesEn, `color:${zh};font-size:${sizeEn}px`) : ht(`o-en ${extraCls}`, s.en, linesEn, `color:${en};font-size:${sizeEn}px`);
    }
    return ht(`o-en ${extraCls}`, s.en, linesEn, `color:${en};font-size:${sizeEn}px`) + (showZh ? ht(`o-zh ${extraCls}`, s.zh, linesZh, `color:${zh};font-size:${sizeZh}px`) : '');
  }
  function renderFloat() {
    const el = $('#win-float');
    const show = S.float.open && appVisible();
    el.classList.toggle('hidden', !show);
    if (!show) return;
    const f = S.frames.float;
    const dens = floatDensity(f.h);
    const plate = S.float.plate;
    el.className = `ovl ovl-float dens-${dens} ${plate ? 'plate' : 'noplate'} ${!plate || S.float.alpha < 0.15 ? 'halo' : ''} ${S.ui.resizing === 'float' ? 'resizing' : ''} ${S.float.pin ? 'pinned' : ''}`;
    el.style.setProperty('--a', plate ? S.float.alpha : 0);
    const v = cur();
    const fs = S.float.font, fz = Math.round(fs * 0.88);
    let body;
    if (!v) body = `<div class="o-ph">${ic('captions', 20)}<span>没有正在播放的视频</span><small>在主窗口添加或选择一个视频</small></div>`;
    else if (!v.sentences.length) {
      const m = { fetching: '正在获取字幕…', pending: '等待获取字幕', failed: '字幕获取失败', none: '这个视频没有字幕', ready: '字幕为空' }[v.status];
      body = `<div class="o-ph">${v.status === 'fetching' ? '<i class="spin"></i>' : ic('captions', 20)}<span>${m}</span></div>`;
    } else {
      const i = curIdx();
      const ss = v.sentences;
      if (dens === 'one') {
        body = `<div class="of-one ${i < 0 ? 'dim' : ''}">${overlayLines(ss[Math.max(0, i)], fs, fz, S.mode === 'bi' ? 2 : 3, 1)}</div>`;
      } else if (dens === 'three') {
        const line = (s, cls) => (s ? `<div class="of-ctx ${cls}" data-seek="${ss.indexOf(s)}">${ht('o-en', S.mode === 'zh' && s.zh ? s.zh : s.en, 1, `color:${S.mode === 'zh' && s.zh ? S.colors.zh : S.colors.en};font-size:${Math.round(fs * 0.78)}px`)}</div>` : `<div class="of-ctx ${cls} empty"></div>`);
        body = `${line(ss[i - 1], 'prev')}<div class="of-cur ${i < 0 ? 'dim' : ''}" data-seek="${Math.max(0, i)}">${overlayLines(ss[Math.max(0, i)], fs, fz, 2, 2)}</div>${line(ss[i + 1] || (i < 0 ? ss[1] : null), 'next')}`;
      } else {
        body = `<div class="of-list" id="ofList">${ss.map((s, k) => `<div class="of-row ${k === i ? 'cur' : ''}" data-seek="${k}">${overlayLines(s, Math.round(fs * 0.86), Math.round(fz * 0.86), 6, 6)}</div>`).join('')}<div class="of-list-end"></div></div>
          ${S.float.follow && !S.float.followPaused ? '' : `<button class="of-return" data-act="floatReturn">${ic('follow', 12)} 回到当前句</button>`}`;
      }
    }
    const b = $('#ofBody');
    const keepScroll = b.querySelector('#ofList') ? b.querySelector('#ofList').scrollTop : 0;
    setHTML(b, body);
    const ol = b.querySelector('#ofList'); if (ol && ol.scrollTop !== keepScroll && !floatScrollPending) ol.scrollTop = keepScroll;
    setHTML($('#ofBar'), `
      <button data-cmd="prev" title="上一句">${ic('prevSent', 14)}</button>
      <button data-cmd="togglePlay" title="播放 / 暂停">${ic(S.playing ? 'pause' : 'play', 14)}</button>
      <button data-cmd="next" title="下一句">${ic('nextSent', 14)}</button>
      <i class="sep"></i>
      <button data-act="floatFont" data-d="-2" title="字号减小">${ic('textSmall', 15)}</button>
      <button data-act="floatFont" data-d="2" title="字号增大">${ic('textLarge', 15)}</button>
      <button class="txt" data-cmd="cycleMode" title="切换显示模式：原文 / 中文 / 双语">${MODE_LABEL[S.mode]}</button>
      <i class="sep"></i>
      <button class="${S.float.follow ? 'on' : ''}" data-act="floatFollow" title="自动跟随">${ic('follow', 14)}</button>
      <button class="${S.float.pin ? 'on' : ''}" data-act="floatPin" title="${S.float.pin ? '取消置顶' : '置顶'}">${ic(S.float.pin ? 'pin' : 'pinOff', 14)}</button>
      <button data-cmd="toggleFloat" title="关闭悬浮字幕">${ic('close', 13)}</button>`);
    setHTML($('#ofSize'), `${Math.round(f.w)} × ${Math.round(f.h)} · ${DENSITY_LABEL[dens]}`);
  }
  let floatScrollPending = false;

  /* ---------- 桌面歌词窗口 ---------- */
  function renderLyrics() {
    const el = $('#win-lyrics');
    const show = S.lyr.open && appVisible();
    el.classList.toggle('hidden', !show);
    if (!show) return;
    const a = S.lyr.alpha;
    el.className = `ovl ovl-lyrics ${S.lyr.locked ? 'locked' : ''} ${a < 0.15 ? 'halo' : ''} ${a === 0 ? 'bg-clear' : ''} align-${S.lyr.align}`;
    el.style.setProperty('--a', a);
    const v = cur();
    let inner;
    const s = v && v.sentences.length ? v.sentences[Math.max(0, curIdx())] : null;
    if (!s) {
      const m = !v ? '没有正在播放的视频' : ({ fetching: '正在获取字幕…', failed: '字幕获取失败', none: '这个视频没有字幕', ready: '字幕为空', pending: '等待获取字幕' }[v.status] || '');
      inner = `<div class="ol-ph">${ht('o-en', 'EchoSub', 1, `color:${S.colors.en};font-size:${Math.round(S.lyr.en * 0.8)}px`)}${ht('o-zh', `暂无字幕 · ${m}`, 1, `color:${S.colors.zh};font-size:${S.lyr.zh}px`)}</div>`;
    } else {
      inner = `<div class="ol-text ${curIdx() < 0 ? 'dim' : ''}">${S.mode === 'zh'
        ? (s.zh ? ht('o-zh', s.zh, 3, `color:${S.colors.zh};font-size:${S.lyr.en}px`) : ht('o-en', s.en, 4, `color:${S.colors.en};font-size:${S.lyr.en}px`))
        : ht('o-en', s.en, 4, `color:${S.colors.en};font-size:${S.lyr.en}px`) + (S.mode === 'bi' && s.zh ? ht('o-zh', s.zh, 3, `color:${S.colors.zh};font-size:${S.lyr.zh}px`) : '')}</div>
        ${S.lyr.progress ? '<div class="ol-prog"><i id="olProg"></i></div>' : ''}`;
    }
    setHTML($('#olPlate'), `${inner}${S.lyr.locked ? `<span class="ol-lock">${ic('lock', 12)}</span>` : ''}`);
    setHTML($('#olBar'), `
      <button data-cmd="prev" title="上一句">${ic('prevSent', 14)}</button>
      <button data-cmd="togglePlay" title="播放 / 暂停">${ic(S.playing ? 'pause' : 'play', 14)}</button>
      <button data-cmd="next" title="下一句">${ic('nextSent', 14)}</button>
      <i class="sep"></i>
      <span class="ol-wide">
        <span class="stp"><em>EN</em><button data-act="lyrFont" data-k="en" data-d="-1" title="英文字号减小">${ic('minus', 12)}</button><output data-out="lyr.en"></output><button data-act="lyrFont" data-k="en" data-d="1" title="英文字号增大">${ic('plus', 12)}</button></span>
        <span class="stp"><em>中</em><button data-act="lyrFont" data-k="zh" data-d="-1" title="中文字号减小">${ic('minus', 12)}</button><output data-out="lyr.zh"></output><button data-act="lyrFont" data-k="zh" data-d="1" title="中文字号增大">${ic('plus', 12)}</button></span>
        <button data-act="lyrAlign" title="对齐：${S.lyr.align === 'center' ? '居中' : '左对齐'}">${ic(S.lyr.align === 'center' ? 'alignCenter' : 'alignLeft', 14)}</button>
        <span class="opa" title="背景透明度">${ic('opacity', 13)}<input type="range" min="0" max="100" data-bind="lyr.alpha" data-scale="100"><output data-out="lyr.alpha" data-fmt="pct"></output></span>
      </span>
      <button class="ol-narrow" data-act="lyrMore" title="字号、对齐、透明度">${ic('textLarge', 15)}</button>
      <i class="sep"></i>
      <button data-cmd="toggleLock" title="锁定（鼠标穿透）">${ic('unlock', 14)}</button>
      <button data-cmd="toggleLyrics" title="关闭桌面歌词">${ic('close', 13)}</button>`);
  }

  /* ---------- 菜单 / 弹出层 ---------- */
  function mi(label, act, o = {}) { return Object.assign({ label, act }, o); }
  const SEP = { sep: true };
  function appMenu() {
    return [
      mi('关于 EchoSub', () => { S.ui.aboutOpen = true; bringFront('about'); S.focus = 'about'; }), SEP,
      mi('设置…', () => CMD.openSettings(), { kbd: '⌘,' }), SEP,
      mi('隐藏 EchoSub', () => { S.ui.appHidden = true; S.focus = S.ui.notesHidden ? 'desktop' : 'notes'; }, { kbd: '⌘H' }),
      mi('隐藏其他', () => { S.ui.notesHidden = true; }, { kbd: '⌥⌘H', disabled: S.ui.notesHidden }),
      mi('全部显示', () => { S.ui.notesHidden = false; S.ui.appHidden = false; }, { disabled: !S.ui.notesHidden && !S.ui.appHidden }), SEP,
      mi('退出 EchoSub', quitApp, { kbd: '⌘Q' }),
    ];
  }
  function editMenu() {
    const n = () => {};
    return [mi('撤销', n, { kbd: '⌘Z' }), mi('重做', n, { kbd: '⇧⌘Z' }), SEP, mi('剪切', n, { kbd: '⌘X' }), mi('拷贝', n, { kbd: '⌘C' }), mi('粘贴', n, { kbd: '⌘V' }), mi('全选', n, { kbd: '⌘A' })];
  }
  function playMenu() {
    const has = !!cur(), hs = has && cur().sentences.length > 0;
    return [
      mi(S.playing ? '暂停' : '播放', CMD.togglePlay, { kbd: '空格', disabled: !has }),
      mi('上一句', CMD.prev, { kbd: '⌥←', disabled: !hs }),
      mi('下一句', CMD.next, { kbd: '⌥→', disabled: !hs }),
      mi('重听本句', CMD.replay, { kbd: 'R', disabled: !hs }),
      mi('单句循环', CMD.toggleLoop, { kbd: 'L', check: S.loopIdx != null, disabled: !hs }), SEP,
      mi('后退 5 秒', CMD.back5, { kbd: '←', disabled: !has }),
      mi('前进 5 秒', CMD.fwd5, { kbd: '→', disabled: !has }), SEP,
      { header: '播放速度' },
      ...SPEEDS.map((sp) => mi(`${sp}×`, () => { S.speed = sp; }, { radio: true, check: S.speed === sp, disabled: !has })),
    ];
  }
  function windowMenu() {
    return [
      mi(`${S.float.open ? '隐藏' : '显示'}悬浮字幕`, CMD.toggleFloat, { kbd: '⇧⌘F' }),
      mi(`${S.lyr.open ? '隐藏' : '显示'}桌面歌词`, CMD.toggleLyrics, { kbd: '⇧⌘D' }),
      mi(`${S.lyr.locked ? '解锁' : '锁定'}桌面歌词`, CMD.toggleLock, { kbd: '⇧⌘L', disabled: !S.lyr.open }),
      mi(`${S.ui.sideAreas ? '隐藏' : '显示'}字幕与播放列表区域`, CMD.toggleSideAreas, { kbd: '⌃⌘T' }),
      mi('播放列表', CMD.togglePlaylist, { kbd: '⌃⌘S', check: S.ui.sidebar && S.ui.sideAreas }), SEP,
      mi('最小化', () => { S.ui.mainMin = true; S.focus = 'desktop'; }, { kbd: '⌘M', disabled: !mainShown() }),
      mi('缩放', () => toggleZoom(), { disabled: !mainShown() }),
      mi(S.ui.sysfs ? '退出全屏幕' : '进入全屏幕', () => { S.ui.sysfs = !S.ui.sysfs; showMain(); }, { kbd: 'fn F', disabled: !S.ui.mainVisible }), SEP,
      mi('EchoSub', () => showMain(), { check: S.focus === 'main' }),
      ...(S.ui.settingsOpen ? [mi('设置', () => { bringFront('settings'); S.focus = 'settings'; }, { check: S.focus === 'settings' })] : []),
    ];
  }
  function statusMenu() {
    const v = cur(), hs = !!v && v.sentences.length > 0;
    return [
      { info: v ? vTitle(v) : '没有正在播放的视频' }, SEP,
      mi(S.playing ? '暂停' : '播放', CMD.togglePlay, { disabled: !v }),
      mi('上一句', CMD.prev, { disabled: !hs }),
      mi('下一句', CMD.next, { disabled: !hs }), SEP,
      mi('悬浮字幕', CMD.toggleFloat, { check: S.float.open }),
      mi('桌面歌词', CMD.toggleLyrics, { check: S.lyr.open }),
      mi('锁定桌面歌词', CMD.toggleLock, { check: S.lyr.locked, disabled: !S.lyr.open }), SEP,
      { header: '字幕模式' },
      ...MODES.map(([k, l]) => mi(l, () => { S.mode = k; }, { radio: true, check: S.mode === k })), SEP,
      mi('打开主窗口', () => showMain()),
      mi('退出 EchoSub', quitApp),
    ];
  }
  function rowMenu(vid) {
    const v = findVideo(vid);
    return [
      mi('播放', () => selectVideo(vid)),
      mi('从头播放', () => selectVideo(vid, { fromStart: true })),
      SEP,
      mi('在浏览器中打开', () => CMD.openYT({ dataset: { vid } }), { icon: 'external' }),
      mi('拷贝视频链接', () => copyText(`https://www.youtube.com/watch?v=${vid}`, '已拷贝视频链接'), { icon: 'link' }),
      SEP,
      mi('重新获取字幕…', () => openDialog('refetch', vid), { icon: 'refetch', disabled: v.status === 'fetching' }),
      mi('隐藏', () => hideVideo(vid), { icon: 'eyeOff' }),
      mi('删除…', () => openDialog('delete', vid), { icon: 'trash', danger: true }),
    ];
  }
  function tpMoreMenu() {
    const v = cur(); if (!v) return [];
    const n = v.sentences.length, miss = missingCount(v);
    return [
      mi('复制全部字幕', copyAll, { icon: 'copy', disabled: !n }),
      mi(`补全翻译（${miss} 句）`, fillTranslate, { icon: 'translate', disabled: !(v.status === 'ready' && miss && S.cfg.key && miss < n) }),
      SEP,
      mi('重新获取字幕…', () => openDialog('refetch', v.id), { icon: 'refetch', disabled: v.status === 'fetching' }),
      mi('在 YouTube 打开', () => CMD.openYT(), { icon: 'external' }),
    ];
  }
  function copyAll() {
    const v = cur(); if (!v) return;
    const txt = v.sentences.map((s) => `[${fmt(s.s)}] ${s.en}${s.zh ? `\n${s.zh}` : ''}`).join('\n\n');
    copyText(txt, `已复制全部字幕（${v.sentences.length} 句${translatedCount(v) ? '双语' : '原文'}）`);
  }
  function fillTranslate() {
    const v = cur(); if (!v) return;
    if (!S.cfg.key) { toast('请先配置 API Key', 'key', 'warn'); return; }
    startTranslate(v, { fill: true });
    toast(`开始补全 ${v.tr ? v.tr.total : 0} 句翻译`, 'translate');
  }
  function quitApp() {
    S.ui.quit = true; S.playing = false; S.ui.settingsOpen = false; S.ui.aboutOpen = false; S.dialog = null; S.focus = 'desktop';
    toast('EchoSub 已退出 · 点击程序坞图标重新打开', 'info');
  }
  function toggleZoom() {
    if (S.ui.zoomed) { S.frames.main = S.ui.zoomed; S.ui.zoomed = null; }
    else { S.ui.zoomed = Object.assign({}, S.frames.main); S.frames.main = { x: 8, y: MB_H + 8, w: DESK_W - 16, h: DESK_H - MB_H - 16 - DOCK_H }; }
  }
  function stageRect(el) {
    const r = el.getBoundingClientRect(), sr = $('#stage').getBoundingClientRect(), k = scale;
    return { x: (r.left - sr.left) / k, y: (r.top - sr.top) / k, w: r.width / k, h: r.height / k };
  }
  function openMenu(items, anchorEl, owner, opts = {}) {
    const r = stageRect(anchorEl);
    S.menu = { type: 'menu', items, x: opts.alignRight ? r.x + r.w : r.x, y: r.y + r.h + (opts.gap ?? 4), owner, alignRight: !!opts.alignRight };
  }
  function openPopover(html, anchorEl, owner, opts = {}) {
    const r = stageRect(anchorEl);
    S.menu = { type: 'pop', html, x: r.x + (opts.dx || 0), y: opts.above ? r.y - 8 : r.y + r.h + 6, above: !!opts.above, owner, cls: opts.cls || '' };
  }
  function closeMenu() { S.menu = null; }
  function renderMenu() {
    const m = S.menu, layer = $('#menu-layer');
    if (!m) { setHTML(layer, ''); return; }
    let html;
    if (m.type === 'menu') {
      html = `<div class="menu lgw" id="menuEl" style="left:${m.x}px;top:${m.y}px">${m.items.map((it, i) => {
        if (it.sep) return '<div class="msep"></div>';
        if (it.header) return `<div class="mhead">${esc(it.header)}</div>`;
        if (it.info) return `<div class="minfo">${esc(it.info)}</div>`;
        return `<div class="mi ${it.disabled ? 'dis' : ''} ${it.danger ? 'danger' : ''}" data-mi="${i}"><span class="mi-c">${it.check ? (it.radio ? '<i class="dot"></i>' : ic('check', 12)) : ''}</span><span class="mi-l">${esc(it.label)}</span>${it.kbd ? `<span class="mi-k">${it.kbd}</span>` : ''}</div>`;
      }).join('')}</div>`;
    } else {
      html = `<div class="pop lgw ${m.cls}" id="menuEl" style="left:${m.x}px;top:${m.y}px">${typeof m.html === 'function' ? m.html() : m.html}</div>`;
    }
    if (layer._h === html) return;
    setHTML(layer, html);
    const el = $('#menuEl');
    const w = el.offsetWidth, h = el.offsetHeight;
    let x = m.alignRight ? m.x - w : m.x, y = m.above ? m.y - h : m.y;
    x = clamp(x, 6, DESK_W - w - 6); y = clamp(y, MB_H, DESK_H - h - 6);
    el.style.left = `${x}px`; el.style.top = `${y}px`;
  }
  function hiddenPopHTML() {
    if (!S.hidden.length) return `<div class="pop-h"><b>已隐藏的视频</b></div><div class="pop-empty">${ic('eyeOff', 24)}<b>没有隐藏的视频</b><span>在视频的 ⋯ 菜单里选择“隐藏”，可以把它从列表收起，字幕和翻译都会保留。</span></div>`;
    return `<div class="pop-h"><b>已隐藏的视频</b><small>${S.hidden.length} 个 · 字幕和翻译都已保留</small></div>
      <div class="pop-list">${S.hidden.map((v) => `<div class="pop-row"><div class="lib-thumb sm">${coverHTML(v)}</div><div class="lib-meta"><div class="lib-title">${esc(vTitle(v))}</div><div class="lib-sub">${esc(v.channel || '')} ${badge(v, true)}</div></div><button class="btn sm" data-act="restore" data-vid="${v.id}">${ic('restore', 12)} 恢复</button></div>`).join('')}</div>`;
  }
  function lyrMorePopHTML() {
    return `<div class="pop-h"><b>桌面歌词</b></div>
      <div class="pop-form">
        <div class="pf-row"><span>英文字号</span><span class="stp"><button data-act="lyrFont" data-k="en" data-d="-1">${ic('minus', 12)}</button><output data-out="lyr.en"></output><button data-act="lyrFont" data-k="en" data-d="1">${ic('plus', 12)}</button></span></div>
        <div class="pf-row"><span>中文字号</span><span class="stp"><button data-act="lyrFont" data-k="zh" data-d="-1">${ic('minus', 12)}</button><output data-out="lyr.zh"></output><button data-act="lyrFont" data-k="zh" data-d="1">${ic('plus', 12)}</button></span></div>
        <div class="pf-row"><span>对齐</span><div class="seg sm"><button data-set="lyr.align" data-val="center">居中</button><button data-set="lyr.align" data-val="left">左对齐</button></div></div>
        <div class="pf-row"><span>背景透明度</span><span class="opa"><input type="range" min="0" max="100" data-bind="lyr.alpha" data-scale="100"><output data-out="lyr.alpha" data-fmt="pct"></output></span></div>
      </div>`;
  }

  /* ---------- 原型控制面板 ---------- */
  const SCENES = [
    ['视频库', [['default', '默认演示'], ['empty', '视频库为空（首次使用）'], ['noHidden', '没有隐藏的视频'], ['filterEmpty', '筛选没有结果']]],
    ['字幕获取', [['fetching', '正在获取（可先播放）'], ['failed', '获取失败（三个来源）'], ['none', '视频没有字幕'], ['emptySubs', '字幕为空']]],
    ['翻译', [['translating', '正在翻译（有进度）'], ['partial', '完成但待补全'], ['noKey', '未配置 API Key'], ['restart', '模拟 App 重启']]],
    ['阅读与播放', [['searchEmpty', '搜索没有结果'], ['playerUnavailable', '播放器不可用'], ['playerError', '播放器报错（禁止嵌入）'], ['inwinFs', '窗口内全屏']]],
    ['设置 · 测试连接', [['testLoading', '进行中'], ['testOk', '成功'], ['testFail', '失败']]],
    ['确认框', [['askDelete', '删除'], ['askClear', '清空'], ['askRefetch', '重新获取字幕']]],
    ['悬浮字幕', [['fOne', '单句（高 < 160）'], ['fThree', '三句（160–300）'], ['fList', '列表（> 300）'], ['fPlate', '有底板'], ['fNoPlate', '无底板'], ['fNoVideo', '没有视频时']]],
    ['桌面歌词', [['lNormal', '普通'], ['lLocked', '锁定（穿透）'], ['lClear', '背景全透明'], ['lNarrow', '窄宽多行'], ['lNoSubs', '没有字幕时']]],
  ];
  const ADD_EXAMPLES = [
    ['watch?v=', 'https://www.youtube.com/watch?v=Zk3pQm8Lx2C&t=42s'],
    ['youtu.be', 'youtu.be/Tq9wEr4Ty6U'],
    ['shorts', 'https://youtube.com/shorts/Ab1Cd2Ef3Gh'],
    ['live', 'https://www.youtube.com/live/Lv7Qw2Er9Ty?feature=share'],
    ['11 位 ID', 'Mx4Nb7Vc2Zq'],
    ['已在列表', 'https://youtu.be/dQw4w9WgXcQ'],
    ['已隐藏', 'Lr5uTq0Zx8B'],
    ['无效链接', 'https://vimeo.com/76979871'],
  ];
  let protoBuilt = false;
  let activeScene = 'default';
  function renderProto() {
    const p = $('#proto');
    p.classList.toggle('collapsed', S.ui.protoCollapsed);
    document.getElementById('app').classList.toggle('proto-collapsed', S.ui.protoCollapsed);
    if (!protoBuilt) {
      protoBuilt = true;
      p.innerHTML = `
        <button class="pp-toggle" data-act="protoToggle" title="收起 / 展开控制台"></button>
        <div class="pp">
          <header class="pp-head"><b>原型控制台</b><small>不属于产品界面 · 用于切换状态</small></header>
          <section><h4>系统外观</h4>
            <div class="pseg"><button data-act="theme" data-v="light">浅色</button><button data-act="theme" data-v="dark">深色</button></div>
            <label class="pchk"><input type="checkbox" data-bind="sim.annot"> 显示技术约束标注</label>
            <label class="pchk"><input type="checkbox" data-bind="sim.actual"> 桌面按 100% 实际尺寸显示</label>
          </section>
          <section><h4>状态场景 <small>功能清单第 13 节</small></h4>
            ${SCENES.map(([g, items]) => `<div class="pgroup"><span>${g}</span><div class="pbtns">${items.map(([k, l]) => `<button data-scene="${k}">${l}</button>`).join('')}</div></div>`).join('')}
          </section>
          <section><h4>添加链接试一试</h4>
            <div class="pbtns">${ADD_EXAMPLES.map(([l, u]) => `<button data-example="${esc(u)}" title="${esc(u)}">${l}</button>`).join('')}</div>
            <label class="pfield"><span>新视频获取结果</span><select data-bind="sim.addOutcome">
              <option value="ok">成功（YouTube 直连）</option><option value="fallback">成功（回退到 yt-dlp）</option><option value="partial">成功，但有批次翻译失败</option>
              <option value="fail">三个来源都失败</option><option value="none">视频没有字幕</option><option value="empty">字幕为空</option></select></label>
            <label class="pfield"><span>模拟剪贴板</span><input data-bind="sim.clip" spellcheck="false"></label>
          </section>
          <section><h4>模拟环境</h4>
            <label class="pfield"><span>测试连接结果</span><select data-bind="sim.testOutcome"><option value="ok">成功</option><option value="fail">失败（401）</option></select></label>
            <label class="pchk"><input type="checkbox" data-bind="ytdlp"> 本机已安装 yt-dlp</label>
            <label class="pchk"><input type="checkbox" data-bind="sim.paused"> 暂停后台获取 / 翻译进度</label>
          </section>
          <section><h4>窗口状态 <small>与菜单栏、App 菜单、主窗口、悬浮窗按钮同步</small></h4>
            <label class="pchk"><input type="checkbox" data-bind="float.open"> 悬浮字幕</label>
            <label class="pchk"><input type="checkbox" data-bind="lyr.open"> 桌面歌词</label>
            <label class="pchk"><input type="checkbox" data-bind="lyr.locked" id="ppLock"> 桌面歌词已锁定</label>
            <div class="pstate" id="ppState"></div>
            <div class="pbtns"><button data-act="openMainP">显示主窗口</button><button data-act="resetLayout">重置窗口布局</button><button data-scene="default">重置全部</button></div>
          </section>
          <section class="pp-help"><h4>提示</h4>
            <p>拖动标题栏移动窗口，拖动边缘 / 角落改变大小。悬浮字幕按高度切换显示密度。右键或 ⋯ 打开视频的更多操作。快捷键：空格、←/→、⌥←/⌥→、R、L、F、⌘F、⌘,、⇧⌘F、⇧⌘D、⇧⌘L、⌃⌘T、⌃⌘S（需先点击主窗口获得焦点；部分组合可能被浏览器占用，可用菜单代替）。</p>
          </section>
        </div>`;
    }
    $$('[data-act="theme"]', p).forEach((b) => b.classList.toggle('on', b.dataset.v === S.theme));
    $$('[data-scene]', p).forEach((b) => b.classList.toggle('on', b.dataset.scene === activeScene));
    const ppl = $('#ppLock'); if (ppl) ppl.disabled = !S.lyr.open;
    setHTML($('#ppState'), `菜单栏 / App 菜单 / 工具栏 当前读到：悬浮字幕 <b>${S.float.open ? '开' : '关'}</b> · 桌面歌词 <b>${S.lyr.open ? '开' : '关'}</b> · 锁定 <b>${S.lyr.locked ? '是' : '否'}</b> · 模式 <b>${MODE_LABEL[S.mode]}</b>`);
  }

  function applyScene(k) {
    activeScene = k;
    const keepTheme = S.theme;
    const fresh = () => { S = initialState(S); S.theme = keepTheme; mwMode = null; settingsDirty = true; addErr.side = addErr.hero = false; $$('[data-addinput]').forEach((i) => { i.value = ''; }); const fi = $('#filterInput'); if (fi) fi.value = ''; const q = $('#qInput'); if (q) q.value = ''; };
    const walking = () => S.lib[0];
    const layout = defaultFrames();
    if (!['fOne', 'fThree', 'fList', 'fPlate', 'fNoPlate', 'lNormal', 'lLocked', 'lClear', 'lNarrow'].includes(k)) fresh();
    S.dialog = null; S.menu = null;
    switch (k) {
      case 'default': S.frames = layout; S.playing = true; break;
      case 'empty': S.lib = []; S.hidden = []; S.curId = null; break;
      case 'noHidden': S.hidden = []; S.ui.sidebar = true; requestAnimationFrame(() => { const b = $('#hiddenBtn'); if (b) { openPopover(hiddenPopHTML, b, 'hidden', { above: true }); render(); } }); break;
      case 'filterEmpty': S.filter = 'kubernetes'; S.ui.sidebar = true; $('#filterInput').value = S.filter; break;
      case 'fetching': {
        const v = V({ id: 'Zk3pQm8Lx2C', status: 'pending', outcome: 'fallback' });
        S.lib.unshift(v); S.curId = v.id; S.t = 4; startFetch(v, 'fallback'); S.playing = true; S.sim.slow = 4; break;
      }
      case 'failed': S.curId = 'dQw4w9WgXcQ'; S.t = 0; break;
      case 'none': S.curId = 'jNQXAC9IVRw'; S.t = 7; break;
      case 'emptySubs': {
        const v = V({ id: 'Qe2Rt5Yu8Io', title: 'Northern Lights Timelapse · 4K', channel: 'Sky Studio', duration: 184, status: 'ready', source: 'supadata', kind: '自动字幕', cover: ['#43cea2', '#185a9d'], glyph: 'SKY' });
        S.lib.unshift(v); S.curId = v.id; S.t = 12; break;
      }
      case 'translating': { const v = walking(); v.sentences.forEach((s, i) => { if (i >= 10) s.zh = null; }); v.status = 'translating'; v.tr = { queue: v.sentences.map((_, i) => i).filter((i) => i >= 10), total: v.sentences.length, done: 10, fill: false, batch: 0 }; S.t = 3; S.playing = true; S.sim.slow = 2; break; }
      case 'partial': { const v = walking(); [14, 15, 16, 17, 38].forEach((i) => { v.sentences[i].zh = null; }); S.t = 80; break; }
      case 'noKey': { S.cfg.key = ''; const v = walking(); v.sentences.forEach((s) => { s.zh = null; }); v.model = null; S.lib[1].status = 'ready'; S.lib[1].tr = null; S.t = 61.5; break; }
      case 'restart': {
        S.lib.forEach((v) => { if (v.status === 'translating') { v.status = 'ready'; v.tr = null; } });
        S.curId = 'Nm4tKs9Qe1R'; S.t = 0;
        toast('已模拟重启：残留的“翻译中”已恢复为可继续（待补全）状态', 'refetch');
        break;
      }
      case 'searchEmpty': S.q = 'xylophone'; S.t = 61.5; requestAnimationFrame(() => { const q = $('#qInput'); if (q) q.value = S.q; }); break;
      case 'playerUnavailable': S.player = 'unavailable'; break;
      case 'playerError': S.player = 'error'; S.playerCode = 150; break;
      case 'inwinFs': S.ui.fs = true; S.playing = true; break;
      case 'testLoading': case 'testOk': case 'testFail':
        S.ui.settingsOpen = true; S.ui.settingsTab = 'translate'; bringFront('settings'); S.focus = 'settings';
        S.test = k === 'testLoading' ? { state: 'loading', model: S.cfg.model } : k === 'testOk' ? { state: 'ok', model: S.cfg.model, ms: 842 } : { state: 'fail', model: S.cfg.model, err: 'HTTP 401 · Authentication Fails, Your api key is invalid' };
        break;
      case 'askDelete': S.dialog = { kind: 'delete', vid: 'Wq7aLk2Pn0s' }; break;
      case 'askClear': S.dialog = { kind: 'clear' }; break;
      case 'askRefetch': S.dialog = { kind: 'refetch', vid: 'Wq7aLk2Pn0s' }; break;
      case 'fOne': S.float.open = true; S.frames.float = Object.assign({}, S.frames.float, { h: 120 }); break;
      case 'fThree': S.float.open = true; S.frames.float = Object.assign({}, S.frames.float, { h: 214 }); break;
      case 'fList': S.float.open = true; S.frames.float = Object.assign({}, S.frames.float, { y: 300, h: 420 }); floatScrollPending = true; break;
      case 'fPlate': S.float.open = true; S.float.plate = true; if (S.float.alpha < 0.3) S.float.alpha = 0.5; break;
      case 'fNoPlate': S.float.open = true; S.float.plate = false; break;
      case 'fNoVideo': S.curId = null; S.float.open = true; S.ui.sidebar = true; break;
      case 'lNormal': S.lyr.open = true; S.lyr.locked = false; S.lyr.alpha = 0.35; S.frames.lyrics = layout.lyrics; break;
      case 'lLocked': S.lyr.open = true; S.lyr.locked = true; toast('桌面歌词已锁定：试着点击它，点击会穿透到下面的窗口', 'lock'); break;
      case 'lClear': S.lyr.open = true; S.lyr.locked = false; S.lyr.alpha = 0; break;
      case 'lNarrow': {
        S.lyr.open = true; S.lyr.locked = false; S.frames.lyrics = { x: 566, y: 858, w: 380 };
        const v = cur(); if (v && v.key === 'walking') { S.t = v.sentences[19].s + 2; S.playing = false; }
        break;
      }
      case 'lNoSubs': { S.lyr.open = true; S.lyr.locked = false; const v = V({ id: 'Zk3pQm8Lx2C', status: 'pending', outcome: 'ok' }); S.lib.unshift(v); S.curId = v.id; startFetch(v, 'ok'); S.sim.paused = true; break; }
    }
    if (k !== 'default' && !['fOne', 'fThree', 'fList', 'fPlate', 'fNoPlate', 'lNormal', 'lLocked', 'lClear', 'lNarrow'].includes(k) && !S.ui.settingsOpen) S.focus = 'main';
    pendingScrollToCurrent = true;
    render();
  }

  /* ---------- 窗口几何 / 层级 ---------- */
  function bringFront(w) { S.z = S.z.filter((x) => x !== w).concat(w); }
  function applyFrames() {
    const F = S.frames;
    const place = (id, f, withH = true) => {
      const el = $(`#win-${id}`); if (!el) return;
      el.style.left = `${f.x}px`; el.style.top = `${f.y}px`; el.style.width = `${f.w}px`;
      if (withH) el.style.height = `${f.h}px`;
    };
    if (S.ui.sysfs && mainShown()) place('main', { x: 0, y: 0, w: DESK_W, h: DESK_H }); else place('main', F.main);
    place('settings', F.settings); place('about', F.about, false); place('notes', F.notes);
    place('float', F.float);
    // 桌面歌词高度随文字变化：以底边为锚点（frames.lyrics.y 表示底边），文字变多时向上长
    const ly = $('#win-lyrics');
    ly.style.left = `${F.lyrics.x}px`; ly.style.width = `${F.lyrics.w}px`; ly.style.top = 'auto'; ly.style.bottom = `${DESK_H - F.lyrics.y}px`;
    S.z.forEach((w, i) => { const el = $(`#win-${w}`); if (el) el.style.zIndex = 10 + i; });
    // 置顶的悬浮字幕在普通窗口之上；不置顶时参与普通窗口层级。桌面歌词始终在最上层。
    $('#win-float').style.zIndex = S.float.pin ? 900 : 10 + S.z.indexOf('float');
    $('#win-lyrics').style.zIndex = 950;
    const mw = $('#mw');
    if (mw) mw.classList.toggle('sb-docked', (S.ui.sysfs ? DESK_W : F.main.w) >= SIDEBAR_DOCK_MIN);
  }

  /* ---------- 同步：输入控件、输出值 ---------- */
  function fmtOut(v, f) { return f === 'pct' ? `${Math.round(v * 100)}%` : f === 'px' ? `${v}` : `${v}`; }
  function sync() {
    const active = document.activeElement;
    $$('[data-bind]').forEach((el) => {
      if (el === active && el.type !== 'checkbox') return;
      const v = getPath(el.dataset.bind);
      if (el.type === 'checkbox') el.checked = !!v;
      else if (el.type === 'range') el.value = v * (Number(el.dataset.scale) || 1);
      else if (el.value !== String(v)) el.value = v;
    });
    $$('[data-out]').forEach((el) => { el.textContent = fmtOut(getPath(el.dataset.out), el.dataset.fmt); });
    $$('[data-set]').forEach((el) => el.classList.toggle('on', String(getPath(el.dataset.set)) === el.dataset.val));
    $$('[data-draft]').forEach((el) => { if (el !== active && S.draft) el.value = S.draft[el.dataset.draft]; });
    $$('[data-cdraft]').forEach((el) => { if (el !== active && S.colorDraft) el.value = S.colorDraft[el.dataset.cdraft]; });
    $$('[data-supa]').forEach((el) => { if (el !== active) el.value = S.supadataDraft; });
    $$('#modeSeg button').forEach((b) => b.classList.toggle('on', b.dataset.mode === S.mode));
    const fb = $('#followBtn');
    if (fb) {
      fb.classList.toggle('on', S.follow && !S.followPaused);
      fb.classList.toggle('paused', S.follow && S.followPaused);
      fb.title = !S.follow ? '自动跟随：已关闭（点击开启）' : S.followPaused ? '自动跟随：已暂停（点击恢复）' : '自动跟随：开启（点击关闭）';
      fb.querySelector('span').textContent = !S.follow ? '跟随关' : S.followPaused ? '已暂停' : '跟随';
    }
    const fi = $('#filterInput'); if (fi && fi !== active && fi.value !== S.filter) fi.value = S.filter;
    $('.side-search') && $('.side-search').classList.toggle('active', !!S.filter);
  }
  const getPath = (p) => p.split('.').reduce((o, k) => (o == null ? o : o[k]), S);
  const setPath = (p, val) => { const ks = p.split('.'); const last = ks.pop(); ks.reduce((o, k) => o[k], S)[last] = val; };

  /* ---------- 每帧：时间推进 ---------- */
  let lastT = performance.now(), simAcc = 0, trAcc = 0, lastSaveBucket = -1;
  function frame(now) {
    const dt = Math.min(0.25, (now - lastT) / 1000); lastT = now;
    const v = cur();
    if (v && S.playing && playerReady() && appRunning()) {
      let t = S.t + dt * S.speed;
      if (S.loopIdx != null && v.sentences[S.loopIdx] && t >= v.sentences[S.loopIdx].e) t = v.sentences[S.loopIdx].s;
      const dur = durationOf(v);
      if (t >= dur) handleEnd(v, dur); else S.t = t;
      const bucket = Math.floor(S.t / AUTOSAVE_EVERY);
      if (bucket !== lastSaveBucket) { lastSaveBucket = bucket; v.progress = S.t; }
    }
    if (!S.sim.paused) {
      simAcc += dt; trAcc += dt;
      if (simAcc >= 0.1) { const had = allVideos().some((x) => x.status === 'fetching'); stepFetch(simAcc / S.sim.slow); simAcc = 0; if (had) render(); }
      if (trAcc >= TRANSLATE_STEP_SECONDS * S.sim.slow) { trAcc = 0; if (allVideos().some((x) => x.status === 'translating')) { stepTranslations(); render(); } }
    }
    tick();
    requestAnimationFrame(frame);
  }
  function handleEnd(v, dur) {
    if (S.loopMode === 'one') { S.t = 0; return; }
    if (S.loopMode === 'list' && S.lib.length > 1) {
      const i = S.lib.findIndex((x) => x.id === v.id);
      v.progress = 0;
      selectVideo(S.lib[(i + 1) % S.lib.length].id, { fromStart: true });
      toast(`列表循环：播放下一个「${esc(vTitle(cur()))}」`, 'repeat');
      render(); return;
    }
    S.t = dur; S.playing = false; v.progress = 0; render();
  }
  let lastPlaying = null;
  function tick() {
    const v = cur();
    const i = curIdx();
    if (i !== lastIdx || S.playing !== lastPlaying) {
      const changed = i !== lastIdx;
      lastPlaying = S.playing;
      render();
      if (changed) { autoScroll(); }
    }
    if (pendingScrollToCurrent) { pendingScrollToCurrent = false; autoScroll(true); }
    if (floatScrollPending) { floatScrollPending = false; floatAutoScroll(true); }
    if (!v) return;
    const dur = durationOf(v);
    const p = dur ? S.t / dur : 0;
    const tc = $('#tCur'); if (tc) tc.textContent = fmt(S.t);
    const sf = $('#scrubFill'); if (sf) sf.style.width = `${p * 100}%`;
    const sk = $('#scrubKnob'); if (sk) sk.style.left = `${p * 100}%`;
    const sl = $('#scrubLoop');
    if (sl) {
      const ls = S.loopIdx != null ? v.sentences[S.loopIdx] : null;
      sl.style.display = ls ? 'block' : 'none';
      if (ls) { sl.style.left = `${(ls.s / dur) * 100}%`; sl.style.width = `${((ls.e - ls.s) / dur) * 100}%`; }
    }
    const s = v.sentences[i];
    const sp = s ? clamp((S.t - s.s) / (s.e - s.s), 0, 1) : 0;
    const rp = $('#tpList .row.cur .row-prog'); if (rp) rp.style.transform = `scaleX(${sp})`;
    const op = $('#olProg'); if (op) op.style.transform = `scaleX(${sp})`;
    const fp = $('#focusProg'); if (fp) fp.style.transform = `scaleX(${sp})`;
  }
  let programmaticScroll = 0;
  function autoScroll(force) {
    const list = $('#tpList');
    if (list && (force || (S.follow && !S.followPaused))) {
      const row = list.querySelector('.row.cur');
      if (row) {
        const head = headHeight();
        const target = row.offsetTop - head - (list.clientHeight - head) * 0.25;
        programmaticScroll = performance.now();
        list.scrollTo({ top: Math.max(0, target), behavior: force ? 'auto' : 'smooth' });
      }
    }
    floatAutoScroll(force);
    updateReturnBtn();
  }
  function floatAutoScroll(force) {
    const ol = $('#ofList');
    if (ol && (force || (S.float.follow && !S.float.followPaused))) {
      const row = ol.querySelector('.of-row.cur');
      if (row) { programmaticScroll = performance.now(); ol.scrollTo({ top: Math.max(0, row.offsetTop - ol.clientHeight * 0.18), behavior: force ? 'auto' : 'smooth' }); }
    }
  }
  function updateReturnBtn() {
    const list = $('#tpList'), btn = $('#tpReturn');
    if (!list || !btn) return;
    const row = list.querySelector('.row.cur');
    let off = false;
    if (row) {
      const top = row.offsetTop - list.scrollTop;
      off = top + row.offsetHeight < headHeight() + 6 || top > list.clientHeight - 24;
    }
    btn.classList.toggle('show', !!row && off && (S.followPaused || !S.follow) && !S.q);
  }
  // 字幕列表在玻璃标题区下方滚动：列表顶部留出与标题区等高的内边距
  function headHeight() { const h = $('#tpHead'); return h ? h.offsetHeight : 0; }

  /* =========================================================
     事件
     ========================================================= */
  function bindPlayerLayout() {
    const list = $('#tpList');
    new ResizeObserver(() => { const tp = $('#tp'); if (tp) tp.style.setProperty('--head-h', `${headHeight()}px`); }).observe($('#tpHead'));
    const userScroll = () => { if (S.follow && !S.followPaused) { S.followPaused = true; sync(); } updateReturnBtn(); };
    list.addEventListener('wheel', userScroll, { passive: true });
    list.addEventListener('touchmove', userScroll, { passive: true });
    list.addEventListener('scroll', () => { if (performance.now() - programmaticScroll > 700 && !S.followPaused && S.follow && list._dragging) userScroll(); updateReturnBtn(); }, { passive: true });
    list.addEventListener('pointerdown', (e) => { if (e.offsetX > list.clientWidth) list._dragging = true; });
    window.addEventListener('pointerup', () => { list._dragging = false; });
    const q = $('#qInput');
    q.value = S.q;
    q.addEventListener('input', () => { S.q = q.value; render(); if (!S.q) autoScroll(true); });
    q.addEventListener('keydown', (e) => { if (e.key === 'Escape') { q.value = ''; S.q = ''; q.blur(); render(); autoScroll(true); } });
    // 进度条
    const sc = $('#scrub');
    const posToT = (e) => { const r = sc.getBoundingClientRect(); return clamp((e.clientX - r.left) / r.width, 0, 1) * durationOf(cur()); };
    sc.addEventListener('pointerdown', (e) => {
      if (!needPlayer()) return;
      sc.setPointerCapture(e.pointerId); sc.classList.add('drag');
      seek(posToT(e), { fromPlayer: true }); render();
      const mv = (ev) => { seek(posToT(ev), { fromPlayer: true }); };
      const up = () => { sc.classList.remove('drag'); sc.removeEventListener('pointermove', mv); sc.removeEventListener('pointerup', up); render(); autoScroll(true); };
      sc.addEventListener('pointermove', mv); sc.addEventListener('pointerup', up);
    });
    sc.addEventListener('pointermove', (e) => {
      const v = cur(); if (!v) return;
      const t = posToT(e), i = idxAt(v, t), tip = $('#scrubTip');
      const r = sc.getBoundingClientRect();
      tip.style.left = `${((e.clientX - r.left) / r.width) * 100}%`;
      tip.innerHTML = `<b>${fmt(t)}</b>${i >= 0 ? `<span>${esc(v.sentences[i].en)}</span>` : ''}`;
    });
    const vs = $('#vstage');
    let hideT;
    vs.addEventListener('pointermove', () => { if (!S.ui.fs) return; $('#mw').classList.add('fs-show'); clearTimeout(hideT); hideT = setTimeout(() => $('#mw') && $('#mw').classList.remove('fs-show'), 2400); });
    vs.addEventListener('dblclick', (e) => { if (e.target.closest('.yt')) { CMD.toggleFs(); render(); } });
  }

  // 点击委托
  let suppressClick = false;
  document.addEventListener('click', (e) => {
    if (suppressClick) { suppressClick = false; e.stopPropagation(); e.preventDefault(); return; }
    const t = e.target;
    // 按钮点击后不保留键盘焦点，避免空格键再次触发按钮
    const btnEl = t.closest('button'); if (btnEl && !btnEl.closest('form')) setTimeout(() => btnEl.blur());
    // 菜单项
    const mItem = t.closest('[data-mi]');
    if (mItem) {
      const it = S.menu && S.menu.items[Number(mItem.dataset.mi)];
      if (it && !it.disabled) { closeMenu(); it.act(); render(); }
      return;
    }
    const inMenu = t.closest('#menu-layer .menu, #menu-layer .pop');
    const mb = t.closest('[data-mb]');
    if (S.menu && !inMenu && !mb) {
      const owner = S.menu.owner;
      const anchorAct = t.closest('[data-act]');
      closeMenu(); render();
      if (anchorAct && ((owner === 'hidden' && anchorAct.dataset.act === 'showHidden') || (owner === 'lyrMore' && anchorAct.dataset.act === 'lyrMore') || (owner === `row:${anchorAct.dataset.vid}` && anchorAct.dataset.act === 'rowMenu') || (owner === 'tpMore' && anchorAct.dataset.act === 'tpMore'))) return;
    }
    if (mb) {
      const k = mb.dataset.mb, owner = k === 'status' ? 'status' : `mb:${k}`;
      if (S.menu && S.menu.owner === owner) { closeMenu(); render(); return; }
      openMenuBar(k, mb); render(); return;
    }
    const tl = t.closest('[data-tl]');
    if (tl) { trafficAction(tl.dataset.w, tl.dataset.tl); render(); return; }
    const dock = t.closest('[data-dock]');
    if (dock) { dockAction(dock.dataset.dock); render(); return; }
    const dlg = t.closest('[data-dlg]');
    if (dlg) { if (dlg.dataset.dlg === 'ok') confirmDialog(); else S.dialog = null; render(); return; }
    const scene = t.closest('[data-scene]');
    if (scene) { applyScene(scene.dataset.scene); return; }
    const ex = t.closest('[data-example]');
    if (ex) { fillExample(ex.dataset.example); return; }
    const rowBtn = t.closest('[data-row]');
    if (rowBtn) { rowAction(rowBtn.dataset.row, Number(rowBtn.closest('.row').dataset.i)); render(); return; }
    const setEl = t.closest('[data-set]');
    if (setEl) { setPath(setEl.dataset.set, setEl.dataset.val); render(); return; }
    const cmd = t.closest('[data-cmd]');
    if (cmd && !cmd.disabled) { CMD[cmd.dataset.cmd](cmd); render(); return; }
    const act = t.closest('[data-act]');
    if (act && !act.disabled) { ACT[act.dataset.act] && ACT[act.dataset.act](act, e); render(); return; }
    const row = t.closest('#tpList .row');
    if (row) { if (needPlayer()) { gotoSentence(Number(row.dataset.i), true); render(); } return; }
    const gotoEl = t.closest('[data-goto]');
    if (gotoEl) { if (needPlayer()) { gotoSentence(Number(gotoEl.dataset.goto), true); render(); } return; }
    const seekEl = t.closest('[data-seek]');
    if (seekEl && t.closest('.ovl-float')) { if (needPlayer()) { gotoSentence(Number(seekEl.dataset.seek), true); render(); } return; }
    const lib = t.closest('.lib-row');
    if (lib) { if (lib.dataset.vid !== S.curId) selectVideo(lib.dataset.vid); render(); return; }
  }, true);
  document.addEventListener('contextmenu', (e) => {
    const lib = e.target.closest('.lib-row');
    if (lib) {
      e.preventDefault();
      const sr = $('#stage').getBoundingClientRect();
      S.menu = { type: 'menu', items: rowMenu(lib.dataset.vid), x: (e.clientX - sr.left) / scale, y: (e.clientY - sr.top) / scale, owner: `row:${lib.dataset.vid}` };
      render();
    }
  });
  // 菜单栏悬停切换（macOS 行为：菜单展开时移到另一个菜单标题上会直接切换）
  document.addEventListener('pointerover', (e) => {
    const mb = e.target.closest('[data-mb]');
    if (!mb || !S.menu || !(S.menu.owner || '').startsWith('mb:') || mb.dataset.mb === 'status') return;
    if (S.menu.owner !== `mb:${mb.dataset.mb}`) { openMenuBar(mb.dataset.mb, mb); render(); }
  });
  function openMenuBar(k, el) {
    const items = { app: appMenu, edit: editMenu, play: playMenu, window: windowMenu, status: statusMenu }[k]();
    openMenu(items, el, k === 'status' ? 'status' : `mb:${k}`, { gap: 2, alignRight: false });
  }
  function trafficAction(w, a) {
    if (w === 'main') {
      if (a === 'close') { S.ui.mainVisible = false; S.ui.fs = false; S.ui.sysfs = false; S.focus = 'desktop'; toast('主窗口已关闭，EchoSub 仍在菜单栏运行', 'bubble'); }
      if (a === 'min') { S.ui.mainMin = true; S.focus = 'desktop'; }
      if (a === 'zoom') { if (S.ui.sysfs) S.ui.sysfs = false; else toggleZoom(); }
    } else if (w === 'settings') { S.ui.settingsOpen = false; S.focus = mainShown() ? 'main' : 'desktop'; }
    else if (w === 'about') { S.ui.aboutOpen = false; S.focus = mainShown() ? 'main' : 'desktop'; }
    else if (w === 'notes') { S.ui.notesHidden = true; S.focus = 'desktop'; }
  }
  function dockAction(k) {
    if (k === 'echosub' || k === 'restoreMain') {
      if (S.ui.quit) { const th = S.theme; S = initialState(S); S.theme = th; mwMode = null; settingsDirty = true; toast('EchoSub 已重新打开', 'bubble'); }
      showMain();
    } else if (k === 'notes') { S.ui.notesHidden = false; bringFront('notes'); S.focus = 'notes'; }
    else { S.focus = 'desktop'; toast('原型中只模拟了 EchoSub 和备忘录', 'info'); }
  }
  function rowAction(a, i) {
    const v = cur(); if (!v) return;
    const s = v.sentences[i];
    if (a === 'play') { if (needPlayer()) gotoSentence(i, true); }
    else if (a === 'loop') {
      if (!needPlayer()) return;
      if (S.loopIdx === i) { S.loopIdx = null; toast('已关闭单句循环', 'loopSent'); }
      else { S.loopIdx = i; gotoSentence(i, true); toast(`单句循环：第 ${i + 1} 句`, 'loopSent'); }
    } else if (a === 'copy') copyText(`${s.en}${s.zh ? `\n${s.zh}` : ''}`, '已复制这一句（中英）');
  }
  function fillExample(url) {
    const input = S.lib.length ? $('#addSide') : $('#addHero');
    if (S.lib.length && !(S.ui.sidebar && S.ui.sideAreas)) { S.ui.sideAreas = true; S.ui.sidebar = true; render(); }
    if (!input) return;
    input.value = url; addErr[input.dataset.addinput] = false;
    input.focus(); updateAddHint(input);
    S.focus = 'main'; render();
  }
  const ACT = {
    paste(el) { const input = el.dataset.src === 'hero' ? $('#addHero') : $('#addSide'); input.value = S.sim.clip; addErr[el.dataset.src] = false; updateAddHint(input); input.focus(); },
    clearFilter() { S.filter = ''; $('#filterInput').value = ''; },
    clearSearch() { S.q = ''; const q = $('#qInput'); if (q) q.value = ''; setTimeout(() => autoScroll(true)); },
    rowMenu(el) { const vid = el.dataset.vid; if (S.menu && S.menu.owner === `row:${vid}`) { closeMenu(); return; } openMenu(rowMenu(vid), el, `row:${vid}`, { alignRight: true }); },
    tpMore(el) { if (S.menu && S.menu.owner === 'tpMore') { closeMenu(); return; } openMenu(tpMoreMenu(), el, 'tpMore', { alignRight: true }); },
    showHidden(el) { if (S.menu && S.menu.owner === 'hidden') { closeMenu(); return; } openPopover(hiddenPopHTML, el, 'hidden', { above: true }); },
    restore(el) { restoreVideo(el.dataset.vid); if (S.menu && S.menu.owner === 'hidden') S.menu.html = hiddenPopHTML; },
    askClear() { openDialog('clear'); },
    askRefetch() { if (cur()) openDialog('refetch', S.curId); },
    retryFetch() { const v = cur(); if (v) { startFetch(v); toast('正在重新尝试获取字幕…', 'refetch'); } },
    fillTranslate() { fillTranslate(); },
    goSettings(el) { CMD.openSettings(); S.ui.settingsTab = el.dataset.tab || 'translate'; settingsDirty = true; },
    toggleFollow() {
      if (!S.follow) { S.follow = true; S.followPaused = false; autoScroll(true); toast('自动跟随：开启', 'follow'); }
      else if (S.followPaused) { S.followPaused = false; autoScroll(true); }
      else { S.follow = false; toast('自动跟随：已关闭', 'follow'); }
    },
    returnCurrent() { S.followPaused = false; autoScroll(true); },
    videoClick() { CMD.togglePlay(); },
    reloadPlayer() { S.player = 'loading'; setTimeout(() => { S.player = 'ok'; render(); }, 900); },
    floatFont(el) { S.float.font = clamp(S.float.font + Number(el.dataset.d), 12, 36); },
    floatFollow() { S.float.follow = !S.float.follow; S.float.followPaused = false; floatScrollPending = true; },
    floatPin() { S.float.pin = !S.float.pin; toast(S.float.pin ? '悬浮字幕：置顶' : '悬浮字幕：取消置顶（会被其他窗口盖住）', 'pin'); },
    floatReturn() { S.float.followPaused = false; floatScrollPending = true; },
    lyrFont(el) { const k = el.dataset.k; S.lyr[k] = clamp(S.lyr[k] + Number(el.dataset.d), 12, 48); },
    lyrAlign() { S.lyr.align = S.lyr.align === 'center' ? 'left' : 'center'; },
    lyrMore(el) { if (S.menu && S.menu.owner === 'lyrMore') { closeMenu(); return; } openPopover(lyrMorePopHTML, el, 'lyrMore', { above: true, cls: 'dark-pop' }); },
    setTab(el) { S.ui.settingsTab = el.dataset.tab; settingsDirty = true; },
    preset(el) { const p = PRESETS[el.dataset.preset]; S.draft.baseUrl = p.baseUrl; S.draft.model = p.model; S.test = { state: 'idle' }; },
    toggleKey() { S.showKey = !S.showKey; },
    saveDraft() { saveDraft(); },
    revertDraft() { S.draft = Object.assign({}, S.cfg); },
    testConn() { runTest(); },
    copyBrew() { copyText('brew install yt-dlp', '已复制安装命令'); },
    redetect() { toast(S.ytdlp ? `已检测到 ${YTDLP_PATH}` : '仍未检测到 yt-dlp（可在原型控制台模拟安装）', S.ytdlp ? 'check' : 'info'); },
    saveSupa() { S.supadata = S.supadataDraft.trim(); S.supadataDraft = S.supadata; toast(S.supadata ? 'Supadata Key 已存储' : '已清除 Supadata Key', 'check'); },
    colorPreset(el) { const p = COLOR_PRESETS.find((c) => c.id === el.dataset.cp); S.colors = { en: p.en, zh: p.zh }; S.colorDraft = { en: p.en, zh: p.zh }; },
    saveColor() { S.colors = { en: S.colorDraft.en.toUpperCase(), zh: S.colorDraft.zh.toUpperCase() }; S.colorDraft = Object.assign({}, S.colors); toast('字幕颜色已应用', 'palette'); },
    revertColor() { S.colorDraft = Object.assign({}, S.colors); },
    theme(el) { S.theme = el.dataset.v; },
    protoToggle() { S.ui.protoCollapsed = !S.ui.protoCollapsed; setTimeout(fitStage, 260); },
    openMainP() { CMD.openMain(); },
    resetLayout() { S.frames = defaultFrames(); S.ui.zoomed = null; S.ui.sysfs = false; },
  };

  // 输入
  document.addEventListener('input', (e) => {
    const t = e.target;
    if (t.dataset.addinput) { addErr[t.dataset.addinput] = false; updateAddHint(t); return; }
    if (t.id === 'filterInput') { S.filter = t.value; render(); return; }
    if (t.dataset.draft) { S.draft[t.dataset.draft] = t.value.trim(); if (S.test.state !== 'loading') S.test = { state: 'idle' }; renderSettingsDynamic(); return; }
    if (t.dataset.cdraft) { S.colorDraft[t.dataset.cdraft] = t.value.trim(); renderSettingsDynamic(); return; }
    if (t.dataset.supa !== undefined) { S.supadataDraft = t.value; renderSettingsDynamic(); return; }
    if (t.dataset.bind) {
      const p = t.dataset.bind;
      let val = t.type === 'checkbox' ? t.checked : t.type === 'range' ? Number(t.value) / (Number(t.dataset.scale) || 1) : t.value;
      if (p === 'lyr.locked' && !S.lyr.open) val = false;
      setPath(p, val);
      if (p === 'lyr.open' && !val) S.lyr.locked = false;
      if (p === 'sim.actual') setTimeout(fitStage);
      if (p === 'ytdlp') settingsDirty = S.ui.settingsTab === 'sources' ? settingsDirty : settingsDirty;
      render();
    }
  });
  document.addEventListener('change', (e) => { if (e.target.dataset.bind && e.target.tagName === 'SELECT') { setPath(e.target.dataset.bind, e.target.value); render(); } });
  document.addEventListener('submit', (e) => {
    const f = e.target.closest('[data-add]');
    if (!f) return;
    e.preventDefault();
    const src = f.dataset.add;
    const input = src === 'hero' ? $('#addHero') : $('#addSide');
    const ok = addVideo(input.value, src);
    if (ok) { input.value = ''; addErr[src] = false; }
    updateAddHint(input);
    if (!ok) { const fld = src === 'hero' ? f : $('#addFieldSide'); fld.classList.remove('shake'); void fld.offsetWidth; fld.classList.add('shake'); }
    render();
    const nInput = S.lib.length ? $('#addSide') : $('#addHero'); if (nInput) updateAddHint(nInput);
  });
  document.addEventListener('keydown', (e) => {
    const t = e.target;
    if (t.dataset && t.dataset.draft && e.key === 'Enter') { saveDraft(); t.blur(); render(); return; }
    if (t.dataset && t.dataset.cdraft && e.key === 'Enter') { if (/^#[0-9a-f]{6}$/i.test(S.colorDraft.en) && /^#[0-9a-f]{6}$/i.test(S.colorDraft.zh)) ACT.saveColor(); t.blur(); render(); return; }
    if (t.dataset && t.dataset.supa !== undefined && e.key === 'Enter') { ACT.saveSupa(); t.blur(); render(); return; }
    const typing = (t.tagName === 'INPUT' && !['range', 'checkbox'].includes(t.type)) || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT';
    const k = e.key.toLowerCase();
    if (e.key === 'Escape') {
      if (S.menu) { closeMenu(); render(); return; }
      if (S.dialog) { S.dialog = null; render(); return; }
      if (S.ui.fs) { S.ui.fs = false; render(); return; }
      if (typing) { t.blur(); return; }
    }
    if (!appActive()) return;
    const m = e.metaKey, c = e.ctrlKey, sh = e.shiftKey, alt = e.altKey;
    let handled = true;
    if (m && !sh && !c && k === ',') CMD.openSettings();
    else if (m && sh && !c && k === 'f') CMD.toggleFloat();
    else if (m && sh && !c && k === 'd') CMD.toggleLyrics();
    else if (m && sh && !c && k === 'l') CMD.toggleLock();
    else if (m && c && k === 't') CMD.toggleSideAreas();
    else if (m && c && k === 's') CMD.togglePlaylist();
    else if (m && !sh && !c && k === 'f' && S.focus === 'main') { e.preventDefault(); CMD.focusSearch(); return; }
    else handled = false;
    if (handled) { e.preventDefault(); render(); return; }
    if (S.focus !== 'main' || typing || m || c || S.dialog || !mainShown()) return;
    handled = true;
    if (e.key === ' ') CMD.togglePlay();
    else if (e.key === 'ArrowLeft') alt ? CMD.prev() : CMD.back5();
    else if (e.key === 'ArrowRight') alt ? CMD.next() : CMD.fwd5();
    else if (k === 'r' && !alt) CMD.replay();
    else if (k === 'l' && !alt) CMD.toggleLoop();
    else if (k === 'f' && !alt) CMD.toggleFs();
    else handled = false;
    if (handled) { e.preventDefault(); render(); }
  });

  // 拖动 / 缩放窗口、焦点
  let drag = null;
  document.addEventListener('pointerdown', (e) => {
    if (e.button !== 0) return;
    const t = e.target;
    if (t.closest('#proto')) return;
    const winEl = t.closest('[data-win]');
    const w = winEl ? winEl.dataset.win : (t.closest('#menubar, #menu-layer, #dock') ? null : 'desktop');
    if (w) {
      const prevFocus = S.focus;
      if (['main', 'settings', 'about', 'notes', 'float'].includes(w)) bringFront(w);
      S.focus = w;
      if (w === 'notes' || w === 'desktop') ripple(e);
      if (prevFocus !== w) { applyFrames(); renderMenubar(); ['main', 'settings', 'about', 'notes'].forEach((x) => { const el = $(`#win-${x}`); el && el.classList.toggle('active', x === w); }); }
    }
    const rzEl = t.closest('[data-rz]');
    if (rzEl) {
      const win = rzEl.dataset.w;
      if (win === 'main' && (S.ui.sysfs)) return;
      drag = { type: 'rz', win, dir: rzEl.dataset.rz, sx: e.clientX, sy: e.clientY, f0: Object.assign({}, S.frames[win]) };
      S.ui.resizing = win; document.body.classList.add('resizing');
      e.preventDefault(); return;
    }
    const dz = t.closest('[data-drag]');
    if (dz && winEl && !t.closest('button, input, select, textarea, a, [data-nodrag], .scrub, .lib-row, .row')) {
      if (winEl.dataset.win === 'main' && (S.ui.sysfs)) return;
      drag = { type: 'mv', win: winEl.dataset.win, sx: e.clientX, sy: e.clientY, f0: Object.assign({}, S.frames[winEl.dataset.win]), moved: false };
    }
  });
  document.addEventListener('pointermove', (e) => {
    if (!drag) return;
    const dx = (e.clientX - drag.sx) / scale, dy = (e.clientY - drag.sy) / scale;
    const f = S.frames[drag.win], f0 = drag.f0;
    if (drag.type === 'mv') {
      if (!drag.moved && Math.hypot(dx, dy) < 4) return;
      drag.moved = true;
      f.x = clamp(f0.x + dx, -f0.w + 80, DESK_W - 80);
      f.y = drag.win === 'lyrics' ? clamp(f0.y + dy, MB_H + 120, DESK_H) : clamp(f0.y + dy, MB_H, DESK_H - 40);
      if (drag.win === 'main' && S.ui.zoomed) S.ui.zoomed = null;
    } else {
      const min = drag.win === 'main' ? MAIN_MIN : drag.win === 'float' ? FLOAT_MIN : { w: LYR_MIN_W, h: 0 };
      const d = drag.dir;
      if (d.includes('e')) f.w = clamp(f0.w + dx, min.w, DESK_W - f0.x);
      if (d.includes('w')) { const nw = clamp(f0.w - dx, min.w, f0.x + f0.w); f.x = f0.x + f0.w - nw; f.w = nw; }
      if (d.includes('s') && f0.h != null) f.h = clamp(f0.h + dy, min.h, DESK_H - f0.y);
      if (d.includes('n') && f0.h != null) { const nh = clamp(f0.h - dy, min.h, f0.y + f0.h - MB_H); f.y = f0.y + f0.h - nh; f.h = nh; }
      if (drag.win === 'main') {
        const docked = f.w >= SIDEBAR_DOCK_MIN, was = f0.w >= SIDEBAR_DOCK_MIN;
        // 变窄时自动收起停靠的侧栏；再变宽时恢复（仅限被自动收起的情况）
        if (was && !docked && S.ui.sidebar) { S.ui.sidebar = false; S.ui.sbAutoClosed = true; }
        if (docked && S.ui.sbAutoClosed) { S.ui.sidebar = true; S.ui.sbAutoClosed = false; }
        if (S.ui.zoomed) S.ui.zoomed = null;
      }
    }
    applyFrames();
    if (drag.type === 'rz') { if (drag.win === 'float') renderFloat(); if (drag.win === 'main') renderMain(); }
  });
  document.addEventListener('pointerup', () => {
    if (!drag) return;
    if (drag.type === 'mv' && drag.moved) { suppressClick = true; setTimeout(() => { suppressClick = false; }, 0); }
    const wasRz = drag.type === 'rz';
    drag = null;
    if (wasRz) { S.ui.resizing = null; document.body.classList.remove('resizing'); render(); autoScroll(true); }
  });
  function ripple(e) {
    const sr = $('#stage').getBoundingClientRect();
    const r = document.createElement('i');
    r.className = 'ripple';
    r.style.left = `${(e.clientX - sr.left) / scale}px`; r.style.top = `${(e.clientY - sr.top) / scale}px`;
    $('#stage').appendChild(r);
    setTimeout(() => r.remove(), 700);
  }

  /* ---------- 舞台缩放 ---------- */
  let scale = 1;
  function fitStage() {
    const wrap = $('#stage-wrap'), st = $('#stage');
    const pad = 16;
    const aw = wrap.clientWidth - pad * 2, ah = wrap.clientHeight - pad * 2;
    scale = S.sim.actual ? 1 : Math.min(aw / DESK_W, ah / DESK_H);
    st.style.transform = `scale(${scale})`;
    st.style.left = `${Math.max(pad, (wrap.clientWidth - DESK_W * scale) / 2)}px`;
    st.style.top = `${Math.max(pad, (wrap.clientHeight - DESK_H * scale) / 2)}px`;
    wrap.classList.toggle('actual', !!S.sim.actual);
  }
  new ResizeObserver(fitStage).observe($('#stage-wrap'));

  /* ---------- 启动 ---------- */
  buildNotes(); buildMain(); buildSettings(); buildAbout(); buildOverlays();
  S.playing = true;
  fitStage();
  render();
  setInterval(renderMenubar, 15000);
  requestAnimationFrame(frame);
})();
