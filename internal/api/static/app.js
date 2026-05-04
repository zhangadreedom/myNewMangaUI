const appViewEl = document.getElementById("app-view");
const feedbackEl = document.getElementById("feedback");
const mangaCountEl = document.getElementById("manga-count");
const currentBookshelfEl = document.getElementById("current-bookshelf");
const latestUpdateEl = document.getElementById("latest-update");
const refreshButton = document.getElementById("refresh-button");
const scanButton = document.getElementById("scan-button");
const homeButton = document.getElementById("home-button");
const onlineButton = document.getElementById("online-button");
const favoritesButton = ensureHeroActionButton("favorites-button", "收藏", onlineButton);
const followingButton = ensureHeroActionButton("following-button", "追漫", favoritesButton);
const downloadsButton = document.getElementById("downloads-button");
const manageButton = document.getElementById("manage-button");
const themeToggleButton = document.getElementById("theme-toggle");
const eyebrowEl = document.getElementById("eyebrow");
const viewTitleEl = document.getElementById("view-title");
const toolbarExtraEl = document.getElementById("toolbar-extra");
const bookshelfStorageKey = "mynewmangaui.currentBookshelfId";
const librarySortStorageKey = "mynewmangaui.librarySort";
const tagFilterStorageKey = "mynewmangaui.libraryTagFilterIds";
const themeStorageKey = "mynewmangaui.theme";
const onlineReturnRouteStorageKey = "mynewmangaui.onlineReturnRoute";
const nextChapterImagePreloadCount = 4;
const nextChapterAutoAdvanceDelayMs = 1000;

function ensureHeroActionButton(id, label, afterNode) {
  let button = document.getElementById(id);
  if (button) {
    return button;
  }
  button = document.createElement("button");
  button.id = id;
  button.className = "ghost-button";
  button.type = "button";
  button.hidden = true;
  button.textContent = label;
  const parent = afterNode?.parentElement || document.querySelector(".hero__actions");
  if (parent && afterNode?.parentElement === parent) {
    afterNode.insertAdjacentElement("afterend", button);
  } else {
    parent?.appendChild(button);
  }
  return button;
}

const state = {
  health: null,
  tags: null,
  bookshelves: null,
  library: null,
  libraryByBookshelfId: new Map(),
  currentBookshelfId: "",
  librarySort: "updated-desc",
  libraryTagFilterIDs: [],
  mangaById: new Map(),
  chaptersByMangaId: new Map(),
  pagesByChapterId: new Map(),
  chapterOrderByMangaId: new Map(),
  librarySearchQuery: "",
  librarySearchDraft: "",
  librarySearchTimer: null,
  scanStatus: null,
  scanPollTimer: null,
  toolbarCleanup: null,
  tagEditorId: "",
  online: {
    sources: null,
    sourceById: new Map(),
    currentSourceId: "18comic",
    searchQuery: "",
    searchResults: null,
    searchResultsByKey: new Map(),
    settings: null,
    settingsBySource: new Map(),
    defaultFeed: null,
    defaultFeedsBySource: new Map(),
    pendingDefaultFeedSources: new Set(),
    mangaByKey: new Map(),
    chaptersByKey: new Map(),
    pagesByKey: new Map(),
    bookmarksByKind: new Map(),
    downloads: null,
    downloadsPollTimer: null,
    pendingCreateKeys: new Set(),
    pendingJobActions: new Set(),
    completedSignatures: new Map(),
    downloadStatusFilter: "all",
    downloadSourceFilter: "all",
    downloadSort: "updated-desc",
    bookmarkKind: "favorite",
    returnRoute: "",
  },
  reader: {
    chapterId: null,
    activePage: 0,
    observer: null,
    toolbarCleanup: null,
  },
};

try {
  state.currentBookshelfId = window.localStorage.getItem(bookshelfStorageKey) || "";
  state.librarySort = window.localStorage.getItem(librarySortStorageKey) || "updated-desc";
  state.libraryTagFilterIDs = JSON.parse(window.localStorage.getItem(tagFilterStorageKey) || "[]");
  state.online.returnRoute = window.localStorage.getItem(onlineReturnRouteStorageKey) || "";
} catch (error) {
  console.warn("failed to restore ui state", error);
}

function getCurrentTheme() {
  return document.documentElement.dataset.theme === "dark" ? "dark" : "light";
}

function updateThemeToggle() {
  if (!themeToggleButton) {
    return;
  }
  const isDark = getCurrentTheme() === "dark";
  const icon = themeToggleButton.querySelector("[data-theme-icon]");
  const label = themeToggleButton.querySelector("[data-theme-label]");
  themeToggleButton.setAttribute("aria-pressed", String(isDark));
  themeToggleButton.setAttribute("aria-label", isDark ? "切换到白天模式" : "切换到黑暗模式");
  themeToggleButton.setAttribute("title", isDark ? "切换到白天模式" : "切换到黑暗模式");
  if (icon) {
    icon.textContent = isDark ? "☀" : "☾";
  }
  if (label) {
    label.textContent = isDark ? "白天" : "暗色";
  }
}

function setTheme(theme) {
  const nextTheme = theme === "dark" ? "dark" : "light";
  if (nextTheme === "dark") {
    document.documentElement.dataset.theme = "dark";
  } else {
    delete document.documentElement.dataset.theme;
  }

  try {
    if (nextTheme === "dark") {
      window.localStorage.setItem(themeStorageKey, "dark");
    } else {
      window.localStorage.removeItem(themeStorageKey);
    }
  } catch (error) {
    console.warn("failed to persist theme", error);
  }

  updateThemeToggle();
}

function persistCurrentBookshelfId() {
  try {
    if (state.currentBookshelfId) {
      window.localStorage.setItem(bookshelfStorageKey, state.currentBookshelfId);
    } else {
      window.localStorage.removeItem(bookshelfStorageKey);
    }
  } catch (error) {
    console.warn("failed to persist bookshelf selection", error);
  }
}

function persistLibrarySort() {
  try {
    window.localStorage.setItem(librarySortStorageKey, state.librarySort);
  } catch (error) {
    console.warn("failed to persist library sort", error);
  }
}

function persistLibraryTagFilters() {
  try {
    if (state.libraryTagFilterIDs.length) {
      window.localStorage.setItem(tagFilterStorageKey, JSON.stringify(state.libraryTagFilterIDs));
    } else {
      window.localStorage.removeItem(tagFilterStorageKey);
    }
  } catch (error) {
    console.warn("failed to persist tag filters", error);
  }
}

function applyRouteLayout(routeName) {
  const normalizedRoute = routeName === "onlineManga"
    ? "manga"
    : routeName === "onlineChapter"
      ? "chapter"
      : routeName === "onlineBookmarks"
        ? "onlineLibrary"
        : routeName;
  document.body.dataset.route = normalizedRoute;
  document.body.dataset.routeName = routeName;
}

function getRoute() {
  const hash = window.location.hash || "#/";
  const trimmed = hash.startsWith("#") ? hash.slice(1) : hash;
  const parts = trimmed.split("/").filter(Boolean);

  if (parts[0] === "manage" || parts[0] === "tags") {
    return { name: "manage", section: parts[1] || "tags" };
  }
  if (parts[0] === "downloads") {
    return { name: "downloads" };
  }
  if (parts[0] === "online") {
    const sourceId = decodeURIComponent(parts[1] || state.online.currentSourceId || "18comic");
    if (parts[2] === "favorites") {
      return { name: "onlineBookmarks", sourceId, kind: "favorite" };
    }
    if (parts[2] === "following") {
      return { name: "onlineBookmarks", sourceId, kind: "follow" };
    }
    if (parts[2] === "manga" && parts[3]) {
      return { name: "onlineManga", sourceId, mangaId: decodeURIComponent(parts[3]) };
    }
    if (parts[2] === "chapter" && parts[3]) {
      return { name: "onlineChapter", sourceId, chapterId: decodeURIComponent(parts[3]) };
    }
    return { name: "onlineLibrary", sourceId };
  }
  if (parts[0] === "manga" && parts[1]) {
    return { name: "manga", mangaId: decodeURIComponent(parts[1]) };
  }
  if (parts[0] === "chapter" && parts[1]) {
    return { name: "chapter", chapterId: decodeURIComponent(parts[1]) };
  }
  return { name: "library" };
}

function setRoute(hash) {
  if (window.location.hash === hash) {
    renderCurrentRoute();
    return;
  }
  window.location.hash = hash;
}

function rememberOnlineReturnRoute(hash = window.location.hash || routeForOnlineLibrary()) {
  state.online.returnRoute = hash || routeForOnlineLibrary();
  try {
    window.localStorage.setItem(onlineReturnRouteStorageKey, state.online.returnRoute);
  } catch (error) {
    console.warn("failed to persist online return route", error);
  }
}

function getOnlineReturnRoute(sourceId = state.online.currentSourceId || "18comic") {
  const route = String(state.online.returnRoute || "").trim();
  if (route && route.includes("#/online/") && !route.includes("/manga/") && !route.includes("/chapter/")) {
    return route;
  }
  return routeForOnlineLibrary(sourceId);
}

function routeForOnlineLibrary(sourceId = state.online.currentSourceId || "18comic") {
  return `#/online/${encodeURIComponent(sourceId)}`;
}

function routeForOnlineBookmarks(kind = "favorite", sourceId = state.online.currentSourceId || "18comic") {
  return `#/online/${encodeURIComponent(sourceId)}/${kind === "follow" ? "following" : "favorites"}`;
}

function routeForManga(manga) {
  if (manga?.sourceId) {
    return `#/online/${encodeURIComponent(manga.sourceId)}/manga/${encodeURIComponent(manga.id)}`;
  }
  return `#/manga/${encodeURIComponent(manga?.id ?? "")}`;
}

function routeForChapter(chapter, manga = null) {
  const sourceId = chapter?.sourceId || manga?.sourceId;
  if (sourceId) {
    return `#/online/${encodeURIComponent(sourceId)}/chapter/${encodeURIComponent(chapter?.id ?? "")}`;
  }
  return `#/chapter/${encodeURIComponent(chapter?.id ?? "")}`;
}

function buildOnlineImageProxyURL(sourceId, remoteURL) {
  if (!remoteURL) {
    return "";
  }
  const encoded = btoa(unescape(encodeURIComponent(remoteURL)))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replace(/=+$/g, "");
  return `/api/online/${encodeURIComponent(sourceId)}/image?target=${encoded}`;
}

function renderOnlineCover(item) {
  const proxyURL = buildOnlineImageProxyURL(item?.sourceId, item?.coverUrl || "");
  if (proxyURL) {
    return `<img src="${escapeHTML(proxyURL)}" alt="${escapeHTML(item?.title || "")}" loading="lazy" />`;
  }
  const label = String(item?.title || "?").trim().slice(0, 1).toUpperCase() || "?";
  return `<div class="manga-tile__cover-placeholder" aria-hidden="true">${escapeHTML(label)}</div>`;
}

async function fetchJSON(url, options) {
  const response = await fetch(url, options);
  if (!response.ok) {
    let message = `Request failed with status ${response.status}`;
    try {
      const payload = await response.json();
      if (payload?.error) {
        message = payload.error;
      }
    } catch (error) {
      try {
        const text = await response.text();
        if (text.trim()) {
          message = text.trim();
        }
      } catch (textError) {
        console.warn("failed to read error response", textError);
      }
    }
    throw new Error(message);
  }
  return response.json();
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function formatUpdatedAt(value) {
  if (!value) {
    return "更新时间未知";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return `更新时间: ${value}`;
  }
  return `更新于 ${date.toLocaleString("zh-CN")}`;
}

function showFeedback(message, tone = "normal") {
  feedbackEl.textContent = message;
  feedbackEl.dataset.tone = tone;
}

function invalidateLibraryCaches() {
  state.bookshelves = null;
  state.library = null;
  state.libraryByBookshelfId.clear();
  state.mangaById.clear();
  state.chaptersByMangaId.clear();
  state.pagesByChapterId.clear();
}

function clearOnlineDownloadsPoll() {
  if (state.online.downloadsPollTimer) {
    window.clearTimeout(state.online.downloadsPollTimer);
    state.online.downloadsPollTimer = null;
  }
}

function isTerminalDownloadStatus(status) {
  return ["done", "failed", "canceled"].includes(String(status || "").toLowerCase());
}

function isActiveDownloadStatus(status) {
  return ["queued", "running", "processing"].includes(String(status || "").toLowerCase());
}

function getDownloadRequestKey(sourceId, mangaId, chapterIds = [], mode = "") {
  const normalizedChapterIDs = [...new Set((chapterIds || []).filter(Boolean).map((id) => String(id).trim()))].sort();
  return `${sourceId || ""}::${mangaId || ""}::${mode || ""}::${normalizedChapterIDs.join(",")}`;
}

function getDownloadJobSignature(job) {
  return `${job?.status || ""}|${job?.updatedAt || ""}|${job?.donePages || 0}|${job?.failedPages || 0}`;
}

async function handleOnlineDownloadsUpdate(previousItems = [], nextItems = []) {
  const route = getRoute();
  let shouldRefreshLibrary = false;

  nextItems.forEach((job) => {
    const signature = getDownloadJobSignature(job);
    const previousSignature = state.online.completedSignatures.get(job.id);
    if (signature === previousSignature) {
      return;
    }

    if (isTerminalDownloadStatus(job.status)) {
      state.online.completedSignatures.set(job.id, signature);
      shouldRefreshLibrary = true;
    }
  });

  if (!shouldRefreshLibrary) {
    return;
  }

  invalidateLibraryCaches();
  if (["library", "manga", "chapter"].includes(route.name)) {
    await Promise.all([
      ensureBookshelves(true),
      ensureLibrary(true, state.currentBookshelfId || "", state.libraryTagFilterIDs || []),
    ]);
    updateHero();
    await renderCurrentRoute();
  }
}

function scheduleOnlineDownloadsPoll(delay = 2200) {
  clearOnlineDownloadsPoll();
  if (!state.online.downloads?.items?.some((job) => isActiveDownloadStatus(job.status))) {
    return;
  }

  state.online.downloadsPollTimer = window.setTimeout(async () => {
    const previousItems = state.online.downloads?.items || [];
    try {
      const payload = await ensureOnlineDownloads(true);
      const nextItems = payload?.items || [];
      await handleOnlineDownloadsUpdate(previousItems, nextItems);
      if (getRoute().name === "downloads") {
        updateHero();
        renderDownloadsView();
      }
      scheduleOnlineDownloadsPoll();
    } catch (error) {
      console.warn("failed to poll online downloads", error);
      scheduleOnlineDownloadsPoll(4000);
    }
  }, delay);
}

function clearScanPollTimer() {
  if (state.scanPollTimer) {
    window.clearTimeout(state.scanPollTimer);
    state.scanPollTimer = null;
  }
}

function updateScanUI() {
  const running = Boolean(state.scanStatus?.running);
  scanButton.disabled = running;
  scanButton.textContent = running ? "扫描中..." : "重新扫描";
}

function scheduleScanStatusPoll(delay = 3000) {
  clearScanPollTimer();
  state.scanPollTimer = window.setTimeout(async () => {
    const wasRunning = Boolean(state.scanStatus?.running);
    try {
      const status = await ensureScanStatus();
      if (status?.running) {
        if (getRoute().name === "library") {
          await Promise.all([
            ensureBookshelves(true),
            ensureLibrary(true, state.currentBookshelfId || ""),
          ]);
          updateHero();
          renderLibraryView();
        }
        if (window.location.hash === "#/" || !window.location.hash) {
          const completed = status.completedBookshelves || 0;
          const total = status.totalBookshelves || 0;
          const current = status.currentBookshelf ? `当前：${status.currentBookshelf}` : "正在准备书架";
          showFeedback(`后台正在扫描书架（${completed}/${total}）。${current}，结果会持续自动刷新。`);
        }
        scheduleScanStatusPoll(3000);
        return;
      }

      if (wasRunning) {
        await refreshAllData();
        showFeedback(
          status?.lastError ? `扫描失败: ${status.lastError}` : "后台扫描完成，书架内容已自动刷新。",
          status?.lastError ? "error" : "normal",
        );
        await renderCurrentRoute();
        return;
      }
      if (status?.running) {
        if (window.location.hash === "#/" || !window.location.hash) {
          showFeedback("后台正在扫描书架，完成后会自动刷新。");
        }
        scheduleScanStatusPoll(3000);
        return;
      }

      if (wasRunning) {
        await refreshAllData();
        showFeedback(
          status?.lastError ? `扫描失败: ${status.lastError}` : "后台扫描完成，书架内容已自动刷新。",
          status?.lastError ? "error" : "normal",
        );
        await renderCurrentRoute();
      }
    } catch (error) {
      console.warn("failed to poll scan status", error);
      scheduleScanStatusPoll(5000);
    }
  }, delay);
}

function updateHero() {
  const route = getRoute();
  if (onlineButton) {
    onlineButton.hidden = false;
  }
  if (favoritesButton) {
    favoritesButton.hidden = false;
  }
  if (followingButton) {
    followingButton.hidden = false;
  }
  if (route.name === "downloads") {
    const jobs = state.online.downloads?.items || [];
    const activeJobs = jobs.filter((job) => isActiveDownloadStatus(job.status));
    mangaCountEl.textContent = jobs.length || "-";
    currentBookshelfEl.textContent = "下载任务";
    latestUpdateEl.textContent = activeJobs.length ? `${activeJobs.length} 个进行中` : "暂无进行中";
    scanButton.disabled = true;
    return;
  }
  if (route.name === "onlineLibrary" || route.name === "onlineBookmarks" || route.name === "onlineManga" || route.name === "onlineChapter") {
    const sourceId = route.sourceId || state.online.currentSourceId;
    const source = state.online.sourceById.get(sourceId);
    if (route.name === "onlineBookmarks") {
      const kind = route.kind || "favorite";
      const payload = state.online.bookmarksByKind.get(getOnlineBookmarkCacheKey(sourceId, kind));
      mangaCountEl.textContent = payload?.items?.length || "-";
      currentBookshelfEl.textContent = kind === "follow" ? "追漫" : "收藏";
      latestUpdateEl.textContent = kind === "follow"
        ? `${(payload?.items || []).filter((item) => item.hasUpdate).length} 部有更新`
        : (source?.name || "在线收藏");
      scanButton.disabled = true;
      return;
    }
    const hasQuery = Boolean(String(state.online.searchQuery || "").trim());
    const defaultFeed = state.online.defaultFeedsBySource.get(sourceId);
    const visibleItems = hasQuery
      ? (state.online.searchResults?.items || [])
      : (defaultFeed?.items || []);
    mangaCountEl.textContent = visibleItems.length || "-";
    currentBookshelfEl.textContent = source?.name || "Online";
    latestUpdateEl.textContent = hasQuery
      ? "搜索结果"
      : (defaultFeed?.title || source?.defaultDisplay?.title || "在线入口");
    scanButton.disabled = true;
    return;
  }
  mangaCountEl.textContent = state.library?.total ?? "-";
  const selectedBookshelf = state.currentBookshelfId
    ? state.bookshelves?.items?.find((item) => item.id === state.currentBookshelfId)
    : null;
  currentBookshelfEl.textContent = selectedBookshelf?.name || "全部书架";
  const latestUpdatedItem = (state.library?.items || [])
    .filter((item) => item.updatedAt)
    .sort((left, right) => new Date(right.updatedAt) - new Date(left.updatedAt))[0];
  latestUpdateEl.textContent = latestUpdatedItem?.updatedAt
    ? new Date(latestUpdatedItem.updatedAt).toLocaleDateString("zh-CN")
    : "暂无更新";
  scanButton.disabled = Boolean(state.scanStatus?.running);
}

async function ensureHealth() {
  state.health = await fetchJSON("/health");
}

async function ensureTags(force = false) {
  if (!force && state.tags) {
    return state.tags;
  }
  state.tags = await fetchJSON("/api/tags");
  syncActiveTagFilters();
  return state.tags;
}

async function ensureScanStatus() {
  const payload = await fetchJSON("/api/tasks/scan/status");
  state.scanStatus = payload.scan;
  updateScanUI();
  return state.scanStatus;
}

function cleanupToolbarExtras() {
  if (state.toolbarCleanup) {
    state.toolbarCleanup();
    state.toolbarCleanup = null;
  }
  if (toolbarExtraEl) {
    toolbarExtraEl.innerHTML = "";
  }
}

async function ensureBookshelves(force = false) {
  if (!force && state.bookshelves) {
    return state.bookshelves;
  }
  state.bookshelves = await fetchJSON("/api/bookshelves");
  if (
    state.currentBookshelfId &&
    !state.bookshelves.items?.some((item) => item.id === state.currentBookshelfId)
  ) {
    state.currentBookshelfId = "";
    persistCurrentBookshelfId();
  }
  return state.bookshelves;
}

function getLibraryCacheKey(bookshelfId = state.currentBookshelfId || "", tagIDs = state.libraryTagFilterIDs || []) {
  return JSON.stringify({
    bookshelfId: bookshelfId || "",
    tagIDs: [...(tagIDs || [])].sort(),
  });
}

async function ensureLibrary(
  force = false,
  bookshelfId = state.currentBookshelfId || "",
  tagIDs = state.libraryTagFilterIDs || [],
) {
  const normalizedTagIDs = [...(tagIDs || [])].filter(Boolean).sort();
  const cacheKey = getLibraryCacheKey(bookshelfId, normalizedTagIDs);
  if (!force && state.libraryByBookshelfId.has(cacheKey)) {
    state.library = state.libraryByBookshelfId.get(cacheKey);
    return state.library;
  }
  const searchParams = new URLSearchParams();
  if (bookshelfId) {
    searchParams.set("bookshelfId", bookshelfId);
  }
  if (normalizedTagIDs.length) {
    searchParams.set("tagIds", normalizedTagIDs.join(","));
  }
  const query = searchParams.toString() ? `?${searchParams.toString()}` : "";
  const library = await fetchJSON(`/api/library${query}`);
  state.libraryByBookshelfId.set(cacheKey, library);
  state.library = library;
  return state.library;
}

async function ensureManga(mangaId, force = false) {
  if (force || !state.mangaById.has(mangaId)) {
    const manga = await fetchJSON(`/api/manga/${encodeURIComponent(mangaId)}`);
    state.mangaById.set(mangaId, manga);
  }
  return state.mangaById.get(mangaId);
}

async function ensureChapters(mangaId, force = false) {
  if (force || !state.chaptersByMangaId.has(mangaId)) {
    const chapters = await fetchJSON(`/api/manga/${encodeURIComponent(mangaId)}/chapters`);
    state.chaptersByMangaId.set(mangaId, chapters);
  }
  return state.chaptersByMangaId.get(mangaId);
}

async function ensurePages(chapterId) {
  if (!state.pagesByChapterId.has(chapterId)) {
    const pages = await fetchJSON(`/api/chapters/${encodeURIComponent(chapterId)}/pages`);
    state.pagesByChapterId.set(chapterId, pages);
  }
  return state.pagesByChapterId.get(chapterId);
}

function getOnlineSearchCacheKey(sourceId, query) {
  return `${sourceId}::${String(query || "").trim().toLocaleLowerCase("zh-CN")}`;
}

function getOnlineDefaultFeedCacheKey(sourceId) {
  return String(sourceId || "").trim() || "default";
}

function getOnlineEntityKey(sourceId, id) {
  return `${sourceId}::${id}`;
}

async function ensureOnlineSources(force = false) {
  if (!force && state.online.sources) {
    return state.online.sources;
  }
  state.online.sources = await fetchJSON("/api/online/sources");
  state.online.sourceById = new Map((state.online.sources.items || []).map((item) => [item.id, item]));
  if (!state.online.currentSourceId || !state.online.sourceById.has(state.online.currentSourceId)) {
    state.online.currentSourceId = state.online.sources.items?.find((item) => item.enabled)?.id
      || state.online.sources.items?.[0]?.id
      || "18comic";
  }
  return state.online.sources;
}

async function ensureOnlineSettings(force = false) {
  if (!force && state.online.settings) {
    return state.online.settings;
  }
  state.online.settings = await fetchJSON("/api/online/settings");
  state.online.settingsBySource = new Map((state.online.settings.items || []).map((item) => [item.sourceId, item]));
  return state.online.settings;
}

function getOnlineSourceSettings(sourceId) {
  return state.online.settingsBySource.get(sourceId) || { sourceId, blacklistedTags: [] };
}

function parseOnlineBlacklistInput(value) {
  const seen = new Set();
  return String(value || "")
    .split(/[\n,，;；]+/)
    .map((item) => item.trim())
    .filter((item) => {
      if (!item) {
        return false;
      }
      const key = item.toLowerCase();
      if (seen.has(key)) {
        return false;
      }
      seen.add(key);
      return true;
    });
}

function formatOnlineBlacklistTags(tags = []) {
  return (tags || []).join("\n");
}

async function saveOnlineSourceSettings(sourceId, blacklistedTags) {
  const payload = await fetchJSON(`/api/online/${encodeURIComponent(sourceId)}/settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ blacklistedTags }),
  });
  state.online.settingsBySource.set(sourceId, payload);
  state.online.settings = {
    items: (state.online.settings?.items || []).filter((item) => item.sourceId !== sourceId).concat(payload),
  };
  state.online.searchResultsByKey.clear();
  state.online.searchResults = null;
  state.online.defaultFeedsBySource.clear();
  state.online.defaultFeed = null;
  return payload;
}

async function ensureOnlineSearch(sourceId, query, force = false) {
  const cacheKey = getOnlineSearchCacheKey(sourceId, query);
  if (!force && state.online.searchResultsByKey.has(cacheKey)) {
    state.online.searchResults = state.online.searchResultsByKey.get(cacheKey);
    return state.online.searchResults;
  }

  const encodedQuery = encodeURIComponent(String(query || "").trim());
  const payload = encodedQuery
    ? await fetchJSON(`/api/online/${encodeURIComponent(sourceId)}/search?q=${encodedQuery}`)
    : { items: [] };

  state.online.searchResultsByKey.set(cacheKey, payload);
  state.online.searchResults = payload;
  return payload;
}

async function ensureOnlineDefaultFeed(sourceId, force = false, page = 1) {
  const cacheKey = getOnlineDefaultFeedCacheKey(sourceId);
  if (!force && page <= 1 && state.online.defaultFeedsBySource.has(cacheKey)) {
    state.online.defaultFeed = state.online.defaultFeedsBySource.get(cacheKey);
    return state.online.defaultFeed;
  }

  const source = state.online.sourceById.get(sourceId);
  const configuredLimit = Number(source?.defaultDisplay?.limit || 30);
  const requestPage = Math.max(1, Number(page) || 1);
  const requestLimit = Math.max(1, configuredLimit || 30);
  const payload = await fetchJSON(
    `/api/online/${encodeURIComponent(sourceId)}/default?page=${requestPage}&limit=${requestLimit}`,
  );
  const previous = !force && requestPage > 1
    ? state.online.defaultFeedsBySource.get(cacheKey)
    : null;

  const previousItems = previous?.items || [];
  const seen = new Set(previousItems.map((item) => String(item.id)));
  const appendedItems = (payload.items || []).filter((item) => {
    const key = String(item.id);
    if (!key || seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
  const merged = previous
    ? {
        ...payload,
        items: [...previousItems, ...appendedItems],
      }
    : payload;

  state.online.defaultFeedsBySource.set(cacheKey, merged);
  state.online.defaultFeed = merged;
  return merged;
}

async function refreshOnlineDefaultFeed(sourceId) {
  const cacheKey = getOnlineDefaultFeedCacheKey(sourceId);
  const source = state.online.sourceById.get(sourceId);
  const requestLimit = Math.max(1, Number(source?.defaultDisplay?.limit || 30));
  const payload = await fetchJSON(
    `/api/online/${encodeURIComponent(sourceId)}/default/refresh?page=1&limit=${requestLimit}`,
    { method: "POST" },
  );
  state.online.defaultFeedsBySource.set(cacheKey, payload);
  state.online.defaultFeed = payload;
  return payload;
}

async function loadMoreOnlineDefaultFeed(sourceId) {
  const cacheKey = getOnlineDefaultFeedCacheKey(sourceId);
  if (state.online.pendingDefaultFeedSources.has(cacheKey)) {
    return null;
  }

  const current = state.online.defaultFeedsBySource.get(cacheKey);
  const nextPage = Math.max(1, Number(current?.page || 1)) + 1;
  state.online.pendingDefaultFeedSources.add(cacheKey);
  try {
    return await ensureOnlineDefaultFeed(sourceId, false, nextPage);
  } finally {
    state.online.pendingDefaultFeedSources.delete(cacheKey);
  }
}

async function ensureOnlineManga(sourceId, mangaId, force = false) {
  const cacheKey = getOnlineEntityKey(sourceId, mangaId);
  if (force || !state.online.mangaByKey.has(cacheKey)) {
    const manga = await fetchJSON(`/api/online/${encodeURIComponent(sourceId)}/manga/${encodeURIComponent(mangaId)}`);
    state.online.mangaByKey.set(cacheKey, manga);
  }
  return state.online.mangaByKey.get(cacheKey);
}

async function ensureOnlineChapters(sourceId, mangaId, force = false) {
  const cacheKey = getOnlineEntityKey(sourceId, mangaId);
  if (force || !state.online.chaptersByKey.has(cacheKey)) {
    const chapters = await fetchJSON(`/api/online/${encodeURIComponent(sourceId)}/manga/${encodeURIComponent(mangaId)}/chapters`);
    state.online.chaptersByKey.set(cacheKey, chapters);
  }
  return state.online.chaptersByKey.get(cacheKey);
}

async function ensureOnlinePages(sourceId, chapterId, force = false) {
  const cacheKey = getOnlineEntityKey(sourceId, chapterId);
  if (force || !state.online.pagesByKey.has(cacheKey)) {
    const pages = await fetchJSON(`/api/online/${encodeURIComponent(sourceId)}/chapters/${encodeURIComponent(chapterId)}/pages`);
    state.online.pagesByKey.set(cacheKey, pages);
  }
  return state.online.pagesByKey.get(cacheKey);
}

function getOnlineBookmarkCacheKey(sourceId, kind = "favorite") {
  return `${sourceId || ""}::${kind === "follow" ? "follow" : "favorite"}`;
}

async function ensureOnlineBookmarks(sourceId, kind = "favorite", force = false) {
  const normalizedKind = kind === "follow" ? "follow" : "favorite";
  const cacheKey = getOnlineBookmarkCacheKey(sourceId, normalizedKind);
  if (!force && state.online.bookmarksByKind.has(cacheKey)) {
    return state.online.bookmarksByKind.get(cacheKey);
  }
  const payload = await fetchJSON(
    `/api/online/${encodeURIComponent(sourceId)}/bookmarks?kind=${encodeURIComponent(normalizedKind)}`,
  );
  state.online.bookmarksByKind.set(cacheKey, payload);
  return payload;
}

function getOnlineBookmarkState(sourceId, mangaId) {
  const favoritePayload = state.online.bookmarksByKind.get(getOnlineBookmarkCacheKey(sourceId, "favorite"));
  const followPayload = state.online.bookmarksByKind.get(getOnlineBookmarkCacheKey(sourceId, "follow"));
  const favorite = (favoritePayload?.items || []).find((item) => item.id === mangaId);
  const following = (followPayload?.items || []).find((item) => item.id === mangaId);
  return {
    favorite: Boolean(favorite || following?.favorite),
    following: Boolean(following || favorite?.following),
    hasUpdate: Boolean(following?.hasUpdate || favorite?.hasUpdate),
  };
}

async function updateOnlineBookmark(sourceId, mangaId, patch) {
  const response = await fetchJSON(
    `/api/online/${encodeURIComponent(sourceId)}/manga/${encodeURIComponent(mangaId)}/bookmark`,
    {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    },
  );
  state.online.bookmarksByKind.delete(getOnlineBookmarkCacheKey(sourceId, "favorite"));
  state.online.bookmarksByKind.delete(getOnlineBookmarkCacheKey(sourceId, "follow"));
  const mangaKey = getOnlineEntityKey(sourceId, mangaId);
  if (state.online.mangaByKey.has(mangaKey)) {
    const cached = state.online.mangaByKey.get(mangaKey);
    state.online.mangaByKey.set(mangaKey, {
      ...cached,
      favorite: response.favorite,
      following: response.following,
      hasUpdate: response.hasUpdate,
      latestChapterId: response.latestChapterId,
    });
  }
  await Promise.all([
    ensureOnlineBookmarks(sourceId, "favorite", true),
    ensureOnlineBookmarks(sourceId, "follow", true),
  ]);
  return response;
}

async function ensureOnlineDownloads(force = false) {
  if (!force && state.online.downloads) {
    return state.online.downloads;
  }
  const previousItems = state.online.downloads?.items || [];
  state.online.downloads = await fetchJSON("/api/online/downloads?limit=200");
  await handleOnlineDownloadsUpdate(previousItems, state.online.downloads?.items || []);
  scheduleOnlineDownloadsPoll();
  return state.online.downloads;
}

function getDownloadStatusText(status) {
  const normalized = String(status || "").toLowerCase();
  if (normalized === "queued") return "排队中";
  if (normalized === "running") return "下载中";
  if (normalized === "processing") return "处理中";
  if (normalized === "paused") return "已暂停";
  if (normalized === "failed") return "失败";
  if (normalized === "done") return "已完成";
  if (normalized === "canceled") return "已取消";
  return status || "未知";
}

function getDownloadStatusGroup(status) {
  const normalized = String(status || "").toLowerCase();
  if (normalized === "queued" || normalized === "running" || normalized === "processing") {
    return "active";
  }
  if (normalized === "paused") {
    return "paused";
  }
  if (normalized === "failed") {
    return "failed";
  }
  if (normalized === "done") {
    return "done";
  }
  if (normalized === "canceled") {
    return "canceled";
  }
  return "other";
}

function getDownloadStatusFilterLabel(filter) {
  switch (filter) {
    case "active":
      return "下载中";
    case "paused":
      return "已暂停";
    case "failed":
      return "失败";
    case "done":
      return "已完成";
    case "canceled":
      return "已取消";
    default:
      return "全部";
  }
}

function getDownloadProgressPercent(job) {
  if (!job?.totalPages) {
    return 0;
  }
  return Math.min(100, Math.round((job.donePages / job.totalPages) * 100));
}

function sortDownloadJobs(items, sortMode = "updated-desc") {
  const sorted = [...items];
  if (sortMode === "created-desc") {
    sorted.sort((left, right) => String(right.createdAt || "").localeCompare(String(left.createdAt || "")));
    return sorted;
  }
  if (sortMode === "progress-desc") {
    sorted.sort((left, right) => getDownloadProgressPercent(right) - getDownloadProgressPercent(left));
    return sorted;
  }
  sorted.sort((left, right) => String(right.updatedAt || right.createdAt || "").localeCompare(String(left.updatedAt || left.createdAt || "")));
  return sorted;
}

function getFilteredDownloadJobs(items) {
  const statusFilter = state.online.downloadStatusFilter || "all";
  const sourceFilter = state.online.downloadSourceFilter || "all";
  return sortDownloadJobs(items, state.online.downloadSort).filter((job) => {
    if (sourceFilter !== "all" && job.sourceId !== sourceFilter) {
      return false;
    }
    if (statusFilter === "all") {
      return true;
    }
    return getDownloadStatusGroup(job.status) === statusFilter;
  });
}

function renderDownloadJobCard(job) {
  const status = String(job.status || "").toLowerCase();
  const progress = getDownloadProgressPercent(job);
  const mangaTitle = job.mangaTitle || job.mangaId;
  const mangaRoute = routeForManga({ sourceId: job.sourceId, id: job.mangaId });
  const coverURL = buildOnlineImageProxyURL(job.sourceId, job.coverUrl || "");
  return `
    <article class="download-job-card">
      <div class="download-job-card__content">
        <div class="download-job-card__cover" aria-hidden="true">
          ${coverURL
            ? `<img src="${escapeHTML(coverURL)}" alt="" loading="lazy" />`
            : `<span>${escapeHTML(String(mangaTitle || "?").slice(0, 1).toUpperCase())}</span>`
          }
        </div>
        <div class="download-job-card__main">
          <div class="download-job-card__heading">
            <a class="download-job-card__title" href="${escapeHTML(mangaRoute)}" data-route="${escapeHTML(mangaRoute)}">
              ${escapeHTML(mangaTitle)}
            </a>
            <span>${escapeHTML(getOnlineSourceName(job.sourceId))}</span>
          </div>
          <p>${escapeHTML(getDownloadStatusText(job.status))} · ${job.donePages}/${job.totalPages || 0} 页 · ${job.doneChapters || 0}/${job.totalChapters || 0} 话</p>
          <div class="download-job-card__progress">
            <span style="width:${progress}%"></span>
          </div>
          ${job.errorMessage ? `<p class="download-job-card__error">${escapeHTML(job.errorMessage)}</p>` : ""}
        </div>
      </div>
      <div class="download-job-card__actions">
        ${status === "running" ? `<button class="ghost-button ghost-button--small" type="button" data-download-action="pause" data-download-job="${escapeHTML(job.id)}">暂停</button>` : ""}
        ${(status === "paused" || status === "failed" || status === "queued") ? `<button class="ghost-button ghost-button--small" type="button" data-download-action="resume" data-download-job="${escapeHTML(job.id)}">继续</button>` : ""}
        ${status === "failed" ? `<button class="ghost-button ghost-button--small" type="button" data-download-action="retry" data-download-job="${escapeHTML(job.id)}">重试</button>` : ""}
        ${["done", "failed", "canceled"].includes(status) ? `<button class="ghost-button ghost-button--small" type="button" data-download-action="redownload" data-download-job="${escapeHTML(job.id)}">重新下载</button>` : ""}
        ${(status !== "done" && status !== "canceled") ? `<button class="ghost-button ghost-button--small" type="button" data-download-action="cancel" data-download-job="${escapeHTML(job.id)}">取消</button>` : ""}
        <button class="ghost-button ghost-button--small" type="button" data-download-delete="record" data-download-job="${escapeHTML(job.id)}">删除记录</button>
        <button class="ghost-button ghost-button--small" type="button" data-download-delete="files" data-download-job="${escapeHTML(job.id)}">删除本地文件和记录</button>
      </div>
    </article>
  `;
}

function bindDownloadJobActions() {
  appViewEl.querySelectorAll("[data-download-action]").forEach((node) => {
    node.addEventListener("click", async () => {
      node.disabled = true;
      try {
        const result = await mutateOnlineDownloadJob(node.dataset.downloadJob, node.dataset.downloadAction);
        if (!result) {
          node.disabled = false;
          showFeedback("这个下载动作已经在处理中。");
          return;
        }
        showFeedback(node.dataset.downloadAction === "redownload" ? "已清理旧文件并重新加入下载。" : "下载任务已更新。");
        await renderCurrentRoute();
      } catch (error) {
        node.disabled = false;
        showFeedback(`更新下载任务失败: ${error.message}`, "error");
      }
    });
  });

  appViewEl.querySelectorAll("[data-download-delete]").forEach((node) => {
    node.addEventListener("click", async () => {
      const removeFiles = node.dataset.downloadDelete === "files";
      const confirmMessage = removeFiles
        ? "确认删除这个下载任务，以及它对应的本地文件吗？"
        : "确认只删除这个下载任务记录吗？";
      if (!window.confirm(confirmMessage)) {
        return;
      }

      node.disabled = true;
      try {
        const result = await deleteOnlineDownloadJob(node.dataset.downloadJob, removeFiles);
        if (!result) {
          node.disabled = false;
          showFeedback("这个删除动作已经在处理中。");
          return;
        }
        showFeedback(removeFiles ? "已删除本地文件和下载记录。" : "已删除下载记录。");
        await renderCurrentRoute();
      } catch (error) {
        node.disabled = false;
        showFeedback(`删除下载任务失败: ${error.message}`, "error");
      }
    });
  });
}

function describeDownloadCreateResult(result) {
  if (!result) {
    return "这个下载请求正在创建中。";
  }
  if (result.existing) {
    if (String(result.status || "").toLowerCase() === "done") {
      return "这个内容已经下载完成，可以在下载页重新下载。";
    }
    return `已复用现有任务：${getDownloadStatusText(result.status)}。`;
  }
  if (String(result.status || "").toLowerCase() === "processing") {
    return "下载已完成，正在整理文件并刷新书架。";
  }
  return "已加入下载队列。";
}

async function createOnlineDownloadJob(sourceId, mangaId, chapterIds = [], mode = "") {
  const requestKey = getDownloadRequestKey(sourceId, mangaId, chapterIds, mode);
  if (state.online.pendingCreateKeys.has(requestKey)) {
    return null;
  }

  state.online.pendingCreateKeys.add(requestKey);
  try {
    const payload = await fetchJSON(`/api/online/${encodeURIComponent(sourceId)}/manga/${encodeURIComponent(mangaId)}/download`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        chapterIds,
        mode,
      }),
    });
    state.online.downloads = null;
    await ensureOnlineDownloads(true);
    return payload;
  } finally {
    state.online.pendingCreateKeys.delete(requestKey);
  }
}

function removeOnlineMangaFromVisibleCaches(sourceId, mangaId) {
  const targetSourceID = String(sourceId || "");
  const targetMangaID = String(mangaId || "");
  const removeFromPayload = (payload) => {
    if (!payload?.items) {
      return payload;
    }
    return {
      ...payload,
      items: payload.items.filter((item) => (
        String(item.sourceId || "") !== targetSourceID ||
        String(item.id || "") !== targetMangaID
      )),
    };
  };

  for (const [key, payload] of state.online.defaultFeedsBySource.entries()) {
    state.online.defaultFeedsBySource.set(key, removeFromPayload(payload));
  }
  for (const [key, payload] of state.online.searchResultsByKey.entries()) {
    state.online.searchResultsByKey.set(key, removeFromPayload(payload));
  }
  state.online.defaultFeed = removeFromPayload(state.online.defaultFeed);
  state.online.searchResults = removeFromPayload(state.online.searchResults);
  state.online.mangaByKey.delete(getOnlineEntityKey(sourceId, mangaId));
}

async function blockOnlineManga(sourceId, mangaId) {
  const payload = await fetchJSON(
    `/api/online/${encodeURIComponent(sourceId)}/manga/${encodeURIComponent(mangaId)}/block`,
    { method: "POST" },
  );
  removeOnlineMangaFromVisibleCaches(sourceId, mangaId);
  return payload;
}

async function mutateOnlineDownloadJob(jobId, action) {
  const actionKey = `${jobId}::${action}`;
  if (state.online.pendingJobActions.has(actionKey)) {
    return null;
  }

  state.online.pendingJobActions.add(actionKey);
  try {
    const payload = await fetchJSON(`/api/online/downloads/${encodeURIComponent(jobId)}/${encodeURIComponent(action)}`, {
      method: "POST",
    });
    state.online.downloads = null;
    await ensureOnlineDownloads(true);
    return payload;
  } finally {
    state.online.pendingJobActions.delete(actionKey);
  }
}

function getCachedLibraryItems() {
  const byId = new Map();
  const addItems = (library) => {
    if (!Array.isArray(library?.items)) {
      return;
    }
    library.items.forEach((item) => {
      if (item?.id && !byId.has(item.id)) {
        byId.set(item.id, item);
      }
    });
  };

  addItems(state.library);
  state.libraryByBookshelfId.forEach(addItems);
  return [...byId.values()];
}

async function fetchLibraryItemsForLookup(bookshelfId = "", tagIDs = []) {
  const normalizedTagIDs = [...(tagIDs || [])].filter(Boolean).sort();
  const items = [];
  let page = 1;
  let hasMore = true;

  while (hasMore && page <= 50) {
    const searchParams = new URLSearchParams();
    searchParams.set("page", String(page));
    searchParams.set("limit", "200");
    if (bookshelfId) {
      searchParams.set("bookshelfId", bookshelfId);
    }
    if (normalizedTagIDs.length) {
      searchParams.set("tagIds", normalizedTagIDs.join(","));
    }

    const library = await fetchJSON(`/api/library?${searchParams.toString()}`);
    if (Array.isArray(library?.items)) {
      items.push(...library.items);
    }
    hasMore = Boolean(library?.hasMore);
    page += 1;
  }

  return items;
}

function findCachedChapterContext(chapterId) {
  for (const [mangaId, chapters] of state.chaptersByMangaId.entries()) {
    const chapter = chapters?.items?.find((entry) => entry.id === chapterId);
    if (chapter) {
      return { mangaId, chapter, chapters };
    }
  }
  return null;
}

async function createReaderContext(mangaId, chapter, chapters, chapterId) {
  const [manga, pages] = await Promise.all([
    ensureManga(mangaId),
    ensurePages(chapterId),
  ]);

  return {
    manga,
    chapter,
    chapters,
    pages,
  };
}

async function findReaderContextInItems(chapterId, libraryItems) {
  const seenMangaIds = new Set();
  for (const item of libraryItems) {
    if (!item?.id || seenMangaIds.has(item.id)) {
      continue;
    }
    seenMangaIds.add(item.id);

    const chapters = await ensureChapters(item.id);
    const matchedChapter = chapters.items.find((entry) => entry.id === chapterId);
    if (!matchedChapter) {
      continue;
    }

    return createReaderContext(item.id, matchedChapter, chapters, chapterId);
  }

  return null;
}

async function refreshAllData() {
  invalidateLibraryCaches();
  state.tags = null;
  await Promise.all([ensureHealth(), ensureBookshelves(true), ensureScanStatus()]);
  await ensureLibrary(true, state.currentBookshelfId || "", state.libraryTagFilterIDs || []);
}

async function refreshCurrentLibraryFromDisk() {
  const bookshelfId = state.currentBookshelfId || "";
  if (!bookshelfId) {
    await fetchJSON("/api/tasks/scan", { method: "POST" });
    await ensureScanStatus();
    scheduleScanStatusPoll(1200);
    return;
  }

  await fetchJSON(`/api/tasks/scan/bookshelf/${encodeURIComponent(bookshelfId)}`, { method: "POST" });
  invalidateLibraryCaches();
  state.tags = null;
  await Promise.all([ensureBookshelves(true), ensureScanStatus()]);
  await ensureLibrary(true, bookshelfId, state.libraryTagFilterIDs || []);
}

async function refreshOnlineData(sourceId = state.online.currentSourceId || "18comic") {
  state.online.sources = null;
  state.online.sourceById.clear();
  state.online.searchResults = null;
  state.online.searchResultsByKey.clear();
  state.online.defaultFeed = null;
  state.online.defaultFeedsBySource.clear();
  state.online.pendingDefaultFeedSources.clear();
  state.online.mangaByKey.clear();
  state.online.chaptersByKey.clear();
  state.online.pagesByKey.clear();
  state.online.bookmarksByKind.clear();
  state.online.downloads = null;
  await ensureOnlineSources(true);
  if (String(state.online.searchQuery || "").trim()) {
    await ensureOnlineSearch(sourceId, state.online.searchQuery || "", true);
  } else {
    await refreshOnlineDefaultFeed(sourceId);
  }
  await ensureOnlineDownloads(true);
  await Promise.all([
    ensureOnlineBookmarks(sourceId, "favorite", true),
    ensureOnlineBookmarks(sourceId, "follow", true),
  ]);
}

function formatDownloadStatusLabel(status) {
  return getDownloadStatusText(status);
}

function getOnlineSourceName(sourceId) {
  return state.online.sourceById.get(sourceId)?.name || sourceId || "在线来源";
}

function renderInlineTagList(tags, max = 4) {
  const items = (tags || []).filter(Boolean).slice(0, max);
  if (!items.length) {
    return "";
  }

  return `
    <div class="detail-tag-list">
      ${items.map((tag) => `<span class="manga-tag">${escapeHTML(tag)}</span>`).join("")}
    </div>
  `;
}

function renderOnlineTileTagList(tags, max = 2) {
  const items = (tags || [])
    .map((tag) => String(typeof tag === "object" ? (tag?.name || tag?.label || "") : tag).trim())
    .filter(Boolean)
    .slice(0, max);
  if (!items.length) {
    return "";
  }

  return `
    <div class="online-tag-list" aria-label="漫画标签">
      ${items.map((tag) => `<span class="online-tag-list__item">${escapeHTML(tag)}</span>`).join("")}
    </div>
  `;
}

function renderOnlineAuthor(author, variant = "tile") {
  const value = String(author || "").trim();
  if (!value) {
    return "";
  }

  const className = variant === "detail" ? "online-author online-author--detail" : "online-author";
  return `
    <p class="${className}" title="${escapeHTML(`作者：${value}`)}">
      <span>作者</span>
      <strong>${escapeHTML(value)}</strong>
    </p>
  `;
}

async function refreshMangaContext(mangaId) {
  const manga = await ensureManga(mangaId, true);
  if (manga?.bookshelfId && state.currentBookshelfId !== manga.bookshelfId) {
    state.currentBookshelfId = manga.bookshelfId;
    persistCurrentBookshelfId();
  }

  try {
    await fetchJSON(`/api/tasks/scan/manga/${encodeURIComponent(mangaId)}`, { method: "POST" });
  } catch (error) {
    console.warn("failed to refresh manga scan", error);
  }

  const [library, refreshedManga, chapters] = await Promise.all([
    ensureLibrary(true, state.currentBookshelfId || "", state.libraryTagFilterIDs || []),
    ensureManga(mangaId, true),
    ensureChapters(mangaId, true),
  ]);

  if (Array.isArray(library?.items)) {
    const item = library.items.find((entry) => entry.id === mangaId);
    if (item) {
      state.mangaById.set(mangaId, {
        ...refreshedManga,
        chapterCount: item.chapterCount,
        pageCount: item.pageCount,
        updatedAt: item.updatedAt,
        coverThumbUrl: item.coverThumbUrl ?? refreshedManga.coverThumbUrl,
      });
    }
  }

  return {
    manga: state.mangaById.get(mangaId),
    chapters,
  };
}

function bindViewActions() {
  appViewEl.querySelectorAll("[data-route]").forEach((node) => {
    node.addEventListener("click", () => {
      setRoute(node.dataset.route);
    });
  });
}

function normalizeSearchText(value) {
  return String(value || "").trim().toLocaleLowerCase("zh-CN");
}

function normalizeTagIDs(tagIDs) {
  return [...new Set((tagIDs || []).map((id) => String(id || "").trim()).filter(Boolean))];
}

function syncActiveTagFilters() {
  const validTagIDs = new Set((state.tags?.items || []).map((item) => item.id));
  const normalizedFilters = normalizeTagIDs((state.libraryTagFilterIDs || []).filter((id) => validTagIDs.has(id)));
  if (normalizedFilters.join("|") !== (state.libraryTagFilterIDs || []).join("|")) {
    state.libraryTagFilterIDs = normalizedFilters;
    persistLibraryTagFilters();
  }
}

function getFilteredLibraryItems(items) {
  const query = normalizeSearchText(state.librarySearchQuery);
  if (!query) {
    return items;
  }
  return items.filter((item) => normalizeSearchText(item.title).includes(query));
}

function getSortedLibraryItems(items) {
  const sorted = [...items];
  switch (state.librarySort) {
    case "title-asc":
      sorted.sort((left, right) => left.title.localeCompare(right.title, "zh-CN"));
      break;
    case "chapters-desc":
      sorted.sort((left, right) => {
        if (right.chapterCount !== left.chapterCount) {
          return right.chapterCount - left.chapterCount;
        }
        return String(right.updatedAt).localeCompare(String(left.updatedAt));
      });
      break;
    case "updated-desc":
    default:
      sorted.sort((left, right) => {
        const byUpdated = String(right.updatedAt).localeCompare(String(left.updatedAt));
        if (byUpdated !== 0) {
          return byUpdated;
        }
        return left.title.localeCompare(right.title, "zh-CN");
      });
      break;
  }
  return sorted;
}

function clearLibrarySearchTimer() {
  if (state.librarySearchTimer) {
    window.clearTimeout(state.librarySearchTimer);
    state.librarySearchTimer = null;
  }
}

function applyLibrarySearch(query, options = {}) {
  const nextQuery = String(query || "");
  state.librarySearchDraft = nextQuery;

  if (state.librarySearchQuery === nextQuery && !options.forceRender) {
    return;
  }

  state.librarySearchQuery = nextQuery;
  renderLibraryView();
}

function scheduleLibrarySearch(query) {
  state.librarySearchDraft = String(query || "");
  clearLibrarySearchTimer();
  state.librarySearchTimer = window.setTimeout(() => {
    state.librarySearchTimer = null;
    applyLibrarySearch(state.librarySearchDraft);
  }, 320);
}

function bindLibraryActions() {
  bindViewActions();
  appViewEl.querySelectorAll("[data-bookshelf-id]").forEach((node) => {
    node.addEventListener("click", async () => {
      const nextId = node.dataset.bookshelfId || "";
      if (state.currentBookshelfId === nextId) {
        return;
      }
      state.currentBookshelfId = nextId;
      persistCurrentBookshelfId();
      await ensureLibrary(false, nextId, state.libraryTagFilterIDs || []);
      renderLibraryView();
    });
  });

  const searchInput = appViewEl.querySelector("[data-library-search]");
  const searchButton = appViewEl.querySelector("[data-library-search-submit]");
  const clearButton = appViewEl.querySelector("[data-library-search-clear]");
  const sortSelect = appViewEl.querySelector("[data-library-sort]");
  const removeTagButtons = appViewEl.querySelectorAll("[data-library-tag-remove]");
  const clearTagsButton = appViewEl.querySelector("[data-library-tags-clear]");

  if (searchInput) {
    searchInput.addEventListener("input", (event) => {
      scheduleLibrarySearch(event.target.value);
    });

    searchInput.addEventListener("keydown", (event) => {
      if (event.key !== "Enter") {
        return;
      }
      event.preventDefault();
      clearLibrarySearchTimer();
      applyLibrarySearch(event.target.value);
    });
  }

  if (searchButton) {
    searchButton.addEventListener("click", () => {
      clearLibrarySearchTimer();
      applyLibrarySearch(searchInput?.value ?? state.librarySearchDraft);
    });
  }

  if (clearButton) {
    clearButton.addEventListener("click", () => {
      clearLibrarySearchTimer();
      state.librarySearchDraft = "";
      applyLibrarySearch("", { forceRender: true });
      const nextInput = appViewEl.querySelector("[data-library-search]");
      nextInput?.focus();
    });
  }

  if (sortSelect) {
    sortSelect.addEventListener("change", (event) => {
      state.librarySort = event.target.value || "updated-desc";
      persistLibrarySort();
      renderLibraryView();
    });
  }

  removeTagButtons.forEach((button) => {
    button.addEventListener("click", async () => {
      const tagID = button.dataset.libraryTagRemove || "";
      state.libraryTagFilterIDs = state.libraryTagFilterIDs.filter((id) => id !== tagID);
      persistLibraryTagFilters();
      await ensureLibrary(true, state.currentBookshelfId || "", state.libraryTagFilterIDs || []);
      updateHero();
      renderLibraryView();
    });
  });

  if (clearTagsButton) {
    clearTagsButton.addEventListener("click", async () => {
      state.libraryTagFilterIDs = [];
      persistLibraryTagFilters();
      await ensureLibrary(true, state.currentBookshelfId || "", []);
      updateHero();
      renderLibraryView();
    });
  }
}

function getOrderedChapters(mangaId, chapters) {
  const order = state.chapterOrderByMangaId.get(mangaId) || "asc";
  const items = [...chapters.items];
  if (order === "desc") {
    items.reverse();
  }
  return { order, items };
}

function getOrderedTags(tags) {
  return [...(tags || [])].sort((left, right) => {
    if (Boolean(right.pinned) !== Boolean(left.pinned)) {
      return Number(Boolean(right.pinned)) - Number(Boolean(left.pinned));
    }
    if ((right.priority || 0) !== (left.priority || 0)) {
      return (right.priority || 0) - (left.priority || 0);
    }
    if ((left.group || "") !== (right.group || "")) {
      return String(left.group || "").localeCompare(String(right.group || ""), "zh-CN");
    }
    return String(left.name || "").localeCompare(String(right.name || ""), "zh-CN");
  });
}

function createTagChipNode(tag, options = {}) {
  const chip = document.createElement(options.asButton ? "button" : "span");
  chip.className = `manga-tag${options.summary ? " manga-tag--summary" : ""}${options.empty ? " manga-tag--empty" : ""}`;
  chip.textContent = options.label || tag?.name || "";
  if (!options.summary && !options.empty && tag?.color) {
    chip.style.setProperty("--tag-accent", tag.color);
  }
  if (options.asButton) {
    chip.type = "button";
  }
  return chip;
}

function renderToolbarTagStrip(stripEl, tags) {
  if (!stripEl) {
    return;
  }

  const orderedTags = getOrderedTags(tags);
  stripEl.innerHTML = "";

  if (!orderedTags.length) {
    stripEl.appendChild(createTagChipNode(null, { label: "未设置标签", empty: true }));
    return;
  }

  const availableWidth = Math.floor(stripEl.getBoundingClientRect().width);
  if (availableWidth <= 0) {
    orderedTags.forEach((tag) => stripEl.appendChild(createTagChipNode(tag)));
    return;
  }

  const probe = document.createElement("div");
  probe.className = "manga-tag-strip manga-tag-strip--measure";
  document.body.appendChild(probe);

  const measureChip = (node) => {
    probe.appendChild(node);
    const width = Math.ceil(node.getBoundingClientRect().width);
    node.remove();
    return width;
  };

  const widths = orderedTags.map((tag) => measureChip(createTagChipNode(tag)));
  const summaryWidthCache = new Map();
  const getSummaryWidth = (hiddenCount) => {
    if (!hiddenCount) {
      return 0;
    }
    if (!summaryWidthCache.has(hiddenCount)) {
      summaryWidthCache.set(hiddenCount, measureChip(createTagChipNode(null, { label: `+${hiddenCount}`, summary: true })));
    }
    return summaryWidthCache.get(hiddenCount);
  };

  let usedWidth = 0;
  let visibleCount = 0;
  for (let index = 0; index < orderedTags.length; index += 1) {
    const remaining = orderedTags.length - index - 1;
    const nextWidth = widths[index];
    const reserveWidth = getSummaryWidth(remaining);
    if (usedWidth + nextWidth + reserveWidth > availableWidth) {
      break;
    }
    usedWidth += nextWidth;
    visibleCount += 1;
  }

  if (visibleCount === 0) {
    stripEl.appendChild(createTagChipNode(null, { label: `+${orderedTags.length}`, summary: true }));
    probe.remove();
    return;
  }

  orderedTags.slice(0, visibleCount).forEach((tag) => {
    stripEl.appendChild(createTagChipNode(tag));
  });

  const hiddenCount = orderedTags.length - visibleCount;
  if (hiddenCount > 0) {
    stripEl.appendChild(createTagChipNode(null, { label: `+${hiddenCount}`, summary: true }));
  }

  probe.remove();
}

function buildTagPickerGroups(tags, selectedTagIDs, searchQuery = "") {
  const query = String(searchQuery || "").trim().toLocaleLowerCase("zh-CN");
  const grouped = new Map();

  getOrderedTags(tags).forEach((tag) => {
    const haystack = `${tag.name} ${tag.group || ""}`.toLocaleLowerCase("zh-CN");
    if (query && !haystack.includes(query)) {
      return;
    }
    const groupName = tag.group || "其他";
    if (!grouped.has(groupName)) {
      grouped.set(groupName, []);
    }
    grouped.get(groupName).push(tag);
  });

  if (!grouped.size) {
    return `<div class="manga-tag-picker__empty">没有匹配的标签</div>`;
  }

  return [...grouped.entries()]
    .map(
      ([groupName, items]) => `
        <section class="manga-tag-picker__group">
          <p class="manga-tag-picker__group-title">${escapeHTML(groupName)}</p>
          <div class="manga-tag-picker__options">
            ${items
              .map((tag) => {
                const checked = selectedTagIDs.has(tag.id) ? "checked" : "";
                return `
                  <label class="manga-tag-option">
                    <input type="checkbox" value="${escapeHTML(tag.id)}" ${checked} />
                    <span class="manga-tag-option__marker" style="--tag-accent: ${escapeHTML(tag.color || "#c77757")}"></span>
                    <span class="manga-tag-option__name">${escapeHTML(tag.name)}</span>
                  </label>
                `;
              })
              .join("")}
          </div>
        </section>
      `,
    )
    .join("");
}

function getSelectedLibraryTags() {
  const tagMap = new Map((state.tags?.items || []).map((tag) => [tag.id, tag]));
  return getOrderedTags(
    normalizeTagIDs(state.libraryTagFilterIDs || [])
      .map((tagID) => tagMap.get(tagID))
      .filter(Boolean),
  );
}

function setupLibraryToolbar() {
  cleanupToolbarExtras();
  if (!toolbarExtraEl) {
    return;
  }

  let allTags = state.tags?.items || [];
  let selectedTagIDs = normalizeTagIDs(state.libraryTagFilterIDs || []);
  let searchQuery = "";

  toolbarExtraEl.innerHTML = `
    <div class="manga-tag-toolbar manga-tag-toolbar--library">
      <div class="manga-tag-strip" data-library-tag-strip aria-label="当前标签筛选"></div>
      <div class="manga-tag-picker" data-library-tag-picker>
        <button class="ghost-button ghost-button--small" type="button" data-library-tag-toggle>
          筛选标签
        </button>
        <div class="manga-tag-picker__panel" data-library-tag-panel hidden>
          <label class="manga-tag-picker__search">
            <span>搜索标签</span>
            <input type="search" placeholder="按名称筛选..." data-library-tag-search />
          </label>
          <div class="manga-tag-picker__actions">
            <button class="ghost-button ghost-button--small" type="button" data-library-tag-clear>
              清空筛选
            </button>
            <button class="ghost-button ghost-button--small" type="button" data-route="#/manage/tags">
              管理标签
            </button>
          </div>
          <div class="manga-tag-picker__body" data-library-tag-body></div>
        </div>
      </div>
    </div>
  `;

  const stripEl = toolbarExtraEl.querySelector("[data-library-tag-strip]");
  const pickerRoot = toolbarExtraEl.querySelector("[data-library-tag-picker]");
  const pickerToggle = toolbarExtraEl.querySelector("[data-library-tag-toggle]");
  const pickerPanel = toolbarExtraEl.querySelector("[data-library-tag-panel]");
  const pickerSearch = toolbarExtraEl.querySelector("[data-library-tag-search]");
  const pickerBody = toolbarExtraEl.querySelector("[data-library-tag-body]");
  const clearButton = toolbarExtraEl.querySelector("[data-library-tag-clear]");
  const manageButton = toolbarExtraEl.querySelector('[data-route="#/manage/tags"]');

  const renderPicker = () => {
    pickerBody.innerHTML = buildTagPickerGroups(allTags, new Set(selectedTagIDs), searchQuery);
    if (clearButton) {
      clearButton.disabled = !selectedTagIDs.length;
    }
  };

  const renderTagStrip = () => {
    const selectedTags = getOrderedTags(
      selectedTagIDs.map((tagID) => allTags.find((tag) => tag.id === tagID)).filter(Boolean),
    );
    if (!selectedTags.length) {
      stripEl.innerHTML = "";
      stripEl.appendChild(createTagChipNode(null, { label: "全部标签", empty: true }));
      return;
    }
    renderToolbarTagStrip(stripEl, selectedTags);
  };

  const setPickerOpen = (open) => {
    pickerPanel.hidden = !open;
    pickerRoot.classList.toggle("is-open", open);
    pickerToggle.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) {
      renderPicker();
      window.requestAnimationFrame(() => pickerSearch?.focus());
      return;
    }
    searchQuery = "";
    if (pickerSearch) {
      pickerSearch.value = "";
    }
  };

  const applyTagFilters = async (nextTagIDs) => {
    selectedTagIDs = normalizeTagIDs(nextTagIDs);
    state.libraryTagFilterIDs = selectedTagIDs;
    persistLibraryTagFilters();
    state.library = null;
    await ensureLibrary(false, state.currentBookshelfId || "", selectedTagIDs);
    updateHero();
    renderLibraryView();
  };

  const onToggleClick = (event) => {
    event.stopPropagation();
    setPickerOpen(pickerPanel.hidden);
  };

  const onDocumentPointerDown = (event) => {
    if (pickerPanel.hidden || pickerRoot.contains(event.target)) {
      return;
    }
    setPickerOpen(false);
  };

  const onSearchInput = (event) => {
    searchQuery = event.target.value || "";
    renderPicker();
  };

  const onPickerChange = async (event) => {
    if (event.target?.type !== "checkbox") {
      return;
    }
    const nextSelected = new Set(selectedTagIDs);
    if (event.target.checked) {
      nextSelected.add(event.target.value);
    } else {
      nextSelected.delete(event.target.value);
    }
    await applyTagFilters([...nextSelected]);
  };

  const onClearClick = async () => {
    await applyTagFilters([]);
  };

  pickerToggle.addEventListener("click", onToggleClick);
  pickerSearch?.addEventListener("input", onSearchInput);
  pickerBody.addEventListener("change", onPickerChange);
  clearButton?.addEventListener("click", onClearClick);
  manageButton?.addEventListener("click", () => setRoute("#/tags"));
  document.addEventListener("pointerdown", onDocumentPointerDown);

  let resizeObserver = null;
  if (window.ResizeObserver && stripEl) {
    resizeObserver = new window.ResizeObserver(() => renderTagStrip());
    resizeObserver.observe(stripEl);
  } else {
    window.addEventListener("resize", renderTagStrip);
  }

  renderTagStrip();

  state.toolbarCleanup = () => {
    pickerToggle.removeEventListener("click", onToggleClick);
    pickerSearch?.removeEventListener("input", onSearchInput);
    pickerBody.removeEventListener("change", onPickerChange);
    clearButton?.removeEventListener("click", onClearClick);
    document.removeEventListener("pointerdown", onDocumentPointerDown);
    if (resizeObserver) {
      resizeObserver.disconnect();
    } else {
      window.removeEventListener("resize", renderTagStrip);
    }
  };
}

function renderTagsView() {
  disconnectReaderObserver();
  cleanupToolbarExtras();
  eyebrowEl.textContent = "Manage";
  refreshButton.hidden = false;
  refreshButton.textContent = "书架";
  refreshButton.dataset.mode = "home";
  viewTitleEl.textContent = "标签管理";

  const tags = getOrderedTags(state.tags?.items || []);
  const onlineSources = state.online.sources?.items || [];
  const editingTag = tags.find((item) => item.id === state.tagEditorId) || null;
  const formTitle = editingTag ? "编辑标签" : "新建标签";
  showFeedback(`当前共有 ${tags.length} 个标签，可以新增、编辑、排序和删除。`);

  appViewEl.innerHTML = `
    <section class="management-shell">
      <nav class="management-tabs" aria-label="管理分区">
        <a class="management-tab is-active" href="#/manage/tags">标签管理</a>
        <a class="management-tab" href="#/manage/types">类型设置</a>
        <a class="management-tab" href="#/manage/online">在线过滤</a>
      </nav>

      <section class="management-section" id="manage-tags">
        <div class="management-section__header">
          <div>
            <p class="panel__eyebrow">Tags</p>
            <h3>标签管理</h3>
          </div>
          <span>${tags.length} 个</span>
        </div>

        <section class="tag-admin-layout">
      <section class="tag-editor-card">
        <div class="bookshelf-section__header">
          <div>
            <p class="panel__eyebrow">Tag Editor</p>
            <h3>${formTitle}</h3>
          </div>
          ${editingTag ? '<span class="bookshelf-section__meta">正在编辑</span>' : ""}
        </div>
        <form class="tag-editor-form" data-tag-form>
          <input type="hidden" name="tagId" value="${escapeHTML(editingTag?.id || "")}" />
          <label>
            <span>标签名</span>
            <input type="text" name="name" value="${escapeHTML(editingTag?.name || "")}" placeholder="例如：连载中" required />
          </label>
          <label>
            <span>分组</span>
            <input type="text" name="group" value="${escapeHTML(editingTag?.group || "")}" placeholder="例如：状态 / 类型" />
          </label>
          <div class="tag-editor-form__row">
            <label>
              <span>颜色</span>
              <input type="color" name="color" value="${escapeHTML(editingTag?.color || "#c77757")}" />
            </label>
            <label>
              <span>优先级</span>
              <input type="number" name="priority" value="${Number(editingTag?.priority || 0)}" step="1" />
            </label>
          </div>
          <label class="tag-editor-form__checkbox">
            <input type="checkbox" name="pinned" ${editingTag?.pinned ? "checked" : ""} />
            <span>置顶显示</span>
          </label>
          <div class="detail-card__actions tag-editor-form__actions">
            <button class="ghost-button" type="submit">${editingTag ? "保存修改" : "新建标签"}</button>
            ${editingTag ? '<button class="ghost-button ghost-button--small" type="button" data-tag-cancel>取消</button>' : ""}
          </div>
        </form>
      </section>

      <section class="bookshelf-section tag-list-panel">
        <div class="bookshelf-section__header">
          <div>
            <p class="panel__eyebrow">Tag List</p>
            <h3>标签列表</h3>
          </div>
          <span class="bookshelf-section__meta">${tags.length} 个</span>
        </div>
        <div class="tag-admin-list">
          ${
            tags.length
              ? tags
                  .map(
                    (tag, index) => `
                      <article class="tag-admin-item" data-tag-id="${escapeHTML(tag.id)}">
                        <div class="tag-admin-item__meta">
                          <span class="tag-admin-item__swatch" style="--tag-accent: ${escapeHTML(tag.color || "#c77757")}"></span>
                          <div>
                            <strong>${escapeHTML(tag.name)}</strong>
                            <p>${escapeHTML(tag.group || "未分组")} · ${Number(tag.count || 0)} 部本地漫画 · 优先级 ${tag.priority}${tag.pinned ? " · 置顶" : ""}</p>
                          </div>
                        </div>
                        <div class="tag-admin-item__actions">
                          <button class="ghost-button ghost-button--small" type="button" data-tag-move="up" ${index === 0 ? "disabled" : ""}>上移</button>
                          <button class="ghost-button ghost-button--small" type="button" data-tag-move="down" ${index === tags.length - 1 ? "disabled" : ""}>下移</button>
                          <button class="ghost-button ghost-button--small" type="button" data-tag-scan>扫描</button>
                          <button class="ghost-button ghost-button--small" type="button" data-tag-edit>编辑</button>
                          <button class="ghost-button ghost-button--small" type="button" data-tag-delete>删除</button>
                        </div>
                      </article>
                    `,
                  )
                  .join("")
              : '<article class="empty-card"><strong>还没有标签。</strong><p>先创建第一个标签，后面就可以在漫画详情页里直接勾选了。</p></article>'
          }
        </div>
      </section>
        </section>
      </section>

      <section class="management-section" id="manage-types">
        <div class="management-section__header">
          <div>
            <p class="panel__eyebrow">Types</p>
            <h3>类型设置</h3>
          </div>
          <span>预留</span>
        </div>
        <article class="management-placeholder">
          <strong>这里会承载后续类型相关操作。</strong>
          <p>例如作品类型、阅读状态、内容分组或自动分类规则，都可以继续放进这个管理页里。</p>
        </article>
      </section>

      <section class="management-section" id="manage-online">
        <div class="management-section__header">
          <div>
            <p class="panel__eyebrow">Online Filters</p>
            <h3>在线漫画黑名单</h3>
          </div>
          <span>${onlineSources.length} 个来源</span>
        </div>

        <div class="online-blacklist-list">
          ${onlineSources.length
            ? onlineSources.map((source) => {
                const settings = getOnlineSourceSettings(source.id);
                const tags = settings.blacklistedTags || [];
                return `
                  <article class="online-blacklist-card">
                    <div class="bookshelf-section__header">
                      <div>
                        <p class="panel__eyebrow">${escapeHTML(source.id)}</p>
                        <h3>${escapeHTML(source.name || source.id)}</h3>
                      </div>
                      <span class="bookshelf-section__meta">${tags.length} 个黑名单 tag</span>
                    </div>
                    <form class="tag-editor-form" data-online-blacklist-form data-online-source-id="${escapeHTML(source.id)}">
                      <label>
                        <span>黑名单 TAG</span>
                        <textarea name="blacklistedTags" rows="6" placeholder="每行一个 tag">${escapeHTML(formatOnlineBlacklistTags(tags))}</textarea>
                      </label>
                      <div class="detail-card__actions tag-editor-form__actions">
                        <button class="ghost-button" type="submit">保存黑名单</button>
                        <button class="ghost-button ghost-button--small" type="button" data-online-blacklist-clear>清空</button>
                      </div>
                    </form>
                  </article>
                `;
              }).join("")
            : '<article class="empty-card"><strong>还没有可用的在线来源。</strong><p>启用在线来源后，这里会显示对应的过滤设置。</p></article>'}
        </div>
      </section>
    </section>
  `;

  bindTagManagerActions();
}

function bindTagManagerActions() {
  bindViewActions();

  const form = appViewEl.querySelector("[data-tag-form]");
  const cancelButton = appViewEl.querySelector("[data-tag-cancel]");
  const listRoot = appViewEl.querySelector(".tag-admin-list");

  const submitTagForm = async (event) => {
    event.preventDefault();
    const formData = new FormData(form);
    const payload = {
      name: String(formData.get("name") || "").trim(),
      group: String(formData.get("group") || "").trim(),
      color: String(formData.get("color") || "").trim(),
      priority: Number(formData.get("priority") || 0),
      pinned: formData.get("pinned") === "on",
    };
    const tagID = String(formData.get("tagId") || "").trim();
    const method = tagID ? "PUT" : "POST";
    const url = tagID ? `/api/tags/${encodeURIComponent(tagID)}` : "/api/tags";

    try {
      const payloadResult = await fetchJSON(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      state.tags = payloadResult;
      syncActiveTagFilters();
      state.tagEditorId = "";
      state.libraryByBookshelfId.clear();
      showFeedback(tagID ? "标签已更新。" : "标签已新建。");
      renderTagsView();
    } catch (error) {
      showFeedback(`标签保存失败: ${error.message}`, "error");
    }
  };

  const reorderTags = async (orderedIDs) => {
    try {
      const payload = await fetchJSON("/api/tags/reorder", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ orderedIds: orderedIDs }),
      });
      state.tags = payload;
      syncActiveTagFilters();
      state.libraryByBookshelfId.clear();
      showFeedback("标签顺序已更新。");
      renderTagsView();
    } catch (error) {
      showFeedback(`标签排序失败: ${error.message}`, "error");
    }
  };

  form?.addEventListener("submit", submitTagForm);
  cancelButton?.addEventListener("click", () => {
    state.tagEditorId = "";
    renderTagsView();
  });

  listRoot?.addEventListener("click", async (event) => {
    const card = event.target.closest("[data-tag-id]");
    if (!card) {
      return;
    }
    const tagID = card.dataset.tagId;
    const orderedTags = getOrderedTags(state.tags?.items || []);
    const currentIndex = orderedTags.findIndex((item) => item.id === tagID);
    if (currentIndex < 0) {
      return;
    }

    if (event.target.closest("[data-tag-edit]")) {
      state.tagEditorId = tagID;
      renderTagsView();
      return;
    }

    if (event.target.closest("[data-tag-delete]")) {
      if (!window.confirm("确定删除这个标签吗？")) {
        return;
      }
      try {
        const payload = await fetchJSON(`/api/tags/${encodeURIComponent(tagID)}`, {
          method: "DELETE",
        });
        state.tags = payload;
        syncActiveTagFilters();
        state.tagEditorId = state.tagEditorId === tagID ? "" : state.tagEditorId;
        state.libraryByBookshelfId.clear();
        showFeedback("标签已删除。");
        renderTagsView();
      } catch (error) {
        showFeedback(`标签删除失败: ${error.message}`, "error");
      }
      return;
    }

    const scanButton = event.target.closest("[data-tag-scan]");
    if (scanButton) {
      scanButton.disabled = true;
      showFeedback("正在扫描该标签下的漫画...");
      try {
        const payload = await fetchJSON(`/api/tasks/scan/tag/${encodeURIComponent(tagID)}`, {
          method: "POST",
        });
        invalidateLibraryCaches();
        state.tags = null;
        await Promise.all([
          ensureTags(true),
          ensureBookshelves(true),
          ensureLibrary(true, state.currentBookshelfId || "", state.libraryTagFilterIDs || []),
        ]);
        const summary = payload?.summary || {};
        showFeedback(`标签扫描完成：${Number(summary.mangaCount || 0)} 部，${Number(summary.chapterCount || 0)} 话。`);
        renderTagsView();
      } catch (error) {
        scanButton.disabled = false;
        showFeedback(`标签扫描失败: ${error.message}`, "error");
      }
      return;
    }

    const move = event.target.closest("[data-tag-move]")?.dataset.tagMove;
    if (!move) {
      return;
    }

    const nextIndex = move === "up" ? currentIndex - 1 : currentIndex + 1;
    if (nextIndex < 0 || nextIndex >= orderedTags.length) {
      return;
    }

    const reordered = [...orderedTags];
    const [moved] = reordered.splice(currentIndex, 1);
    reordered.splice(nextIndex, 0, moved);
    await reorderTags(reordered.map((item) => item.id));
  });

  appViewEl.querySelectorAll("[data-online-blacklist-form]").forEach((formNode) => {
    formNode.addEventListener("submit", async (event) => {
      event.preventDefault();
      const sourceId = formNode.dataset.onlineSourceId || "";
      const textarea = formNode.querySelector('textarea[name="blacklistedTags"]');
      const submitButton = formNode.querySelector('button[type="submit"]');
      const blacklistedTags = parseOnlineBlacklistInput(textarea?.value || "");
      submitButton.disabled = true;
      try {
        await saveOnlineSourceSettings(sourceId, blacklistedTags);
        showFeedback("在线黑名单已保存，新的搜索和最新列表会自动过滤。");
        renderTagsView();
      } catch (error) {
        submitButton.disabled = false;
        showFeedback(`在线黑名单保存失败: ${error.message}`, "error");
      }
    });
  });

  appViewEl.querySelectorAll("[data-online-blacklist-clear]").forEach((node) => {
    node.addEventListener("click", () => {
      const formNode = node.closest("[data-online-blacklist-form]");
      const textarea = formNode?.querySelector('textarea[name="blacklistedTags"]');
      if (textarea) {
        textarea.value = "";
        textarea.focus();
      }
    });
  });
}

function setupMangaToolbar(manga) {
  cleanupToolbarExtras();
  if (!toolbarExtraEl) {
    return;
  }

  if (manga?.sourceId) {
    toolbarExtraEl.innerHTML = `
      <div class="manga-tag-toolbar manga-tag-toolbar--online-detail">
        <button class="ghost-button ghost-button--small" type="button" data-online-toolbar-back>
          返回浏览页
        </button>
        <button class="ghost-button ghost-button--small" type="button" data-online-toolbar-downloads>
          下载任务
        </button>
      </div>
    `;
    const backButton = toolbarExtraEl.querySelector("[data-online-toolbar-back]");
    const downloadsButton = toolbarExtraEl.querySelector("[data-online-toolbar-downloads]");
    const onBackClick = () => setRoute(getOnlineReturnRoute(manga.sourceId));
    const onDownloadsClick = () => setRoute("#/downloads");
    backButton?.addEventListener("click", onBackClick);
    downloadsButton?.addEventListener("click", onDownloadsClick);
    state.toolbarCleanup = () => {
      backButton?.removeEventListener("click", onBackClick);
      downloadsButton?.removeEventListener("click", onDownloadsClick);
    };
    return;
  }

  const allTags = state.tags?.items || [];
  let currentTags = getOrderedTags(manga.tags || []);
  let searchQuery = "";

  toolbarExtraEl.innerHTML = `
    <div class="manga-tag-toolbar">
      <div class="manga-tag-strip" data-manga-tag-strip aria-label="当前漫画标签"></div>
      <div class="manga-tag-picker" data-manga-tag-picker>
        <button class="ghost-button ghost-button--small" type="button" data-tag-picker-toggle>
          标签
        </button>
        <div class="manga-tag-picker__panel" data-tag-picker-panel hidden>
          <label class="manga-tag-picker__search">
            <span>搜索标签</span>
            <input type="search" placeholder="按名称筛选..." data-tag-picker-search />
          </label>
          <div class="manga-tag-picker__body" data-tag-picker-body></div>
        </div>
      </div>
    </div>
  `;

  const stripEl = toolbarExtraEl.querySelector("[data-manga-tag-strip]");
  const pickerRoot = toolbarExtraEl.querySelector("[data-manga-tag-picker]");
  const pickerToggle = toolbarExtraEl.querySelector("[data-tag-picker-toggle]");
  const pickerPanel = toolbarExtraEl.querySelector("[data-tag-picker-panel]");
  const pickerSearch = toolbarExtraEl.querySelector("[data-tag-picker-search]");
  const pickerBody = toolbarExtraEl.querySelector("[data-tag-picker-body]");

  const renderPicker = () => {
    const selectedTagIDs = new Set(currentTags.map((tag) => tag.id));
    pickerBody.innerHTML = buildTagPickerGroups(allTags, selectedTagIDs, searchQuery);
  };

  const renderTagStrip = () => {
    renderToolbarTagStrip(stripEl, currentTags);
  };

  const setPickerOpen = (open) => {
    pickerPanel.hidden = !open;
    pickerRoot.classList.toggle("is-open", open);
    pickerToggle.setAttribute("aria-expanded", open ? "true" : "false");
    if (open) {
      renderPicker();
      window.requestAnimationFrame(() => {
        pickerSearch?.focus();
      });
    } else {
      searchQuery = "";
      if (pickerSearch) {
        pickerSearch.value = "";
      }
    }
  };

  const persistTags = async (nextTagIDs) => {
    pickerRoot.classList.add("is-saving");
    try {
      const payload = await fetchJSON(`/api/manga/${encodeURIComponent(manga.id)}/tags`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tagIds: nextTagIDs }),
      });
      currentTags = getOrderedTags(payload.items || []);
      manga.tags = currentTags;
      const cached = state.mangaById.get(manga.id);
      if (cached) {
        cached.tags = currentTags;
      }
      state.libraryByBookshelfId.clear();
      const refreshedTags = await ensureTags(true);
      allTags = refreshedTags?.items || [];
      renderTagStrip();
      renderPicker();
      showFeedback(`当前漫画共 ${manga.chapterCount} 话，标签已更新。`);
    } catch (error) {
      showFeedback(`标签更新失败: ${error.message}`, "error");
      renderPicker();
    } finally {
      pickerRoot.classList.remove("is-saving");
    }
  };

  const onToggleClick = (event) => {
    event.stopPropagation();
    setPickerOpen(pickerPanel.hidden);
  };

  const onDocumentPointerDown = (event) => {
    if (pickerPanel.hidden) {
      return;
    }
    if (pickerRoot.contains(event.target)) {
      return;
    }
    setPickerOpen(false);
  };

  const onSearchInput = (event) => {
    searchQuery = event.target.value || "";
    renderPicker();
  };

  const onPickerChange = async (event) => {
    if (event.target?.type !== "checkbox") {
      return;
    }
    const selectedTagIDs = new Set(currentTags.map((tag) => tag.id));
    if (event.target.checked) {
      selectedTagIDs.add(event.target.value);
    } else {
      selectedTagIDs.delete(event.target.value);
    }
    await persistTags([...selectedTagIDs]);
  };

  pickerToggle.addEventListener("click", onToggleClick);
  pickerSearch?.addEventListener("input", onSearchInput);
  pickerBody.addEventListener("change", onPickerChange);
  document.addEventListener("pointerdown", onDocumentPointerDown);

  let resizeObserver = null;
  if (window.ResizeObserver && stripEl) {
    resizeObserver = new window.ResizeObserver(() => {
      renderTagStrip();
    });
    resizeObserver.observe(stripEl);
  } else {
    window.addEventListener("resize", renderTagStrip);
  }

  renderTagStrip();

  state.toolbarCleanup = () => {
    pickerToggle.removeEventListener("click", onToggleClick);
    pickerSearch?.removeEventListener("input", onSearchInput);
    pickerBody.removeEventListener("change", onPickerChange);
    document.removeEventListener("pointerdown", onDocumentPointerDown);
    if (resizeObserver) {
      resizeObserver.disconnect();
    } else {
      window.removeEventListener("resize", renderTagStrip);
    }
  };
}

function bindMangaDetailActions(manga, chapters) {
  bindViewActions();

  appViewEl.querySelector("[data-online-back]")?.addEventListener("click", () => {
    setRoute(getOnlineReturnRoute(manga.sourceId));
  });

  const toggle = appViewEl.querySelector("[data-chapter-order-toggle]");
  if (toggle) {
    toggle.addEventListener("click", () => {
      const current = state.chapterOrderByMangaId.get(manga.id) || "asc";
      state.chapterOrderByMangaId.set(manga.id, current === "asc" ? "desc" : "asc");
      renderMangaView(manga, chapters);
    });
  }

  if (manga?.sourceId) {
    const ordered = getOrderedChapters(manga.id, chapters);
    const chapterButtons = [...appViewEl.querySelectorAll(".chapter-list > .chapter-row")];
    chapterButtons.forEach((node, index) => {
      if (node.closest(".chapter-row--online")) {
        return;
      }
      const chapter = ordered.items[index];
      if (!chapter) {
        return;
      }

      const wrapper = document.createElement("article");
      wrapper.className = "chapter-row chapter-row--online";

      const link = node.cloneNode(true);
      link.classList.add("chapter-row__link");
      link.addEventListener("click", () => {
        setRoute(link.dataset.route);
      });
      wrapper.append(link);

      const downloadButton = document.createElement("button");
      downloadButton.className = "ghost-button ghost-button--small chapter-row__download";
      downloadButton.type = "button";
      downloadButton.dataset.onlineDownloadChapter = chapter.id;
      const chapterRequestKey = getDownloadRequestKey(manga.sourceId, manga.id, [chapter.id], "chapter");
      const isCreatingChapterDownload = state.online.pendingCreateKeys.has(chapterRequestKey);
      downloadButton.disabled = isCreatingChapterDownload;
      downloadButton.textContent = isCreatingChapterDownload ? "创建中..." : "下载";
      wrapper.append(downloadButton);

      node.replaceWith(wrapper);
    });

    appViewEl.querySelectorAll("[data-online-bookmark-toggle]").forEach((node) => {
      node.addEventListener("click", async () => {
        const kind = node.dataset.onlineBookmarkToggle || "favorite";
        const isFavorite = kind === "favorite";
        const nextValue = node.getAttribute("aria-pressed") !== "true";
        node.disabled = true;
        node.textContent = nextValue ? "保存中..." : "取消中...";
        try {
          const patch = isFavorite ? { favorite: nextValue } : { following: nextValue };
          const nextState = await updateOnlineBookmark(manga.sourceId, manga.id, patch);
          manga.favorite = nextState.favorite;
          manga.following = nextState.following;
          manga.hasUpdate = nextState.hasUpdate;
          showFeedback(isFavorite
            ? (nextValue ? "已收藏这部漫画。" : "已取消收藏。")
            : (nextValue ? "已加入追漫，后续检测到新章节会提示更新。" : "已取消追漫。"));
          renderMangaView(manga, chapters);
        } catch (error) {
          showFeedback(`保存失败: ${error.message}`, "error");
          renderMangaView(manga, chapters);
        }
      });
    });

    appViewEl.querySelector("[data-online-download-all]")?.addEventListener("click", async () => {
      const button = appViewEl.querySelector("[data-online-download-all]");
      if (button) {
        button.disabled = true;
        button.textContent = "正在创建...";
      }
      try {
        const result = await createOnlineDownloadJob(manga.sourceId, manga.id, [], "manga");
        showFeedback(describeDownloadCreateResult(result));
        await renderCurrentRoute();
      } catch (error) {
        if (button) {
          button.disabled = false;
          button.textContent = "下载整本";
        }
        showFeedback(`创建下载任务失败: ${error.message}`, "error");
      }
    });

    appViewEl.querySelectorAll("[data-online-download-chapter]").forEach((node) => {
      node.addEventListener("click", async (event) => {
        event.stopPropagation();
        node.disabled = true;
        node.textContent = "创建中...";
        try {
          const result = await createOnlineDownloadJob(
            manga.sourceId,
            manga.id,
            [node.dataset.onlineDownloadChapter],
            "chapter",
          );
          showFeedback(describeDownloadCreateResult(result));
          await renderCurrentRoute();
        } catch (error) {
          node.disabled = false;
          node.textContent = "下载";
          showFeedback(`创建下载任务失败: ${error.message}`, "error");
        }
      });
    });
  }

  setupMangaToolbar(manga);
}

function disconnectReaderObserver() {
  if (state.reader.observer) {
    state.reader.observer.disconnect();
    state.reader.observer = null;
  }
  if (state.reader.toolbarCleanup) {
    state.reader.toolbarCleanup();
    state.reader.toolbarCleanup = null;
  }
}

function renderChapterLoadingView(route) {
  disconnectReaderObserver();
  cleanupToolbarExtras();
  eyebrowEl.textContent = route.name === "onlineChapter" ? "ONLINE READER" : "READER";
  refreshButton.hidden = true;
  refreshButton.textContent = "书架";
  refreshButton.dataset.mode = "home";
  viewTitleEl.textContent = "正在加载章节...";
  feedbackEl.textContent = "";
  appViewEl.innerHTML = `
    <section class="reader-shell reader-shell--loading" data-reader-shell>
      <article class="empty-card reader-loading-card">
        <strong>正在加载章节内容...</strong>
        <p>图片和章节信息正在准备，请稍候。</p>
      </article>
    </section>
  `;
}

function getCurrentBookshelfMeta() {
  const shelves = state.bookshelves?.items ?? [];
  if (!state.currentBookshelfId) {
    const pageCount = shelves.reduce((sum, shelf) => sum + (shelf.pageCount || 0), 0);
    const updatedAt = shelves
      .map((shelf) => shelf.updatedAt)
      .filter(Boolean)
      .sort()
      .at(-1) || "";
    return {
      id: "",
      name: "全部书架",
      mangaCount: state.library?.total ?? 0,
      pageCount,
      updatedAt,
    };
  }
  return shelves.find((item) => item.id === state.currentBookshelfId) || null;
}

function renderBookshelfTabs() {
  const shelves = state.bookshelves?.items ?? [];
  const allCount = shelves.reduce((sum, shelf) => sum + (shelf.mangaCount || 0), 0);
  return `
    <section class="bookshelf-strip">
      <button
        class="bookshelf-pill ${state.currentBookshelfId ? "" : "is-active"}"
        type="button"
        data-bookshelf-id=""
      >
        <span>全部</span>
        <strong>${allCount}</strong>
      </button>
      ${shelves
        .map(
          (shelf) => `
            <button
              class="bookshelf-pill ${state.currentBookshelfId === shelf.id ? "is-active" : ""}"
              type="button"
              data-bookshelf-id="${escapeHTML(shelf.id)}"
            >
              <span>${escapeHTML(shelf.name)}</span>
              <strong>${shelf.mangaCount}</strong>
            </button>
          `,
        )
        .join("")}
    </section>
  `;
}

function renderRecentUpdates(items) {
  const recent = [...items]
    .sort((left, right) => String(right.updatedAt).localeCompare(String(left.updatedAt)))
    .slice(0, 6);

  if (!recent.length) {
    return "";
  }

  return `
    <section class="bookshelf-section">
      <div class="bookshelf-section__header">
        <div>
          <p class="panel__eyebrow">Recent Updates</p>
          <h3>最近更新</h3>
        </div>
        <span class="bookshelf-section__meta">${recent.length} 部</span>
      </div>
      <div class="recent-grid">
        ${recent
          .map(
            (item) => `
              <article class="recent-card" data-route="#/manga/${encodeURIComponent(item.id)}">
                <img
                  class="recent-card__cover"
                  src="${escapeHTML(item.coverThumbUrl)}"
                  alt="${escapeHTML(item.title)}"
                  loading="lazy"
                />
                <div class="recent-card__body">
                  <strong>${escapeHTML(item.title)}</strong>
                  <span>${formatUpdatedAt(item.updatedAt)}</span>
                </div>
              </article>
            `,
          )
          .join("")}
      </div>
    </section>
  `;
}

function renderLibraryView() {
  disconnectReaderObserver();
  setupLibraryToolbar();
  eyebrowEl.textContent = "Library";
  refreshButton.hidden = false;
  refreshButton.textContent = "刷新";
  refreshButton.dataset.mode = "refresh";

  const currentShelf = getCurrentBookshelfMeta();
  const currentShelfRoot = String(currentShelf?.rootPath || "").replaceAll("\\", "/");
  const isOnlineDownloadShelf = currentShelfRoot.includes("data/downloads");
  const items = state.library?.items ?? [];
  const sortedItems = getSortedLibraryItems(items);
  const filteredItems = getFilteredLibraryItems(sortedItems);
  const hasQuery = Boolean(normalizeSearchText(state.librarySearchQuery));
  const activeTags = getSelectedLibraryTags();
  const hasTagFilters = activeTags.length > 0;

  viewTitleEl.textContent = currentShelf?.name || "漫画书库";

  if (hasQuery || hasTagFilters) {
    showFeedback(
      filteredItems.length
        ? `当前书架共有 ${items.length} 部漫画，筛选后匹配 ${filteredItems.length} 部。`
        : "当前筛选条件下没有找到漫画。",
    );
  } else if (items.length) {
    showFeedback(`当前书架已收录 ${items.length} 部漫画。`);
  } else {
    showFeedback("当前书架还没有扫描到漫画，填好路径后可以点击重新扫描。");
  }

  if (!items.length && !hasQuery && !hasTagFilters) {
    appViewEl.innerHTML = `
      ${renderBookshelfTabs()}
      <section class="bookshelf-hero">
        <div>
          <p class="panel__eyebrow">Bookshelf</p>
          <h3>${escapeHTML(currentShelf?.name || "全部书架")}</h3>
          <p>把不同来源的漫画目录配置成多个书架后，这里会按书架分别展示。</p>
        </div>
        <div class="bookshelf-hero__stats">
          <div class="bookshelf-stat">
            <strong>${currentShelf?.mangaCount ?? 0}</strong>
            <span>漫画</span>
          </div>
          <div class="bookshelf-stat">
            <strong>${currentShelf?.pageCount ?? 0}</strong>
            <span>总页数</span>
          </div>
        </div>
      </section>
      <article class="empty-card">
        <strong>这个书架还是空的。</strong>
        <p>先在配置文件里填好书架目录，再点击“重新扫描”。</p>
      </article>
    `;
    bindLibraryActions();
    return;
  }

  appViewEl.innerHTML = `
    ${renderBookshelfTabs()}
    <section class="bookshelf-hero">
      <div>
        <p class="panel__eyebrow">Bookshelf</p>
        <h3>${escapeHTML(currentShelf?.name || "全部书架")}</h3>
        <p>把本地目录当作内容频道来浏览。书架切换、搜索和标签筛选都会跟随当前书架上下文变化。</p>
      </div>
      <div class="bookshelf-hero__stats">
        <div class="bookshelf-stat">
          <strong>${hasQuery || hasTagFilters ? filteredItems.length : items.length}</strong>
          <span>${hasQuery || hasTagFilters ? "匹配结果" : "漫画"}</span>
        </div>
        <div class="bookshelf-stat">
          <strong>${currentShelf?.pageCount ?? 0}</strong>
          <span>总页数</span>
        </div>
        <div class="bookshelf-stat">
          <strong>${formatUpdatedAt(currentShelf?.updatedAt || "").replace("更新于", "").trim()}</strong>
          <span>最近更新</span>
        </div>
      </div>
    </section>

    <section class="library-tools">
      ${isOnlineDownloadShelf ? `<button class="ghost-button library-tools__shortcut" type="button" data-route="${routeForOnlineLibrary()}">Online</button>` : ""}
      <label class="library-search library-search--inline">
        <div class="library-search__control">
          <input
            type="search"
            class="library-search__input"
            placeholder="按标题搜索..."
            value="${escapeHTML(state.librarySearchDraft)}"
            data-library-search
          />
          <button class="ghost-button ghost-button--small" type="button" data-library-search-submit>
            搜索
          </button>
          <button
            class="ghost-button ghost-button--small"
            type="button"
            data-library-search-clear
            ${state.librarySearchDraft || hasQuery || hasTagFilters ? "" : "disabled"}
          >
            清空
          </button>
        </div>
      </label>
      <label class="library-sort">
        <span class="library-search__label">排序方式</span>
        <select class="library-sort__select" data-library-sort>
          <option value="updated-desc" ${state.librarySort === "updated-desc" ? "selected" : ""}>最近更新</option>
          <option value="title-asc" ${state.librarySort === "title-asc" ? "selected" : ""}>标题 A-Z</option>
          <option value="chapters-desc" ${state.librarySort === "chapters-desc" ? "selected" : ""}>章节最多</option>
        </select>
      </label>
      <div class="library-summary">
        <strong>${filteredItems.length}</strong>
        <span>${hasQuery || hasTagFilters ? "当前匹配" : "当前全部"}</span>
      </div>
    </section>

    ${
      hasTagFilters
        ? `<section class="library-tag-summary">
            ${activeTags.map((tag) => `<span class="manga-tag">${escapeHTML(tag.name)}</span>`).join("")}
          </section>`
        : ""
    }

    ${!hasQuery && !hasTagFilters ? renderRecentUpdates(items) : ""}

    ${
      filteredItems.length
        ? `<section class="bookshelf-section">
            <div class="bookshelf-section__header">
              <div>
                <p class="panel__eyebrow">Collection</p>
                <h3>${hasQuery || hasTagFilters ? "搜索结果" : "漫画目录"}</h3>
              </div>
              <span class="bookshelf-section__meta">${filteredItems.length} 部</span>
            </div>
            <section class="library-grid">
              ${filteredItems
                .map(
                  (item) => `
                    <article class="manga-tile" data-route="#/manga/${encodeURIComponent(item.id)}">
                      <div class="manga-tile__cover">
                        <img
                          src="${escapeHTML(item.coverThumbUrl)}"
                          alt="${escapeHTML(item.title)}"
                          loading="lazy"
                        />
                      </div>
                      <div class="manga-tile__body">
                        <h3>${escapeHTML(item.title)}</h3>
                        <p>${formatUpdatedAt(item.updatedAt)}</p>
                        <div class="manga-metrics">
                          <span>${item.chapterCount} 话</span>
                          <span>${item.pageCount} 页</span>
                        </div>
                      </div>
                    </article>
                  `,
                )
                .join("")}
            </section>
          </section>`
        : `<article class="empty-card empty-card--search">
            <strong>没有找到匹配的漫画。</strong>
            <p>试试更换关键词，或者清空标签筛选后再看一次。</p>
          </article>`
    }
  `;

  bindLibraryActions();
}

function renderMangaView(manga, chapters) {
  disconnectReaderObserver();
  cleanupToolbarExtras();
  const isOnline = Boolean(manga.sourceId);
  const onlineSourceName = isOnline ? getOnlineSourceName(manga.sourceId) : "";
  const jobs = isOnline ? (state.online.downloads?.items || []).filter((job) => job.sourceId === manga.sourceId && job.mangaId === manga.id) : [];
  const latestJob = jobs[0] || null;
  const isCreatingWholeDownload = isOnline && state.online.pendingCreateKeys.has(
    getDownloadRequestKey(manga.sourceId, manga.id, [], "manga"),
  );
  const latestJobStatusText = latestJob ? getDownloadStatusText(latestJob.status) : "可下载";
  const bookmarkState = isOnline
    ? {
        ...getOnlineBookmarkState(manga.sourceId, manga.id),
        favorite: Boolean(manga.favorite || getOnlineBookmarkState(manga.sourceId, manga.id).favorite),
        following: Boolean(manga.following || getOnlineBookmarkState(manga.sourceId, manga.id).following),
        hasUpdate: Boolean(manga.hasUpdate || getOnlineBookmarkState(manga.sourceId, manga.id).hasUpdate),
      }
    : { favorite: false, following: false, hasUpdate: false };

  eyebrowEl.textContent = isOnline ? "在线漫画" : "Manga";
  refreshButton.hidden = false;
  refreshButton.textContent = "刷新";
  refreshButton.dataset.mode = "refresh";
  if (manga.bookshelfId && state.currentBookshelfId !== manga.bookshelfId) {
    state.currentBookshelfId = manga.bookshelfId;
    persistCurrentBookshelfId();
  }
  viewTitleEl.textContent = manga.title;
  showFeedback(
    isOnline
      ? `已加载 ${onlineSourceName} 的章节目录，共 ${manga.chapterCount} 话，可以直接在线阅读或加入下载。`
      : `当前漫画共 ${manga.chapterCount} 话，选择一话进入阅读页。`,
  );
  const ordered = getOrderedChapters(manga.id, chapters);
  const firstChapter = ordered.items[0];
  const lastChapter = ordered.items[ordered.items.length - 1];
  const firstChapterRoute = firstChapter ? routeForChapter(firstChapter, manga) : "";
  const lastChapterRoute = lastChapter ? routeForChapter(lastChapter, manga) : "";
  const orderLabel = ordered.order === "asc" ? "最早在前" : "最新在前";
  const heroSummary = isOnline
    ? `当前来源：${onlineSourceName}。支持直接阅读、单话下载和整本下载，图片会通过本地服务代理和解码。`
    : "当前已接入本地扫描、封面缩略图、章节浏览和分页阅读。你可以从下面任意一话继续进入阅读。";
  const primaryReadLabel = ordered.order === "asc" ? "从第一话开始" : "从最新一话开始";
  const secondaryReadLabel = ordered.order === "asc" ? "阅读最新一话" : "阅读最早一话";
  const latestJobSummary = latestJob
    ? `${latestJobStatusText} · ${latestJob.donePages}/${latestJob.totalPages || 0} 页`
    : "暂无下载任务";
  const latestJobHint = latestJob?.errorMessage
    ? latestJob.errorMessage
    : latestJob
      ? "下载任务会在后台继续执行，完成后自动进入本地书架。"
      : "可以整本加入下载，也可以在单话右侧直接下载。";
  const latestJobHintText = String(latestJob?.status || "").toLowerCase() === "processing"
    ? "页码已经下载完成，正在整理文件并刷新书架。"
    : latestJobHint;

  appViewEl.innerHTML = `
    <section class="detail-layout">
      <section class="detail-hero">
        <article class="detail-card">
          <div class="detail-card__poster">
            <img
              class="detail-card__cover"
              src="${escapeHTML(manga.coverThumbUrl)}"
              alt="${escapeHTML(manga.title)}"
            />
          </div>
          <div class="detail-card__content">
            <p class="detail-card__eyebrow">${isOnline ? "Online Detail" : "Series Overview"}</p>
            <p class="detail-card__summary">${escapeHTML(heroSummary)}</p>
            <div class="manga-metrics manga-metrics--large">
              <span>${manga.chapterCount} 话</span>
              ${isOnline
                ? `<span>${escapeHTML(onlineSourceName)}</span>`
                : `<span>${manga.pageCount} 页</span>`
              }
              ${isOnline ? "" : `<span>${formatUpdatedAt(manga.updatedAt)}</span>`}
              ${isOnline ? `<span>${latestJob ? escapeHTML(latestJobStatusText) : "可下载"}</span>` : ""}
            </div>
            ${isOnline ? renderOnlineAuthor(manga.author, "detail") : ""}
            ${isOnline ? renderInlineTagList(manga.tags, 6) : ""}
            <div class="detail-card__actions">
              ${isOnline ? `
                <button
                  class="ghost-button"
                  type="button"
                  data-online-back
                >
                  返回浏览页
                </button>
              ` : ""}
              <button
                class="ghost-button"
                type="button"
                data-route="${firstChapterRoute}"
                ${ordered.items.length ? "" : "disabled"}
              >
                ${primaryReadLabel}
              </button>
              <button
                class="ghost-button"
                type="button"
                data-route="${lastChapterRoute}"
                ${ordered.items.length ? "" : "disabled"}
              >
                ${secondaryReadLabel}
              </button>
              ${manga.sourceId ? `
                <button
                  class="ghost-button ${bookmarkState.favorite ? "is-active" : ""}"
                  type="button"
                  data-online-bookmark-toggle="favorite"
                  aria-pressed="${bookmarkState.favorite ? "true" : "false"}"
                >
                  ${bookmarkState.favorite ? "已收藏" : "收藏"}
                </button>
                <button
                  class="ghost-button ${bookmarkState.following ? "is-active" : ""}"
                  type="button"
                  data-online-bookmark-toggle="follow"
                  aria-pressed="${bookmarkState.following ? "true" : "false"}"
                >
                  ${bookmarkState.following ? (bookmarkState.hasUpdate ? "追漫 · 有更新" : "已追漫") : "追漫"}
                </button>
                <button
                  class="ghost-button"
                  type="button"
                  data-online-download-all
                  ${ordered.items.length && !isCreatingWholeDownload ? "" : "disabled"}
                >
                  ${isCreatingWholeDownload ? "正在创建..." : "下载整本"}
                </button>
              ` : ""}
            </div>
          </div>
        </article>

        <aside class="detail-sidebar">
          <div class="detail-sidebar__card">
            <span class="detail-sidebar__label">${isOnline ? "Source" : "Reading Status"}</span>
            <strong>${isOnline ? escapeHTML(onlineSourceName) : "已收录完整章节列表"}</strong>
            <p>${isOnline ? "元数据和图片都会经过后端代理处理，前端不直接访问目标站点。" : "章节已按顺序整理，可以直接连续阅读。"}</p>
          </div>
          <div class="detail-sidebar__card">
            <span class="detail-sidebar__label">${isOnline ? "Download" : "Latest Update"}</span>
            <strong>${isOnline ? `最近任务：${escapeHTML(latestJobSummary)}` : formatUpdatedAt(manga.updatedAt)}</strong>
            <p>${escapeHTML(isOnline ? latestJobHintText : "最近更新时间会随着本地重新扫描而更新。")}</p>
            ${isOnline ? `
              <button class="ghost-button ghost-button--small detail-sidebar__action" type="button" data-route="#/downloads">
                查看下载页
              </button>
            ` : ""}
          </div>
        </aside>
      </section>

      <section class="chapters-panel">
        <div class="chapters-panel__header">
          <div>
            <p class="panel__eyebrow">Chapters</p>
            <h3>章节目录</h3>
          </div>
          <div class="chapters-panel__tools">
            <span class="chapters-panel__count">${ordered.items.length} 话</span>
            <button class="ghost-button ghost-button--small" type="button" data-chapter-order-toggle>
              ${orderLabel}
            </button>
          </div>
        </div>
        <section class="chapter-list">
          ${ordered.items
            .map(
              (chapter, index) => `
                <button
                  class="chapter-row"
                  type="button"
                  data-route="${routeForChapter(chapter, manga)}"
                >
                  <div class="chapter-row__main">
                    <span class="chapter-row__index">${index + 1}</span>
                    <div>
                      <strong>${escapeHTML(chapter.title)}</strong>
                      <p>${escapeHTML(
                        isOnline
                          ? (chapter.updatedAt ? formatUpdatedAt(chapter.updatedAt) : `章节 ID ${chapter.id}`)
                          : formatUpdatedAt(chapter.updatedAt),
                      )}</p>
                    </div>
                  </div>
                  <span class="chapter-row__meta">${chapter.pageCount ? `${chapter.pageCount} 页` : (isOnline ? "在线读取" : "0 页")}</span>
                </button>
              `,
            )
            .join("")}
        </section>
      </section>
    </section>
  `;
  bindMangaDetailActions(manga, chapters);
}

function updateReaderProgress(pageIndex, totalPages) {
  const currentLabel = document.querySelector("[data-reader-current]");
  const compactCurrent = document.querySelector("[data-reader-current-compact]");
  const compactToggle = document.querySelector("[data-reader-toggle]");
  const pageSelect = document.querySelector("[data-page-select]");
  const miniCurrent = document.querySelector("[data-page-inline]");
  const progressFill = document.querySelector("[data-progress-fill]");
  const pagePills = document.querySelectorAll("[data-page-pill]");

  state.reader.activePage = pageIndex;

  if (currentLabel) {
    currentLabel.textContent = `${pageIndex + 1} / ${totalPages}`;
  }
  if (compactCurrent) {
    compactCurrent.textContent = `${pageIndex + 1} / ${totalPages}`;
  }
  if (compactToggle) {
    compactToggle.style.display = "inline-flex";
  }
  if (miniCurrent) {
    miniCurrent.textContent = `第 ${pageIndex + 1} 页`;
  }
  if (pageSelect) {
    pageSelect.value = String(pageIndex);
  }
  if (progressFill) {
    const percent = totalPages <= 1 ? 100 : (pageIndex / (totalPages - 1)) * 100;
    progressFill.style.width = `${percent}%`;
  }
  pagePills.forEach((pill) => {
    pill.classList.toggle("is-active", Number(pill.dataset.pagePill) === pageIndex);
  });
}

function scrollToReaderPage(pageIndex) {
  const target = document.querySelector(`[data-page-anchor="${pageIndex}"]`);
  if (!target) {
    return;
  }
  target.scrollIntoView({ behavior: "smooth", block: "start" });
}

function scrollToReaderStart(behavior = "auto") {
  const target =
    document.querySelector('[data-page-anchor="0"]') ||
    document.querySelector(".reader-pages");
  if (!target) {
    return;
  }
  target.scrollIntoView({ behavior, block: "start" });
}

function preloadImageURL(url) {
  if (!url) {
    return;
  }
  const image = new Image();
  image.decoding = "async";
  image.loading = "eager";
  image.src = url;
}

async function preloadChapterPages(chapterId) {
  const route = getRoute();
  const pages = route.name === "onlineChapter"
    ? await ensureOnlinePages(route.sourceId, chapterId)
    : await ensurePages(chapterId);
  const items = pages.pages || pages.items || [];
  items.slice(0, nextChapterImagePreloadCount).forEach((page) => {
    preloadImageURL(page.imageUrl);
  });
  return pages;
}

function isNearReaderEnd() {
  const doc = document.documentElement;
  const threshold = Math.max(120, window.innerHeight * 0.12);
  return window.scrollY + window.innerHeight >= doc.scrollHeight - threshold;
}

function setupReaderInteractions(chapter, manga, chapters, pages) {
  disconnectReaderObserver();

  eyebrowEl.textContent = "阅读";
  refreshButton.hidden = true;
  refreshButton.textContent = "书架";
  refreshButton.dataset.mode = "home";
  viewTitleEl.textContent = `${chapter.title} / 共 ${chapters.items.length} 章节`;
  showFeedback("");

  const chapterIndex = chapters.items.findIndex((item) => item.id === chapter.id);
  const previous = chapterIndex > 0 ? chapters.items[chapterIndex - 1] : null;
  const next = chapterIndex >= 0 && chapterIndex < chapters.items.length - 1
    ? chapters.items[chapterIndex + 1]
    : null;
  let nextChapterPreload = null;
  let autoAdvanceTimer = 0;
  let autoAdvanceTriggered = false;
  let endPullArmed = false;
  let lastTouchY = null;

  const preloadNextChapter = () => {
    if (!next) {
      return null;
    }
    if (!nextChapterPreload) {
      nextChapterPreload = preloadChapterPages(next.id).catch((error) => {
        console.warn("failed to preload next chapter", error);
        nextChapterPreload = null;
        return null;
      });
    }
    return nextChapterPreload;
  };

  const scheduleAutoAdvance = () => {
    if (!next || autoAdvanceTriggered || autoAdvanceTimer) {
      return;
    }
    autoAdvanceTriggered = true;
    void preloadNextChapter();
    autoAdvanceTimer = window.setTimeout(() => {
      autoAdvanceTimer = 0;
      if (state.reader.chapterId === chapter.id && ["chapter", "onlineChapter"].includes(getRoute().name)) {
        setRoute(routeForChapter(next, manga));
      }
    }, nextChapterAutoAdvanceDelayMs);
  };

  const cancelAutoAdvance = () => {
    if (autoAdvanceTimer) {
      window.clearTimeout(autoAdvanceTimer);
      autoAdvanceTimer = 0;
    }
    autoAdvanceTriggered = false;
  };

  const handleEndPullIntent = () => {
    if (!endPullArmed || !isNearReaderEnd()) {
      return;
    }
    scheduleAutoAdvance();
  };

  const previousButtons = [...document.querySelectorAll("[data-reader-prev]")];
  const nextButtons = [...document.querySelectorAll("[data-reader-next]")];
  const pageSelect = document.querySelector("[data-page-select]");
  const pagePills = document.querySelectorAll("[data-page-pill]");
  const readerShell = document.querySelector("[data-reader-shell]");
  const toolbarToggle = document.querySelector("[data-reader-toggle]");
  const toolbarOverlay = document.querySelector("[data-reader-toolbar-overlay]");
  const toolbarPanel = document.querySelector(".reader-toolbar__panel");
  const overviewPanel = document.querySelector(".reader-overview");
  const stripPanel = document.querySelector(".reader-strip");

  if (toolbarPanel && overviewPanel && stripPanel) {
    let extras = toolbarPanel.querySelector(".reader-toolbar__extras");
    if (!extras) {
      extras = document.createElement("div");
      extras.className = "reader-toolbar__extras";
      toolbarPanel.appendChild(extras);
    }
    overviewPanel.classList.add("reader-overview--overlay");
    stripPanel.classList.add("reader-strip--overlay");
    extras.replaceChildren(overviewPanel, stripPanel);
  }

  previousButtons.forEach((button) => {
    button.disabled = !previous;
    button.addEventListener("click", () => {
      if (previous) {
        setRoute(routeForChapter(previous, manga));
      }
    });
  });

  nextButtons.forEach((button) => {
    button.disabled = !next;
    button.addEventListener("click", () => {
      if (next) {
        setRoute(routeForChapter(next, manga));
      }
    });
  });

  if (pageSelect) {
    pageSelect.addEventListener("change", (event) => {
      scrollToReaderPage(Number(event.target.value));
    });
  }

  pagePills.forEach((pill) => {
    pill.addEventListener("click", () => {
      scrollToReaderPage(Number(pill.dataset.pagePill));
    });
  });

  if (toolbarOverlay && toolbarToggle) {
    let suppressCollapseUntil = 0;
    let expandedScrollY = 0;

    const setToolbarExpanded = (expanded) => {
      readerShell?.classList.toggle("is-toolbar-expanded", expanded);
      readerShell?.classList.toggle("is-toolbar-collapsed", !expanded);
      toolbarOverlay.classList.toggle("is-visible", expanded);
      toolbarOverlay.hidden = !expanded;
      toolbarToggle.setAttribute("aria-expanded", expanded ? "true" : "false");
      if (expanded) {
        suppressCollapseUntil = window.performance.now() + 280;
        expandedScrollY = window.scrollY;
      }
    };

    setToolbarExpanded(false);

    const onToggleClick = (event) => {
      event.stopPropagation();
      setToolbarExpanded(!toolbarOverlay.classList.contains("is-visible"));
    };

    const onScrollCollapse = () => {
      if (toolbarOverlay.hidden) {
        return;
      }
      if (window.performance.now() < suppressCollapseUntil) {
        return;
      }
      if (Math.abs(window.scrollY - expandedScrollY) < 12) {
        return;
      }
      setToolbarExpanded(false);
    };

    const onDocumentPointerDown = (event) => {
      if (toolbarOverlay.contains(event.target) || toolbarToggle.contains(event.target)) {
        return;
      }
      setToolbarExpanded(false);
    };

    toolbarToggle.addEventListener("click", onToggleClick);
    window.addEventListener("scroll", onScrollCollapse, { passive: true });
    document.addEventListener("pointerdown", onDocumentPointerDown);

    state.reader.toolbarCleanup = () => {
      toolbarToggle.removeEventListener("click", onToggleClick);
      window.removeEventListener("scroll", onScrollCollapse);
      document.removeEventListener("pointerdown", onDocumentPointerDown);
    };
  }

  const figures = [...document.querySelectorAll("[data-page-anchor]")];
  state.reader.chapterId = chapter.id;
  state.reader.activePage = 0;

  const syncActivePage = () => {
    if (!figures.length) {
      return;
    }

    const probeY = window.innerHeight * 0.35;
    let bestIndex = 0;
    let bestDistance = Number.POSITIVE_INFINITY;

    figures.forEach((figure, index) => {
      const rect = figure.getBoundingClientRect();
      const distance = Math.abs(rect.top - probeY);
      if (distance < bestDistance) {
        bestDistance = distance;
        bestIndex = index;
      }
    });

    updateReaderProgress(bestIndex, pages.pages.length);
    if (next && bestIndex + 1 >= Math.ceil(pages.pages.length / 2)) {
      void preloadNextChapter();
    }
    if (next && bestIndex === pages.pages.length - 1 && isNearReaderEnd()) {
      endPullArmed = true;
    } else {
      endPullArmed = false;
      cancelAutoAdvance();
    }
  };

  let frameId = 0;
  const requestSync = () => {
    if (frameId) {
      return;
    }
    frameId = window.requestAnimationFrame(() => {
      frameId = 0;
      syncActivePage();
    });
  };

  const onWheelAdvance = (event) => {
    if (event.deltaY > 0) {
      handleEndPullIntent();
    } else if (event.deltaY < 0) {
      cancelAutoAdvance();
    }
  };

  const onTouchStartAdvance = (event) => {
    lastTouchY = event.touches?.[0]?.clientY ?? null;
  };

  const onTouchMoveAdvance = (event) => {
    const currentY = event.touches?.[0]?.clientY ?? null;
    if (currentY == null || lastTouchY == null) {
      lastTouchY = currentY;
      return;
    }

    const deltaY = lastTouchY - currentY;
    lastTouchY = currentY;
    if (deltaY > 8) {
      handleEndPullIntent();
    } else if (deltaY < -8) {
      cancelAutoAdvance();
    }
  };

  const onKeyAdvance = (event) => {
    const forwardKeys = new Set(["ArrowDown", "PageDown", "End", " "]);
    const backwardKeys = new Set(["ArrowUp", "PageUp", "Home"]);
    if (forwardKeys.has(event.key)) {
      handleEndPullIntent();
    } else if (backwardKeys.has(event.key)) {
      cancelAutoAdvance();
    }
  };

  window.addEventListener("scroll", requestSync, { passive: true });
  window.addEventListener("resize", requestSync);
  window.addEventListener("wheel", onWheelAdvance, { passive: true });
  window.addEventListener("touchstart", onTouchStartAdvance, { passive: true });
  window.addEventListener("touchmove", onTouchMoveAdvance, { passive: true });
  window.addEventListener("keydown", onKeyAdvance);

  state.reader.toolbarCleanup = ((previousCleanup) => () => {
    previousCleanup?.();
    if (autoAdvanceTimer) {
      window.clearTimeout(autoAdvanceTimer);
      autoAdvanceTimer = 0;
    }
    if (frameId) {
      window.cancelAnimationFrame(frameId);
      frameId = 0;
    }
    window.removeEventListener("scroll", requestSync);
    window.removeEventListener("resize", requestSync);
    window.removeEventListener("wheel", onWheelAdvance);
    window.removeEventListener("touchstart", onTouchStartAdvance);
    window.removeEventListener("touchmove", onTouchMoveAdvance);
    window.removeEventListener("keydown", onKeyAdvance);
  })(state.reader.toolbarCleanup);

  requestSync();

  const backButton = document.querySelector("[data-reader-back]");
  if (backButton) {
    backButton.addEventListener("click", () => {
      setRoute(routeForManga(manga));
    });
  }

  const homeReaderButton = document.querySelector("[data-reader-home]");
  if (homeReaderButton) {
    homeReaderButton.setAttribute("aria-label", "返回书架");
    homeReaderButton.setAttribute("title", "返回书架");
    homeReaderButton.textContent = "?";
    homeReaderButton.addEventListener("click", () => {
      setRoute("#/");
    });
  }

  if (manga?.sourceId) {
    const toolbarRight = document.querySelector(".reader-toolbar--sticky .reader-toolbar__right");
    if (toolbarRight && !toolbarRight.querySelector("[data-reader-download]")) {
      const downloadButton = document.createElement("button");
      downloadButton.className = "ghost-button";
      downloadButton.type = "button";
      downloadButton.dataset.readerDownload = "true";
      downloadButton.textContent = "下载本话";
      toolbarRight.insertBefore(downloadButton, toolbarRight.firstChild);
      downloadButton.addEventListener("click", async () => {
        downloadButton.disabled = true;
        downloadButton.textContent = "创建中...";
        try {
          const result = await createOnlineDownloadJob(manga.sourceId, manga.id, [chapter.id], "chapter");
          showFeedback(describeDownloadCreateResult(result));
        } catch (error) {
          downloadButton.disabled = false;
          downloadButton.textContent = "下载本话";
          showFeedback(`创建下载任务失败: ${error.message}`, "error");
        }
      });
    }
  }
}

async function deleteOnlineDownloadJob(jobId, removeFiles = false) {
  const action = removeFiles ? "delete-files" : "delete-record";
  const actionKey = `${jobId}::${action}`;
  if (state.online.pendingJobActions.has(actionKey)) {
    return null;
  }

  state.online.pendingJobActions.add(actionKey);
  try {
    const payload = await fetchJSON(
      removeFiles ? `/api/online/downloads/${encodeURIComponent(jobId)}/files` : `/api/online/downloads/${encodeURIComponent(jobId)}`,
      { method: "DELETE" },
    );
    await ensureOnlineDownloads(true);
    invalidateLibraryCaches();
    return payload;
  } finally {
    state.online.pendingJobActions.delete(actionKey);
  }
}

function renderReaderView(chapter, manga, chapters, pages) {
  cleanupToolbarExtras();
  if (manga.bookshelfId && state.currentBookshelfId !== manga.bookshelfId) {
    state.currentBookshelfId = manga.bookshelfId;
    persistCurrentBookshelfId();
  }

  eyebrowEl.textContent = "阅读";
  refreshButton.hidden = true;
  refreshButton.textContent = "书架";
  refreshButton.dataset.mode = "home";
  viewTitleEl.textContent = `${chapter.title} / 共 ${chapters.items.length} 章节`;
  showFeedback("");

  const chapterIndex = chapters.items.findIndex((item) => item.id === chapter.id);
  const previous = chapterIndex > 0 ? chapters.items[chapterIndex - 1] : null;
  const next = chapterIndex >= 0 && chapterIndex < chapters.items.length - 1
    ? chapters.items[chapterIndex + 1]
    : null;

  appViewEl.innerHTML = `
    <section class="reader-shell is-toolbar-collapsed" data-reader-shell>
      <div class="reader-toolbar reader-toolbar--sticky">
        <button class="reader-toolbar__compact" type="button" data-reader-toggle aria-expanded="false">
          <span class="reader-toolbar__compact-label" data-reader-current-compact>1 / ${pages.pages.length}</span>
        </button>
        <div class="reader-toolbar__overlay" data-reader-toolbar-overlay hidden>
          <div class="reader-toolbar__panel">
            <div class="reader-toolbar__left">
              <button
                class="reader-toolbar__back"
                type="button"
                data-reader-back
                aria-label="返回目录"
                title="返回目录"
              >
                ←
              </button>
              <div class="reader-heading">
                <strong>${escapeHTML(manga.title)}</strong>
                <span>${escapeHTML(chapter.title)}</span>
              </div>
              <button
                class="reader-toolbar__icon"
                type="button"
                data-reader-home
                aria-label="&#36820;&#22238;&#20070;&#26550;"
                title="&#36820;&#22238;&#20070;&#26550;"
              >
                &#8962;
              </button>
            </div>
            <div class="reader-toolbar__right">
              <button class="ghost-button" type="button" data-reader-prev ${previous ? "" : "disabled"}>
                上一话
              </button>
              <div class="reader-current" data-reader-current>1 / ${pages.pages.length}</div>
              <button class="ghost-button" type="button" data-reader-next ${next ? "" : "disabled"}>
                下一话
              </button>
            </div>
          </div>
        </div>
      </div>

      <div class="reader-overview">
        <div class="reader-progress">
          <div class="reader-progress__track">
            <div class="reader-progress__fill" data-progress-fill></div>
          </div>
          <span data-page-inline>第 1 页</span>
        </div>
        <label class="reader-jump">
          <span>跳转页码</span>
          <select data-page-select>
            ${pages.pages
              .map(
                (page) => `
                  <option value="${page.index}">第 ${page.index + 1} 页</option>
                `,
              )
              .join("")}
          </select>
        </label>
      </div>

      <div class="reader-strip">
        ${pages.pages
          .map(
            (page) => `
              <button class="reader-pill" type="button" data-page-pill="${page.index}">
                ${page.index + 1}
              </button>
            `,
          )
          .join("")}
      </div>

      <div class="reader-pages">
        ${pages.pages
          .map(
            (page) => `
              <figure class="reader-page" data-page-anchor="${page.index}">
                <img
                  src="${escapeHTML(page.imageUrl)}"
                  alt="${escapeHTML(`${chapter.title} 第 ${page.index + 1} 页`)}"
                  loading="${page.index < 2 ? "eager" : "lazy"}"
                  decoding="async"
                  style="display:block;width:100%;height:auto;max-width:100%;object-fit:contain;aspect-ratio:auto;vertical-align:top"
                />
                <figcaption>第 ${page.index + 1} 页</figcaption>
              </figure>
            `,
          )
          .join("")}
      </div>

      <div class="reader-toolbar reader-toolbar--footer">
        <div class="reader-toolbar__left">
          <span class="reader-footer-label">${previous ? `上一话：${escapeHTML(previous.title)}` : "已经是第一话"}</span>
        </div>
        <div class="reader-toolbar__right">
          <button class="ghost-button" type="button" data-reader-prev ${previous ? "" : "disabled"}>
            \u4e0a\u4e00\u8bdd
          </button>
          <button class="ghost-button" type="button" data-reader-next ${next ? "" : "disabled"}>
            \u4e0b\u4e00\u8bdd
          </button>
        </div>
      </div>
    </section>
  `;

  setupReaderInteractions(chapter, manga, chapters, pages);
}

function renderOnlineLibraryView() {
  disconnectReaderObserver();
  cleanupToolbarExtras();
  eyebrowEl.textContent = "在线";
  refreshButton.hidden = false;
  refreshButton.textContent = "刷新";
  refreshButton.dataset.mode = "refresh";
  viewTitleEl.textContent = "在线漫画";

  const sources = state.online.sources?.items || [];
  const activeSource = state.online.sourceById.get(state.online.currentSourceId) || sources[0];
  const defaultFeed = state.online.defaultFeedsBySource.get(getOnlineDefaultFeedCacheKey(activeSource?.id));
  const jobs = state.online.downloads?.items || [];
  const hasQuery = Boolean(String(state.online.searchQuery || "").trim());
  const items = hasQuery ? (state.online.searchResults?.items || []) : (defaultFeed?.items || []);
  const sourceJobs = jobs.filter((job) => job.sourceId === activeSource?.id);
  const activeJobs = sourceJobs.filter((job) => !["done", "canceled"].includes(job.status));
  const completedJobs = sourceJobs.filter((job) => job.status === "done");
  const defaultTitle = defaultFeed?.title || activeSource?.defaultDisplay?.title || "在线入口";
  const defaultDescription = defaultFeed?.description
    || activeSource?.defaultDisplay?.description
    || "这里会展示当前来源默认提供的在线漫画内容。";
  const defaultFeedKey = getOnlineDefaultFeedCacheKey(activeSource?.id);
  const isLoadingMoreDefaultFeed = state.online.pendingDefaultFeedSources.has(defaultFeedKey);

  showFeedback(
    hasQuery
      ? `当前关键词共找到 ${items.length} 部漫画，点击封面进入详情页。`
      : `${defaultTitle} 已加载 ${items.length} 部漫画，可以直接阅读或加入下载。`,
  );

  appViewEl.innerHTML = `
    <section class="bookshelf-hero online-hero">
      <div>
        <p class="panel__eyebrow">Online Source</p>
        <h3>${escapeHTML(activeSource?.name || state.online.currentSourceId || "在线来源")}</h3>
        <p>
          ${escapeHTML(
            hasQuery
              ? `当前正在搜索“${state.online.searchQuery}”。结果支持直接阅读，也可以先加入下载任务。`
              : defaultDescription,
          )}
        </p>
      </div>
      <div class="bookshelf-hero__stats">
        <article class="bookshelf-stat">
          <strong>${items.length}</strong>
          <span>${hasQuery ? "搜索结果" : defaultTitle}</span>
        </article>
        <article class="bookshelf-stat">
          <strong>${activeJobs.length}</strong>
          <span>进行中的任务</span>
        </article>
        <article class="bookshelf-stat">
          <strong>${completedJobs.length}</strong>
          <span>已完成下载</span>
        </article>
      </div>
    </section>

    <section class="bookshelf-section online-search-panel">
      <div class="bookshelf-section__header">
        <div>
          <p class="panel__eyebrow">Search</p>
          <h3>来源与搜索</h3>
        </div>
        <span class="bookshelf-section__meta">代理连接 · 服务端解码</span>
      </div>

      <div class="management-tabs">
          ${sources.map((item) => `
            <button
              class="management-tab ${item.id === state.online.currentSourceId ? "is-active" : ""}"
              type="button"
              data-online-source="${escapeHTML(item.id)}"
            >
              ${escapeHTML(item.name)}
            </button>
          `).join("")}
      </div>

      <div class="library-tools library-tools--online-search">
        <label class="library-search library-search--inline">
          <div class="library-search__control">
            <input
              class="library-search__input"
              type="search"
              placeholder="输入标题、作者或关键词..."
              value="${escapeHTML(state.online.searchQuery || "")}"
              data-online-search-input
            />
            <button class="ghost-button ghost-button--small" type="button" data-online-search-button>搜索</button>
            <button class="ghost-button ghost-button--small" type="button" data-online-search-clear ${hasQuery ? "" : "disabled"}>清空</button>
          </div>
        </label>
          <div class="library-summary">
          <strong>${hasQuery ? items.length : sourceJobs.length}</strong>
          <span>${hasQuery ? "当前匹配" : "任务总数"}</span>
          </div>
        </div>
      </section>

      <section class="bookshelf-section">
        <div class="bookshelf-section__header">
          <div>
            <p class="panel__eyebrow">Collection</p>
            <h3>${hasQuery ? "搜索结果" : defaultTitle}</h3>
          </div>
          <span class="bookshelf-section__meta">${items.length} 部</span>
        </div>

      ${items.length
        ? `
          <section class="library-grid">
            ${items.map((item) => `
              <article class="manga-tile manga-tile--online" data-route="${routeForManga(item)}">
                <div class="manga-tile__cover">
                  ${renderOnlineCover(item)}
                </div>
                <div class="manga-tile__body">
                  <h3>${escapeHTML(item.title)}</h3>
                  ${renderOnlineAuthor(item.author)}
                  ${renderOnlineTileTagList(item.tags)}
                  ${getOnlineBookmarkState(item.sourceId, item.id).hasUpdate ? `<span class="manga-update-badge">有更新</span>` : ""}
                  <div class="manga-tile__quick-actions">
                    <button
                      class="ghost-button ghost-button--small ${getOnlineBookmarkState(item.sourceId, item.id).favorite ? "is-active" : ""}"
                      type="button"
                      data-online-card-bookmark="favorite"
                      data-source-id="${escapeHTML(item.sourceId)}"
                      data-manga-id="${escapeHTML(item.id)}"
                      aria-pressed="${getOnlineBookmarkState(item.sourceId, item.id).favorite ? "true" : "false"}"
                    >
                      ${getOnlineBookmarkState(item.sourceId, item.id).favorite ? "已收藏" : "收藏"}
                    </button>
                    <button
                      class="ghost-button ghost-button--small ${getOnlineBookmarkState(item.sourceId, item.id).following ? "is-active" : ""}"
                      type="button"
                      data-online-card-bookmark="follow"
                      data-source-id="${escapeHTML(item.sourceId)}"
                      data-manga-id="${escapeHTML(item.id)}"
                      aria-pressed="${getOnlineBookmarkState(item.sourceId, item.id).following ? "true" : "false"}"
                    >
                      ${getOnlineBookmarkState(item.sourceId, item.id).following ? "已追漫" : "追漫"}
                    </button>
                    <button
                      class="ghost-button ghost-button--small"
                      type="button"
                      data-online-card-block
                      data-source-id="${escapeHTML(item.sourceId)}"
                      data-manga-id="${escapeHTML(item.id)}"
                    >
                      屏蔽
                    </button>
                    <button
                      class="ghost-button ghost-button--small"
                      type="button"
                      data-online-card-download
                      data-source-id="${escapeHTML(item.sourceId)}"
                      data-manga-id="${escapeHTML(item.id)}"
                      ${state.online.pendingCreateKeys.has(getDownloadRequestKey(item.sourceId, item.id, [], "manga")) ? "disabled" : ""}
                    >
                      ${state.online.pendingCreateKeys.has(getDownloadRequestKey(item.sourceId, item.id, [], "manga")) ? "创建中..." : "下载"}
                    </button>
                  </div>
                </div>
              </article>
            `).join("")}
          </section>
          ${!hasQuery ? `
            <div class="online-feed-footer">
              <span class="online-feed-footer__meta">
                第 ${defaultFeed?.page || 1} 页 · 已显示 ${items.length} 部
              </span>
              <button
                class="ghost-button"
                type="button"
                data-online-default-load-more
                ${defaultFeed?.hasMore && !isLoadingMoreDefaultFeed ? "" : "disabled"}
              >
                ${isLoadingMoreDefaultFeed ? "加载中..." : (defaultFeed?.hasMore ? "加载更多" : "没有更多了")}
              </button>
            </div>
          ` : ""}`
        : hasQuery
          ? `<article class="empty-card"><strong>没有找到结果</strong><p>试试换一个标题、作者名或更短一点的关键词。</p></article>`
          : `<article class="empty-card"><strong>${escapeHTML(defaultTitle)}</strong><p>${escapeHTML(defaultDescription)}</p></article>`
      }
    </section>
  `;

  bindViewActions();

  appViewEl.querySelectorAll("[data-online-card-bookmark]").forEach((node) => {
    node.addEventListener("click", async (event) => {
      event.stopPropagation();
      const kind = node.dataset.onlineCardBookmark || "favorite";
      const sourceId = node.dataset.sourceId || state.online.currentSourceId;
      const mangaId = node.dataset.mangaId || "";
      const nextValue = node.getAttribute("aria-pressed") !== "true";
      node.disabled = true;
      node.textContent = nextValue ? "保存中..." : "取消中...";
      try {
        await updateOnlineBookmark(sourceId, mangaId, kind === "follow" ? { following: nextValue } : { favorite: nextValue });
        showFeedback(kind === "follow"
          ? (nextValue ? "已加入追漫。" : "已取消追漫。")
          : (nextValue ? "已收藏。" : "已取消收藏。"));
        updateHero();
        renderOnlineLibraryView();
      } catch (error) {
        node.disabled = false;
        node.textContent = kind === "follow" ? "追漫" : "收藏";
        showFeedback(`保存失败: ${error.message}`, "error");
      }
    });
  });

  appViewEl.querySelectorAll("[data-online-card-block]").forEach((node) => {
    node.addEventListener("click", async (event) => {
      event.stopPropagation();
      const sourceId = node.dataset.sourceId || state.online.currentSourceId;
      const mangaId = node.dataset.mangaId || "";
      node.disabled = true;
      node.textContent = "屏蔽中...";
      try {
        await blockOnlineManga(sourceId, mangaId);
        showFeedback("已屏蔽此在线漫画，当前列表已更新。");
        updateHero();
        renderOnlineLibraryView();
      } catch (error) {
        node.disabled = false;
        node.textContent = "屏蔽";
        showFeedback(`屏蔽失败: ${error.message}`, "error");
      }
    });
  });

  appViewEl.querySelectorAll("[data-online-card-download]").forEach((node) => {
    node.addEventListener("click", async (event) => {
      event.stopPropagation();
      const sourceId = node.dataset.sourceId || state.online.currentSourceId;
      const mangaId = node.dataset.mangaId || "";
      node.disabled = true;
      node.textContent = "创建中...";
      try {
        const result = await createOnlineDownloadJob(sourceId, mangaId, [], "manga");
        showFeedback(describeDownloadCreateResult(result));
        updateHero();
        renderOnlineLibraryView();
      } catch (error) {
        node.disabled = false;
        node.textContent = "下载";
        showFeedback(`创建下载任务失败: ${error.message}`, "error");
      }
    });
  });

  appViewEl.querySelectorAll("[data-online-source]").forEach((node) => {
    node.addEventListener("click", () => {
      state.online.currentSourceId = node.dataset.onlineSource;
      state.online.searchQuery = "";
      state.online.searchResults = null;
      setRoute(routeForOnlineLibrary(state.online.currentSourceId));
    });
  });

  const searchInput = appViewEl.querySelector("[data-online-search-input]");
  const searchButton = appViewEl.querySelector("[data-online-search-button]");
  const clearButton = appViewEl.querySelector("[data-online-search-clear]");
  const runSearch = async () => {
    state.online.searchQuery = searchInput?.value?.trim?.() || "";
    await ensureOnlineSearch(state.online.currentSourceId, state.online.searchQuery, true);
    renderOnlineLibraryView();
  };
  searchButton?.addEventListener("click", () => {
    void runSearch();
  });
  searchInput?.addEventListener("keydown", (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      void runSearch();
    }
  });
  clearButton?.addEventListener("click", async () => {
    state.online.searchQuery = "";
    if (searchInput) {
      searchInput.value = "";
    }
    await ensureOnlineDefaultFeed(state.online.currentSourceId, true);
    renderOnlineLibraryView();
  });

  appViewEl.querySelector("[data-online-default-load-more]")?.addEventListener("click", async (event) => {
    const node = event.currentTarget;
    node.disabled = true;
    node.textContent = "加载中...";
    try {
      const result = await loadMoreOnlineDefaultFeed(state.online.currentSourceId);
      if (!result) {
        showFeedback("正在加载下一页，请稍等。");
        return;
      }
      updateHero();
      renderOnlineLibraryView();
    } catch (error) {
      node.disabled = false;
      node.textContent = "加载更多";
      showFeedback(`加载更多失败: ${error.message}`, "error");
    }
  });
}

function renderOnlineBookmarkView(kind = "favorite") {
  disconnectReaderObserver();
  cleanupToolbarExtras();
  const normalizedKind = kind === "follow" ? "follow" : "favorite";
  const isFollow = normalizedKind === "follow";
  const sourceId = state.online.currentSourceId || "18comic";
  const source = state.online.sourceById.get(sourceId);
  const payload = state.online.bookmarksByKind.get(getOnlineBookmarkCacheKey(sourceId, normalizedKind));
  const items = payload?.items || [];
  const updatedItems = items.filter((item) => item.hasUpdate);

  eyebrowEl.textContent = isFollow ? "追漫" : "收藏";
  refreshButton.hidden = false;
  refreshButton.textContent = "刷新";
  refreshButton.dataset.mode = "refresh";
  viewTitleEl.textContent = isFollow ? "追漫" : "收藏";
  showFeedback(isFollow
    ? `追漫列表共有 ${items.length} 部漫画，其中 ${updatedItems.length} 部提示有更新。`
    : `收藏列表共有 ${items.length} 部漫画。`);

  appViewEl.innerHTML = `
    <section class="bookshelf-hero online-hero">
      <div>
        <p class="panel__eyebrow">${isFollow ? "Following" : "Favorites"}</p>
        <h3>${isFollow ? "追漫" : "收藏"}</h3>
        <p>${isFollow ? "这里显示你正在追的在线漫画。后台检测到同一漫画 ID 的章节数量增加时，会在这里提示有更新。" : "这里显示你收藏的在线漫画，适合临时保存想回看的作品。"}</p>
      </div>
      <div class="bookshelf-hero__stats">
        <article class="bookshelf-stat">
          <strong>${items.length}</strong>
          <span>${isFollow ? "追漫" : "收藏"}</span>
        </article>
        <article class="bookshelf-stat">
          <strong>${updatedItems.length}</strong>
          <span>有更新</span>
        </article>
        <article class="bookshelf-stat">
          <strong>${escapeHTML(source?.name || sourceId)}</strong>
          <span>来源</span>
        </article>
      </div>
    </section>

    <section class="bookshelf-section">
      <div class="bookshelf-section__header">
        <div>
          <p class="panel__eyebrow">Collection</p>
          <h3>${isFollow ? "追漫列表" : "收藏列表"}</h3>
        </div>
        <span class="bookshelf-section__meta">${items.length} 部</span>
      </div>
      ${items.length
        ? `<section class="library-grid">
            ${items.map((item) => `
              <article class="manga-tile manga-tile--online" data-route="${routeForManga(item)}">
                <div class="manga-tile__cover">
                  ${renderOnlineCover(item)}
                </div>
                <div class="manga-tile__body">
                  <h3>${escapeHTML(item.title)}</h3>
                  ${item.hasUpdate ? `<span class="manga-update-badge">有更新</span>` : ""}
                  ${renderOnlineAuthor(item.author)}
                  ${renderOnlineTileTagList(item.tags)}
                  <div class="manga-metrics">
                    <span>${item.chapterCount || 0} 话</span>
                    <span>${item.following ? "已追漫" : "未追漫"}</span>
                  </div>
                  <div class="manga-tile__quick-actions">
                    <button
                      class="ghost-button ghost-button--small"
                      type="button"
                      data-online-card-bookmark="${isFollow ? "follow" : "favorite"}"
                      data-source-id="${escapeHTML(item.sourceId)}"
                      data-manga-id="${escapeHTML(item.id)}"
                      aria-pressed="true"
                    >
                      ${isFollow ? "取消追漫" : "取消收藏"}
                    </button>
                  </div>
                </div>
              </article>
            `).join("")}
          </section>`
        : `<article class="empty-card"><strong>${isFollow ? "还没有追漫。" : "还没有收藏。"}</strong><p>在在线漫画详情页点击“${isFollow ? "追漫" : "收藏"}”后，会出现在这里。</p></article>`
      }
    </section>
  `;

  bindViewActions();
  appViewEl.querySelectorAll("[data-online-card-bookmark]").forEach((node) => {
    node.addEventListener("click", async (event) => {
      event.stopPropagation();
      const sourceId = node.dataset.sourceId || state.online.currentSourceId;
      const mangaId = node.dataset.mangaId || "";
      node.disabled = true;
      node.textContent = "保存中...";
      try {
        await updateOnlineBookmark(sourceId, mangaId, isFollow ? { following: false } : { favorite: false });
        await ensureOnlineBookmarks(sourceId, normalizedKind, true);
        updateHero();
        renderOnlineBookmarkView(normalizedKind);
      } catch (error) {
        showFeedback(`保存失败: ${error.message}`, "error");
        node.disabled = false;
      }
    });
  });
}

function renderDownloadsView() {
  disconnectReaderObserver();
  cleanupToolbarExtras();
  eyebrowEl.textContent = "Downloads";
  refreshButton.hidden = false;
  refreshButton.textContent = "刷新";
  refreshButton.dataset.mode = "refresh";
  viewTitleEl.textContent = "下载任务";

  const jobs = state.online.downloads?.items || [];
  const filteredJobs = getFilteredDownloadJobs(jobs);
  const sources = state.online.sources?.items || [];
  const activeCount = jobs.filter((job) => isActiveDownloadStatus(job.status)).length;
  const failedCount = jobs.filter((job) => getDownloadStatusGroup(job.status) === "failed").length;
  const doneCount = jobs.filter((job) => getDownloadStatusGroup(job.status) === "done").length;
  const pausedCount = jobs.filter((job) => getDownloadStatusGroup(job.status) === "paused").length;
  const statusFilters = [
    { id: "all", label: "全部", count: jobs.length },
    { id: "active", label: "下载中", count: activeCount },
    { id: "paused", label: "已暂停", count: pausedCount },
    { id: "failed", label: "失败", count: failedCount },
    { id: "done", label: "已完成", count: doneCount },
    { id: "canceled", label: "已取消", count: jobs.filter((job) => getDownloadStatusGroup(job.status) === "canceled").length },
  ];

  showFeedback(
    jobs.length
      ? `共有 ${jobs.length} 个下载任务，当前筛选显示 ${filteredJobs.length} 个。`
      : "还没有下载任务，可以从在线漫画详情页创建下载。",
  );

  appViewEl.innerHTML = `
    <section class="bookshelf-hero downloads-hero">
      <div>
        <p class="panel__eyebrow">Queue</p>
        <h3>下载任务</h3>
        <p>这里集中管理所有在线来源的下载队列。你可以暂停、继续、重试、重新下载，或者清理记录和本地文件。</p>
      </div>
      <div class="bookshelf-hero__stats">
        <article class="bookshelf-stat">
          <strong>${activeCount}</strong>
          <span>进行中</span>
        </article>
        <article class="bookshelf-stat">
          <strong>${failedCount}</strong>
          <span>失败</span>
        </article>
        <article class="bookshelf-stat">
          <strong>${doneCount}</strong>
          <span>已完成</span>
        </article>
      </div>
    </section>

    <section class="bookshelf-section downloads-filter-panel">
      <div class="bookshelf-section__header">
        <div>
          <p class="panel__eyebrow">Filters</p>
          <h3>筛选与排序</h3>
        </div>
        <span class="bookshelf-section__meta">${filteredJobs.length} 个</span>
      </div>

      <div class="management-tabs downloads-status-tabs">
        ${statusFilters.map((item) => `
          <button
            class="management-tab ${state.online.downloadStatusFilter === item.id ? "is-active" : ""}"
            type="button"
            data-download-status-filter="${escapeHTML(item.id)}"
          >
            ${escapeHTML(item.label)} ${item.count}
          </button>
        `).join("")}
      </div>

      <div class="downloads-controls">
        <label class="library-sort">
          <span class="library-search__label">来源</span>
          <select class="library-sort__select" data-download-source-filter>
            <option value="all" ${state.online.downloadSourceFilter === "all" ? "selected" : ""}>全部来源</option>
            ${sources.map((source) => `
              <option value="${escapeHTML(source.id)}" ${state.online.downloadSourceFilter === source.id ? "selected" : ""}>
                ${escapeHTML(source.name)}
              </option>
            `).join("")}
          </select>
        </label>
        <label class="library-sort">
          <span class="library-search__label">排序</span>
          <select class="library-sort__select" data-download-sort>
            <option value="updated-desc" ${state.online.downloadSort === "updated-desc" ? "selected" : ""}>最近更新</option>
            <option value="created-desc" ${state.online.downloadSort === "created-desc" ? "selected" : ""}>创建时间</option>
            <option value="progress-desc" ${state.online.downloadSort === "progress-desc" ? "selected" : ""}>进度最高</option>
          </select>
        </label>
      </div>
    </section>

    <section class="bookshelf-section downloads-list-panel">
      <div class="bookshelf-section__header">
        <div>
          <p class="panel__eyebrow">Tasks</p>
          <h3>${escapeHTML(getDownloadStatusFilterLabel(state.online.downloadStatusFilter))}</h3>
        </div>
        <span class="bookshelf-section__meta">${filteredJobs.length} 个任务</span>
      </div>
      ${filteredJobs.length
        ? `<div class="download-job-list">${filteredJobs.map(renderDownloadJobCard).join("")}</div>`
        : `<article class="empty-card"><strong>没有匹配的下载任务。</strong><p>切换筛选条件，或者从在线漫画详情页创建新的下载任务。</p></article>`
      }
    </section>
  `;

  bindViewActions();
  bindDownloadJobActions();

  appViewEl.querySelectorAll("[data-download-status-filter]").forEach((node) => {
    node.addEventListener("click", () => {
      state.online.downloadStatusFilter = node.dataset.downloadStatusFilter || "all";
      renderDownloadsView();
    });
  });

  appViewEl.querySelector("[data-download-source-filter]")?.addEventListener("change", (event) => {
    state.online.downloadSourceFilter = event.target.value || "all";
    renderDownloadsView();
  });

  appViewEl.querySelector("[data-download-sort]")?.addEventListener("change", (event) => {
    state.online.downloadSort = event.target.value || "updated-desc";
    renderDownloadsView();
  });
}


async function resolveReaderContext(chapterId) {
  await Promise.all([
    ensureHealth(),
    ensureBookshelves(),
    ensureLibrary(false, state.currentBookshelfId || "", state.libraryTagFilterIDs || []),
    ensureLibrary(false, ""),
  ]);

  const cachedContext = findCachedChapterContext(chapterId);
  if (cachedContext) {
    return createReaderContext(
      cachedContext.mangaId,
      cachedContext.chapter,
      cachedContext.chapters,
      chapterId,
    );
  }

  const cachedLibraryContext = await findReaderContextInItems(chapterId, getCachedLibraryItems());
  if (cachedLibraryContext) {
    return cachedLibraryContext;
  }

  if (state.currentBookshelfId || state.libraryTagFilterIDs.length) {
    const currentLibraryItems = await fetchLibraryItemsForLookup(
      state.currentBookshelfId || "",
      state.libraryTagFilterIDs || [],
    );
    const currentLibraryContext = await findReaderContextInItems(chapterId, currentLibraryItems);
    if (currentLibraryContext) {
      return currentLibraryContext;
    }
  }

  const allLibraryItems = await fetchLibraryItemsForLookup("", []);
  const allLibraryContext = await findReaderContextInItems(chapterId, allLibraryItems);
  if (allLibraryContext) {
    return allLibraryContext;
  }

  throw new Error("未找到对应章节");
}

async function renderCurrentRoute() {
  try {
    const route = getRoute();
    if (route.name !== "library") {
      clearLibrarySearchTimer();
    }
    applyRouteLayout(route.name);
    updateHero();
    if (route.name === "chapter" || route.name === "onlineChapter") {
      renderChapterLoadingView(route);
    }

      if (route.name === "library") {
      await Promise.all([
        ensureHealth(),
        ensureBookshelves(),
        ensureScanStatus(),
        ensureTags(),
        ensureLibrary(false, state.currentBookshelfId || "", state.libraryTagFilterIDs || []),
      ]);
      updateHero();
      renderLibraryView();
      if (state.scanStatus?.running) {
        scheduleScanStatusPoll(1500);
      }
        return;
      }

      if (route.name === "downloads") {
        await Promise.all([ensureHealth(), ensureOnlineSources(), ensureOnlineDownloads(true)]);
        updateHero();
        renderDownloadsView();
        return;
      }

      if (route.name === "onlineLibrary") {
        state.online.currentSourceId = route.sourceId;
        rememberOnlineReturnRoute(window.location.hash || routeForOnlineLibrary(route.sourceId));
        await Promise.all([ensureHealth(), ensureOnlineSources(), ensureOnlineDownloads()]);
        if (String(state.online.searchQuery || "").trim()) {
          await ensureOnlineSearch(route.sourceId, state.online.searchQuery || "", false);
        } else {
          await ensureOnlineDefaultFeed(route.sourceId, false);
        }
        await Promise.all([
          ensureOnlineBookmarks(route.sourceId, "favorite"),
          ensureOnlineBookmarks(route.sourceId, "follow"),
        ]);
        updateHero();
        renderOnlineLibraryView();
        return;
      }

      if (route.name === "onlineBookmarks") {
        state.online.currentSourceId = route.sourceId;
        state.online.bookmarkKind = route.kind || "favorite";
        rememberOnlineReturnRoute(routeForOnlineBookmarks(state.online.bookmarkKind, route.sourceId));
        await Promise.all([
          ensureHealth(),
          ensureOnlineSources(),
          ensureOnlineDownloads(),
          ensureOnlineBookmarks(route.sourceId, route.kind || "favorite", true),
        ]);
        updateHero();
        renderOnlineBookmarkView(route.kind || "favorite");
        return;
      }

    if (route.name === "manage") {
      await Promise.all([
        ensureHealth(),
        ensureBookshelves(),
        ensureScanStatus(),
        ensureTags(),
        ensureOnlineSources(),
        ensureOnlineSettings(),
      ]);
      updateHero();
      renderTagsView();
      if (state.scanStatus?.running) {
        scheduleScanStatusPoll(1500);
      }
      return;
    }

    if (route.name === "manga") {
      await Promise.all([ensureHealth(), ensureScanStatus(), ensureTags()]);
      const { manga, chapters } = await refreshMangaContext(route.mangaId);
      updateHero();
      renderMangaView(manga, chapters);
      if (state.scanStatus?.running) {
        scheduleScanStatusPoll(1500);
      }
      return;
    }

    if (route.name === "onlineManga") {
      await Promise.all([
        ensureHealth(),
        ensureOnlineSources(),
        ensureOnlineDownloads(),
        ensureOnlineBookmarks(route.sourceId, "favorite"),
        ensureOnlineBookmarks(route.sourceId, "follow"),
      ]);
      state.online.currentSourceId = route.sourceId;
      const [manga, chapters] = await Promise.all([
        ensureOnlineManga(route.sourceId, route.mangaId),
        ensureOnlineChapters(route.sourceId, route.mangaId),
      ]);
      updateHero();
      renderMangaView({
        ...manga,
        coverThumbUrl: buildOnlineImageProxyURL(route.sourceId, manga.coverUrl),
        chapterCount: chapters.items?.length || 0,
        pageCount: 0,
      }, chapters);
      return;
    }

    if (route.name === "onlineChapter") {
      await Promise.all([ensureHealth(), ensureOnlineSources(), ensureOnlineDownloads()]);
      state.online.currentSourceId = route.sourceId;
      const pages = await ensureOnlinePages(route.sourceId, route.chapterId);

      let manga = null;
      let chapters = { items: [{ id: route.chapterId, title: `Chapter ${route.chapterId}`, sourceId: route.sourceId, pageCount: (pages.items || []).length }] };
      let chapter = chapters.items[0];

      for (const [key, chapterList] of state.online.chaptersByKey.entries()) {
        const matched = (chapterList.items || []).find((item) => item.id === route.chapterId);
        if (!matched) {
          continue;
        }
        chapter = matched;
        chapters = chapterList;
        const mangaId = key.split("::")[1];
        manga = await ensureOnlineManga(route.sourceId, mangaId);
        break;
      }

      if (!manga) {
        manga = {
          id: route.chapterId,
          title: "Online Manga",
          coverThumbUrl: "",
          sourceId: route.sourceId,
        };
      }

      updateHero();
      renderReaderView(
        chapter,
        { ...manga, coverThumbUrl: buildOnlineImageProxyURL(route.sourceId, manga.coverUrl || manga.coverThumbUrl), sourceId: route.sourceId },
        chapters,
        { pages: pages.items || [] },
      );
      window.requestAnimationFrame(() => {
        scrollToReaderStart("auto");
      });
      return;
    }

    const readerContext = await resolveReaderContext(route.chapterId);
    await ensureScanStatus();
    updateHero();
    renderReaderView(
      readerContext.chapter,
      readerContext.manga,
      readerContext.chapters,
      readerContext.pages,
    );
    window.requestAnimationFrame(() => {
      scrollToReaderStart("auto");
    });
    if (state.scanStatus?.running) {
      scheduleScanStatusPoll(1500);
    }
  } catch (error) {
    applyRouteLayout("error");
    disconnectReaderObserver();
    cleanupToolbarExtras();
    appViewEl.innerHTML = `
      <article class="empty-card">
        <strong>页面加载失败。</strong>
        <p>${escapeHTML(error.message)}</p>
      </article>
    `;
    showFeedback(`请求失败: ${error.message}`, "error");
  }
}

refreshButton.addEventListener("click", async () => {
  if (refreshButton.dataset.mode === "home") {
    setRoute("#/");
    return;
  }
  if (getRoute().name === "downloads") {
    showFeedback("正在刷新下载任务...");
    state.online.downloads = null;
    await ensureOnlineDownloads(true);
    await renderCurrentRoute();
    return;
  }
  if (String(getRoute().name || "").startsWith("online")) {
    showFeedback("正在刷新在线数据...");
    try {
      await refreshOnlineData(getRoute().sourceId || state.online.currentSourceId || "18comic");
      await renderCurrentRoute();
    } catch (error) {
      try {
        await ensureOnlineSources(true);
        renderOnlineLibraryView();
      } catch (renderError) {
        console.warn("failed to render online sources after refresh failure", renderError);
      }
      showFeedback(`刷新失败: ${error.message}`, "error");
    }
    return;
  }
  if (getRoute().name === "library") {
    showFeedback(state.currentBookshelfId ? "正在同步当前书架..." : "正在重新扫描全部书架...");
    try {
      await refreshCurrentLibraryFromDisk();
      if (state.currentBookshelfId) {
        showFeedback("当前书架已刷新。");
      }
      await renderCurrentRoute();
    } catch (error) {
      showFeedback(`刷新失败: ${error.message}`, "error");
    }
    return;
  }
  showFeedback("正在刷新数据...");
  await refreshAllData();
  await renderCurrentRoute();
});

scanButton.addEventListener("click", async () => {
  showFeedback("正在重新扫描本地漫画目录，完成后会自动刷新...");
  scanButton.disabled = true;
  try {
    await fetchJSON("/api/tasks/scan", { method: "POST" });
    await ensureScanStatus();
    updateHero();
    scheduleScanStatusPoll(1200);
  } catch (error) {
    showFeedback(`扫描失败: ${error.message}`, "error");
  } finally {
    updateScanUI();
  }
});

homeButton.addEventListener("click", () => {
  setRoute("#/");
});

onlineButton?.addEventListener("click", async () => {
  await ensureOnlineSources();
  setRoute(routeForOnlineLibrary(state.online.currentSourceId || "18comic"));
});

favoritesButton?.addEventListener("click", async () => {
  await ensureOnlineSources();
  setRoute(routeForOnlineBookmarks("favorite", state.online.currentSourceId || "18comic"));
});

followingButton?.addEventListener("click", async () => {
  await ensureOnlineSources();
  setRoute(routeForOnlineBookmarks("follow", state.online.currentSourceId || "18comic"));
});

downloadsButton?.addEventListener("click", () => {
  setRoute("#/downloads");
});

manageButton?.addEventListener("click", () => {
  setRoute("#/manage/tags");
});

themeToggleButton?.addEventListener("click", () => {
  setTheme(getCurrentTheme() === "dark" ? "light" : "dark");
});

window.addEventListener("hashchange", () => {
  renderCurrentRoute();
});

updateThemeToggle();
renderCurrentRoute();


