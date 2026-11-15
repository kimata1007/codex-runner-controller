const state = {
  conversationID: new URL(location.href).searchParams.get('conversation') || localStorage.getItem('codex.currentConversation') || '',
  repository: readStoredRepository(),
  repositories: readStoredRepositories(),
  sending: false,
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
  contextToggle: document.querySelector('#context-toggle'),
  contextMenu: document.querySelector('#context-menu'),
  accountIndicator: document.querySelector('#codex-account'),
  accountLabel: document.querySelector('#codex-account-label'),
  manageUsage: document.querySelector('#manage-usage'),
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
    throw new Error(detail || `リクエストに失敗しました（${response.status}）`);
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
  const usesChatGPTPlan = account?.type === 'chatgpt';
  elements.accountLabel.textContent = usesChatGPTPlan
    ? `ChatGPT${account.planType ? ` · ${account.planType}` : ''} でCodexを実行しています`
    : 'Codex に接続済み';
  elements.manageUsage.hidden = !usesChatGPTPlan;
}

function clearCodexAccount() {
  elements.accountIndicator.hidden = true;
  elements.accountLabel.textContent = '';
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

function renderHistory(conversations) {
  elements.historyList.replaceChildren();
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
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'history-item';
      button.textContent = conversation.title || '新しいチャット';
      button.title = conversation.title || '新しいチャット';
      if (conversation.id === state.conversationID) button.setAttribute('aria-current', 'page');
      button.addEventListener('click', () => openConversation(conversation.id));
      list.append(button);
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
  const paragraph = document.createElement('p');
  paragraph.textContent = content;
  item.append(paragraph);
  elements.messages.append(item);
  elements.emptyState.hidden = true;
  scrollTranscriptToEnd();
  return {item, paragraph};
}

function addApproval(runID, payload) {
  const item = document.createElement('li');
  item.className = 'approval-card';
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

async function openConversation(id) {
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
    setStatus('');
    await loadConversationList();
  } catch (error) {
    setStatus(`チャットを開けませんでした: ${error.message}`, 'error');
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

async function streamEvents(runID, assistantMessage) {
  const response = await fetch(`/v1/runs/${encodeURIComponent(runID)}/events`, {credentials: 'same-origin'});
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
      const eventType = chunk.split('\n').find((line) => line.startsWith('event: '))?.slice(7) || '';
      const dataLines = chunk.split('\n').filter((line) => line.startsWith('data: '));
      if (dataLines.length) {
        try {
          const payload = JSON.parse(dataLines.map((line) => line.slice(6)).join('\n'));
          if (eventType === 'approval/requested') {
            addApproval(runID, payload);
            setStatus('承認待ち', 'working');
            continue;
          }
          const text = payload.text || payload.delta || payload.content || '';
          if (text) {
            assistantMessage.paragraph.textContent += text;
            scrollTranscriptToEnd();
          }
        } catch (_) {
          // Ignore event payloads that are not user-visible JSON.
        }
      }
      if (eventType === 'turn/completed' || eventType === 'turn/failed') {
        await reader.cancel();
        return eventType === 'turn/completed';
      }
    }
    if (done) return false;
  }
}

function addAuthGate(challenge) {
  const server = window.location.origin;
  const command = `go run ./cmd/codex-runner auth connect --server ${server} --attempt ${challenge.attemptId} --host-id ${challenge.hostId}`;
  const item = document.createElement('li');
  item.className = 'auth-gate';
  const icon = document.createElement('span');
  icon.className = 'auth-gate-mark';
  icon.append(makeIcon('<path d="M6.5 9V6.8a3.5 3.5 0 0 1 7 0V9"/><rect x="4.5" y="9" width="11" height="8" rx="1.8"/><path d="M10 12v2"/>'));
  const title = document.createElement('h2');
  title.textContent = 'ChatGPTに接続してください';
  const description = document.createElement('p');
  description.textContent = '最初の一度だけ、ローカルの認証ヘルパーを実行してChatGPTにサインインします。認証情報はブラウザーを経由せず、Runner内に保管されます。';
  const commandLabel = document.createElement('span');
  commandLabel.className = 'auth-code-label';
  commandLabel.textContent = 'リポジトリのルートで実行';
  const codeRow = document.createElement('div');
  codeRow.className = 'auth-code-row';
  const commandCode = document.createElement('code');
  commandCode.className = 'auth-command';
  commandCode.textContent = command;
  const copy = document.createElement('button');
  copy.type = 'button';
  copy.className = 'auth-copy';
  copy.textContent = 'コピー';
  copy.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(command);
      copy.textContent = 'コピーしました';
    } catch (_) {
      copy.textContent = 'コピーできません';
    }
  });
  codeRow.append(commandCode, copy);
  const helper = document.createElement('p');
  helper.className = 'auth-helper';
  helper.textContent = 'コマンドがブラウザーを開いたらChatGPTで許可してください。完了すると、この画面から会話を続けます。';
  const waiting = document.createElement('p');
  waiting.className = 'auth-waiting';
  waiting.setAttribute('role', 'status');
  waiting.textContent = '認証を確認しています…';
  const cancel = document.createElement('button');
  cancel.type = 'button';
  cancel.className = 'auth-cancel';
  cancel.textContent = 'あとで';
  item.append(icon, title, description, commandLabel, codeRow, helper, waiting, cancel);
  elements.messages.append(item);
  elements.emptyState.hidden = true;
  scrollTranscriptToEnd();
  copy.focus({preventScroll: true});
  return {item, waiting, cancel};
}

function waitForCodexAuth(challenge) {
  const gate = addAuthGate(challenge);
  setStatus('ChatGPT へのサインインを待っています…', 'working');
  return new Promise((resolve) => {
    let finished = false;
    let checking = false;
    const deadline = Date.now() + 10 * 60 * 1000;
    const timer = setInterval(check, 2000);
    const finish = (authenticated, message = '', status = null) => {
      if (finished) return;
      finished = true;
      clearInterval(timer);
      gate.cancel.removeEventListener('click', cancel);
      gate.item.remove();
      if (!elements.messages.children.length) elements.emptyState.hidden = false;
      setStatus(message || (authenticated ? '認証済みです。会話を開始します…' : '認証を中断しました'));
      resolve(authenticated ? status : null);
    };
    const cancel = async () => {
      gate.cancel.disabled = true;
      try {
        await request(`/v1/auth/login/${encodeURIComponent(challenge.attemptId)}`, {method: 'DELETE'});
      } catch (_) { /* The local draft can still be restored if cancellation cannot reach the server. */ }
      finish(false);
    };
    gate.cancel.addEventListener('click', cancel);
    async function check() {
      if (finished || checking) return;
      if (Date.now() >= deadline) {
        finish(false, '認証を確認できませんでした。もう一度送信してやり直してください。');
        return;
      }
      checking = true;
      try {
        const status = await request('/v1/auth/status');
        if (status.authenticated) finish(true, '', status);
      } catch (_) {
        gate.waiting.textContent = '接続を確認しています…';
      } finally {
        checking = false;
      }
    }
  });
}

async function sendMessage(event) {
  event.preventDefault();
  const content = elements.message.value.trim();
  if (!content || state.sending) return;

  state.sending = true;
  elements.sendButton.disabled = true;
  elements.message.value = '';
  resizeComposer();
  let userMessage = null;
  let conversationCreated = false;

  try {
    let authStatus = await request('/v1/auth/status');
    if (!authStatus.authenticated) {
      const attempt = await request('/v1/auth/login', {method: 'POST'});
      authStatus = await waitForCodexAuth(attempt);
    }
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
    if (!result.run) {
      setStatus('');
      await loadConversationList();
      return;
    }
    renderRepository();
    setStatus('応答中…', 'working');
    const assistantMessage = addMessage('assistant', '');
    const completed = await streamEvents(result.run.id, assistantMessage);
    setStatus(completed ? '完了' : '応答が終了しました');
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
    elements.sendButton.disabled = false;
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
elements.historyToggle.addEventListener('click', toggleSidebar);
elements.sidebarBackdrop.addEventListener('click', closeSidebar);
elements.contextToggle.addEventListener('click', () => {
  if (elements.contextMenu.hidden) openContextMenu();
  else closeContextMenu();
});
elements.removeRepository.addEventListener('click', () => setRepository(null));
elements.composer.addEventListener('submit', sendMessage);
elements.message.addEventListener('input', resizeComposer);
elements.message.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
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
  if (!elements.contextMenu.hidden && !elements.contextMenu.contains(event.target) && !elements.contextToggle.contains(event.target)) closeContextMenu();
});
document.addEventListener('keydown', (event) => {
  if (event.key === 'Escape' && !elements.contextMenu.hidden) closeContextMenu({restoreFocus: true});
});

renderRepository();
request('/v1/auth/status').then((status) => {
  if (status.authenticated) setCodexAccount(status.account);
}).catch(() => {});
loadConversationList().then(async () => {
  if (state.conversationID) await openConversation(state.conversationID);
});
if ('serviceWorker' in navigator) navigator.serviceWorker.register('sw.js');
