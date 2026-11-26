import {OutputRenderer} from './output-renderer.js';
const state = {
  conversationID: new URL(location.href).searchParams.get('conversation') || localStorage.getItem('codex.currentConversation') || '',
  repository: readStoredRepository(),
  repositories: readStoredRepositories(),
  sending: false,
  deleting: false,
  deleteTarget: null,
};

// Preserve repositories selected by earlier versions, before the recent list existed.
if (state.repository && !state.repositories.some((item) => item.url === state.repository.url && item.revision === state.repository.revision)) {
  state.repositories = [state.repository, ...state.repositories].slice(0, 12);
  localStorage.setItem('codex.repositories', JSON.stringify(state.repositories));
}

const elements = {
  shell: document.querySelector('.app-shell'),
  sidebar: document.querySelector('#sidebar'),
  sidebarBackdrop: document.querySelector('#sidebar-backdrop'),
  historyToggle: document.querySelector('#history-toggle'),
  historyList: document.querySelector('#history-list'),
  historyError: document.querySelector('#history-error'),
  newConversation: document.querySelector('#new-conversation'),
  conversationTitle: document.querySelector('#conversation-title'),
  conversationContext: document.querySelector('#conversation-context'),
  transcript: document.querySelector('#transcript'),
  emptyState: document.querySelector('#empty-state'),
  messages: document.querySelector('#messages'),
  status: document.querySelector('#status'),
  composer: document.querySelector('#message-form'),
  message: document.querySelector('#message'),
  sendButton: document.querySelector('#send-message'),
  deleteSelected: document.querySelector('#delete-selected-conversation'),
  contextToggle: document.querySelector('#context-toggle'),
  contextMenu: document.querySelector('#context-menu'),
  accountIndicator: document.querySelector('#codex-account'),
  accountLabel: document.querySelector('#codex-account-label'),
  manageUsage: document.querySelector('#manage-usage'),
  connectChatGPT: document.querySelector('#connect-chatgpt'),
  authDialog: document.querySelector('#auth-dialog'),
  authVerificationLink: document.querySelector('#auth-verification-link'),
  authUserCode: document.querySelector('#auth-user-code'),
  authWaiting: document.querySelector('#auth-waiting'),
  copyAuthCode: document.querySelector('#copy-auth-code'),
  cancelAuth: document.querySelector('#cancel-auth'),
  closeAuthDialog: document.querySelector('#close-auth-dialog'),
  repositoryChip: document.querySelector('#selected-repository'),
  repositoryLabel: document.querySelector('#repository-label'),
  removeRepository: document.querySelector('#remove-repository'),
  repositoryDialog: document.querySelector('#repository-dialog'),
  repositoryForm: document.querySelector('#repository-form'),
  repositoryURL: document.querySelector('#repository-url'),
  repositoryRevision: document.querySelector('#repository-revision'),
  repositoryError: document.querySelector('#repository-error'),
  closeRepositoryDialog: document.querySelector('#close-repository-dialog'),
  cancelRepository: document.querySelector('#cancel-repository'),
};

function readStoredRepository() {
  try { return JSON.parse(localStorage.getItem('codex.selectedRepository') || 'null'); }
  catch (_) { return null; }
}

function readStoredRepositories() {
  try {
    const value = JSON.parse(localStorage.getItem('codex.repositories') || '[]');
    return Array.isArray(value) ? value.filter((item) => item && item.url && item.revision) : [];
  } catch (_) { return []; }
}

function setStatus(message = '', kind = '') {
  elements.status.textContent = message;
  if (kind) elements.status.dataset.kind = kind;
  else delete elements.status.dataset.kind;
}

async function request(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (options.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  const response = await fetch(path, {...options, headers, credentials: 'same-origin'});
  if (!response.ok) {
    let detail = '';
    try {
      const body = await response.json();
      detail = body.message || body.error || '';
    } catch (_) { /* Use the status-based message when the response is not JSON. */ }
    const error = new Error(detail || `リクエストに失敗しました（${response.status}）`);
    error.status = response.status;
    throw error;
  }
  if (response.status === 204) return null;
  return response.json();
}

function setConversationURL(id) {
  const url = new URL(location.href);
  if (id) url.searchParams.set('conversation', id);
  else url.searchParams.delete('conversation');
  history.replaceState({}, '', url);
}

function repositoryName(repository) {
  if (!repository?.url) return '';
  try {
    const parsed = new URL(repository.url);
    return `${parsed.pathname.replace(/^\//, '').replace(/\.git$/, '').replace(/\/$/, '')} · ${repository.revision}`;
  } catch (_) { return `${repository.url} · ${repository.revision}`; }
}

function renderRepository() {
  const repository = state.repository;
  const name = repositoryName(repository);
  elements.repositoryChip.hidden = !name;
  elements.repositoryLabel.textContent = name;
  elements.removeRepository.hidden = Boolean(state.conversationID);
  elements.contextToggle.disabled = Boolean(state.conversationID);
  elements.contextToggle.setAttribute('aria-label', state.conversationID ? '作業先はチャット作成時に固定されています' : 'リポジトリを追加');
  elements.conversationContext.replaceChildren();
  if (name && state.conversationID) {
    const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    icon.setAttribute('viewBox', '0 0 20 20');
    icon.setAttribute('aria-hidden', 'true');
    icon.innerHTML = '<path d="M3 5.5h5l1.6 1.8H17v8.2H3z"/><path d="M3 5.5V4h5l1.6 1.5"/>';
    const label = document.createElement('span');
    label.textContent = name;
    elements.conversationContext.append(icon, label);
    elements.conversationContext.hidden = false;
  } else {
    elements.conversationContext.hidden = true;
  }
}

function setCodexAccount(account) {
  elements.accountIndicator.hidden = false;
  elements.accountIndicator.dataset.connected = 'true';
  delete elements.accountIndicator.dataset.connecting;
  const usesChatGPTPlan = account?.type === 'chatgpt';
  elements.accountLabel.textContent = usesChatGPTPlan
    ? `ChatGPT 接続済み${account.planType ? ` · ${account.planType}` : ''}`
    : 'Codex に接続済み';
  elements.connectChatGPT.hidden = true;
  elements.manageUsage.hidden = !usesChatGPTPlan;
}

function clearCodexAccount() {
  elements.accountIndicator.hidden = false;
  delete elements.accountIndicator.dataset.connected;
  delete elements.accountIndicator.dataset.connecting;
  elements.accountLabel.textContent = 'ChatGPT 未接続';
  elements.connectChatGPT.hidden = false;
  elements.manageUsage.hidden = true;
}

function makeIcon(path) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 20 20');
  svg.setAttribute('aria-hidden', 'true');
  svg.innerHTML = path;
  return svg;
}

function setRepository(repository) {
  state.repository = repository;
  if (repository) {
    localStorage.setItem('codex.selectedRepository', JSON.stringify(repository));
    state.repositories = [repository, ...state.repositories.filter((item) => item.url !== repository.url || item.revision !== repository.revision)].slice(0, 12);
    localStorage.setItem('codex.repositories', JSON.stringify(state.repositories));
  } else {
    localStorage.removeItem('codex.selectedRepository');
  }
  renderRepository();
  renderContextMenu();
}

function renderContextMenu() {
  elements.contextMenu.replaceChildren();
  const repositories = state.repository
    ? [state.repository, ...state.repositories.filter((item) => item.url !== state.repository.url || item.revision !== state.repository.revision)]
    : state.repositories;
  if (repositories.length) {
    const label = document.createElement('p');
    label.className = 'menu-section-label';
    label.textContent = 'リポジトリ';
    elements.contextMenu.append(label);
    for (const repository of repositories) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'repository-option';
      button.append(makeIcon('<path d="M3 5.5h5l1.6 1.8H17v8.2H3z"/><path d="M3 5.5V4h5l1.6 1.5"/>'));
      const name = document.createElement('span');
      name.textContent = repositoryName(repository);
      button.append(name);
      if (state.repository?.url === repository.url && state.repository?.revision === repository.revision) {
        button.setAttribute('aria-pressed', 'true');
        const selected = document.createElement('small');
        selected.textContent = '選択中';
        button.append(selected);
      }
      button.addEventListener('click', () => {
        setRepository(repository);
        closeContextMenu();
        elements.message.focus();
      });
      elements.contextMenu.append(button);
    }
    const divider = document.createElement('div');
    divider.className = 'menu-divider';
    elements.contextMenu.append(divider);
  }
  const add = document.createElement('button');
  add.type = 'button';
  add.append(makeIcon('<path d="M3 5.5h5l1.6 1.8H17v8.2H3z"/><path d="M3 5.5V4h5l1.6 1.5"/>'));
  const text = document.createElement('span');
  text.textContent = 'リポジトリを追加';
  add.append(text);
  add.addEventListener('click', () => {
    closeContextMenu();
    elements.repositoryError.hidden = true;
    elements.repositoryURL.value = '';
    elements.repositoryRevision.value = 'main';
    elements.repositoryDialog.showModal();
    elements.repositoryURL.focus();
  });
  elements.contextMenu.append(add);
}

function openContextMenu() {
  renderContextMenu();
  elements.contextMenu.hidden = false;
  elements.contextToggle.setAttribute('aria-expanded', 'true');
  const firstItem = elements.contextMenu.querySelector('button');
  firstItem?.focus();
}

function closeContextMenu({restoreFocus = false} = {}) {
  elements.contextMenu.hidden = true;
  elements.contextToggle.setAttribute('aria-expanded', 'false');
  if (restoreFocus) elements.contextToggle.focus();
}

function formatConversationDate(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const now = new Date();
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const startOfDate = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  const days = Math.round((startOfToday - startOfDate) / 86400000);
  if (days === 0) return '今日';
  if (days === 1) return '昨日';
  if (days < 7) return '過去7日';
  return 'それ以前';
}

async function loadConversationList() {
  elements.historyError.hidden = true;
  try {
    const result = await request('/v1/conversations');
    renderHistory(result.conversations || []);
  } catch (error) {
    elements.historyError.textContent = `履歴を読み込めませんでした: ${error.message}`;
    elements.historyError.hidden = false;
    elements.historyList.replaceChildren();
  }
}

function selectConversationForDeletion(id, title) {
  state.deleteTarget = {id, title};
  elements.deleteSelected.hidden = false;
  elements.deleteSelected.setAttribute('aria-label', `「${title}」をアーカイブ`);
}

function clearDeleteSelection() {
  state.deleteTarget = null;
  elements.deleteSelected.hidden = true;
}

async function archiveConversation(id, title) {
  if (state.deleting || state.sending) {
    setStatus('応答中はチャットをアーカイブできません', 'error');
    return;
  }
  if (!window.confirm(`「${title}」をアーカイブしますか？\n会話履歴は残ります。セッションのKubernetesリソースを削除しますが、ストレージ設定によって作業データが残る場合があります。`)) return;
  state.deleting = true;
  try {
    await request(`/v1/conversations/${encodeURIComponent(id)}`, {method: 'DELETE'});
    clearDeleteSelection();
    if (state.conversationID === id) startNewConversation();
    await loadConversationList();
    setStatus('チャットをアーカイブしました');
  } catch (error) {
    setStatus(`アーカイブできませんでした: ${error.message}`, 'error');
  } finally {
    state.deleting = false;
  }
}

function renderHistory(conversations) {
  elements.historyList.replaceChildren();
  if (state.deleteTarget && !conversations.some((item) => item.id === state.deleteTarget.id)) clearDeleteSelection();
  const groups = new Map();
  for (const conversation of conversations) {
    const groupName = formatConversationDate(conversation.updatedAt || conversation.createdAt);
    if (!groups.has(groupName)) groups.set(groupName, []);
    groups.get(groupName).push(conversation);
  }
  const groupOrder = ['今日', '昨日', '過去7日', 'それ以前'];
  for (const groupName of groupOrder) {
    const items = groups.get(groupName);
    if (!items?.length) continue;
    const group = document.createElement('section');
    group.className = 'history-group';
    const heading = document.createElement('h2');
    heading.textContent = groupName;
    const list = document.createElement('div');
    list.className = 'history-items';
    for (const conversation of items) {
      const title = conversation.title || '新しいチャット';
      const row = document.createElement('div');
      row.className = 'history-row';
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'history-item';
      button.textContent = title;
      button.title = title;
      if (conversation.id === state.conversationID) button.setAttribute('aria-current', 'page');
      let suppressClick = false;
      let pressTimer = null;
      const cancelPress = () => {
        if (pressTimer !== null) clearTimeout(pressTimer);
        pressTimer = null;
      };
      button.addEventListener('pointerdown', (event) => {
        if (event.pointerType === 'mouse' || event.button !== 0) return;
        cancelPress();
        pressTimer = setTimeout(() => {
          pressTimer = null;
          suppressClick = true;
          selectConversationForDeletion(conversation.id, title);
          menu.hidden = false;
          options.setAttribute('aria-expanded', 'true');
        }, 550);
      });
      button.addEventListener('pointerup', cancelPress);
      button.addEventListener('pointercancel', cancelPress);
      button.addEventListener('pointerleave', cancelPress);
      button.addEventListener('contextmenu', (event) => {
        event.preventDefault();
        cancelPress();
        suppressClick = true;
        selectConversationForDeletion(conversation.id, title);
        menu.hidden = false;
        options.setAttribute('aria-expanded', 'true');
      });
      button.addEventListener('click', () => {
        if (suppressClick) { suppressClick = false; return; }
        clearDeleteSelection();
        openConversation(conversation.id);
      });

      const options = document.createElement('button');
      options.className = 'history-options';
      options.type = 'button';
      options.textContent = '⋯';
      options.setAttribute('aria-label', `「${title}」の操作`);
      options.setAttribute('aria-haspopup', 'menu');
      options.setAttribute('aria-expanded', 'false');
      const menu = document.createElement('div');
      menu.className = 'history-options-menu';
      menu.setAttribute('role', 'menu');
      menu.hidden = true;
      const remove = document.createElement('button');
      remove.type = 'button';
      remove.setAttribute('role', 'menuitem');
      remove.className = 'history-delete-action';
      remove.textContent = 'チャットをアーカイブ';
      remove.addEventListener('click', () => {
        menu.hidden = true;
        options.setAttribute('aria-expanded', 'false');
        archiveConversation(conversation.id, title);
      });
      menu.append(remove);
      options.addEventListener('click', () => {
        const opening = menu.hidden;
        elements.historyList.querySelectorAll('.history-options-menu').forEach((item) => { item.hidden = true; });
        elements.historyList.querySelectorAll('.history-options').forEach((item) => item.setAttribute('aria-expanded', 'false'));
        menu.hidden = !opening;
        options.setAttribute('aria-expanded', String(opening));
      });
      row.append(button, options, menu);
      list.append(row);
    }
    group.append(heading, list);
    elements.historyList.append(group);
  }
  if (!conversations.length) {
    const empty = document.createElement('p');
    empty.className = 'history-empty';
    empty.textContent = 'チャットはまだありません';
    elements.historyList.append(empty);
  }
}

function addMessage(role, content) {
  const item = document.createElement('li');
  item.className = `message ${role === 'user' ? 'user' : 'assistant'}`;
  const paragraph = document.createElement(role === 'user' ? 'p' : 'div');
  const renderer = role === 'user' ? null : new OutputRenderer(paragraph);
  if (renderer) renderer.render(content); else paragraph.textContent = content;
  item.append(paragraph);
  elements.messages.append(item);
  elements.emptyState.hidden = true;
  scrollTranscriptToEnd();
  return {item, paragraph, renderer, source: content};
}

function addApproval(runID, payload) {
  if ([...elements.messages.querySelectorAll('.approval-card')].some((item) =>
    item.dataset.runId === runID && item.dataset.requestId === payload.requestId)) return;
  const item = document.createElement('li');
  item.className = 'approval-card';
  item.dataset.runId = runID;
  item.dataset.requestId = payload.requestId || '';
  const heading = document.createElement('div');
  heading.className = 'approval-heading';
  const mark = document.createElement('span');
  mark.className = 'approval-mark';
  mark.append(makeIcon('<path d="M10 3.5 17 16H3z"/><path d="M10 7.5v4M10 14v.2"/>'));
  const copy = document.createElement('div');
  copy.className = 'approval-copy';
  const title = document.createElement('h3');
  title.textContent = approvalTitle(payload.method);
  const summary = document.createElement('p');
  summary.textContent = approvalSummary(payload.params);
  copy.append(title, summary);
  heading.append(mark, copy);

  const details = document.createElement('details');
  details.className = 'approval-details';
  const summaryLabel = document.createElement('summary');
  summaryLabel.textContent = '詳細を表示';
  const data = document.createElement('pre');
  data.textContent = JSON.stringify(payload.params || {}, null, 2);
  details.append(summaryLabel, data);

  const actions = document.createElement('div');
  actions.className = 'approval-actions';
  for (const [label, decision] of [['拒否', 'decline'], ['許可', 'accept']]) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = label;
    button.addEventListener('click', async () => {
      actions.querySelectorAll('button').forEach((action) => { action.disabled = true; });
      try {
        await request(`/v1/approvals/${encodeURIComponent(runID)}/${encodeURIComponent(payload.requestId)}`, {
          method: 'POST',
          body: JSON.stringify({response: {decision}}),
        });
        title.textContent = decision === 'accept' ? '許可しました' : '拒否しました';
        summary.textContent = 'Codex Runner に結果を送信しました。';
        actions.remove();
        setStatus('承認結果を送信しました');
      } catch (error) {
        actions.querySelectorAll('button').forEach((action) => { action.disabled = false; });
        setStatus(`承認できませんでした: ${error.message}`, 'error');
      }
    });
    actions.append(button);
  }
  item.append(heading, details, actions);
  elements.messages.append(item);
  elements.emptyState.hidden = true;
  scrollTranscriptToEnd();
}

function approvalTitle(method) {
  const labels = {
    'item/commandExecution/requestApproval': 'コマンドの実行を許可しますか？',
    'item/fileChange/requestApproval': 'ファイルの変更を許可しますか？',
    'execCommandApproval': 'コマンドの実行を許可しますか？',
    'applyPatchApproval': 'ファイルの変更を許可しますか？',
  };
  return labels[method] || 'この操作を許可しますか？';
}

function approvalSummary(params) {
  if (!params || typeof params !== 'object') return 'Codex Runner が操作の承認を求めています。';
  const command = params.command || params.cmd;
  if (Array.isArray(command)) return command.join(' ');
  if (typeof command === 'string') return command;
  const path = params.path || params.filePath;
  if (typeof path === 'string') return path;
  return '操作の内容を確認してから選択してください。';
}

function scrollTranscriptToEnd() {
  requestAnimationFrame(() => { elements.transcript.scrollTop = elements.transcript.scrollHeight; });
}

let resumedRunController = null;
let activeTurn = null;

function updateSendButton() {
  const stopping = activeTurn && activeTurn.conversationID === state.conversationID;
  elements.sendButton.type = stopping ? 'button' : 'submit';
  elements.sendButton.disabled = stopping ? activeTurn.interrupting : state.sending;
  elements.sendButton.setAttribute('aria-label', stopping ? '応答を中断' : 'メッセージを送信');
  elements.sendButton.title = stopping ? '応答を中断' : 'メッセージを送信';
  elements.sendButton.querySelector('svg').toggleAttribute('hidden', Boolean(stopping));
  elements.sendButton.querySelector('.stop-icon').hidden = !stopping;
}

async function interruptActiveTurn() {
  const turn = activeTurn;
  if (!turn || turn.conversationID !== state.conversationID || turn.interrupting) return;
  turn.interrupting = true;
  updateSendButton();
  try {
    await request(`/v1/runs/${encodeURIComponent(turn.runID)}/interrupt`, {method: 'POST'});
    if (activeTurn === turn) setStatus('中断しています…', 'working');
  } catch (error) {
    if (activeTurn === turn) {
      turn.interrupting = false;
      updateSendButton();
      setStatus(`中断できませんでした: ${error.message}`, 'error');
    }
  }
}

function clearActiveTurn() {
  activeTurn = null;
  updateSendButton();
}


async function resumeActiveRuns(conversationID, signal) {
  try {
    const result = await request(`/v1/conversations/${encodeURIComponent(conversationID)}/runs/active`, {signal});
    for (const run of result.runs || []) {
      if (signal.aborted || state.conversationID !== conversationID) return;
      setStatus(run.status === 'waiting_approval' ? '承認待ち' : '応答を再接続しています…', 'working');
      const assistantMessage = addMessage('assistant', '');
      try {
        const outcome = await streamEvents(run.id, assistantMessage, {signal});
        if (signal.aborted || state.conversationID !== conversationID) return;
        if (outcome === 'completed') setStatus('完了');
        else if (outcome === 'failed') setStatus('実行に失敗しました', 'error');
        else if (outcome === 'interrupted') setStatus('応答が中断されました');
        else setStatus('応答が終了しました');
      } catch (error) {
        if (!signal.aborted && state.conversationID === conversationID)
          setStatus(`応答の再接続に失敗しました: ${error.message}`, 'error');
      }
    }
  } catch (error) {
    if (!signal.aborted && state.conversationID === conversationID)
      setStatus(`実行状態を取得できませんでした: ${error.message}`, 'error');
  }
}

async function openConversation(id) {
  resumedRunController?.abort();
  resumedRunController = null;
  clearActiveTurn();
  closeSidebar();
  setStatus('チャットを開いています…');
  try {
    const result = await request(`/v1/conversations/${encodeURIComponent(id)}`);
    state.conversationID = result.id;
    localStorage.setItem('codex.currentConversation', result.id);
    setConversationURL(result.id);
    setRepository(result.repository || null);
    elements.conversationTitle.textContent = conversationTitle(result);
    elements.messages.replaceChildren();
    for (const message of result.messages || []) addMessage(message.role, message.content);
    elements.emptyState.hidden = Boolean((result.messages || []).length);
    try {
      const pending = await request(`/v1/conversations/${encodeURIComponent(result.id)}/approvals`);
      for (const approval of pending.approvals || []) {
        addApproval(approval.runId, {
          requestId: approval.requestId,
          method: approval.method,
          params: approval.params,
        });
      }
    } catch (_) {
      // Older or development repositories may not persist approval records.
    }
    setStatus('');
    await loadConversationList();
    if (state.conversationID === result.id) {
      resumedRunController = new AbortController();
      void resumeActiveRuns(result.id, resumedRunController.signal);
    }
  } catch (error) {
    if (error.status === 410) {
      // The user might have selected a different conversation while this request was in flight.
      if (state.conversationID === id || !state.conversationID) {
        startNewConversation();
        await loadConversationList();
        setStatus('期限切れのチャットをアーカイブしました');
      }
    } else {
      setStatus(`チャットを開けませんでした: ${error.message}`, 'error');
    }
  }
}

function conversationTitle(conversation) {
  if (conversation.title) return conversation.title;
  const firstUserMessage = (conversation.messages || []).find((message) => message.role === 'user');
  if (!firstUserMessage) return '新しいチャット';
  const title = firstUserMessage.content.replace(/\s+/g, ' ').trim();
  return [...title].length > 48 ? [...title].slice(0, 48).join('') + '…' : title;
}

function startNewConversation() {
  resumedRunController?.abort();
  resumedRunController = null;
  clearActiveTurn();
  clearDeleteSelection();
  state.conversationID = '';
  localStorage.removeItem('codex.currentConversation');
  setConversationURL('');
  elements.messages.replaceChildren();
  elements.conversationTitle.textContent = '新しいチャット';
  elements.emptyState.hidden = false;
  setStatus('');
  renderRepository();
  renderHistorySelection();
  closeSidebar();
}

function renderHistorySelection() {
  elements.historyList.querySelectorAll('.history-item').forEach((item) => item.removeAttribute('aria-current'));
}

function closeSidebar() {
  elements.shell.classList.remove('sidebar-open');
  elements.historyToggle.setAttribute('aria-expanded', 'false');
  elements.sidebarBackdrop.hidden = true;
}

function toggleSidebar() {
  const open = !elements.shell.classList.contains('sidebar-open');
  elements.shell.classList.toggle('sidebar-open', open);
  elements.historyToggle.setAttribute('aria-expanded', String(open));
  elements.sidebarBackdrop.hidden = !open;
}

async function createConversation() {
  const body = state.repository ? {repository: state.repository} : {};
  const result = await request('/v1/conversations', {method: 'POST', body: JSON.stringify(body)});
  state.conversationID = result.id;
  localStorage.setItem('codex.currentConversation', result.id);
  setConversationURL(result.id);
  elements.conversationTitle.textContent = '新しいチャット';
  renderRepository();
  return result;
}

function createRunProgress(assistantMessage) {
  const panel = document.createElement('details');
  panel.className = 'run-progress';
  const heading = document.createElement('summary');
  heading.textContent = '作業の途中経過';
  const entries = document.createElement('ol');
  panel.append(heading, entries);
  const items = new Map();
  const labels = {
    commandExecution: ['コマンドを実行中', 'コマンドの実行が終了'],
    fileChange: ['ファイルを変更中', 'ファイルの変更処理が終了'],
    webSearch: ['ウェブを検索中', 'ウェブ検索が終了'],
    mcpToolCall: ['ツールを実行中', 'ツールの実行が終了'],
  };

  function entry(id) {
    if (!items.has(id)) {
      const row = document.createElement('li');
      items.set(id, row);
      entries.append(row);
      if (!panel.parentNode) {
        panel.open = true;
        assistantMessage.paragraph.before(panel);
      }
    }
    return items.get(id);
  }

  return {
    update(eventType, payload) {
      if (eventType === 'item/reasoning/summaryTextDelta') {
        const {itemId, summaryIndex, delta} = payload;
        if (typeof itemId !== 'string' || !Number.isSafeInteger(summaryIndex) ||
            summaryIndex < 0 || typeof delta !== 'string' || !delta) return;
        const row = entry(itemId);
        let part = [...row.children].find((child) => Number(child.dataset.index) >= summaryIndex);
        if (!part || Number(part.dataset.index) !== summaryIndex) {
          const paragraph = document.createElement('p');
          paragraph.dataset.index = summaryIndex;
          row.insertBefore(paragraph, part || null);
          part = paragraph;
        }
        part.textContent += delta;
        return;
      }
      if (eventType !== 'item/started' && eventType !== 'item/completed') return;
      const item = payload.item;
      if (typeof item?.id !== 'string') return;
      const completed = eventType === 'item/completed';
      if (item.type === 'reasoning' && completed && Array.isArray(item.summary)) {
        const summaries = item.summary.filter((part) => typeof part === 'string' && part.trim());
        if (summaries.length) {
          entry(item.id).replaceChildren(...summaries.map((text) => {
            const paragraph = document.createElement('p');
            paragraph.textContent = text;
            return paragraph;
          }));
        }
      } else if (Object.hasOwn(labels, item.type)) {
        entry(item.id).textContent = labels[item.type][Number(completed)];
      }
    },
    finish() {
      panel.open = false;
    },
  };
}

async function streamEvents(runID, assistantMessage, options = {}) {
  const progress = createRunProgress(assistantMessage);
  const turn = {runID, conversationID: state.conversationID, interrupting: false};
  activeTurn = turn;
  updateSendButton();
  try {
    const outcome = await readRunEvents(runID, assistantMessage, progress, options);
    if (outcome !== 'unknown') progress.finish();
    return outcome;
  } finally {
    if (activeTurn === turn) clearActiveTurn();
  }
}

async function readRunEvents(runID, assistantMessage, progress, options) {
  const response = await fetch(`/v1/runs/${encodeURIComponent(runID)}/events`, {credentials: 'same-origin', ...options});
  if (!response.ok || !response.body) throw new Error(`イベントを受信できませんでした（${response.status}）`);
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  while (true) {
    const {value, done} = await reader.read();
    buffer += decoder.decode(value || new Uint8Array(), {stream: !done});
    const chunks = buffer.split('\n\n');
    buffer = chunks.pop() || '';
    for (const chunk of chunks) {
      const lines = chunk.split('\n');
      const eventType = lines.find((line) => line.startsWith('event: '))?.slice(7) || '';
      const dataLines = lines.filter((line) => line.startsWith('data: '));
      let payload;
      if (dataLines.length) {
        try {
          payload = JSON.parse(dataLines.map((line) => line.slice(6)).join('\n'));
        } catch (_) {
          // A terminal event can still be handled when its payload is invalid.
        }
      }
      if (payload && typeof payload === 'object') {
        if (eventType === 'approval/requested') {
          addApproval(runID, payload);
          setStatus('承認待ち', 'working');
          continue;
        }
        progress.update(eventType, payload);
        if (['agentmessage/delta', 'item/agentmessage/delta', 'item/agent_message/delta'].includes(eventType.toLowerCase())) {
          const text = payload.delta || payload.text || payload.content;
          if (typeof text === 'string' && text) {
            assistantMessage.source += text;
            assistantMessage.renderer.render(assistantMessage.source);
            scrollTranscriptToEnd();
          }
        }
      }
      if (eventType === 'turn/completed') {
        await reader.cancel();
        const status = payload?.turn?.status;
        if (status === 'completed' || status === 'failed' || status === 'interrupted') return status;
        return 'unknown';
      }
      if (eventType === 'turn/failed' || eventType === 'turn/error') {
        await reader.cancel();
        return 'failed';
      }
      if (eventType === 'turn/interrupted' || eventType === 'turn/aborted') {
        await reader.cancel();
        return 'interrupted';
      }
    }
    if (done) return 'unknown';
  }
}

let authLoginID = '';
let finishAuthLogin = null;

async function connectChatGPT() {
  if (elements.authDialog.open) return null;
  let challenge;
  try {
    challenge = await request('/v1/auth/login', {method: 'POST'});
  } catch (error) {
    setStatus(`ChatGPTに接続できませんでした: ${error.message}`, 'error');
    return null;
  }
  authLoginID = challenge.loginId;
  elements.authVerificationLink.href = challenge.verificationUrl;
  elements.authUserCode.textContent = challenge.userCode;
  elements.authWaiting.textContent = 'ChatGPTでコードを入力してください。サインイン後、自動で接続されます。';
  elements.copyAuthCode.textContent = 'コードをコピー';
  elements.accountIndicator.dataset.connecting = 'true';
  elements.accountLabel.textContent = 'ChatGPT 接続を確認中…';
  elements.authDialog.showModal();
  setStatus('ChatGPT へのサインインを待っています…', 'working');

  return new Promise((resolve) => {
    let finished = false;
    let checking = false;
    const deadline = Date.now() + 10 * 60 * 1000;
    const finish = (status) => {
      if (finished) return;
      finished = true;
      clearInterval(timer);
      elements.cancelAuth.removeEventListener('click', cancel);
      elements.closeAuthDialog.removeEventListener('click', cancel);
      finishAuthLogin = null;
      authLoginID = '';
      if (elements.authDialog.open) elements.authDialog.close();
      if (status?.authenticated) {
        setCodexAccount(status.account);
        setStatus('');
        resolve(status);
      } else {
        clearCodexAccount();
        setStatus('ChatGPTへの接続を中断しました');
        resolve(null);
      }
    };
    const cancel = async () => {
      elements.cancelAuth.disabled = true;
      try { await request(`/v1/auth/login/${encodeURIComponent(authLoginID)}`, {method: 'DELETE'}); } catch (_) { /* The dialog can still close if the agent is unavailable. */ }
      elements.cancelAuth.disabled = false;
      finish(null);
    };
    finishAuthLogin = finish;
    elements.cancelAuth.addEventListener('click', cancel);
    elements.closeAuthDialog.addEventListener('click', cancel);
    const timer = setInterval(async () => {
      if (finished || checking) return;
      if (Date.now() >= deadline) {
        elements.authWaiting.textContent = '時間切れです。もう一度接続してください。';
        await cancel();
        return;
      }
      checking = true;
      try {
        const status = await request('/v1/auth/status');
        if (status.authenticated) finish(status);
      } catch (_) {
        elements.authWaiting.textContent = '接続を確認しています…';
      } finally { checking = false; }
    }, 2000);
  });
}

async function sendMessage(event) {
  event.preventDefault();
  const content = elements.message.value.trim();
  if (!content || state.sending || activeTurn?.conversationID === state.conversationID) return;

  state.sending = true;
  updateSendButton();
  elements.message.value = '';
  resizeComposer();
  let userMessage = null;
  let conversationCreated = false;

  try {
    let authStatus = await request('/v1/auth/status');
    if (!authStatus.authenticated) authStatus = await connectChatGPT();
    if (!authStatus) {
      elements.message.value = content;
      resizeComposer();
      elements.message.focus({preventScroll: true});
      return;
    }
    setCodexAccount(authStatus.account);
    if (!state.conversationID) {
      await createConversation();
      conversationCreated = true;
    }
    userMessage = addMessage('user', content);
    setStatus('Codex を起動しています…', 'working');
    const result = await request(`/v1/conversations/${encodeURIComponent(state.conversationID)}/messages`, {
      method: 'POST',
      body: JSON.stringify({content, clientMessageId: crypto.randomUUID()}),
    });
    if (conversationCreated) {
      const title = [...content];
      elements.conversationTitle.textContent = title.length > 48 ? `${title.slice(0, 48).join('')}…` : content;
    }
    if (result.queued) {
      await waitForQueuedMessage(state.conversationID, result.message.id);
      await loadConversationList();
      return;
    }
    if (!result.run) {
      setStatus('');
      await loadConversationList();
      return;
    }
    renderRepository();
    setStatus('応答中…', 'working');
    const assistantMessage = addMessage('assistant', '');
    const outcome = await streamEvents(result.run.id, assistantMessage);
    if (outcome === 'completed') setStatus('完了');
    else if (outcome === 'failed') setStatus('実行に失敗しました', 'error');
    else if (outcome === 'interrupted') setStatus('応答が中断されました');
    else setStatus('応答が終了しました');
    await loadConversationList();
  } catch (error) {
    if (userMessage && !userMessage.item.isConnected) return;
    if (!userMessage) {
      elements.message.value = content;
      resizeComposer();
    }
    if (conversationCreated && userMessage) {
      const errorMessage = document.createElement('p');
      errorMessage.className = 'message-error';
      errorMessage.textContent = `送信できませんでした: ${error.message}`;
      elements.messages.append(errorMessage);
    }
    setStatus(`送信できませんでした: ${error.message}`, 'error');
    await loadConversationList();
  } finally {
    state.sending = false;
    updateSendButton();
  }
}

async function waitForQueuedMessage(conversationID, messageID) {
  setStatus('前の返信が終わるまで順番待ちしています…', 'working');
  while (state.conversationID === conversationID) {
    await new Promise((resolve) => setTimeout(resolve, 1500));
    let result;
    try {
      result = await request(`/v1/conversations/${encodeURIComponent(conversationID)}/messages`);
    } catch (_) {
      setStatus('順番待ちの状態を確認しています…', 'working');
      continue;
    }
    const message = (result.messages || []).find((item) => item.id === messageID);
    if (!message || message.status === 'queued') continue;
    if (message.status === 'running') {
      setStatus('応答中…', 'working');
      continue;
    }
    await openConversation(conversationID);
    if (message.status === 'completed') setStatus('完了');
    else setStatus('実行に失敗しました', 'error');
    return;
  }
}

function resizeComposer() {
  elements.message.style.height = 'auto';
  elements.message.style.height = `${Math.min(elements.message.scrollHeight, 180)}px`;
}

function saveRepository(event) {
  event.preventDefault();
  const repository = {
    url: elements.repositoryURL.value.trim(),
    revision: elements.repositoryRevision.value.trim(),
  };
  if (!repository.url || !repository.revision) return;
  const existingIndex = state.repositories.findIndex((item) => item.url === repository.url && item.revision === repository.revision);
  if (existingIndex >= 0) state.repositories.splice(existingIndex, 1);
  state.repositories.unshift(repository);
  state.repositories = state.repositories.slice(0, 12);
  localStorage.setItem('codex.repositories', JSON.stringify(state.repositories));
  setRepository(repository);
  elements.repositoryDialog.close();
  elements.message.focus({preventScroll: true});
}

elements.newConversation.addEventListener('click', startNewConversation);
elements.connectChatGPT.addEventListener('click', () => connectChatGPT());
elements.copyAuthCode.addEventListener('click', async () => {
  try {
    await navigator.clipboard.writeText(elements.authUserCode.textContent);
    elements.copyAuthCode.textContent = 'コピーしました';
  } catch (_) {
    elements.authWaiting.textContent = 'コードを選択してコピーしてください。';
  }
});
elements.authDialog.addEventListener('cancel', (event) => {
  event.preventDefault();
  if (finishAuthLogin) elements.cancelAuth.click();
});
elements.historyToggle.addEventListener('click', toggleSidebar);
elements.sidebarBackdrop.addEventListener('click', closeSidebar);
elements.contextToggle.addEventListener('click', () => {
  if (elements.contextMenu.hidden) openContextMenu();
  else closeContextMenu();
});
elements.removeRepository.addEventListener('click', () => setRepository(null));
elements.composer.addEventListener('submit', sendMessage);
elements.sendButton.addEventListener('click', () => {
  if (activeTurn?.conversationID === state.conversationID) void interruptActiveTurn();
});
elements.deleteSelected.addEventListener('click', () => {
  if (state.deleteTarget) archiveConversation(state.deleteTarget.id, state.deleteTarget.title);
});
elements.message.addEventListener('input', resizeComposer);
elements.message.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing && activeTurn?.conversationID !== state.conversationID) {
    event.preventDefault();
    elements.composer.requestSubmit();
  }
});
elements.repositoryForm.addEventListener('submit', saveRepository);
elements.closeRepositoryDialog.addEventListener('click', () => elements.repositoryDialog.close());
elements.cancelRepository.addEventListener('click', () => elements.repositoryDialog.close());
elements.repositoryDialog.addEventListener('click', (event) => {
  if (event.target === elements.repositoryDialog) elements.repositoryDialog.close();
});
elements.repositoryDialog.addEventListener('close', () => elements.contextToggle.focus({preventScroll: true}));
document.addEventListener('click', (event) => {
  if (!event.target.closest('.history-row')) {
    elements.historyList.querySelectorAll('.history-options-menu').forEach((item) => { item.hidden = true; });
    elements.historyList.querySelectorAll('.history-options').forEach((item) => item.setAttribute('aria-expanded', 'false'));
  }
  if (!elements.contextMenu.hidden && !elements.contextMenu.contains(event.target) && !elements.contextToggle.contains(event.target)) closeContextMenu();
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && !elements.contextMenu.hidden) closeContextMenu({restoreFocus: true});
});

renderRepository();
async function refreshCodexAccount() {
  try {
    const status = await request('/v1/auth/status', {cache: 'no-store'});
    if (status.authenticated) setCodexAccount(status.account);
    else clearCodexAccount();
  } catch (_) {
    // A temporary API/network failure does not mean the OAuth session expired.
    elements.accountLabel.textContent = 'ChatGPT 接続状態を確認できません';
    elements.accountIndicator.hidden = false;
  }
}

refreshCodexAccount();
window.addEventListener('pageshow', refreshCodexAccount);
// Keep the history list in sync when the server archives a conversation.
async function refreshArchivedConversations() {
  if (document.hidden) return;
  try {
    await loadConversationList();
    if (!state.conversationID || state.sending) return;
    const conversationID = state.conversationID;
    try {
      await request(`/v1/conversations/${encodeURIComponent(conversationID)}`);
    } catch (error) {
      // Ignore delayed responses for a conversation the user has left.
      if (error.status === 410 && state.conversationID === conversationID) {
        startNewConversation();
        setStatus('期限切れのチャットをアーカイブしました');
      }
    }
  } catch (_) {
    // An intermittent list error should not discard the current conversation.
  }
}
setInterval(refreshArchivedConversations, 60 * 1000);
window.addEventListener('pageshow', refreshArchivedConversations);
window.addEventListener('online', refreshCodexAccount);
document.addEventListener('visibilitychange', () => {
  if (!document.hidden) {
    refreshCodexAccount();
    refreshArchivedConversations();
  }
});
loadConversationList().then(async () => {
  if (state.conversationID) await openConversation(state.conversationID);
});
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.register('sw.js', {updateViaCache: 'none'})
    .then((registration) => registration.update())
    .catch(() => { /* The web app remains usable without a service worker. */ });
}
